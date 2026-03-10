// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/format"
	"go.astrophena.name/tools/cmd/tgfeed/internal/retry"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
)

// Feed fetching and message sending.

const (
	errorThreshold        = 12 // failing continuously for N fetches will disable feed and complain loudly
	fetchConcurrencyLimit = 10 // N fetches that can run at the same time
	sendConcurrencyLimit  = 2  // N sends that can run at the same time
	retryLimit            = 3  // N attempts to retry feed fetching

	// lookbackPeriod is the period for which new items are processed even if
	// they have an old publication date, but only if always_send_new_items
	// is enabled for the feed.
	lookbackPeriod = 14 * 24 * time.Hour

	// seenItemsCleanupPeriod is the period after which an item is removed from
	// the seen items list.
	seenItemsCleanupPeriod = 28 * 24 * time.Hour
)

//go:embed defaults/message.tmpl
var messageTemplate string

// update is the unit delivered to the sending pipeline.
//
// It can contain either a single item or a digest batch depending on feed
// configuration.
type update struct {
	feed  *feed
	items []*gofeed.Item
}

// feedStatusResult describes what a response status handler decided to do next.
//
// A status can be fully handled (for example, 304 Not Modified), in which case
// fetch should stop and return. Some handled statuses also request a retry with
// backoff information.
type feedStatusResult struct {
	handled bool
	retry   bool
	retryIn time.Duration
	class   int
}

// feedItemDecision captures both item processing and seen-state updates.
//
// selection controls whether the item is processed further. markSeen stores the
// key that should be recorded in seen-items state immediately.
type feedItemDecision struct {
	selection feedItemSelection
	markSeen  string
	reason    feedItemSkipReason
}

// enqueueAction determines how an accepted item is emitted to the updates
// channel.
type enqueueAction uint8

const (
	enqueueActionSkip enqueueAction = iota
	enqueueActionSingle
	enqueueActionDigest
)

// feedItemSelection describes whether an item should be skipped, only marked
// as seen, or processed and potentially sent.
type feedItemSelection uint8

const (
	feedItemSelectionSkip feedItemSelection = iota
	feedItemSelectionMarkSeenOnly
	feedItemSelectionProcess
)

type feedItemSkipReason uint8

const (
	feedItemSkipReasonNone feedItemSkipReason = iota
	feedItemSkipReasonOld
	feedItemSkipReasonSeen
)

// feedItemContext groups immutable fetch-time context used while deciding how
// to handle a specific parsed item.
type feedItemContext struct {
	feed        *feed
	state       *state.Feed
	exists      bool
	justEnabled bool
	starlarkVal starlark.Value
}

// fetch fetches one feed and either emits updates, asks caller to retry, or
// records a failure.
//
// High-level flow:
//  1. Load feed state and skip disabled feeds.
//  2. Build and execute an HTTP request with conditional cache headers.
//  3. Handle status-specific outcomes (not-modified, rate-limit, failures).
//  4. Parse feed items, update cache metadata, and enqueue outgoing updates.
//  5. Mark successful fetch statistics.
func (f *fetcher) fetch(ctx context.Context, fd *feed, updates chan *update) (retry bool, retryIn time.Duration) {
	startTime := time.Now()

	var (
		exists       bool
		disabled     bool
		etag         string
		lastModified string
	)
	if err := f.withFeedState(ctx, fd.url, func(state *state.Feed, hasState bool) bool {
		exists = hasState
		disabled = state.IsDisabled()
		etag, lastModified = state.CacheHeaders()
		return false
	}); err != nil {
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}
	if !exists {
		// If we don't remember this feed, it's probably new. Set its last update
		// date to current so we don't get a lot of unread articles and trigger
		// Telegram Bot API rate limit.
		f.slog.Debug("initializing state", "feed", fd.url)
	}

	if disabled {
		f.slog.Debug("skipping, feed is disabled", "feed", fd.url)
		return false, 0
	}

	req, err := f.newFeedRequest(ctx, fd, etag, lastModified)
	if err != nil {
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}

	res, err := f.makeFeedRequest(req)
	if err != nil {
		if isTransientError(err) {
			f.slog.Warn("transient network error, retrying", "feed", fd.url, "error", err)
			return true, 5 * time.Second
		}
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}
	defer res.Body.Close()

	f.logFeedResponse(fd, res)

	status, err := f.handleFeedStatus(req, res, fd)
	if err != nil {
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}
	if status.handled {
		return status.retry, status.retryIn
	}

	parsedFeed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.stats.WriteAccess(func(s *stats) {
			s.ParseErrorCount += 1
		})
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}

	if err := f.withFeedState(ctx, fd.url, func(state *state.Feed, hasState bool) bool {
		f.updateFeedStateFromHeaders(state, res)
		f.enqueueFeedItems(fd, state, hasState, parsedFeed.Items, updates)
		state.MarkFetchSuccess(time.Now())
		return true
	}); err != nil {
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}
	f.markFetchSuccess(fd.url, len(parsedFeed.Items), startTime)

	return false, 0
}

