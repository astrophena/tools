// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/atomicio"
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

	// SeenItems tracks processed items for feeds with always_send_new_items
	// enabled. The key is the item GUID and the value is the time it was first
	// seen.
	SeenItems map[string]time.Time `json:"seen_items,omitempty"`

	// Stats.
	FetchCount     int64 `json:"fetch_count"`      // successful fetches
	FetchFailCount int64 `json:"fetch_fail_count"` // failed fetches
}

func newFeedState(now time.Time) *feedState {
	return &feedState{
		LastUpdated: now,
	}
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

func (f *fetcher) getOrCreateState(url string) (state *feedState, exists bool) {
	f.state.WriteAccess(func(s map[string]*feedState) {
		state, exists = s[url]
		if !exists {
			state = newFeedState(time.Now())
			s[url] = state
		}
	})
	return
}

func marshalStateMap(stateMap map[string]*feedState) ([]byte, error) {
	return json.MarshalIndent(stateMap, "", "  ")
}

func unmarshalStateMap(b []byte) (map[string]*feedState, error) {
	stateMap := make(map[string]*feedState)
	if len(b) == 0 {
		return stateMap, nil
	}
	if err := json.Unmarshal(b, &stateMap); err != nil {
		return nil, err
	}
	return stateMap, nil
}

func (f *fetcher) loadState(ctx context.Context) error {
	if f.remoteURL != "" {
		return f.loadStateRemote(ctx)
	}

	errorTemplateBytes, err := os.ReadFile(filepath.Join(f.stateDir, "error.tmpl"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f.errorTemplate = cmp.Or(string(errorTemplateBytes), defaultErrorTemplate)

	configBytes, err := os.ReadFile(filepath.Join(f.stateDir, "config.star"))
	if err != nil {
		return err
	}
	if err := f.loadConfig(ctx, string(configBytes)); err != nil {
		return err
	}

	state, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	stateMap, err := unmarshalStateMap(state)
	if err != nil {
		return err
	}
	f.state = syncx.Protect(stateMap)

	return nil
}

type errorResponse struct {
	Error string `json:"error"`
}

func (f *fetcher) loadStateRemote(ctx context.Context) error {
	fetch := func(url string) ([]byte, error) {
		b, err := request.Make[request.Bytes](ctx, request.Params{
			Method: http.MethodGet,
			Headers: map[string]string{
				"User-Agent": version.UserAgent(),
			},
			URL:        f.apiURL(url),
			HTTPClient: f.httpClient(),
		})
		if err != nil {
			var statusErr *request.StatusError
			if errors.As(err, &statusErr) {
				var errResp *errorResponse
				if jsonErr := json.Unmarshal(statusErr.Body, &errResp); jsonErr == nil {
					err = errors.New(errResp.Error)
				}
			}
			return nil, err
		}
		return b, err
	}

	configBytes, err := fetch("/api/config")
	if err != nil {
		return fmt.Errorf("failed to fetch config from remote: %w", err)
	}
	if err := f.loadConfig(ctx, string(configBytes)); err != nil {
		return err
	}

	stateBytes, err := fetch("/api/state")
	if err != nil {
		return fmt.Errorf("failed to fetch state from remote: %w", err)
	}
	stateMap, err := unmarshalStateMap(stateBytes)
	if err != nil {
		return fmt.Errorf("failed to parse state JSON: %w", err)
	}
	f.state = syncx.Protect(stateMap)

	errorTemplateBytes, err := fetch("/api/error-template")
	if err != nil {
		return fmt.Errorf("failed to fetch error template from remote: %w", err)
	}
	f.errorTemplate = string(errorTemplateBytes)

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

	return feeds, nil
}

func (f *fetcher) saveState(ctx context.Context) error {
	if f.remoteURL != "" {
		return f.saveStateRemote(ctx)
	}

	var (
		state []byte
		err   error
	)
	f.state.ReadAccess(func(s map[string]*feedState) {
		state, err = marshalStateMap(s)
	})
	if err != nil {
		return err
	}
	return atomicio.WriteFile(filepath.Join(f.stateDir, "state.json"), state, 0o644)
}

func (f *fetcher) saveStateRemote(ctx context.Context) error {
	var (
		state []byte
		err   error
	)
	f.state.ReadAccess(func(s map[string]*feedState) {
		state, err = marshalStateMap(s)
	})
	if err != nil {
		return err
	}

	_, err = request.Make[request.IgnoreResponse](ctx, request.Params{
		Method: http.MethodPut,
		URL:    f.apiURL("/api/state"),
		Body:   state,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   version.UserAgent(),
		},
		WantStatusCode: http.StatusNoContent,
		HTTPClient:     f.httpClient(),
	})
	if err != nil {
		return fmt.Errorf("failed to save state to remote: %w", err)
	}

	return nil
}

func (f *fetcher) saveConfig(ctx context.Context) error {
	if f.remoteURL != "" {
		return f.saveConfigRemote(ctx)
	}
	return atomicio.WriteFile(filepath.Join(f.stateDir, "config.star"), []byte(f.config), 0o644)
}

func (f *fetcher) saveConfigRemote(ctx context.Context) error {
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method: http.MethodPut,
		URL:    f.apiURL("/api/config"),
		Body:   []byte(f.config),
		Headers: map[string]string{
			"Content-Type": "text/plain",
			"User-Agent":   version.UserAgent(),
		},
		WantStatusCode: http.StatusNoContent,
		HTTPClient:     f.httpClient(),
	})
	if err != nil {
		return fmt.Errorf("failed to save config to remote: %w", err)
	}
	return nil
}

