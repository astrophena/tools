// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
)

// Feed state.

type feed struct {
	url                string
	title              string
	messageThreadID    int64
	blockRule          *starlark.Function
	keepRule           *starlark.Function
	digest             bool
	format             *starlark.Function
	alwaysSendNewItems bool
}

func newFeedBuiltin(feeds *[]*feed) *starlark.Builtin {
	return starlark.NewBuiltin("feed", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) > 0 {
			return nil, fmt.Errorf("unexpected positional arguments")
		}
		f := new(feed)
		if err := starlark.UnpackArgs("feed", args, kwargs,
			"url", &f.url,
			"title?", &f.title,
			"message_thread_id?", &f.messageThreadID,
			"block_rule?", &f.blockRule,
			"keep_rule?", &f.keepRule,
			"digest?", &f.digest,
			"format?", &f.format,
			"always_send_new_items?", &f.alwaysSendNewItems,
		); err != nil {
			return nil, err
		}
		*feeds = append(*feeds, f)
		return starlark.None, nil
	})
}

type feedState struct {
	Disabled     bool      `json:"disabled"`
	LastUpdated  time.Time `json:"last_updated"`
	LastModified string    `json:"last_modified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ErrorCount   int       `json:"error_count,omitempty"`
	LastError    string    `json:"last_error,omitempty"`

	SeenItems map[string]time.Time `json:"seen_items,omitempty"`

	FetchCount     int64 `json:"fetch_count"`
	FetchFailCount int64 `json:"fetch_fail_count"`
}

func newFeedState(now time.Time) *feedState {
	return &feedState{LastUpdated: now}
}

func (s *feedState) markNotModified(now time.Time) {
	s.LastUpdated = now
	s.ErrorCount = 0
	s.LastError = ""
}

func (s *feedState) updateCacheHeaders(etag string, lastModified string) {
	s.ETag = etag
	if lastModified != "" {
		s.LastModified = lastModified
	}
}

func (s *feedState) markFetchSuccess(now time.Time) {
	s.LastUpdated = now
	s.ErrorCount = 0
	s.LastError = ""
	s.FetchCount += 1
}

func (s *feedState) markFetchFailure(err error, threshold int) (disabled bool) {
	s.FetchFailCount += 1
	s.ErrorCount += 1
	s.LastError = err.Error()
	if threshold > 0 && s.ErrorCount >= threshold && !s.Disabled {
		s.Disabled = true
		return true
	}
	return false
}

func (s *feedState) reenable() {
	s.Disabled = false
	s.ErrorCount = 0
	s.LastError = ""
}

func (s *feedState) prepareSeenItems(now time.Time) (justEnabled bool) {
	if s.SeenItems == nil {
		s.SeenItems = make(map[string]time.Time)
		justEnabled = true
	}

	for guid, seenAt := range s.SeenItems {
		if now.Sub(seenAt) > seenItemsCleanupPeriod {
			delete(s.SeenItems, guid)
		}
	}

	return justEnabled
}

func (s *feedState) isSeen(guid string) bool {
	_, ok := s.SeenItems[guid]
	return ok
}

func (s *feedState) markSeen(guid string, now time.Time) {
	if s.SeenItems == nil {
		s.SeenItems = make(map[string]time.Time)
	}
	s.SeenItems[guid] = now
}

func (s *feedState) decideAlwaysSendItem(feedItem *gofeed.Item, now time.Time, exists bool, justEnabled bool) feedItemDecision {
	// Skip items older than lookbackPeriod.
	if feedItem.PublishedParsed != nil && now.Sub(*feedItem.PublishedParsed) > lookbackPeriod {
		return feedItemDecision{
			selection: feedItemSelectionSkip,
		}
	}

	guid := cmp.Or(feedItem.GUID, feedItem.Link)
	if s.isSeen(guid) {
		return feedItemDecision{
			selection: feedItemSelectionSkip,
		}
	}

	decision := feedItemDecision{
		selection: feedItemSelectionMarkSeenOnly,
		markSeen:  guid,
	}

	// Don't send anything on the first run for a new feed or if we
	// just enabled always_send_new_items.
	if !exists || justEnabled {
		return decision
	}

	decision.selection = feedItemSelectionProcess
	return decision
}

func (s *feedState) decideRegularItem(feedItem *gofeed.Item) feedItemDecision {
	if feedItem.PublishedParsed != nil && feedItem.PublishedParsed.Before(s.LastUpdated) {
		return feedItemDecision{
			selection: feedItemSelectionSkip,
		}
	}
	return feedItemDecision{
		selection: feedItemSelectionProcess,
	}
}

func (f *fetcher) getState(url string) (state *feedState, exists bool) {
	f.state.ReadAccess(func(s map[string]*feedState) {
		state, exists = s[url]
	})
	return
}

func (f *fetcher) withFeedState(url string, fn func(*feedState, bool)) {
	f.state.WriteAccess(func(s map[string]*feedState) {
		state, exists := s[url]
		if !exists {
			state = newFeedState(time.Now())
			s[url] = state
		}
		fn(state, exists)
	})
}

func (f *fetcher) loadState(ctx context.Context) error {
	snapshot, err := f.store.LoadSnapshot(ctx)
	if err != nil {
		return err
	}
	if err := f.loadConfig(ctx, snapshot.Config); err != nil {
		return err
	}
	f.errorTemplate = snapshot.ErrorTemplate
	f.state = syncx.Protect(fromStateMap(snapshot.State))
	return nil
}

func (f *fetcher) loadConfig(ctx context.Context, config string) error {
	feeds, err := f.parseConfig(ctx, config)
	if err != nil {
		return err
	}
	f.config = config
	f.feeds = feeds
	return nil
}

func (f *fetcher) parseConfig(ctx context.Context, config string) ([]*feed, error) {
	var feeds []*feed
	intr := &interpreter.Interpreter{
		Predeclared: starlark.StringDict{
			"feed": newFeedBuiltin(&feeds),
		},
		Packages: map[string]interpreter.Loader{
			interpreter.MainPkg: interpreter.MemoryLoader(map[string]string{
				"config.star": config,
			}),
		},
		Logger: func(file string, line int, message string) {
			f.slog.Info(message, "file", file, "line", line)
		},
	}
	if err := intr.Init(ctx); err != nil {
		return nil, err
	}

	if _, err := intr.LoadModule(ctx, interpreter.MainPkg, "config.star"); err != nil {
		return nil, err
	}

	for _, feed := range feeds {
		if _, err := url.Parse(feed.url); err != nil {
			return nil, fmt.Errorf("invalid URL %q of feed %q", feed.url, feed.title)
		}
	}

	seenURLs := make(map[string]struct{}, len(feeds))
	for _, feed := range feeds {
		if _, seen := seenURLs[feed.url]; seen {
			return nil, fmt.Errorf("duplicate feed URL %q", feed.url)
		}
		seenURLs[feed.url] = struct{}{}
	}

	return feeds, nil
}

func (f *fetcher) saveState(ctx context.Context) error {
	var current map[string]*feedState
	f.state.ReadAccess(func(s map[string]*feedState) { current = s })
	return f.store.SaveState(ctx, toStateMap(current))
}

func (f *fetcher) saveConfig(ctx context.Context) error {
	return f.store.SaveConfig(ctx, f.config)
}

func (f *fetcher) acquireRunLock() error {
	if f.locker == nil {
		f.locker = state.NewLocker()
	}
	lockPath := filepath.Join(f.stateDir, ".run.lock")
	lock, err := f.locker.Acquire(lockPath, fmt.Sprintf("pid=%d\n", os.Getpid()))
	if err != nil {
		if errors.Is(err, state.ErrAlreadyRunning) {
			err = errAlreadyRunning
		}
		return fmt.Errorf("%w: lock file exists at %s", err, lockPath)
	}
	f.runLock = lock
	return nil
}

func (f *fetcher) releaseRunLock() error {
	err := f.runLock.Release()
	f.runLock = nil
	return err
}

func (f *fetcher) isRunLocked() bool {
	if f.locker == nil {
		f.locker = state.NewLocker()
	}
	return f.locker.IsLocked(filepath.Join(f.stateDir, ".run.lock"))
}

func fromStateMap(input map[string]*state.FeedState) map[string]*feedState {
	out := make(map[string]*feedState, len(input))
	for key, value := range input {
		if value == nil {
			out[key] = nil
			continue
		}
		out[key] = &feedState{
			Disabled:       value.Disabled,
			LastUpdated:    value.LastUpdated,
			LastModified:   value.LastModified,
			ETag:           value.ETag,
			ErrorCount:     value.ErrorCount,
			LastError:      value.LastError,
			SeenItems:      value.SeenItems,
			FetchCount:     value.FetchCount,
			FetchFailCount: value.FetchFailCount,
		}
	}
	return out
}

func toStateMap(input map[string]*feedState) map[string]*state.FeedState {
	out := make(map[string]*state.FeedState, len(input))
	for key, value := range input {
		if value == nil {
			out[key] = nil
			continue
		}
		out[key] = &state.FeedState{
			Disabled:       value.Disabled,
			LastUpdated:    value.LastUpdated,
			LastModified:   value.LastModified,
			ETag:           value.ETag,
			ErrorCount:     value.ErrorCount,
			LastError:      value.LastError,
			SeenItems:      value.SeenItems,
			FetchCount:     value.FetchCount,
			FetchFailCount: value.FetchFailCount,
		}
	}
	return out
}
