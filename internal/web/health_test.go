package web

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
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
			h.checks = tc.checks

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

func BenchmarkHealthHandler(b *testing.B) {
	b.ReportAllocs()

	mux := http.NewServeMux()
	h := Health(mux)
	h.RegisterFunc("foo", func() (status string, ok bool) {
		return "foo", true
	})

	for i := 0; i < b.N; i++ {
		send(b, mux, http.MethodGet, "/health", http.StatusOK)
	}
}