func (f *fetcher) httpClient() *http.Client {
	if strings.HasPrefix(f.remoteURL, "/") {
		// Unix socket.
		return &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", f.remoteURL)
				},
			},
		}
	}
	return f.httpc
}

func (f *fetcher) apiURL(endpoint string) string {
	if strings.HasPrefix(f.remoteURL, "/") {
		// For Unix sockets, use dummy host.
		return "http://unix" + endpoint
	}
	return f.remoteURL + endpoint
}

type runLocker struct{}

func (runLocker) acquire(path string) (*os.File, error) {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := lockFile.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errAlreadyRunning
		}
		return nil, err
	}

	return lockFile, nil
}

func (runLocker) writePayload(lockFile *os.File, payload string) error {
	if payload == "" {
		return nil
	}
	if err := lockFile.Truncate(0); err != nil {
		return err
	}
	if _, err := lockFile.Seek(0, 0); err != nil {
		return err
	}
	if _, err := lockFile.WriteString(payload); err != nil {
		return err
	}
	return nil
}

func (runLocker) release(lockFile *os.File) error {
	if lockFile == nil {
		return nil
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		if closeErr := lockFile.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return lockFile.Close()
}

func (l runLocker) isLocked(path string) bool {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer lockFile.Close()

	err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		return false
	}

	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func (f *fetcher) acquireRunLock() error {
	lockPath := filepath.Join(f.stateDir, ".run.lock")
	lockFile, err := (runLocker{}).acquire(lockPath)
	if err != nil {
		return fmt.Errorf("%w: lock file exists at %s", err, lockPath)
	}
	if err := (runLocker{}).writePayload(lockFile, fmt.Sprintf("pid=%d\n", os.Getpid())); err != nil {
		_ = (runLocker{}).release(lockFile)
		return err
	}
	f.runLock = lockFile
	return nil
}

func (f *fetcher) releaseRunLock() error {
	err := (runLocker{}).release(f.runLock)
	f.runLock = nil
	return err
}

func (f *fetcher) isRunLocked() bool {
	return (runLocker{}).isLocked(filepath.Join(f.stateDir, ".run.lock"))
}