func (f *fetcher) newFeedRequest(ctx context.Context, fd *feed, etag string, lastModified string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	return req, nil
}

func (f *fetcher) logFeedResponse(fd *feed, res *http.Response) {
	f.slog.Debug(
		"fetched feed",
		"feed", fd.url,
		"proto", res.Proto,
		"len", res.ContentLength,
		"status", res.StatusCode,
		"headers", res.Header,
	)
}

func (f *fetcher) handleFeedStatus(req *http.Request, res *http.Response, fd *feed) (feedStatusResult, error) {
	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.slog.Debug("unmodified feed", "feed", fd.url)
		f.stats.WriteAccess(func(s *stats) {
			s.NotModifiedFeeds += 1
			s.HTTP3xxCount += 1
			s.feedStats(fd.url).LastStatusClass = 3
		})
		_ = f.withFeedState(req.Context(), fd.url, func(state *state.Feed, _ bool) bool {
			state.MarkNotModified(time.Now())
			return true
		})
		return feedStatusResult{
			handled: true,
			class:   3,
		}, nil
	}
	if res.StatusCode == http.StatusOK {
		f.stats.WriteAccess(func(s *stats) {
			s.HTTP2xxCount += 1
			s.feedStats(fd.url).LastStatusClass = 2
		})
		return feedStatusResult{class: 2}, nil
	}

	f.stats.WriteAccess(func(s *stats) {
		s.addHTTPStatusClass(fd.url, res.StatusCode)
	})

	body, hasBody := readFeedErrorBody(res.Body)

	// Handle custom rate limiting.
	if hasBody {
		if t, found := retry.Retryable(req.URL.Host, body); found {
			f.slog.Warn("rate-limited", "host", req.URL.Host, "feed", fd.url, "retry_in", t)
			return feedStatusResult{
				handled: true,
				retry:   true,
				retryIn: t,
				class:   4,
			}, nil
		}
	}

	// Handle standard HTTP Retry-After for Too Many Requests and Service Unavailable.
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		if t, found := retry.RetryAfter(res.Header.Get("Retry-After")); found {
			// Limit backoff to a reasonable maximum (context deadline or 5 minutes),
			// as Retry-After could be hours or days which would block the goroutine.
			const maxRetryAfter = 5 * time.Minute
			if t <= maxRetryAfter {
				f.slog.Warn("rate-limited (Retry-After)", "feed", fd.url, "retry_in", t)
				return feedStatusResult{
					handled: true,
					retry:   true,
					retryIn: t,
					class:   res.StatusCode / 100,
				}, nil
			}
			f.slog.Warn("rate-limited (Retry-After), but wait time is too long", "feed", fd.url, "retry_in", t, "max_retry_in", maxRetryAfter)
		}
	}

	// Retry on generic 5xx server errors without Retry-After header.
	if res.StatusCode >= 500 && res.StatusCode < 600 {
		return feedStatusResult{
			handled: true,
			retry:   true,
			retryIn: 5 * time.Second,
			class:   res.StatusCode / 100,
		}, nil
	}

	return feedStatusResult{
		handled: true,
		class:   res.StatusCode / 100,
	}, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body)
}

