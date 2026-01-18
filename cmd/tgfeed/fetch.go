// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
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
)

//go:embed message.tmpl
var messageTemplate string

type item struct {
	threadID int64
	*gofeed.Item
}

// fetch fetches a single feed. Each fetch runs in it's own goroutine.
func (f *fetcher) fetch(ctx context.Context, fd *feed, updates chan *item) (retry bool, retryIn time.Duration) {
	startTime := time.Now()

	state, exists := f.getState(fd.url)
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

	if state.Disabled {
		f.slog.Debug("skipping, feed is disabled", "feed", fd.url)
		return false, 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.url, nil)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}

	res, err := f.makeFeedRequest(req)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}
	defer res.Body.Close()

	f.slog.Debug(
		"fetched feed",
		"feed", fd.url,
		"proto", res.Proto,
		"len", res.ContentLength,
		"status", res.StatusCode,
		"headers", res.Header,
	)

	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.slog.Debug("unmodified feed", "feed", fd.url)
		f.stats.WriteAccess(func(s *stats) {
			s.NotModifiedFeeds += 1
		})
		state.LastUpdated = time.Now()
		state.ErrorCount = 0
		state.LastError = ""
		return false, 0
	}
	if res.StatusCode != http.StatusOK {
		const readLimit = 16384 // 16 KB is enough for error messages (probably)

		var (
			body    []byte
			hasBody = true
		)

		body, err = io.ReadAll(io.LimitReader(res.Body, readLimit))
		if err != nil {
			body = []byte("unable to read body")
			hasBody = false
		}

		// Handle tg.i-c-a.su rate limiting.
		if req.URL.Host == "tg.i-c-a.su" && hasBody {
			var response struct {
				Errors []any `json:"errors"`
			}
			if err := json.Unmarshal(body, &response); err == nil {
				var t time.Duration
				var found bool
				for _, e := range response.Errors {
					s, ok := e.(string)
					if !ok {
						continue
					}

					const floodPrefix = "FLOOD_WAIT_"
					if after, ok0 := strings.CutPrefix(s, floodPrefix); ok0 {
						d, err := time.ParseDuration(after + "s")
						if err == nil {
							t = d
							found = true
							break
						}
					}

					const unlockPrefix = "Time to unlock access: "
					if after, ok0 := strings.CutPrefix(s, unlockPrefix); ok0 {
						parts := strings.Split(after, ":")
						if len(parts) == 3 {
							h, err1 := strconv.Atoi(parts[0])
							m, err2 := strconv.Atoi(parts[1])
							sec, err3 := strconv.Atoi(parts[2])
							if err1 == nil && err2 == nil && err3 == nil {
								t = time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
								found = true
								break
							}
						}
					}
				}
				if found {
					f.slog.Warn("rate-limited by tg.i-c-a.su", "feed", fd.url, "retry_in", t)
					return true, t
				}
			}
		}

		f.handleFetchFailure(ctx, state, fd.url, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body))
		return false, 0
	}

	feed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.url, err)
		return false, 0
	}

	state.ETag = res.Header.Get("ETag")
	if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
		state.LastModified = lastModified
	}

	for _, feedItem := range feed.Items {
		if feedItem.PublishedParsed.Before(state.LastUpdated) {
			continue
		}

		if fd.blockRule != nil {
			if blocked := f.applyRule(fd.blockRule, feedItem); blocked {
				f.slog.Debug("blocked by block rule", "item", feedItem.Link)
				continue
			}
		}

		if fd.keepRule != nil {
			if keep := f.applyRule(fd.keepRule, feedItem); !keep {
				f.slog.Debug("skipped by keep rule", "item", feedItem.Link)
				continue
			}
		}

		updates <- &item{
			Item:     feedItem,
			threadID: fd.messageThreadID,
		}
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.FetchCount += 1

	f.stats.WriteAccess(func(s *stats) {
		s.TotalItemsParsed += len(feed.Items)
		s.SuccessFeeds += 1
		s.TotalFetchTime += time.Since(startTime)
	})

	return false, 0
}

func (f *fetcher) makeFeedRequest(req *http.Request) (*http.Response, error) {
	if isSpecialFeed(req.URL.String()) {
		return f.handleSpecialFeed(req)
	}
	return f.httpc.Do(req)
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item) bool {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	extensions, err := go2star.To(item.Extensions)
	if err != nil {
		f.slog.Warn("failed to convert item extensions to Starlark", "item", item.Link, "error", err)
		return false
	}
	val, err := starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.slog.Info(msg) },
		},
		rule,
		starlark.Tuple{starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"title":       starlark.String(item.Title),
				"url":         starlark.String(item.Link),
				"description": starlark.String(item.Description),
				"content":     starlark.String(item.Content),
				"categories":  starlark.NewList(categories),
				"extensions":  extensions,
			},
		)},
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

func (f *fetcher) sendUpdate(ctx context.Context, feedItem *item) {
	title := feedItem.Title
	if feedItem.Title == "" {
		title = feedItem.Link
	}

	msg := fmt.Sprintf(messageTemplate, title, feedItem.Link)

	if u, err := urlpkg.Parse(feedItem.Link); err == nil {
		switch u.Hostname() {
		case "t.me":
			msg += " #tg" // Telegram
		case "www.youtube.com":
			msg += " #youtube" // YouTube
		default:
			msg += " #" + nonAlphaNumRe().ReplaceAllString(u.Hostname(), "")
		}
	}

	inlineKeyboardButtons := []inlineKeyboardButton{}

	if strings.HasPrefix(feedItem.GUID, "https://news.ycombinator.com/item?id=") {
		inlineKeyboardButtons = append(inlineKeyboardButtons, inlineKeyboardButton{
			Text: "↪ Hacker News",
			URL:  feedItem.GUID,
		})
	}

	f.slog.Debug("sending message", "item", feedItem.Link, "message", msg)
	if f.dry {
		return
	}

	if err := f.send(ctx, strings.TrimSpace(msg), false, &inlineKeyboard{inlineKeyboardButtons}, feedItem.threadID); err != nil {
		f.slog.Warn("failed to send message", "chat_id", f.chatID, "error", err)
	}
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
	msg.Message = tgmarkup.FromMarkdown(text)
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
		f.slog.Warn("sending rate limited, waiting", "chat_id", f.chatID, "message", text, "wait", wait)
		time.Sleep(wait)
	}
	return err
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
