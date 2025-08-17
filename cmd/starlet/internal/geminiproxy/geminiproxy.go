// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package geminiproxy implements an HTTP handler to proxy Gemini API requests.
package geminiproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/gemini"

	"github.com/golang-jwt/jwt/v5"
)

// Handler returns a HTTP handler to proxy Gemini API requests.
func Handler(secretKey string, client *gemini.Client) http.Handler {
	return &handler{
		secretKey: secretKey,
		client:    client,
	}
}

type handler struct {
	secretKey string
	client    *gemini.Client
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle preflight request.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "1728000")
	if r.Method == http.MethodOptions {
		return
	}

	tokStr := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tokStr == "" {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}

	token, err := jwt.Parse(tokStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(h.secretKey), nil
	})
	if err != nil || !token.Valid {
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
