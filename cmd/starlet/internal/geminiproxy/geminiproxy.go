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
	"sync"
	"sync/atomic"
	"time"

	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/gemini"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// Claims are the custom claims for the Gemini proxy JWT.
type Claims struct {
	Description string     `json:"gemini_description,omitempty"`
	RateLimit   rate.Limit `json:"gemini_rate_limit,omitempty"`
	jwt.RegisteredClaims
}

// TokenStat holds the statistics and the rate limiter for each token.
type TokenStat struct {
	init        sync.Once
	limiter     *rate.Limiter
	description string
	limit       rate.Limit
	requests    atomic.Uint64
	lastUsed    atomic.Pointer[time.Time]
}

// Description returns the description of the token.
func (s *TokenStat) Description() string {
	return s.description
}

// Limit returns the rate limit of the token.
func (s *TokenStat) Limit() rate.Limit {
	return s.limit
}

// Requests returns the number of requests made with the token.
func (s *TokenStat) Requests() uint64 {
	return s.requests.Load()
}

// LastUsed returns the last time the token was used.
func (s *TokenStat) LastUsed() *time.Time {
	return s.lastUsed.Load()
}

var stats syncx.Map[string, *TokenStat]

// RangeStats ranges over the stats map.
func RangeStats(f func(id string, stat *TokenStat) bool) {
	stats.Range(f)
}

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

	token, err := jwt.ParseWithClaims(tokStr, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(h.secretKey), nil
	})
	if err != nil {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}

	// Create a new stat entry. The limiter is nil at this point.
	newStat := &TokenStat{
		description: claims.Description,
		limit:       claims.RateLimit,
	}
	// Load the existing stat or store the new one.
	stat, _ := stats.LoadOrStore(claims.ID, newStat)

	// Initialize the limiter exactly once. All goroutines for the same
	// token will block here until the first one finishes initialization.
	stat.init.Do(func() {
		stat.limiter = rate.NewLimiter(stat.limit, int(stat.limit))
	})

	if !stat.limiter.Allow() {
		web.RespondJSONError(w, r, web.StatusErr(http.StatusTooManyRequests))
		return
	}

	stat.requests.Add(1)
	now := time.Now()
	stat.lastUsed.Store(&now)

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
