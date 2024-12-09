// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package tgauth provides middleware for handling Telegram authentication.
//
// See https://core.telegram.org/widgets/login for details.
package tgauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/web"
)

// Middleware is a middleware for handling Telegram authentication.
type Middleware struct {
	// CheckFunc is a function that checks if the authenticated user is allowed to
	// access the resource. If CheckFunc is nil, all authenticated users are
	// allowed.
	//
	// The function receives a map of authentication data, where keys are field
	// names and values are field values.
	// See https://core.telegram.org/widgets/login#receiving-authorization-data for details.
	//
	// The function should return true if the user is allowed to access the
	// resource, and false otherwise.
	CheckFunc func(map[string]string) bool
	// Token is a Telegram bot token.
	Token string
}

// LoginHandler returns a handler that handles Telegram authentication.
//
// It expects to receive authentication data as query parameters from Telegram,
// validates it, and sets cookies.
//
// If authentication is successful, the user is redirected to redirectTarget.
func (mw *Middleware) LoginHandler(redirectTarget string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// See https://core.telegram.org/widgets/login#receiving-authorization-data.
		data := r.URL.Query()
		hash := data.Get("hash")
		if hash == "" {
			web.RespondError(w, r, fmt.Errorf("%w: no hash present in auth data", web.ErrBadRequest))
			return
		}
		data.Del("hash")

		var keys []string
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var sb strings.Builder
		for i, k := range keys {
			sb.WriteString(k + "=" + data.Get(k))
			// Don't append newline on last key.
			if i+1 != len(keys) {
				sb.WriteString("\n")
			}
		}
		checkString := sb.String()

		if !mw.validateAuthData(checkString, hash) {
			web.RespondError(w, r, fmt.Errorf("%w: hash is not valid", web.ErrBadRequest))
			return
		}

		setCookie(w, "auth_data", base64.URLEncoding.EncodeToString([]byte(checkString)))
		setCookie(w, "auth_data_hash", hash)

		http.Redirect(w, r, redirectTarget, http.StatusFound)

	})
}

// LoggedIn reports if the user is logged in.
func (mw *Middleware) LoggedIn(r *http.Request) bool {
	if len(r.Cookies()) == 0 {
		return false
	}

	var data, hash string
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case "auth_data":
			bdata, err := base64.URLEncoding.DecodeString(cookie.Value)
			if err != nil {
				return false
			}
			data = string(bdata)
		case "auth_data_hash":
			hash = cookie.Value
		}
	}

	if !mw.validateAuthData(data, hash) {
		return false
	}

	if mw.CheckFunc != nil {
		dataMap := extractAuthData(data)
		return mw.CheckFunc(dataMap)
	}

	return true
}

func (mw *Middleware) validateAuthData(data, hash string) bool {
	// Compute SHA256 hash of the token, serving as the secret key for HMAC.
	h := sha256.New()
	h.Write([]byte(mw.Token))
	tokenHash := h.Sum(nil)

	// Compute HMAC signature of authentication data.
	hm := hmac.New(sha256.New, tokenHash)
	hm.Write([]byte(data))
	gotHash := hex.EncodeToString(hm.Sum(nil))

	return gotHash == hash
}

func setCookie(w http.ResponseWriter, key, val string) {
	http.SetCookie(w, &http.Cookie{
		Name:     key,
		Value:    val,
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})
}

func extractAuthData(data string) map[string]string {
	dataMap := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			dataMap[parts[0]] = parts[1]
		}
	}
	return dataMap
}

// Middleware returns a middleware that checks if the user is logged in.
//
// If the user is not logged in, it responds with an error.
//
// Otherwise, it calls the next handler.
func (mw *Middleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mw.LoggedIn(r) {
			web.RespondError(w, r, web.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
