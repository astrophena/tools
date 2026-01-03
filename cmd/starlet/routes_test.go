// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"net/http"
	"testing"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))

	// Test public routes.
	for _, path := range []string{
		"/" + e.srv.StaticHashName("static/css/main.css"),
	} {
		_, err := request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method:     http.MethodGet,
			URL:        path,
			HTTPClient: testutil.MockHTTPClient(e.srv),
		})
		if err != nil {
			t.Errorf("public: %s: %v", path, err)
		}
	}

	// Test admin routes.
	adminSrv := &web.Server{
		Mux:        e.adminMux,
		StaticFS:   staticFS,
		Debuggable: true,
	}
	for _, path := range []string{
		"/debug/",
		"/debug/logs",
		"/debug/reload",
		"/" + adminSrv.StaticHashName("static/css/main.css"),
		"/" + adminSrv.StaticHashName("static/js/logs.js"),
	} {
		_, err := request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method:     http.MethodGet,
			URL:        path,
			HTTPClient: testutil.MockHTTPClient(adminSrv),
		})
		if err != nil {
			t.Errorf("admin: %s: %v", path, err)
		}
	}
}
