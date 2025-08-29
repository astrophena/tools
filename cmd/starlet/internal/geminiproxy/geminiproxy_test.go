// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package geminiproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/api/gemini"
	"go.astrophena.name/tools/internal/rr"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// Updating this test:
//
//	$  GEMINI_API_KEY=... go test -httprecord testdata/*.httprr
//
// (notice an extra space before command to prevent recording it in shell
// history)

const handlerSecretKey = "test"

func TestHandler(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		authToken  string
		method     string
		path       string
		body       any
		wantStatus int
	}{
		"no_auth_token": {
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusUnauthorized,
		},
		"invalid_auth_token": {
			authToken:  "invalid",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusUnauthorized,
		},
		"not_found": {
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusInternalServerError,
			authToken:  "valid",
		},
		"model_info": {
			method:     http.MethodGet,
			path:       "/models/gemini-2.0-flash",
			wantStatus: http.StatusOK,
			authToken:  "valid",
		},
		"generate_content": {
			method: http.MethodPost,
			path:   "/models/gemini-2.0-flash:generateContent",
			body: &gemini.GenerateContentParams{
				Contents: []*gemini.Content{{Parts: []*gemini.Part{{Text: "Hello! How are you?"}}}},
			},
			wantStatus: http.StatusOK,
			authToken:  "valid",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recFile := filepath.Join("testdata", name+".httprr")
			rec, err := rr.Open(recFile, http.DefaultTransport)
			if err != nil {
				t.Fatal(err)
			}
			defer rec.Close()
			rec.ScrubReq(func(r *http.Request) error {
				r.Header.Del("x-goog-api-key")
				return nil
			})

			c := &gemini.Client{
				HTTPClient: rec.Client(),
			}
			if rec.Recording() {
				c.APIKey = os.Getenv("GEMINI_API_KEY")
			}

			h := Handler(handlerSecretKey, c)
			w := httptest.NewRecorder()

			var body io.Reader
			if tc.body != nil {
				b, err := json.Marshal(tc.body)
				if err != nil {
					t.Fatal(err)
				}
				body = bytes.NewReader(b)
			}

			ctx := cli.WithEnv(context.Background(), &cli.Env{
				Stderr: logger.Logf(t.Logf),
			})

			r := httptest.NewRequestWithContext(ctx, tc.method, tc.path, body)
			if tc.authToken != "" {
				var tokenString string
				if tc.authToken == "valid" {
					type geminiProxyClaims struct {
						Description string     `json:"gemini_description,omitempty"`
						RateLimit   rate.Limit `json:"gemini_rate_limit,omitempty"`
						jwt.RegisteredClaims
					}
					token := jwt.NewWithClaims(jwt.SigningMethodHS256, &geminiProxyClaims{
						RateLimit: 100,
						RegisteredClaims: jwt.RegisteredClaims{
							ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						},
					})
					var err error
					tokenString, err = token.SignedString([]byte(handlerSecretKey))
					if err != nil {
						t.Fatal(err)
					}
				} else {
					tokenString = tc.authToken
				}
				r.Header.Set("Authorization", "Bearer "+tokenString)
			}
			if body != nil {
				r.Header.Set("Content-Type", "application/json")
			}

			h.ServeHTTP(w, r)

			testutil.AssertEqual(t, w.Code, tc.wantStatus)
		})
	}
}
