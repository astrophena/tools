// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package web

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"go.astrophena.name/base/syncx"
)

func TestHealthHandler(t *testing.T) {
	cases := map[string]struct {
		checks       map[string]HealthFunc
		wantResponse *HealthResponse
		wantStatus   int
	}{
		"no checks": {
			checks: map[string]HealthFunc{},
			wantResponse: &HealthResponse{
				OK:     true,
				Checks: map[string]CheckResponse{},
			},
			wantStatus: http.StatusOK,
		},
		"check that always returns ok": {
			checks: map[string]HealthFunc{
				"always-ok": func() (status string, ok bool) {
					return "this check always returns ok", true
				},
			},
			wantResponse: &HealthResponse{
				OK: true,
				Checks: map[string]CheckResponse{
					"always-ok": {
						OK:     true,
						Status: "this check always returns ok",
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		"check that always returns not ok": {
			checks: map[string]HealthFunc{
				"always-not-ok": func() (status string, ok bool) {
					return "this check always returns not ok", false
				},
			},
			wantResponse: &HealthResponse{
				OK: false,
				Checks: map[string]CheckResponse{
					"always-not-ok": {
						OK:     false,
						Status: "this check always returns not ok",
					},
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		"two checks": {
			checks: map[string]HealthFunc{
				"ok": func() (status string, ok bool) {
					return "ok", true
				},
				"not-ok": func() (status string, ok bool) {
					return "not ok", false
				},
			},
			wantResponse: &HealthResponse{
				OK: false,
				Checks: map[string]CheckResponse{
					"ok": {
						OK:     true,
						Status: "ok",
					},
					"not-ok": {
						OK:     false,
						Status: "not ok",
					},
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			h := Health(mux)
			h.checks = syncx.Protect(tc.checks)

			gotStr := send(t, mux, http.MethodGet, "/health", tc.wantStatus)
			got := new(HealthResponse)
			if err := json.Unmarshal([]byte(gotStr), got); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tc.wantResponse) {
				t.Fatalf("wanted response %#v, but got %#v", *tc.wantResponse, *got)
			}
		})
	}
}

func TestHealthHandlerRegisterFuncDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("RegisterFunc did not panic when using an already existing name")
		}
	}()

	mux := http.NewServeMux()
	h := Health(mux)
	h.RegisterFunc("foo", func() (status string, ok bool) {
		return "foo", true
	})
	h.RegisterFunc("foo", func() (status string, ok bool) {
		return "not foo", true
	})
}
