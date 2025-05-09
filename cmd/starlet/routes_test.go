// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
)

func TestStatic(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))

	for _, path := range []string{
		"static/css/main.css",
		"static/js/logs.js",
	} {
		_, err := request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method:     http.MethodGet,
			URL:        "/" + e.srv.StaticHashName(path),
			HTTPClient: testutil.MockHTTPClient(e.mux),
		})
		if err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))
	health, err := request.Make[web.HealthResponse](t.Context(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

//go:embed testdata/message.txtar
var reloadTxtar []byte

func TestReload(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil)
	tm.gist = txtarToGist(t, reloadTxtar)
	e := testEngine(t, tm)

	cases := map[string]struct {
		authHeader string
		wantStatus int
		wantBody   string
	}{
		"unauthorized": {
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"status":"error","error":"unauthorized"}`,
		},
		"authorized": {
			authHeader: "Bearer " + e.reloadToken,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"success"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodPost, "/reload", nil)
			r.Header.Set("Authorization", tc.authHeader)
			w := httptest.NewRecorder()

			e.handleReload(w, r)

			var got bytes.Buffer
			if err := json.Compact(&got, w.Body.Bytes()); err != nil {
				t.Fatal(err)
			}

			testutil.AssertEqual(t, w.Code, tc.wantStatus)
			testutil.AssertEqual(t, got.String(), tc.wantBody)
		})
	}
}