func isTransientError(err error) bool {
	if netErr, ok := errors.AsType[net.Error](err); ok {
		return netErr.Timeout() || strings.Contains(err.Error(), "connection reset by peer")
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func readFeedErrorBody(r io.Reader) (body []byte, hasBody bool) {
	const readLimit = 16384 // 16 KB is enough for error messages (probably)

	body, err := io.ReadAll(io.LimitReader(r, readLimit))
	if err != nil {
		return []byte("unable to read body"), false
	}
	return body, true
}

func (f *fetcher) updateFeedStateFromHeaders(state *state.Feed, res *http.Response) {
	state.UpdateCacheHeaders(res.Header.Get("ETag"), res.Header.Get("Last-Modified"))
}

func (f *fetcher) enqueueFeedItems(fd *feed, state *state.Feed, exists bool, items []*gofeed.Item, updates chan *update) {
	var validItems []*gofeed.Item

	itemCtx := feedItemContext{
		feed:        fd,
		state:       state,
		exists:      exists,
		justEnabled: f.prepareSeenItemsState(fd, state),
	}

	f.stats.WriteAccess(func(s *stats) {
		s.ItemsSeenTotal += len(items)
	})

	for _, feedItem := range items {
		decision := decideFeedItem(itemCtx, feedItem)
		// Seen state is updated even when we later skip the item so future runs do
		// not repeatedly reconsider the same entry.
		if decision.markSeen != "" {
			state.MarkSeen(decision.markSeen, time.Now())
		}
		if decision.selection != feedItemSelectionProcess {
			f.stats.WriteAccess(func(s *stats) {
				s.recordItemDecision(decision)
			})
			continue
		}

		itemCtx.starlarkVal = f.itemToStarlark(feedItem)

		switch f.decideEnqueueAction(itemCtx, feedItem) {
		case enqueueActionSkip:
			continue
		case enqueueActionDigest:
			validItems = append(validItems, feedItem)
			f.stats.WriteAccess(func(s *stats) {
				s.ItemsKeptTotal += 1
				s.ItemsEnqueuedTotal += 1
				s.feedStats(fd.url).ItemsEnqueued += 1
			})
		case enqueueActionSingle:
			updates <- &update{
				feed:  fd,
				items: []*gofeed.Item{feedItem},
			}
			f.stats.WriteAccess(func(s *stats) {
				s.ItemsKeptTotal += 1
				s.ItemsEnqueuedTotal += 1
				s.feedStats(fd.url).ItemsEnqueued += 1
			})
		}
	}
	publishDigestUpdate(fd, validItems, updates)
}

func (f *fetcher) prepareSeenItemsState(fd *feed, state *state.Feed) (justEnabled bool) {
	if !fd.alwaysSendNewItems {
		return false
	}
	justEnabled, pruned := state.PrepareSeenItems(time.Now(), seenItemsCleanupPeriod)
	f.stats.WriteAccess(func(s *stats) {
		s.SeenItemsPrunedTotal += pruned
	})
	return justEnabled
}

func decideFeedItem(itemCtx feedItemContext, feedItem *gofeed.Item) feedItemDecision {
	if itemCtx.feed.alwaysSendNewItems {
		now := time.Now()
		if feedItem.PublishedParsed != nil && now.Sub(*feedItem.PublishedParsed) > lookbackPeriod {
			return feedItemDecision{selection: feedItemSelectionSkip, reason: feedItemSkipReasonOld}
		}
		guid := cmp.Or(feedItem.GUID, feedItem.Link)
		if itemCtx.state.IsSeen(guid) {
			return feedItemDecision{selection: feedItemSelectionSkip, reason: feedItemSkipReasonSeen}
		}
		decision := feedItemDecision{selection: feedItemSelectionMarkSeenOnly, markSeen: guid}
		if !itemCtx.exists || itemCtx.justEnabled {
			return decision
		}
		decision.selection = feedItemSelectionProcess
		return decision
	}

	if feedItem.PublishedParsed != nil && feedItem.PublishedParsed.Before(itemCtx.state.LastUpdated) {
		return feedItemDecision{selection: feedItemSelectionSkip, reason: feedItemSkipReasonOld}
	}
	return feedItemDecision{selection: feedItemSelectionProcess}
}

func (f *fetcher) decideEnqueueAction(itemCtx feedItemContext, feedItem *gofeed.Item) enqueueAction {
	if !f.feedItemPassesRules(itemCtx.feed, feedItem, itemCtx.starlarkVal) {
		return enqueueActionSkip
	}
	if itemCtx.feed.digest {
		return enqueueActionDigest
	}
	return enqueueActionSingle
}

func (f *fetcher) feedItemPassesRules(fd *feed, feedItem *gofeed.Item, starlarkVal starlark.Value) bool {
	if fd.blockRule != nil {
		if blocked := f.applyRule(fd.blockRule, feedItem, starlarkVal); blocked {
			f.slog.Debug("blocked by block rule", "item", feedItem.Link)
			return false
		}
	}

	if fd.keepRule != nil {
		if keep := f.applyRule(fd.keepRule, feedItem, starlarkVal); !keep {
			f.slog.Debug("skipped by keep rule", "item", feedItem.Link)
			return false
		}
	}

	return true
}

func publishDigestUpdate(fd *feed, validItems []*gofeed.Item, updates chan *update) {
	if !fd.digest || len(validItems) == 0 {
		return
	}

	updates <- &update{
		feed:  fd,
		items: validItems,
	}
}

func (f *fetcher) markFetchSuccess(url string, parsedItems int, startTime time.Time) {
	f.stats.WriteAccess(func(s *stats) {
		elapsed := time.Since(startTime)
		s.TotalItemsParsed += parsedItems
		s.SuccessFeeds += 1
		s.TotalFetchTime += elapsed
		s.fetchLatencySamples = append(s.fetchLatencySamples, elapsed)
		s.feedStats(url).FetchDuration += elapsed
	})
}

func (f *fetcher) makeFeedRequest(req *http.Request) (*http.Response, error) {
	if isSpecialFeed(req.URL.String()) {
		return f.handleSpecialFeed(req)
	}
	return f.httpc.Do(req)
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item, starlarkVal starlark.Value) bool {
	val, err := starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.slog.Info(msg) },
		},
		rule,
		starlark.Tuple{starlarkVal},
		[]starlark.Tuple{},
	)
	if err != nil {
		f.slog.Warn("applying rule for item", "item", item.Link, "error", err)
		return false
	}

	ret, ok := val.(starlark.Bool)
	if !ok {
		f.slog.Warn("rule returned non-boolean value", "item", item.Link)
		return false
	}
	return bool(ret)
}

