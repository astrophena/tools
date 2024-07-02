package request_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/request"
)

func TestMakeJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check the request method and path.
		if r.Method != http.MethodPost || r.URL.Path != "/test" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.Body == nil {
			http.Error(w, "missing request body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": "success"}`))
	}))
	defer ts.Close()

	cases := map[string]struct {
		params  request.Params
		want    string
		wantErr bool
	}{
		"successful request": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/test",
				Body:   map[string]string{"key": "value"},
			},
			want: `{"message": "success"}`,
		},
		"successful request with headers": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/test",
				Headers: map[string]string{
					"X-Test": "test",
				},
				Body: map[string]string{"key": "value"},
			},
			want: `{"message": "success"}`,
		},
		"custom HTTP client": {
			params: request.Params{
				Method:     http.MethodPost,
				URL:        ts.URL + "/test",
				HTTPClient: &http.Client{},
				Body:       map[string]string{"key": "value"},
			},
			want: `{"message": "success"}`,
		},
		"invalid request method": {
			params: request.Params{
				Method: http.MethodGet,
				URL:    ts.URL + "/test",
			},
			wantErr: true,
		},
		"invalid request path": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/invalid",
			},
			wantErr: true,
		},
		"invalid value for JSON": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/test",
				Body:   make(chan int),
			},
			wantErr: true,
		},
		"scrubbed token": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/invalid",
				Body:   map[string]string{"key": "value"},
				Headers: map[string]string{
					"X-Token": "hello",
				},
				Scrubber: strings.NewReplacer("hello", "[EXPUNGED]"),
			},
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var resp json.RawMessage
			resp, err := request.MakeJSON[json.RawMessage](context.Background(), tc.params)
			if err != nil {
				if !tc.wantErr {
					t.Errorf("MakeJSON() error = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}
			if tc.wantErr {
				t.Errorf("MakeJSON() expected error, got none")
			} else if string(resp) != tc.want {
				t.Errorf("MakeJSON() got = %v, want %v", resp, tc.want)
			}
		})
	}
}
