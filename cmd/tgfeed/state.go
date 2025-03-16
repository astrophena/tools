// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"go.astrophena.name/base/syncx"
	"go.astrophena.name/tools/internal/api/github/gist"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Feed state.

type feed struct {
	URL       string             `json:"url"`
	Title     string             `json:"title,omitempty"`
	BlockRule *starlark.Function `json:"-"`
	KeepRule  *starlark.Function `json:"-"`
}

func (f *feed) String() string        { return fmt.Sprintf("<feed url=%q>", f.URL) }
func (f *feed) Type() string          { return "feed" }
func (f *feed) Freeze()               {} // immutable
func (f *feed) Truth() starlark.Bool  { return starlark.Bool(f.URL != "") }
func (f *feed) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", f.Type()) }

func feedBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("unexpected positional arguments")
	}
	f := new(feed)
	if err := starlark.UnpackArgs("feed", args, kwargs,
		"url", &f.URL,
		"title?", &f.Title,
		"block_rule?", &f.BlockRule,
		"keep_rule?", &f.KeepRule,
	); err != nil {
		return nil, err
	}
	return f, nil
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

func (f *fetcher) loadFromGist(ctx context.Context) error {
	g, err := f.gistc.Get(ctx, f.gistID)
	if err != nil {
		return err
	}

	errorTemplate, ok := g.Files["error.tmpl"]
	if ok {
		f.errorTemplate = errorTemplate.Content
	} else {
		f.errorTemplate = defaultErrorTemplate
	}

	config, ok := g.Files["config.star"]
	if !ok {
		return errors.New("config.star not found")
	}
	f.config = config.Content

	f.feeds, err = f.parseConfig(f.config)
	if err != nil {
		return err
	}

	stateMap := make(map[string]*feedState)
	state, ok := g.Files["state.json"]
	if ok {
		if err := json.Unmarshal([]byte(state.Content), &stateMap); err != nil {
			return err
		}
	}
	f.state = syncx.Protect(stateMap)

	return nil
}

func (f *fetcher) parseConfig(config string) ([]*feed, error) {
	globals, err := starlark.ExecFileOptions(
		&syntax.FileOptions{
			TopLevelControl: true,
		},
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
		},
		"config.star",
		config,
		starlark.StringDict{
			"feed": starlark.NewBuiltin("feed", feedBuiltin),
		},
	)
	if err != nil {
		return nil, err
	}

	feedsList, ok := globals["feeds"].(*starlark.List)
	if !ok {
		return nil, errors.New("feeds must be defined and be a list")
	}

	var feeds []*feed

	for elem := range feedsList.Elements() {
		feed, ok := elem.(*feed)
		if !ok {
			continue
		}
		_, err := url.Parse(feed.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q of feed %q", feed.URL, feed.Title)
		}
		feeds = append(feeds, feed)
	}

	return feeds, nil
}

func (f *fetcher) saveToGist(ctx context.Context) error {
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
	ng := &gist.Gist{
		Files: map[string]gist.File{
			"config.star": {Content: f.config},
			"state.json":  {Content: string(state)},
		},
	}
	_, err = f.gistc.Update(ctx, f.gistID, ng)
	return err
}
