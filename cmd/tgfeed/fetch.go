// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/tgmarkup"

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
	sendRetryLimit        = 5  // N attempts to retry message sending

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

	state, exists := f.fetchState(fd)

	if state.Disabled {
		f.slog.Debug("skipping, feed is disabled", "feed", fd.url)
		return false, 0
	}

	req, err := f.newFeedRequest(ctx, fd, state)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}

	res, err := f.makeFeedRequest(req)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}
	defer res.Body.Close()

	f.logFeedResponse(fd, res)

	status, err := f.handleFeedStatus(req, res, fd, state)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}
	if status.handled {
		return status.retry, status.retryIn
	}

	parsedFeed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}

	f.updateFeedStateFromHeaders(state, res)
	f.enqueueFeedItems(fd, state, exists, parsedFeed.Items, updates)
	f.markFetchSuccess(state, len(parsedFeed.Items), startTime)

	return false, 0
}

func (f *fetcher) fetchState(fd *feed) (state *feedState, exists bool) {
	state, exists = f.getState(fd.url)
	// If we don't remember this feed, it's probably new. Set it's last update
	// date to current so we don't get a lot of unread articles and trigger
	// Telegram Bot API rate limit.
	if !exists {
		f.slog.Debug("initializing state", "feed", fd.url)
		f.state.WriteAccess(func(s map[string]*feedState) {
			s[fd.url] = new(feedState)
			state = s[fd.url]
		})
		state.LastUpdated = time.Now()
	}
	return state, exists
}

func (f *fetcher) newFeedRequest(ctx context.Context, fd *feed, state *feedState) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
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

func (f *fetcher) handleFeedStatus(req *http.Request, res *http.Response, fd *feed, state *feedState) (feedStatusResult, error) {
	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.slog.Debug("unmodified feed", "feed", fd.url)
		f.stats.WriteAccess(func(s *stats) {
			s.NotModifiedFeeds += 1
		})
		state.LastUpdated = time.Now()
		state.ErrorCount = 0
		state.LastError = ""
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
	state.ETag = res.Header.Get("ETag")
	if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
		state.LastModified = lastModified
	}
}

func (f *fetcher) enqueueFeedItems(fd *feed, state *feedState, exists bool, items []*gofeed.Item, updates chan *update) {
	var validItems []*gofeed.Item

	justEnabled := prepareSeenItemsState(fd, state)

	for _, feedItem := range items {
		if !shouldProcessFeedItem(fd, state, feedItem, exists, justEnabled) {
			continue
		}

		if !f.feedItemPassesRules(fd, feedItem) {
			continue
		}

		if fd.digest {
			validItems = append(validItems, feedItem)
		} else {
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

	if state.SeenItems == nil {
		state.SeenItems = make(map[string]time.Time)
		justEnabled = true
	}

	// Clean up old seen items.
	for guid, seenAt := range state.SeenItems {
		if time.Since(seenAt) > seenItemsCleanupPeriod {
			delete(state.SeenItems, guid)
		}
	}

	return justEnabled
}

func shouldProcessFeedItem(fd *feed, state *feedState, feedItem *gofeed.Item, exists bool, justEnabled bool) bool {
	if fd.alwaysSendNewItems {
		// Skip items older than lookbackPeriod.
		if feedItem.PublishedParsed != nil && time.Since(*feedItem.PublishedParsed) > lookbackPeriod {
			return false
		}

		guid := cmp.Or(feedItem.GUID, feedItem.Link)
		if _, ok := state.SeenItems[guid]; ok {
			return false
		}
		state.SeenItems[guid] = time.Now()

		// Don't send anything on the first run for a new feed or if we
		// just enabled always_send_new_items.
		if !exists || justEnabled {
			return false
		}

		return true
	}

	if feedItem.PublishedParsed != nil && feedItem.PublishedParsed.Before(state.LastUpdated) {
		return false
	}

	return true
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

func (f *fetcher) markFetchSuccess(state *feedState, parsedItems int, startTime time.Time) {
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.FetchCount += 1

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

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.stats.WriteAccess(func(s *stats) {
		s.FailedFeeds += 1
	})

	state.FetchFailCount += 1
	state.ErrorCount += 1
	state.LastError = err.Error()

	f.slog.Debug("fetch failed", "feed", url, "error", err)

	// Complain loudly and disable feed, if we failed previously enough.
	if state.ErrorCount >= errorThreshold {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed reenable %q'", url, state.ErrorCount, err, url)
		state.Disabled = true

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

	if err := f.send(ctx, strings.TrimSpace(msg), disableLinkPreview, replyMarkup, u.feed.messageThreadID); err != nil {
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
			f.slog.Warn("format function returned unexpected type", "feed", u.feed.url, "type", val.Type())
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
		if len(val) >= 1 {
			if s, ok := val[0].(starlark.String); ok {
				msg = s.GoString()
			}
		}
		if len(val) >= 2 {
			if keyboard, ok := parseInlineKeyboard(val[1]); ok {
				replyMarkup = keyboard
			}
		}
		return msg, replyMarkup, true
	default:
		return "", nil, false
	}
}

func parseInlineKeyboard(v starlark.Value) (*inlineKeyboard, bool) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, false
	}

	rows := make([][]inlineKeyboardButton, 0, list.Len())
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
			out.Text = val.GoString()
		case "url":
			out.URL = val.GoString()
		}
	}

	if out.Text == "" || out.URL == "" {
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
				Text: "↪ Hacker News",
				URL:  u.items[0].GUID,
			},
		}}
	}

	return msg, replyMarkup
}

