// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/format"
	"go.astrophena.name/tools/cmd/tgfeed/internal/retry"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
)

// Fetch flow.

const (
	errorThreshold        = 12 // failing continuously for N fetches will disable feed and complain loudly
	fetchConcurrencyLimit = 10 // N fetches that can run at the same time
	sendConcurrencyLimit  = 2  // N sends that can run at the same time
	retryLimit            = 3  // N attempts to retry feed fetching

	maxRetryTime = 5 * time.Minute

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
// A zero value means the caller should continue parsing the feed body.
type feedStatusResult struct {
	notModified bool
	retryIn     time.Duration
}

// feedItemDecision captures both item processing and seen-state updates.
//
// process controls whether the item is processed further. markSeen stores the
// key that should be recorded in seen-items state immediately. skipReason is
// reported to stats when process is false.
type feedItemDecision struct {
	process    bool
	markSeen   string
	skipReason stats.FeedItemDecisionReason
}

// feedItemContext groups immutable fetch-time context used while deciding how
// to handle a specific parsed item.
type feedItemContext struct {
	feed                 *feed
	state                *state.Feed
	exists               bool
	seenItemsInitialized bool
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

	fdState, exists := f.feedState(fd.url)
	disabled := fdState.IsDisabled()
	etag, lastModified := fdState.CacheHeaders()
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

	t := &timings{start: time.Now()}
	req, err := f.newFeedRequest(ctx, fd, etag, lastModified, t)
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
	res.Body = &timingReadCloser{ReadCloser: res.Body, timings: t}
	defer func() {
		res.Body.Close()
		f.logFeedResponse(ctx, fd, res, t)
		f.recordRequestTimings(t)
	}()

	status, err := f.handleFeedStatus(req, res, fd)
	if err != nil {
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}
	if status.notModified {
		fdState.MarkNotModified(time.Now())
		return false, 0
	}
	if status.retryIn > 0 {
		return true, status.retryIn
	}

	parsedFeed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.stats.WriteAccess(func(s *stats.Run) {
			s.ParseErrorCount += 1
		})
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}

	f.updateFeedStateFromHeaders(fdState, res)
	f.enqueueFeedItems(fd, fdState, exists, parsedFeed.Items, updates)
	fdState.MarkFetchSuccess(time.Now())
	f.markFetchSuccess(fd.url, len(parsedFeed.Items), startTime)

	return false, 0
}

// Request construction and response handling.

func (f *fetcher) newFeedRequest(ctx context.Context, fd *feed, etag string, lastModified string, t *timings) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.url, nil)
	if err != nil {
		return nil, err
	}

	trace := newTrace(t)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	req.Header.Set("User-Agent", version.UserAgent())
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	return req, nil
}

func (f *fetcher) makeFeedRequest(req *http.Request) (*http.Response, error) {
	if isSpecialFeed(req.URL.String()) {
		return f.handleSpecialFeed(req)
	}
	return f.httpc.Do(req)
}

func (f *fetcher) recordRequestTimings(t *timings) {
	f.stats.WriteAccess(func(s *stats.Run) {
		s.RecordRequestTimings(t.statsSample())
	})
}

func (f *fetcher) logFeedResponse(ctx context.Context, fd *feed, res *http.Response, t *timings) {
	attrs := []slog.Attr{
		slog.String("feed", fd.url),
		slog.String("proto", res.Proto),
		slog.Int64("len", res.ContentLength),
		slog.Int("status", res.StatusCode),
		slog.Any("headers", res.Header),
	}

	addDuration := func(name string, start time.Time, end time.Time) {
		if start.IsZero() || end.IsZero() {
			return
		}
		attrs = append(attrs, slog.Duration(name, end.Sub(start)))
	}

	addDuration("dns_duration", t.dnsStart, t.dnsDone)
	addDuration("tcp_connect_duration", t.connectStart, t.connectDone)
	addDuration("tls_handshake_duration", t.tlsStart, t.tlsDone)
	addDuration("request_write_duration", t.gotConn, t.wroteRequest)
	addDuration("response_wait_duration", t.wroteRequest, t.firstByte)
	addDuration("time_to_first_byte", t.start, t.firstByte)
	if t.bodyComplete {
		addDuration("response_body_read_duration", t.firstByte, t.done)
	}
	addDuration("total_duration", t.start, t.done)

	f.slog.LogAttrs(ctx, slog.LevelDebug, "fetched feed", attrs...)
}

