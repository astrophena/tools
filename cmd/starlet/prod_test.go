// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
)

func TestSetWebhook(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		host        string
		wantSetHook bool
		wantErr     error
	}{
		"host not set": {
			wantErr: errNoHost,
		},
		"webhook set": {
			host:        "bot.astrophena.name",
			wantSetHook: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var called atomic.Bool

			tm := testMux(t, map[string]http.HandlerFunc{
				"POST api.telegram.org/{token}/setWebhook": func(w http.ResponseWriter, r *http.Request) {
					testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
					wantURL := "https://bot.astrophena.name/telegram"
					gotURL := testutil.UnmarshalJSON[map[string]any](t, read(t, r.Body))["url"]
					testutil.AssertEqual(t, gotURL, wantURL)

					w.WriteHeader(http.StatusOK)
					web.RespondJSON(w, map[string]bool{"ok": true})
					called.Store(true)
				},
			})
			e := testEngine(t, tm)
			e.host = tc.host

			err := e.setWebhook(t.Context())

			if tc.wantErr != nil {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("wanted error %v, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			if tc.wantSetHook {
				if !called.Load() {
					t.Fatalf("setWebhook must be called for this case")
				}
			}
		})
	}
}

func TestRenderSelfPing(t *testing.T) {
	recv := make(chan struct{})

	e := testEngine(t, testMux(t, map[string]http.HandlerFunc{
		"GET bot.astrophena.name/health": func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertEqual(t, r.URL.Scheme, "https")
			web.RespondJSON(w, web.HealthResponse{OK: true})
			recv <- struct{}{}
		},
	}))

	env := &cli.Env{
		Getenv: func(key string) string {
			if key != "RENDER_EXTERNAL_URL" {
				t.Fatalf("selfPing tried to read environment variable %s", key)
			}
			return "https://bot.astrophena.name"
		},
	}

	go e.renderSelfPing(cli.WithEnv(t.Context(), env), 10*time.Millisecond)

	<-recv
}
