// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/tools/cmd/tgfeed/internal/format"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/internal/filelock"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.starlark.net/starlark"
)

type feedState = state.Feed

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

func (f *fetcher) getState(url string) (fdState *state.Feed, exists bool) {
	return f.state.Get(url)
}

func (f *fetcher) withFeedState(ctx context.Context, url string, fn func(*state.Feed, bool) bool) error {
	return f.state.Update(ctx, url, func(fd *state.Feed, exists bool) (bool, error) {
		return fn(fd, exists), nil
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
	f.state = state.NewFeedSet(f.store, snapshot.State)
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

	for _, feed := range feeds {
		if err := f.validateFeedFormat(feed); err != nil {
			return nil, err
		}
	}

	return feeds, nil
}

func (f *fetcher) validateFeedFormat(fd *feed) error {
	if fd.format == nil {
		return nil
	}

	update := format.Update{
		Feed: format.Feed{
			URL:    fd.url,
			Title:  fd.title,
			Digest: fd.digest,
		},
		Items: []*gofeed.Item{{
			Title:       "Sample title",
			Description: "Sample description",
			Link:        "https://example.com/item",
			GUID:        "sample-guid",
			Published:   time.Now().Format(time.RFC3339),
		}},
	}
	items, _ := format.BuildFormatInput(update)

	value, err := format.CallStarlarkFormatter(fd.format, items, func(msg string) { f.slog.Info(msg) })
	if err != nil {
		return fmt.Errorf("format() for feed %q failed: %w", fd.url, err)
	}

	if _, err := format.ParseFormattedMessage(value); err != nil {
		return fmt.Errorf("format() for feed %q returned invalid output: %w", fd.url, err)
	}

	return nil
}

func (f *fetcher) saveConfig(ctx context.Context) error {
	return f.store.SaveConfig(ctx, f.config)
}

func (f *fetcher) acquireRunLock() error {
	lockPath := filepath.Join(f.stateDir, ".run.lock")
	lock, err := filelock.Acquire(lockPath, fmt.Sprintf("pid=%d\n", os.Getpid()))
	if err != nil {
		if errors.Is(err, filelock.ErrAlreadyLocked) {
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
	return filelock.IsLocked(filepath.Join(f.stateDir, ".run.lock"))
}