func (f *fetcher) handleFeedStatus(req *http.Request, res *http.Response, fd *feed) (feedStatusResult, error) {
	switch res.StatusCode {
	case http.StatusNotModified:
		f.slog.Debug("unmodified feed", "feed", fd.url)
		f.stats.WriteAccess(func(s *stats.Run) {
			s.NotModifiedFeeds += 1
			s.HTTP3xxCount += 1
			s.FeedStats(fd.url).LastStatusClass = 3
		})
		return feedStatusResult{
			notModified: true,
		}, nil
	case http.StatusOK:
		f.stats.WriteAccess(func(s *stats.Run) {
			s.HTTP2xxCount += 1
			s.FeedStats(fd.url).LastStatusClass = 2
		})
		return feedStatusResult{}, nil
	}

	f.stats.WriteAccess(func(s *stats.Run) {
		s.AddHTTPStatusClass(fd.url, res.StatusCode)
	})

	body, hasBody := readFeedErrorBody(res.Body)

	// Handle custom rate limiting.
	if hasBody {
		if t, found := retry.Retryable(req.URL.Host, body); found {
			f.slog.Warn("rate-limited", "host", req.URL.Host, "feed", fd.url, "retry_in", t.String())
			return feedStatusResult{
				retryIn: t,
			}, nil
		}
	}

	switch res.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		if t, found := retry.RetryAfter(res.Header.Get("Retry-After")); found {
			// Limit backoff to a reasonable maximum (context deadline or 5 minutes),
			// as Retry-After could be hours or days which would block the goroutine.
			const maxRetryAfter = 5 * time.Minute
			if t <= maxRetryAfter {
				f.slog.Warn("rate-limited (Retry-After)", "feed", fd.url, "retry_in", t.String())
				return feedStatusResult{
					retryIn: t,
				}, nil
			}
			f.slog.Warn("rate-limited (Retry-After), but wait time is too long", "feed", fd.url, "retry_in", t.String(), "max_retry_in", maxRetryAfter.String())
		}
	}

	if res.StatusCode >= 500 && res.StatusCode < 600 {
		return feedStatusResult{
			retryIn: 5 * time.Second,
		}, nil
	}

	return feedStatusResult{}, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body)
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

// Request tracing and performance statistics.
// Based on https://blainsmith.com/articles/httptrace-with-go/.

type timings struct {
	start        time.Time
	dnsStart     time.Time
	dnsDone      time.Time
	connectStart time.Time
	connectDone  time.Time
	tlsStart     time.Time
	tlsDone      time.Time
	gotConn      time.Time
	wroteRequest time.Time
	firstByte    time.Time
	done         time.Time
	bodyComplete bool
}

func (t *timings) markDone(bodyComplete bool) {
	if t.done.IsZero() {
		t.done = time.Now()
	}
	if bodyComplete {
		t.bodyComplete = true
	}
}

func (t *timings) statsSample() stats.RequestTimingSample {
	sample := stats.RequestTimingSample{
		DNS:             durationPtr(t.dnsStart, t.dnsDone),
		TCPConnect:      durationPtr(t.connectStart, t.connectDone),
		TLSHandshake:    durationPtr(t.tlsStart, t.tlsDone),
		RequestWrite:    durationPtr(t.gotConn, t.wroteRequest),
		ResponseWait:    durationPtr(t.wroteRequest, t.firstByte),
		TimeToFirstByte: durationPtr(t.start, t.firstByte),
		Total:           durationPtr(t.start, t.done),
	}
	if t.bodyComplete {
		sample.ResponseBodyRead = durationPtr(t.firstByte, t.done)
	}
	return sample
}

