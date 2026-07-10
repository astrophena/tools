// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"go.astrophena.name/tools/cmd/tgfeed/internal/ghnotify"
)

// Special feed support.

func isSpecialFeed(url string) bool { return strings.HasPrefix(url, "tgfeed://") }

func (f *fetcher) handleSpecialFeed(req *http.Request) (*http.Response, error) {
	if !isSpecialFeed(req.URL.String()) {
		return nil, errors.New("not a special feed")
	}

	var (
		h   http.Handler
		rec = httptest.NewRecorder()
	)

	switch typ := req.URL.Host; typ {
	case "github-notifications":
		h = ghnotify.Handler(f.ghToken, f.slog, f.httpc)
	default:
		return nil, fmt.Errorf("unknown special feed type %s", typ)
	}

	h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func (f *fetcher) specialFeedAcknowledger(url string) func(context.Context, []string) error {
	if url != "tgfeed://github-notifications" {
		return nil
	}
	return func(ctx context.Context, ids []string) error {
		return ghnotify.MarkAsDone(ctx, f.ghToken, f.httpc, ids)
	}
}
