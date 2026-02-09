// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/internal/starlark/go2star"

	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
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

//go:embed message.tmpl
var messageTemplate string

type update struct {
	feed  *feed
	items []*gofeed.Item
}

type feedStatusResult struct {
	handled bool
	retry   bool
	retryIn time.Duration
}

type feedItemDecision struct {
	selection feedItemSelection
	markSeen  string
}

type enqueueAction uint8

const (
	enqueueActionSkip enqueueAction = iota
	enqueueActionSingle
	enqueueActionDigest
)

type feedItemSelection uint8

const (
	feedItemSelectionSkip feedItemSelection = iota
	feedItemSelectionMarkSeenOnly
	feedItemSelectionProcess
)

type feedItemContext struct {
	feed        *feed
	state       *feedState
	exists      bool
	justEnabled bool
}

// fetch fetches one feed and either emits updates, asks caller to retry, or
// records a failure.
//
// Flow:
//
//	+-------------------+
//	| load/init state   |
//	+-------------------+
//	          |
//	          v
//	+-------------------+    yes   +------------------+
//	| state disabled?   |--------->| return (skip)    |
//	+-------------------+          +------------------+
//	          |
//	          no
//	          v
//	+-------------------+    +-------------------+    +-------------------+
//	| build HTTP req    |--->| execute request   |--->| classify status   |
//	+-------------------+    +-------------------+    +-------------------+
//	                                                     | 304 -> reset err + return
//	                                                     | rate-limited -> retry=true
//	                                                     | other non-200 -> failure
//	                                                     v
//	                                            +-------------------+
//	                                            | parse feed        |
//	                                            +-------------------+
//	                                                     |
//	                                                     v
//	+-------------------+    +-------------------+    +-------------------+
//	| update headers    |--->| filter/enqueue    |--->| mark success      |
//	+-------------------+    +-------------------+    +-------------------+
func (f *fetcher) fetch(ctx context.Context, fd *feed, updates chan *update) (retry bool, retryIn time.Duration) {
	startTime := time.Now()

	var (
		exists       bool
		disabled     bool
		etag         string
		lastModified string
	)
	f.withFeedState(fd.url, func(state *feedState, hasState bool) {
		exists = hasState
		disabled = state.Disabled
		etag = state.ETag
		lastModified = state.LastModified
	})
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
		f.handleFetchFailure(ctx, fd.url, err)
		return false, 0
	}

	f.withFeedState(fd.url, func(state *feedState, hasState bool) {
		f.updateFeedStateFromHeaders(state, res)
		f.enqueueFeedItems(fd, state, hasState, parsedFeed.Items, updates)
	})
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
		})
		f.withFeedState(fd.url, func(state *feedState, _ bool) {
			state.markNotModified(time.Now())
		})
		return feedStatusResult{
			handled: true,
		}, nil
	}
	if res.StatusCode == http.StatusOK {
		return feedStatusResult{}, nil
	}

	body, hasBody := readFeedErrorBody(res.Body)

	// Handle tg.i-c-a.su rate limiting.
	if req.URL.Host == "tg.i-c-a.su" && hasBody {
		if t, found := parseTGICASURetryIn(body); found {
			f.slog.Warn("rate-limited by tg.i-c-a.su", "feed", fd.url, "retry_in", t)
			return feedStatusResult{
				handled: true,
				retry:   true,
				retryIn: t,
			}, nil
		}
	}

	return feedStatusResult{
		handled: true,
	}, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body)
}

func readFeedErrorBody(r io.Reader) (body []byte, hasBody bool) {
	const readLimit = 16384 // 16 KB is enough for error messages (probably)

	body, err := io.ReadAll(io.LimitReader(r, readLimit))
	if err != nil {
		return []byte("unable to read body"), false
	}
	return body, true
}