var bmStripper = bluemonday.StrictPolicy()

func (f *fetcher) itemToStarlark(item *gofeed.Item) starlark.Value {
	// Let's create a copy so we don't mutate the original parsed item structure.
	cleanedItem := *item
	cleanedItem.Content = bmStripper.Sanitize(cleanedItem.Content)
	cleanedItem.Description = bmStripper.Sanitize(cleanedItem.Description)
	return format.ItemToStarlark(&cleanedItem)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, url string, err error) {
	f.stats.WriteAccess(func(s *stats) {
		s.FailedFeeds += 1
		s.classifyFailure(url, err)
		s.feedStats(url).Failures += 1
	})

	var (
		disabled   bool
		errorCount int
	)
	if updateErr := f.withFeedState(ctx, url, func(state *state.Feed, _ bool) bool {
		disabled = state.MarkFetchFailure(err, errorThreshold)
		errorCount = state.ErrorCount
		return true
	}); updateErr != nil {
		f.slog.Warn("failed to persist feed failure state", "feed", url, "error", updateErr)
	}

	f.slog.Debug("fetch failed", "feed", url, "error", err)

	// Complain loudly and disable feed, if we failed previously enough.
	if disabled {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed reenable %q'", url, errorCount, err, url)

		if err := f.errNotify(ctx, err); err != nil {
			f.slog.Warn("failed to send error notification", "error", err)
		} else {
			// Successfully notified, clear the pending flag.
			if updateErr := f.withFeedState(ctx, url, func(state *state.Feed, _ bool) bool {
				state.DisabledNotifyPending = false
				return true
			}); updateErr != nil {
				f.slog.Warn("failed to persist feed failure state (DisabledNotifyPending)", "feed", url, "error", updateErr)
			}
		}
	}
}

