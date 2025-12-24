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
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
)

// Feed state.

type feed struct {
	url             string
	title           string
	messageThreadID int64
	blockRule       *starlark.Function
	keepRule        *starlark.Function
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

	// Stats.
	FetchCount     int64 `json:"fetch_count"`      // successful fetches
	FetchFailCount int64 `json:"fetch_fail_count"` // failed fetches
}

func (f *fetcher) getState(url string) (state *feedState, exists bool) {
	f.state.ReadAccess(func(s map[string]*feedState) {
		state, exists = s[url]
	})
	return
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
	f.config = string(configBytes)

	f.feeds, err = f.parseConfig(ctx, f.config)
	if err != nil {
		return err
	}

	stateMap := make(map[string]*feedState)
	state, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if state != nil {
		if err := json.Unmarshal([]byte(state), &stateMap); err != nil {
			return err
		}
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
	f.config = string(configBytes)
	f.feeds, err = f.parseConfig(ctx, f.config)
	if err != nil {
		return err
	}

	stateBytes, err := fetch("/api/state")
	if err != nil {
		return fmt.Errorf("failed to fetch state from remote: %w", err)
	}

	stateMap := make(map[string]*feedState)
	if len(stateBytes) > 0 {
		if err := json.Unmarshal(stateBytes, &stateMap); err != nil {
			return fmt.Errorf("failed to parse state JSON: %w", err)
		}
	}
	f.state = syncx.Protect(stateMap)

	errorTemplateBytes, err := fetch("/api/error-template")
	if err != nil {
		return fmt.Errorf("failed to fetch error template from remote: %w", err)
	}
	f.errorTemplate = string(errorTemplateBytes)

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
		state, err = json.MarshalIndent(s, "", "  ")
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.stateDir, "state.json"), state, 0o644)
}

func (f *fetcher) saveStateRemote(ctx context.Context) error {
	var (
		state []byte
		err   error
	)
	f.state.ReadAccess(func(s map[string]*feedState) {
		state, err = json.MarshalIndent(s, "", "  ")
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
	return os.WriteFile(filepath.Join(f.stateDir, "config.star"), []byte(f.config), 0o644)
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

func (f *fetcher) acquireRunLock() error {
	lockPath := filepath.Join(f.stateDir, ".run.lock")
	if _, err := os.Stat(lockPath); err == nil {
		return fmt.Errorf("%w: lock file exists at %s", errAlreadyRunning, lockPath)
	}
	return os.WriteFile(lockPath, fmt.Append(nil, os.Getpid()), 0o644)
}

func (f *fetcher) releaseRunLock() error {
	return os.Remove(filepath.Join(f.stateDir, ".run.lock"))
}

func (f *fetcher) isRunLocked() bool {
	_, err := os.Stat(filepath.Join(f.stateDir, ".run.lock"))
	return err == nil
}