func parseTGICASURetryIn(body []byte) (time.Duration, bool) {
	var response struct {
		Errors []any `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, false
	}

	for _, e := range response.Errors {
		s, ok := e.(string)
		if !ok {
			continue
		}

		const floodPrefix = "FLOOD_WAIT_"
		if after, ok := strings.CutPrefix(s, floodPrefix); ok {
			d, err := time.ParseDuration(after + "s")
			if err == nil {
				return d, true
			}
		}

		const unlockPrefix = "Time to unlock access: "
		if after, ok := strings.CutPrefix(s, unlockPrefix); ok {
			parts := strings.Split(after, ":")
			if len(parts) != 3 {
				continue
			}
			h, err1 := strconv.Atoi(parts[0])
			m, err2 := strconv.Atoi(parts[1])
			sec, err3 := strconv.Atoi(parts[2])
			if err1 == nil && err2 == nil && err3 == nil {
				return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
			}
		}
	}

	return 0, false
}

func (f *fetcher) updateFeedStateFromHeaders(state *feedState, res *http.Response) {
	state.updateCacheHeaders(res.Header.Get("ETag"), res.Header.Get("Last-Modified"))
}

func (f *fetcher) enqueueFeedItems(fd *feed, state *feedState, exists bool, items []*gofeed.Item, updates chan *update) {
	var validItems []*gofeed.Item

	itemCtx := feedItemContext{
		feed:        fd,
		state:       state,
		exists:      exists,
		justEnabled: prepareSeenItemsState(fd, state),
	}

	for _, feedItem := range items {
		decision := decideFeedItem(itemCtx, feedItem)
		if decision.markSeen != "" {
			state.markSeen(decision.markSeen, time.Now())
		}
		if decision.selection != feedItemSelectionProcess {
			continue
		}

		switch f.decideEnqueueAction(itemCtx, feedItem) {
		case enqueueActionSkip:
			continue
		case enqueueActionDigest:
			validItems = append(validItems, feedItem)
		case enqueueActionSingle:
			updates <- &update{
				feed:  fd,
				items: []*gofeed.Item{feedItem},
			}
		}
	}
	publishDigestUpdate(fd, validItems, updates)
}

func prepareSeenItemsState(fd *feed, state *feedState) (justEnabled bool) {
	if !fd.alwaysSendNewItems {
		return false
	}
	return state.prepareSeenItems(time.Now())
}

func decideFeedItem(itemCtx feedItemContext, feedItem *gofeed.Item) feedItemDecision {
	if itemCtx.feed.alwaysSendNewItems {
		return itemCtx.state.decideAlwaysSendItem(feedItem, time.Now(), itemCtx.exists, itemCtx.justEnabled)
	}
	return itemCtx.state.decideRegularItem(feedItem)
}

func (f *fetcher) decideEnqueueAction(itemCtx feedItemContext, feedItem *gofeed.Item) enqueueAction {
	if !f.feedItemPassesRules(itemCtx.feed, feedItem) {
		return enqueueActionSkip
	}
	if itemCtx.feed.digest {
		return enqueueActionDigest
	}
	return enqueueActionSingle
}

func (f *fetcher) feedItemPassesRules(fd *feed, feedItem *gofeed.Item) bool {
	if fd.blockRule != nil {
		if blocked := f.applyRule(fd.blockRule, feedItem); blocked {
			f.slog.Debug("blocked by block rule", "item", feedItem.Link)
			return false
		}
	}

	if fd.keepRule != nil {
		if keep := f.applyRule(fd.keepRule, feedItem); !keep {
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
	f.withFeedState(url, func(state *feedState, _ bool) {
		state.markFetchSuccess(time.Now())
	})

	f.stats.WriteAccess(func(s *stats) {
		s.TotalItemsParsed += parsedItems
		s.SuccessFeeds += 1
		s.TotalFetchTime += time.Since(startTime)
	})
}

func (f *fetcher) makeFeedRequest(req *http.Request) (*http.Response, error) {
	if isSpecialFeed(req.URL.String()) {
		return f.handleSpecialFeed(req)
	}
	return f.httpc.Do(req)
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item) bool {
	val, err := starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.slog.Info(msg) },
		},
		rule,
		starlark.Tuple{f.itemToStarlark(item)},
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

func (f *fetcher) itemToStarlark(item *gofeed.Item) starlark.Value {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	extensions, _ := go2star.To(item.Extensions)
	return starlarkstruct.FromStringDict(
		starlarkstruct.Default,
		starlark.StringDict{
			"title":       starlark.String(item.Title),
			"url":         starlark.String(item.Link),
			"description": starlark.String(item.Description),
			"content":     starlark.String(item.Content),
			"categories":  starlark.NewList(categories),
			"extensions":  extensions,
			"guid":        starlark.String(item.GUID),
			"published":   starlark.String(item.Published),
		},
	)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, url string, err error) {
	f.stats.WriteAccess(func(s *stats) {
		s.FailedFeeds += 1
	})

	var (
		disabled   bool
		errorCount int
	)
	f.withFeedState(url, func(state *feedState, _ bool) {
		disabled = state.markFetchFailure(err, errorThreshold)
		errorCount = state.ErrorCount
	})

	f.slog.Debug("fetch failed", "feed", url, "error", err)

	// Complain loudly and disable feed, if we failed previously enough.
	if disabled {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed reenable %q'", url, errorCount, err, url)

		if err := f.errNotify(ctx, err); err != nil {
			f.slog.Warn("failed to send error notification", "error", err)
		}
	}
}

var nonAlphaNumRe = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("[^a-zA-Z0-9/]+")
})

func (f *fetcher) sendUpdate(ctx context.Context, u *update) {
	msg, replyMarkup, ok := f.buildUpdateMessage(u)
	if !ok {
		return
	}

	f.slog.Debug("sending message", "feed", u.feed.url, "message", msg)
	if f.dry {
		return
	}

	var disableLinkPreview bool
	if u.feed.digest {
		disableLinkPreview = true
	}

	actions := []sender.ActionRow(nil)
	if replyMarkup != nil {
		actions = *replyMarkup
	}

	if err := f.sender.Send(ctx, sender.Message{
		Body: strings.TrimSpace(msg),
		Target: sender.Target{
			Topic: strconv.FormatInt(u.feed.messageThreadID, 10),
		},
		Options: sender.Options{
			SuppressLinkPreview: disableLinkPreview,
		},
		Actions: actions,
	}); err != nil {
		f.slog.Warn("failed to send message", "chat_id", f.chatID, "error", err)
	}
}

func (f *fetcher) buildUpdateMessage(u *update) (msg string, replyMarkup *inlineKeyboard, ok bool) {
	items, defaultTitle := f.buildFormatInput(u)

	if u.feed.format != nil {
		val, err := f.callStarlarkFormatter(u.feed.format, items)
		if err != nil {
			f.slog.Warn("formatting message", "feed", u.feed.url, "error", err)
			return "", nil, false
		}

		msg, replyMarkup, ok = parseFormattedMessage(val)
		if !ok {
			f.slog.Warn(
				"format function returned invalid output",
				slog.String("feed", u.feed.url),
				slog.String("value_type", val.Type()),
				slog.Bool("is_tuple", isTupleValue(val)),
				slog.Int("tuple_len", tupleLen(val)),
				slog.String("title_type", tupleTitleType(val)),
				slog.Bool("title_is_empty", tupleTitleIsEmpty(val)),
				slog.String("keyboard_type", tupleKeyboardType(val)),
			)
		}
		return msg, replyMarkup, ok
	}

	msg, replyMarkup = defaultUpdateMessage(u, defaultTitle)
	return msg, replyMarkup, true
}

func (f *fetcher) buildFormatInput(u *update) (starlark.Value, string) {
	if u.feed.digest {
		list := make([]starlark.Value, 0, len(u.items))
		for _, item := range u.items {
			list = append(list, f.itemToStarlark(item))
		}
		return starlark.NewList(list), fmt.Sprintf("Updates from %s", cmp.Or(u.feed.title, u.feed.url))
	}

	item := u.items[0]
	return f.itemToStarlark(item), cmp.Or(item.Title, item.Link)
}

func (f *fetcher) callStarlarkFormatter(format *starlark.Function, items starlark.Value) (starlark.Value, error) {
	return starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.slog.Info(msg) },
		},
		format,
		starlark.Tuple{items},
		[]starlark.Tuple{},
	)
}

func parseFormattedMessage(v starlark.Value) (msg string, replyMarkup *inlineKeyboard, ok bool) {
	switch val := v.(type) {
	case starlark.String:
		return val.GoString(), nil, true
	case starlark.Tuple:
		if len(val) == 0 {
			return "", nil, false
		}

		s, ok := val[0].(starlark.String)
		if !ok || s.GoString() == "" {
			return "", nil, false
		}
		msg = s.GoString()

		if len(val) >= 2 {
			keyboard, ok := parseInlineKeyboard(val[1])
			if !ok {
				return "", nil, false
			}
			replyMarkup = keyboard
		}
		return msg, replyMarkup, true
	default:
		return "", nil, false
	}
}

func isTupleValue(v starlark.Value) bool {
	_, ok := v.(starlark.Tuple)
	return ok
}

func tupleLen(v starlark.Value) int {
	tuple, ok := v.(starlark.Tuple)
	if !ok {
		return 0
	}
	return len(tuple)
}

func tupleTitleType(v starlark.Value) string {
	tuple, ok := v.(starlark.Tuple)
	if !ok || len(tuple) == 0 {
		return ""
	}
	return tuple[0].Type()
}

func tupleTitleIsEmpty(v starlark.Value) bool {
	tuple, ok := v.(starlark.Tuple)
	if !ok || len(tuple) == 0 {
		return false
	}
	title, ok := tuple[0].(starlark.String)
	if !ok {
		return false
	}
	return title.GoString() == ""
}

func tupleKeyboardType(v starlark.Value) string {
	tuple, ok := v.(starlark.Tuple)
	if !ok || len(tuple) < 2 {
		return ""
	}
	return tuple[1].Type()
}

func parseInlineKeyboard(v starlark.Value) (*inlineKeyboard, bool) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, false
	}

	rows := make([]sender.ActionRow, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()

	var rowValue starlark.Value
	for iter.Next(&rowValue) {
		rowList, ok := rowValue.(*starlark.List)
		if !ok {
			continue
		}

		buttons := make([]inlineKeyboardButton, 0, rowList.Len())
		rowIter := rowList.Iterate()
		var buttonValue starlark.Value
		for rowIter.Next(&buttonValue) {
			buttonDict, ok := buttonValue.(*starlark.Dict)
			if !ok {
				continue
			}
			if button, ok := parseInlineKeyboardButton(buttonDict); ok {
				buttons = append(buttons, button)
			}
		}
		rowIter.Done()

		if len(buttons) > 0 {
			rows = append(rows, buttons)
		}
	}

	if len(rows) == 0 {
		return nil, true
	}

	kb := inlineKeyboard(rows)
	return &kb, true
}

func parseInlineKeyboardButton(button *starlark.Dict) (inlineKeyboardButton, bool) {
	var out inlineKeyboardButton
	for _, item := range button.Items() {
		key, ok1 := item[0].(starlark.String)
		val, ok2 := item[1].(starlark.String)
		if !ok1 || !ok2 {
			continue
		}

		switch key.GoString() {
		case "text":
			out.Label = val.GoString()
		case "url":
			out.URL = val.GoString()
		}
	}

	if out.Label == "" || out.URL == "" {
		return inlineKeyboardButton{}, false
	}
	return out, true
}

func defaultUpdateMessage(u *update, defaultTitle string) (msg string, replyMarkup *inlineKeyboard) {
	if u.feed.digest {
		var sb strings.Builder
		fmt.Fprintf(&sb, "<b>%s</b>\n\n", defaultTitle)
		for _, item := range u.items {
			fmt.Fprintf(&sb, "• <a href=%q>%s</a>\n", item.Link, cmp.Or(item.Title, item.Link))
		}
		return sb.String(), nil
	}

	msg = fmt.Sprintf(messageTemplate, defaultTitle, u.items[0].Link)
	if u, err := urlpkg.Parse(u.items[0].Link); err == nil {
		switch u.Hostname() {
		case "t.me":
			msg += " #tg" // Telegram
		case "www.youtube.com":
			msg += " #youtube" // YouTube
		default:
			msg += " #" + nonAlphaNumRe().ReplaceAllString(u.Hostname(), "")
		}
	}
	if strings.HasPrefix(u.items[0].GUID, "https://news.ycombinator.com/item?id=") {
		replyMarkup = &inlineKeyboard{{
			{
				Label: "↪ Hacker News",
				URL:   u.items[0].GUID,
			},
		}}
	}

	return msg, replyMarkup
}

type inlineKeyboard = []sender.ActionRow

// https://core.telegram.org/bots/api#inlinekeyboardbutton
type inlineKeyboardButton = sender.Action

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