func (s *stats) addHTTPStatusClass(url string, statusCode int) {
	class := statusCode / 100
	s.feedStats(url).LastStatusClass = class
	switch class {
	case 2:
		s.HTTP2xxCount += 1
	case 3:
		s.HTTP3xxCount += 1
	case 4:
		s.HTTP4xxCount += 1
	case 5:
		s.HTTP5xxCount += 1
	}
}

func (s *stats) classifyFailure(url string, err error) {
	if netErr, ok := errors.AsType[net.Error](err); ok {
		s.NetworkErrorCount += 1
		if netErr.Timeout() {
			s.TimeoutCount += 1
		}
		s.feedStats(url).LastStatusClass = 0
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.TimeoutCount += 1
		s.NetworkErrorCount += 1
		s.feedStats(url).LastStatusClass = 0
		return
	}
	s.feedStats(url).LastStatusClass = 0
}

func (s *stats) recordItemDecision(decision feedItemDecision) {
	switch decision.reason {
	case feedItemSkipReasonOld:
		s.ItemsSkippedOldTotal += 1
	case feedItemSkipReasonSeen:
		s.ItemsDedupedTotal += 1
	}
}

func (f *fetcher) sendUpdate(ctx context.Context, u *update) {
	rendered, ok := f.buildUpdateMessage(u)
	if !ok {
		return
	}

	f.slog.Debug("sending message", "feed", u.feed.url, "message", rendered.Body)
	if f.dry {
		return
	}

	f.stats.WriteAccess(func(s *stats) {
		s.MessagesAttempted += 1
	})

	start := time.Now()

	if err := f.sender.Send(ctx, sender.Message{
		Body: strings.TrimSpace(rendered.Body),
		Target: sender.Target{
			Topic: strconv.FormatInt(u.feed.messageThreadID, 10),
		},
		Options: sender.Options{
			SuppressLinkPreview: rendered.DisablePreview,
		},
		Actions: rendered.Actions,
		Media:   rendered.Media,
	}); err != nil {
		f.stats.WriteAccess(func(s *stats) {
			s.MessagesFailed += 1
			s.sendLatencySamples = append(s.sendLatencySamples, time.Since(start))
		})
		f.slog.Warn("failed to send message", "chat_id", f.chatID, "error", err)
		return
	}

	f.stats.WriteAccess(func(s *stats) {
		s.MessagesSent += 1
		s.sendLatencySamples = append(s.sendLatencySamples, time.Since(start))
	})
}

func (f *fetcher) buildUpdateMessage(u *update) (format.Rendered, bool) {
	fmtUpdate := format.Update{
		Feed:  format.Feed{URL: u.feed.url, Title: u.feed.title, Digest: u.feed.digest},
		Items: u.items,
	}

	items, defaultTitle := format.BuildFormatInput(fmtUpdate)

	if u.feed.format != nil {
		val, err := format.CallStarlarkFormatter(u.feed.format, items, func(msg string) { f.slog.Info(msg) })
		if err != nil {
			f.slog.Warn("formatting message", "feed", u.feed.url, "error", err)
			return format.Rendered{}, false
		}

		rendered, err := format.ParseFormattedMessage(val)
		if err != nil {
			attrs := []any{slog.String("feed", u.feed.url), slog.String("value_type", val.Type())}
			if vErr, ok := err.(*format.ValidationError); ok {
				attrs = append(attrs,
					slog.String("reason", vErr.Reason),
					slog.Int("tuple_len", vErr.TupleLen),
					slog.String("field", vErr.Field),
					slog.String("field_type", vErr.FieldType),
				)
			}
			f.slog.Warn("format function returned invalid output", attrs...)
			return format.Rendered{}, false
		}
		rendered.DisablePreview = u.feed.digest
		return rendered, true
	}

	return format.DefaultUpdateMessage(fmtUpdate, defaultTitle, messageTemplate), true
}

func (f *fetcher) errNotify(ctx context.Context, err error) error {
	tmpl := f.errorTemplate
	if tmpl == "" {
		tmpl = defaultErrorTemplate
	}
	return f.sender.Send(ctx, sender.Message{
		Body: fmt.Sprintf(tmpl, err),
		Target: sender.Target{
			Topic: strconv.FormatInt(f.errorThreadID, 10),
		},
		Options: sender.Options{
			SuppressLinkPreview: true,
		},
	})
}
