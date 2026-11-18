// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package state reads and writes tgfeed configuration, feed state, and error templates.
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
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/safefile"
	"go.astrophena.name/base/version"
)

// Feed is the persisted state of a feed.
type Feed struct {
	Disabled              bool                 `json:"disabled"`
	DisabledNotifyPending bool                 `json:"disabled_notify_pending,omitempty"`
	LastUpdated           time.Time            `json:"last_updated"`
	LastModified          string               `json:"last_modified,omitempty"`
	ETag                  string               `json:"etag,omitempty"`
	ErrorCount            int                  `json:"error_count,omitempty"`
	LastError             string               `json:"last_error,omitempty"`
	SeenItems             map[string]time.Time `json:"seen_items,omitempty"`
	PendingItems          map[string]time.Time `json:"pending_items,omitempty"`
	FetchCount            int64                `json:"fetch_count"`
	FetchFailCount        int64                `json:"fetch_fail_count"`
}

// NewFeed initializes a feed state record with a non-zero LastUpdated value.
func NewFeed(now time.Time) *Feed {
	return &Feed{
		LastUpdated: now,
	}
}

// Snapshot is the full persisted state returned by [Store.LoadSnapshot].
type Snapshot struct {
	// Config is tgfeed Starlark configuration content.
	Config string
	// State maps feed URL to persisted feed runtime state.
	State map[string]*Feed
	// ErrorTemplate is the template used for fetch failure notifications.
	ErrorTemplate string
}

// Options configures [NewStore].
type Options struct {
	// StateDir is a local directory used when RemoteURL is empty.
	StateDir string
	// RemoteURL enables remote mode when non-empty.
	//
	// If it starts with "/", it is treated as a Unix socket path.
	RemoteURL string
	// HTTPClient is used for remote HTTP mode.
	//
	// It is ignored for Unix socket mode.
	HTTPClient *http.Client
	// DefaultErrorTemplate is used when error.tmpl does not exist.
	DefaultErrorTemplate string
}

// Store reads and writes tgfeed persisted state.
type Store struct {
	opts       Options
	baseURL    string
	httpClient *http.Client
}

// NewStore opens local files or configures a remote admin API client.
func NewStore(opts Options) *Store {
	s := &Store{
		opts:       opts,
		baseURL:    opts.RemoteURL,
		httpClient: opts.HTTPClient,
	}
	if strings.HasPrefix(opts.RemoteURL, "/") {
		s.baseURL = "http://unix"
		s.httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", opts.RemoteURL)
				},
			},
		}
	}
	return s
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *Store) LoadSnapshot(ctx context.Context) (*Snapshot, error) {
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
	return &Snapshot{
		Config:        config,
		State:         stateMap,
		ErrorTemplate: errorTemplate,
	}, nil
}

func (s *Store) LoadConfig(ctx context.Context) (string, error) {
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

func (s *Store) LoadState(ctx context.Context) (map[string]*Feed, error) {
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

func (s *Store) LoadErrorTemplate(ctx context.Context) (string, error) {
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

func (s *Store) fetch(ctx context.Context, endpoint string) ([]byte, error) {
	b, err := request.Make[request.Bytes](ctx, request.Params{
		Method:     http.MethodGet,
		Headers:    map[string]string{"User-Agent": version.UserAgent()},
		URL:        s.baseURL + endpoint,
		HTTPClient: s.httpClient,
	})
	if statusErr, ok := errors.AsType[*request.StatusError](err); ok {
		var response errorResponse
		if json.Unmarshal(statusErr.Body, &response) == nil && response.Error != "" {
			return nil, errors.New(response.Error)
		}
	}
	return b, err
}

func (s *Store) SaveState(ctx context.Context, state map[string]*Feed) error {
	b, err := MarshalStateMap(state)
	if err != nil {
		return err
	}
	return s.SaveStateJSON(ctx, b)
}

func (s *Store) SaveStateJSON(ctx context.Context, content []byte) error {
	if s.opts.RemoteURL == "" {
		return safefile.WriteFile(filepath.Join(s.opts.StateDir, "state.json"), content, 0o644)
	}
	if err := s.saveRemote(ctx, "/api/state", "application/json", content); err != nil {
		return fmt.Errorf("failed to save state to remote: %w", err)
	}
	return nil
}

func (s *Store) SaveConfig(ctx context.Context, config string) error {
	if s.opts.RemoteURL == "" {
		return safefile.WriteFile(filepath.Join(s.opts.StateDir, "config.star"), []byte(config), 0o644)
	}
	if err := s.saveRemote(ctx, "/api/config", "text/plain", []byte(config)); err != nil {
		return fmt.Errorf("failed to save config to remote: %w", err)
	}
	return nil
}

func (s *Store) SaveErrorTemplate(ctx context.Context, content string) error {
	if s.opts.RemoteURL == "" {
		return safefile.WriteFile(filepath.Join(s.opts.StateDir, "error.tmpl"), []byte(content), 0o644)
	}
	if err := s.saveRemote(ctx, "/api/error-template", "text/plain", []byte(content)); err != nil {
		return fmt.Errorf("failed to save error template to remote: %w", err)
	}
	return nil
}

// MarshalStateMap encodes feed state as stable, indented JSON for persistence.
func MarshalStateMap(stateMap map[string]*Feed) ([]byte, error) {
	return json.MarshalIndent(stateMap, "", "  ")
}

// UnmarshalStateMap decodes feed state JSON.
//
// Empty input returns an empty map.
func UnmarshalStateMap(b []byte) (map[string]*Feed, error) {
	stateMap := make(map[string]*Feed)
	if len(b) == 0 {
		return stateMap, nil
	}
	if err := json.Unmarshal(b, &stateMap); err != nil {
		return nil, err
	}
	return stateMap, nil
}

func (s *Store) saveRemote(ctx context.Context, endpoint, contentType string, content []byte) error {
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method:         http.MethodPut,
		URL:            s.baseURL + endpoint,
		Body:           content,
		Headers:        map[string]string{"Content-Type": contentType, "User-Agent": version.UserAgent()},
		WantStatusCode: http.StatusNoContent,
		HTTPClient:     s.httpClient,
	})
	return err
}
