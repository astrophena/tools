// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package geminiproxy implements an HTTP handler to proxy Gemini API requests.
package geminiproxy

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/google/gemini"
)

// Handler returns a HTTP handler to proxy Gemini API requests.
func Handler(token string, client *gemini.Client) http.Handler {
	return &handler{
		token:  token,
		client: client,
	}
}

type handler struct {
	token  string
	client *gemini.Client
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(h.token)) != 1 {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}

	var body any
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			web.RespondJSONError(w, r, err)
			return
		}
		if err := json.Unmarshal(b, &body); err != nil {
			web.RespondJSONError(w, r, err)
			return
		}
	}

	resp, err := gemini.RawRequest[any](r.Context(), h.client, r.Method, r.URL.Path, body)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	web.RespondJSON(w, resp)
}