func durationPtr(start time.Time, end time.Time) *time.Duration {
	if start.IsZero() || end.IsZero() {
		return nil
	}
	duration := end.Sub(start)
	return &duration
}

func newTrace(t *timings) *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			t.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			t.dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			t.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			t.connectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			t.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			t.tlsDone = time.Now()
		},
		GotConn: func(_ httptrace.GotConnInfo) {
			t.gotConn = time.Now()
		},
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			t.wroteRequest = time.Now()
		},
		GotFirstResponseByte: func() {
			t.firstByte = time.Now()
		},
	}
}

type timingReadCloser struct {
	io.ReadCloser
	timings *timings
}

func (r *timingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		r.timings.markDone(true)
	}
	return n, err
}

func (r *timingReadCloser) Close() error {
	r.timings.markDone(false)
	return r.ReadCloser.Close()
}

// Item processing.

func (f *fetcher) enqueueFeedItems(fd *feed, state *state.Feed, exists bool, items []*gofeed.Item, updates chan *update) {
	var validItems []*gofeed.Item

	itemCtx := feedItemContext{
		feed:                 fd,
		state:                state,
		exists:               exists,
		seenItemsInitialized: f.prepareSeenItemsState(fd, state),
	}

	f.stats.WriteAccess(func(s *stats.Run) {
		s.ItemsSeenTotal += len(items)
	})

	now := time.Now()
	for _, feedItem := range items {
		decision := decideFeedItem(now, itemCtx, feedItem)
		// Seen state is updated even when we later skip the item so future runs do
		// not repeatedly reconsider the same entry.
		if decision.markSeen != "" {
			state.MarkSeen(decision.markSeen, now)
		}
		if !decision.process {
			f.stats.WriteAccess(func(s *stats.Run) {
				s.RecordItemDecision(decision.skipReason)
			})
			continue
		}

		starlarkVal := f.itemToStarlark(feedItem)
		if !f.feedItemPassesRules(fd, feedItem, starlarkVal) {
			continue
		}

		if fd.digest {
			validItems = append(validItems, feedItem)
			f.recordItemEnqueued(fd.url)
			continue
		}

		f.recordItemEnqueued(fd.url)
		updates <- &update{
			feed:  fd,
			items: []*gofeed.Item{feedItem},
		}
	}
	publishDigestUpdate(fd, validItems, updates)
}

func (f *fetcher) recordItemEnqueued(feedURL string) {
	f.stats.WriteAccess(func(s *stats.Run) {
		s.ItemsKeptTotal += 1
		s.ItemsEnqueuedTotal += 1
		s.FeedStats(feedURL).ItemsEnqueued += 1
	})
}

func publishDigestUpdate(fd *feed, validItems []*gofeed.Item, updates chan *update) {
	if len(validItems) > 0 {
		updates <- &update{
			feed:  fd,
			items: validItems,
		}
	}
}

func (f *fetcher) prepareSeenItemsState(fd *feed, state *state.Feed) (seenItemsInitialized bool) {
	seenItemsInitialized, pruned := state.PrepareSeenItems(time.Now(), seenItemsCleanupPeriod)
	f.stats.WriteAccess(func(s *stats.Run) {
		s.SeenItemsPrunedTotal += pruned
	})
	if !fd.alwaysSendNewItems {
		return false
	}
	return seenItemsInitialized
}

func feedItemPublishedTime(feedItem *gofeed.Item) *time.Time {
	if feedItem.PublishedParsed != nil {
		return feedItem.PublishedParsed
	}
	return feedItem.UpdatedParsed
}

func feedItemActivityTime(feedItem *gofeed.Item) *time.Time {
	if feedItem.PublishedParsed != nil {
		return feedItem.PublishedParsed
	}
	return feedItem.UpdatedParsed
}

