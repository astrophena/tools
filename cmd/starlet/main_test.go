// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/gist"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

func TestRun(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) *engine {
		e := new(engine)
		e.httpc = testutil.MockHTTPClient(testMux(t, nil).mux)
		e.noServerStart = true
		return e
	}, map[string]clitest.Case[*engine]{
		"prints usage with help flag": {
			Args:    []string{"-h"},
			WantErr: flag.ErrHelp,
		},
		"sets telegram token passed by env": {
			Args: []string{},
			Env: map[string]string{
				"TG_TOKEN": tgToken,
			},
			CheckFunc: func(t *testing.T, e *engine) {
				testutil.AssertEqual(t, e.tgToken, tgToken)
			},
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
	},
	)
}

func testEngine(t *testing.T, m *mux) *engine {
	t.Helper()
	e := &engine{
		ghToken:  "test",
		gistID:   "test",
		httpc:    testutil.MockHTTPClient(m.mux),
		tgOwner:  123456789,
		tgSecret: "test",
		tgToken:  tgToken,
	}
	if err := e.init.Get(func() error {
		return e.doInit(t.Context())
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

type mux struct {
	mux           *http.ServeMux
	mu            sync.Mutex
	gist          []byte
	telegramCalls []call
}

type call struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

const (
	getGist       = "GET api.github.com/gists/test"
	getMeTelegram = "GET api.telegram.org/{token}/getMe"
	postTelegram  = "POST api.telegram.org/{token}/{method}"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.gist = txtarToGist(t, []byte("-- bot.star --\n"))
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
		}
	}))
	m.mux.HandleFunc(getMeTelegram, orHandler(overrides[getMeTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		var resp getMeResponse
		resp.OK = true
		resp.Result.ID = 123456789
		resp.Result.Username = "foo_bot"
		web.RespondJSON(w, resp)
	}))
	m.mux.HandleFunc(postTelegram, orHandler(overrides[postTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		b := read(t, r.Body)
		m.telegramCalls = append(m.telegramCalls, call{
			Method: r.PathValue("method"),
			Args:   testutil.UnmarshalJSON[map[string]any](t, b),
		})
		web.RespondJSON(w, map[string]string{"status": "success"})
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == postTelegram || pat == getMeTelegram {
			continue
		}
		m.mux.HandleFunc(pat, h)
	}
	return m
}

func orHandler(hh ...http.HandlerFunc) http.HandlerFunc {
	for _, h := range hh {
		if h != nil {
			return h
		}
	}
	return nil
}

func read(t *testing.T, r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func txtarToGist(t *testing.T, b []byte) []byte {
	ar := txtar.Parse(b)

	g := &gist.Gist{
		Files: make(map[string]gist.File),
	}

	for _, f := range ar.Files {
		g.Files[f.Name] = gist.File{Content: string(f.Data)}
	}

	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return b
}
