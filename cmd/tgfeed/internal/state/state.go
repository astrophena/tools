// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package state provides persistence and run-lock primitives used by tgfeed.
package state

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/atomicio"
)

type FeedState struct {
	Disabled       bool                 `json:"disabled"`
	LastUpdated    time.Time            `json:"last_updated"`
	LastModified   string               `json:"last_modified,omitempty"`
	ETag           string               `json:"etag,omitempty"`
	ErrorCount     int                  `json:"error_count,omitempty"`
	LastError      string               `json:"last_error,omitempty"`
	SeenItems      map[string]time.Time `json:"seen_items,omitempty"`
	FetchCount     int64                `json:"fetch_count"`
	FetchFailCount int64                `json:"fetch_fail_count"`
}

func NewFeedState(now time.Time) *FeedState {
	return &FeedState{LastUpdated: now}
}

type Snapshot struct {
	Config        string
	State         map[string]*FeedState
	ErrorTemplate string
}

type Store interface {
	LoadSnapshot(ctx context.Context) (*Snapshot, error)
	LoadConfig(ctx context.Context) (string, error)
	LoadState(ctx context.Context) (map[string]*FeedState, error)
	LoadErrorTemplate(ctx context.Context) (string, error)
	SaveConfig(ctx context.Context, config string) error
	SaveState(ctx context.Context, state map[string]*FeedState) error
	SaveStateJSON(ctx context.Context, content []byte) error
	SaveErrorTemplate(ctx context.Context, content string) error
}

type Lock interface{ Release() error }

type Locker interface {
	Acquire(path string, payload string) (Lock, error)
	IsLocked(path string) bool
}

var ErrAlreadyRunning = errors.New("already running")

type Options struct {
	StateDir             string
	RemoteURL            string
	HTTPClient           *http.Client
	DefaultErrorTemplate string
}

func NewStore(opts Options) Store { return &store{opts: opts} }
func NewLocker() Locker           { return fileLocker{} }

type store struct{ opts Options }

type errorResponse struct {
	Error string `json:"error"`
}

func (s *store) LoadSnapshot(ctx context.Context) (*Snapshot, error) {
	config, err := s.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	stateMap, err := s.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	errorTemplate, err := s.LoadErrorTemplate(ctx)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Config: config, State: stateMap, ErrorTemplate: errorTemplate}, nil
}

func (s *store) LoadConfig(ctx context.Context) (string, error) {
	if s.opts.RemoteURL == "" {
		configBytes, err := os.ReadFile(filepath.Join(s.opts.StateDir, "config.star"))
		if err != nil {
			return "", err
		}
		return string(configBytes), nil
	}
	b, err := s.fetch(ctx, "/api/config")
	if err != nil {
		return "", fmt.Errorf("failed to fetch config from remote: %w", err)
	}
	return string(b), nil
}