func decideFeedItem(now time.Time, itemCtx feedItemContext, feedItem *gofeed.Item) feedItemDecision {
	guid := cmp.Or(feedItem.GUID, feedItem.Link)
	if guid == "" {
		return feedItemDecision{skipReason: stats.FeedItemSkipReasonUnknown}
	}

	if itemCtx.feed.alwaysSendNewItems {
		itemDate := feedItemPublishedTime(feedItem)
		if itemDate != nil && now.Sub(*itemDate) > lookbackPeriod {
			return feedItemDecision{skipReason: stats.FeedItemSkipReasonOld}
		}
		if itemCtx.state.IsSeen(guid) {
			return feedItemDecision{skipReason: stats.FeedItemSkipReasonSeen}
		}
		decision := feedItemDecision{markSeen: guid}
		if !itemCtx.exists || itemCtx.seenItemsInitialized {
			return decision
		}
		decision.process = true
		return decision
	}

	if itemCtx.state.IsSeen(guid) {
		return feedItemDecision{skipReason: stats.FeedItemSkipReasonSeen}
	}

	itemDate := feedItemActivityTime(feedItem)
	if itemDate != nil && itemDate.Before(itemCtx.state.LastUpdated) {
		return feedItemDecision{skipReason: stats.FeedItemSkipReasonOld}
	}

	if itemDate == nil && !itemCtx.exists {
		return feedItemDecision{markSeen: guid}
	}

	return feedItemDecision{process: true, markSeen: guid}
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

func (f *fetcher) markFetchSuccess(url string, parsedItems int, startTime time.Time) {
	f.stats.WriteAccess(func(s *stats.Run) {
		elapsed := time.Since(startTime)
		s.TotalItemsParsed += parsedItems
		s.SuccessFeeds += 1
		s.TotalFetchTime += elapsed
		s.FetchLatencySamples = append(s.FetchLatencySamples, elapsed)
		s.FeedStats(url).FetchDuration += elapsed
	})
}

// Rendering and delivery.

func (f *fetcher) sendUpdate(ctx context.Context, u *update) {
	rendered, ok := f.buildUpdateMessage(u)
	if !ok {
		return
	}

	f.slog.Debug("sending message", "feed", u.feed.url, "message", rendered.Body)
	if f.dry {
		return
	}

	f.stats.WriteAccess(func(s *stats.Run) {
		s.MessagesAttempted += 1
	})

	start := time.Now()

	if err := f.sender.Send(ctx, sender.Message{
		Body: strings.TrimSpace(rendered.Body),
		Target: sender.Target{
			Thread: strconv.FormatInt(u.feed.messageThreadID, 10),
		},
		Options: sender.Options{
			SuppressLinkPreview: rendered.DisablePreview,
		},
		Actions: rendered.Actions,
		Media:   rendered.Media,
	}); err != nil {
		f.stats.WriteAccess(func(s *stats.Run) {
			s.MessagesFailed += 1
			s.SendLatencySamples = append(s.SendLatencySamples, time.Since(start))
		})
		f.slog.Warn("failed to send message", "chat_id", f.chatID, "error", err)
		return
	}

	f.stats.WriteAccess(func(s *stats.Run) {
		s.MessagesSent += 1
		s.SendLatencySamples = append(s.SendLatencySamples, time.Since(start))
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
			Thread: strconv.FormatInt(f.errorThreadID, 10),
		},
		Options: sender.Options{
			SuppressLinkPreview: true,
		},
	})
}

// Failure handling.

func (f *fetcher) handleFetchFailure(ctx context.Context, url string, err error) {
	f.stats.WriteAccess(func(s *stats.Run) {
		s.FailedFeeds += 1
		s.ClassifyFailure(url, err)
		s.FeedStats(url).Failures += 1
	})

	var (
		disabled   bool
		errorCount int
	)
	state, _ := f.feedState(url)
	disabled = state.MarkFetchFailure(err, errorThreshold)
	errorCount = state.ErrorCount

	f.slog.Debug("fetch failed", "feed", url, "error", err)

	if disabled {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed reenable %q'", url, errorCount, err, url)
		if err := f.errNotify(ctx, err); err != nil {
			f.slog.Warn("failed to send error notification", "error", err)
		} else {
			state.DisabledNotifyPending = false
		}
	}
}
