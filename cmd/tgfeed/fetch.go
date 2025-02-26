// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/ghnotify"
	"go.astrophena.name/tools/internal/util/starlarkconv"

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
)

// fetch fetches a single feed. Each fetch runs in it's own goroutine.
func (f *fetcher) fetch(ctx context.Context, fd *feed, updates chan *gofeed.Item) (retry bool, retryIn time.Duration) {
	startTime := time.Now()

	state, exists := f.getState(fd.URL)
	// If we don't remember this feed, it's probably new. Set it's last update
	// date to current so we don't get a lot of unread articles and trigger
	// Telegram Bot API rate limit.
	if !exists {
		f.dlogf("State for feed %q doesn't exist, creating it.", fd.URL)
		f.state.Access(func(s map[string]*feedState) {
			s[fd.URL] = new(feedState)
			state = s[fd.URL]
		})
		state.LastUpdated = time.Now()
	}

	if state.Disabled {
		f.dlogf("Skipping disabled feed %q.", fd.URL)
		return false, 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.URL, nil)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.URL, err)
		return false, 0
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}

	var res *http.Response
	if fd.URL == "tgfeed://github-notifications" {
		h := ghnotify.Handler(f.ghToken, f.httpc)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		res = w.Result()
	} else {
		res, err = f.httpc.Do(req)
		if err != nil {
			f.handleFetchFailure(ctx, state, fd.URL, err)
			return false, 0
		}
	}

	defer res.Body.Close()

	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.dlogf("Feed %q was unmodified since last fetch.", fd.URL)
		f.stats.Access(func(s *stats) {
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
			var errors struct {
				Errors []string `json:"errors"`
			}
			if err := json.Unmarshal(body, &errors); err == nil {
				var t time.Duration
				for _, s := range errors.Errors {
					const prefix = "FLOOD_WAIT_"
					if !strings.HasPrefix(s, prefix) {
						continue
					}
					var err error
					t, err = time.ParseDuration(strings.TrimPrefix(s, prefix) + "s")
					if err != nil {
						continue
					}
				}
				f.logf("Feed %q got rate-limited by tg.i-c-a.su; can be retried in %s", fd.URL, t)
				return true, t
			}
		}

		f.handleFetchFailure(ctx, state, fd.URL, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body))
		return false, 0
	}

	feed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.URL, err)
		return false, 0
	}

	state.ETag = res.Header.Get("ETag")
	if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
		state.LastModified = lastModified
	}

	for _, item := range feed.Items {
		if item.PublishedParsed.Before(state.LastUpdated) {
			continue
		}

		if fd.BlockRule != nil {
			if blocked := f.applyRule(fd.BlockRule, item); blocked {
				f.dlogf("Item %q was blocked due to block rule.", item.Link)
				continue
			}
		}

		if fd.KeepRule != nil {
			if keep := f.applyRule(fd.KeepRule, item); !keep {
				f.dlogf("Item %q was not kept due to keep rule.", item.Link)
				continue
			}
		}

		updates <- item
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.FetchCount += 1

	f.stats.Access(func(s *stats) {
		s.TotalItemsParsed += len(feed.Items)
		s.SuccessFeeds += 1
		s.TotalFetchTime += time.Since(startTime)
	})

	return false, 0
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item) bool {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	extensions, err := starlarkconv.ToValue(item.Extensions)
	if err != nil {
		f.logf("Error converting item extensions to Starlark value: %v", err)
		return false
	}
	val, err := starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
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
		f.logf("Error applying rule for item %q: %v", item.Link, err)
		return false
	}

	ret, ok := val.(starlark.Bool)
	if !ok {
		f.logf("Rule for item %q returned not a boolean value.", item.Link)
		return false
	}
	return bool(ret)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.stats.Access(func(s *stats) {
		s.FailedFeeds += 1
	})

	state.FetchFailCount += 1
	state.ErrorCount += 1
	state.LastError = err.Error()

	f.dlogf("Feed %q failed with an error: %v", url, err)

	// Complain loudly and disable feed, if we failed previously enough.
	if state.ErrorCount >= errorThreshold {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed -reenable %q'", url, state.ErrorCount, err, url)
		state.Disabled = true
		if err := f.errNotify(ctx, err); err != nil {
			f.logf("Notifying about a disabled feed failed: %v", err)
		}
	}
}

func (f *fetcher) sendUpdate(ctx context.Context, item *gofeed.Item) {
	title := item.Title
	if item.Title == "" {
		title = item.Link
	}

	msg := fmt.Sprintf(
		`🔗 <a href="%[1]s">%[2]s</a>`,
		item.Link,
		html.EscapeString(title),
	)

	inlineKeyboardButtons := []inlineKeyboardButton{}

	// hnrss.org feeds have Hacker News entry URL set as GUID. Also send it
	// because I often read comments on Hacker News entries.
	if strings.HasPrefix(item.GUID, "https://news.ycombinator.com/item?id=") {
		inlineKeyboardButtons = append(inlineKeyboardButtons, inlineKeyboardButton{
			Text: "↪ Hacker News",
			URL:  item.GUID,
		})
	}

	// If in dry mode, simply log the message, but don't send it.
	if f.dry {
		f.logf("Will send message:\n\t%s\n", msg)
		return
	}

	if err := f.send(ctx, strings.TrimSpace(msg), func(args map[string]any) {
		args["reply_markup"] = map[string]any{
			"inline_keyboard": [][]inlineKeyboardButton{inlineKeyboardButtons},
		}
	}); err != nil {
		f.logf("Sending %q to %q failed: %v", msg, f.chatID, err)
	}
}

func (f *fetcher) send(ctx context.Context, message string, modify func(args map[string]any)) error {
	args := map[string]any{
		"chat_id":    f.chatID,
		"parse_mode": "HTML",
		"text":       message,
	}
	if modify != nil {
		modify(args)
	}
	return f.makeTelegramRequest(ctx, "sendMessage", args)
}

func (f *fetcher) errNotify(ctx context.Context, err error) error {
	return f.send(ctx, fmt.Sprintf(f.errorTemplate, html.EscapeString(err.Error())), disableLinkPreview)
}

// https://core.telegram.org/bots/api#linkpreviewoptions
type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

func disableLinkPreview(args map[string]any) {
	args["link_preview_options"] = linkPreviewOptions{
		IsDisabled: true,
	}
}

// https://core.telegram.org/bots/api#inlinekeyboardbutton
type inlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := request.Make[any](ctx, request.Params{
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
