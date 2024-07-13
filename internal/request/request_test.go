package request_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/request"
	"go.astrophena.name/tools/internal/web"
)

func ExampleMakeJSON() {
	// Checking health of Starlet.
	health, err := request.MakeJSON[web.HealthResponse](context.Background(), request.Params{
		Method: http.MethodGet,
		URL:    "https://bot.astrophena.name/health",
	})
	if err != nil {
		log.Fatal(err)
	}

	if health.OK {
		log.Println("Alive.")
	} else {
		log.Printf("Not alive: %+v", health)
	}
}

func ExampleMakeJSON_scrub() {
	// Making request to GitHub API, scrubbing token out of error messages.
	user, err := request.MakeJSON[map[string]any](context.Background(), request.Params{
		Method: http.MethodGet,
		URL:    "https://api.github.com/user",
		Headers: map[string]string{
			"Authorization": "Bearer " + os.Getenv("GITHUB_TOKEN"),
		},
		Scrubber: strings.NewReplacer(os.Getenv("GITHUB_TOKEN"), "[EXPUNGED]"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user["login"])
}

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
		params          request.Params
		want            string
		wantErr         bool
		wantInErrorText string
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
			wantErr:         true,
			wantInErrorText: "want 200, got 400: invalid request",
		},
		"invalid request path": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/invalid",
			},
			wantErr:         true,
			wantInErrorText: "want 200, got 400: invalid request",
		},
		"invalid value for JSON": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/test",
				Body:   make(chan int),
			},
			wantErr:         true,
			wantInErrorText: "json: unsupported type: chan int",
		},
		"scrubbed token": {
			params: request.Params{
				Method: http.MethodPost,
				URL:    ts.URL + "/hello",
				Body:   map[string]string{"key": "value"},
				Headers: map[string]string{
					"X-Token": "hello",
				},
				Scrubber: strings.NewReplacer("hello", "[EXPUNGED]"),
			},
			wantErr:         true,
			wantInErrorText: "[EXPUNGED]\": want 200, got 400: invalid request",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var resp json.RawMessage
			resp, err := request.MakeJSON[json.RawMessage](context.Background(), tc.params)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("MakeJSON() error = %v, wantErr %v", err, tc.wantErr)
				}
			}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("MakeJSON() expected error, got none")
				}
				if !strings.Contains(err.Error(), tc.wantInErrorText) {
					t.Fatalf("MakeJSON(): got error %q, wanted in it %q", err.Error(), tc.wantInErrorText)
				}
			}

			if string(resp) != tc.want {
				t.Errorf("MakeJSON() got = %v, want %v", resp, tc.want)
			}
		})
	}
}
