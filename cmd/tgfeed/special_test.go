// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"net/http"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/rr"
)

func TestGitHubNotificationsFeed(t *testing.T) {
	t.Parallel()

	rec, err := rr.Open(filepath.Join("internal", "ghnotify", "testdata", "handler.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	tm := testMux(t, nil)
	tm.gist = txtarToGist(t, githubNotificationsTxtar)
	f := testFetcher(t, tm)
	f.httpc = &http.Client{
		Transport: &roundTripper{f.httpc.Transport, rec.Client().Transport},
	}

	if err := f.run(cli.WithEnv(t.Context(), &cli.Env{
		Stderr: t.Output(),
	})); err != nil {
		t.Fatal(err)
	}

	state := tm.state(t)["tgfeed://github-notifications"]
	testutil.AssertEqual(t, state.ErrorCount, 0)
	testutil.AssertEqual(t, state.LastError, "")
}

type roundTripper struct{ main, notifications http.RoundTripper }

func (rt *roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "api.github.com" && r.URL.Path == "/notifications" {
		r.Header.Del("Authorization")
		return rt.notifications.RoundTrip(r)
	}
	return rt.main.RoundTrip(r)
}