func (s *store) LoadState(ctx context.Context) (map[string]*FeedState, error) {
	if s.opts.RemoteURL == "" {
		stateBytes, err := os.ReadFile(filepath.Join(s.opts.StateDir, "state.json"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return UnmarshalStateMap(stateBytes)
	}
	b, err := s.fetch(ctx, "/api/state")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch state from remote: %w", err)
	}
	stateMap, err := UnmarshalStateMap(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state JSON: %w", err)
	}
	return stateMap, nil
}

func (s *store) LoadErrorTemplate(ctx context.Context) (string, error) {
	if s.opts.RemoteURL == "" {
		errorTemplateBytes, err := os.ReadFile(filepath.Join(s.opts.StateDir, "error.tmpl"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return cmp.Or(string(errorTemplateBytes), s.opts.DefaultErrorTemplate), nil
	}
	b, err := s.fetch(ctx, "/api/error-template")
	if err != nil {
		return "", fmt.Errorf("failed to fetch error template from remote: %w", err)
	}
	return string(b), nil
}

func (s *store) fetch(ctx context.Context, url string) ([]byte, error) {
	b, err := request.Make[request.Bytes](ctx, request.Params{Method: http.MethodGet, Headers: map[string]string{"User-Agent": version.UserAgent()}, URL: s.apiURL(url), HTTPClient: s.httpClient()})
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
	return b, nil
}

func (s *store) SaveState(ctx context.Context, state map[string]*FeedState) error {
	b, err := MarshalStateMap(state)
	if err != nil {
		return err
	}
	return s.SaveStateJSON(ctx, b)
}

func (s *store) SaveStateJSON(ctx context.Context, content []byte) error {
	if s.opts.RemoteURL == "" {
		return atomicio.WriteFile(filepath.Join(s.opts.StateDir, "state.json"), content, 0o644)
	}
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{Method: http.MethodPut, URL: s.apiURL("/api/state"), Body: content, Headers: map[string]string{"Content-Type": "application/json", "User-Agent": version.UserAgent()}, WantStatusCode: http.StatusNoContent, HTTPClient: s.httpClient()})
	if err != nil {
		return fmt.Errorf("failed to save state to remote: %w", err)
	}
	return nil
}

func (s *store) SaveConfig(ctx context.Context, config string) error {
	if s.opts.RemoteURL == "" {
		return atomicio.WriteFile(filepath.Join(s.opts.StateDir, "config.star"), []byte(config), 0o644)
	}
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{Method: http.MethodPut, URL: s.apiURL("/api/config"), Body: []byte(config), Headers: map[string]string{"Content-Type": "text/plain", "User-Agent": version.UserAgent()}, WantStatusCode: http.StatusNoContent, HTTPClient: s.httpClient()})
	if err != nil {
		return fmt.Errorf("failed to save config to remote: %w", err)
	}
	return nil
}

func (s *store) SaveErrorTemplate(ctx context.Context, content string) error {
	if s.opts.RemoteURL == "" {
		return atomicio.WriteFile(filepath.Join(s.opts.StateDir, "error.tmpl"), []byte(content), 0o644)
	}
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{Method: http.MethodPut, URL: s.apiURL("/api/error-template"), Body: []byte(content), Headers: map[string]string{"Content-Type": "text/plain", "User-Agent": version.UserAgent()}, WantStatusCode: http.StatusNoContent, HTTPClient: s.httpClient()})
	if err != nil {
		return fmt.Errorf("failed to save error template to remote: %w", err)
	}
	return nil
}

func MarshalStateMap(stateMap map[string]*FeedState) ([]byte, error) {
	return json.MarshalIndent(stateMap, "", "  ")
}

func UnmarshalStateMap(b []byte) (map[string]*FeedState, error) {
	stateMap := make(map[string]*FeedState)
	if len(b) == 0 {
		return stateMap, nil
	}
	if err := json.Unmarshal(b, &stateMap); err != nil {
		return nil, err
	}
	return stateMap, nil
}

func (s *store) httpClient() *http.Client {
	if strings.HasPrefix(s.opts.RemoteURL, "/") {
		return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", s.opts.RemoteURL)
		}}}
	}
	return s.opts.HTTPClient
}

func (s *store) apiURL(endpoint string) string {
	if strings.HasPrefix(s.opts.RemoteURL, "/") {
		return "http://unix" + endpoint
	}
	return s.opts.RemoteURL + endpoint
}

type fileLock struct{ file *os.File }

func (l *fileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		if closeErr := l.file.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return l.file.Close()
}

type fileLocker struct{}

func (fileLocker) Acquire(path string, payload string) (Lock, error) {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := lockFile.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	if payload != "" {
		if err := lockFile.Truncate(0); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
		if _, err := lockFile.Seek(0, 0); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
		if _, err := lockFile.WriteString(payload); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
	}
	return &fileLock{file: lockFile}, nil
}

func (fileLocker) IsLocked(path string) bool {
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