type message struct {
	ChatID             string `json:"chat_id"`
	MessageThreadID    int64  `json:"message_thread_id,omitempty"`
	LinkPreviewOptions struct {
		IsDisabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
	ReplyMarkup *replyMarkup `json:"reply_markup,omitempty"`
	tgmarkup.Message
}

type replyMarkup struct {
	InlineKeyboard *inlineKeyboard `json:"inline_keyboard"`
}

type inlineKeyboard = [][]inlineKeyboardButton

// https://core.telegram.org/bots/api#inlinekeyboardbutton
type inlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (f *fetcher) send(ctx context.Context, text string, disableLinkPreview bool, inlineKeyboard *inlineKeyboard, threadID int64) error {
	msg := &message{
		ChatID:          f.chatID,
		MessageThreadID: threadID,
	}
	if inlineKeyboard != nil {
		msg.ReplyMarkup = &replyMarkup{inlineKeyboard}
	}
	msg.LinkPreviewOptions.IsDisabled = disableLinkPreview

	chunks := splitMessage(text)
	for _, chunk := range chunks {
		msg.Message = tgmarkup.FromMarkdown(chunk)
		var err error
		for range sendRetryLimit {
			err = f.makeTelegramRequest(ctx, "sendMessage", msg)
			if err == nil {
				break
			}
			retryable, wait := isSendingRateLimited(err)
			if !retryable {
				break
			}
			f.slog.Warn("sending rate limited, waiting", "chat_id", f.chatID, "message", chunk, "wait", wait)
			if !sleep(ctx, wait) {
				return ctx.Err()
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func splitMessage(text string) []string {
	if len(text) <= 4096 {
		return []string{text}
	}
	var chunks []string
	for len(text) > 4096 {
		// Try to split at the last newline before 4096.
		splitAt := strings.LastIndex(text[:4096], "\n")
		if splitAt == -1 {
			// No newline found, split at 4096.
			splitAt = 4096
		}
		chunks = append(chunks, text[:splitAt])
		text = text[splitAt:]
	}
	chunks = append(chunks, text)
	return chunks
}

func isSendingRateLimited(err error) (retryable bool, wait time.Duration) {
	var statusErr *request.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
		return false, 0
	}

	var errorResponse struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(statusErr.Body, &errorResponse); err != nil {
		return false, 0
	}

	return true, time.Duration(errorResponse.Parameters.RetryAfter) * time.Second
}

func (f *fetcher) errNotify(ctx context.Context, err error) error {
	tmpl := f.errorTemplate
	if tmpl == "" {
		tmpl = defaultErrorTemplate
	}
	return f.send(ctx, fmt.Sprintf(tmpl, err), true, nil, f.errorThreadID)
}

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method: http.MethodPost,
		URL:    tgAPI + "/bot" + f.tgToken + "/" + method,
		Body:   args,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: f.httpc,
		Scrubber:   f.scrubber,
	}); err != nil {
		return err
	}
	return nil
}
