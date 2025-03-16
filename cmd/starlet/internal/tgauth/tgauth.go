// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package tgauth provides middleware for handling Telegram authentication.
//
// See https://core.telegram.org/widgets/login for details.
package tgauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.astrophena.name/base/web"
)

type ctxKey string

const identityKey ctxKey = "identity"

// Identify returns the [Identity] value stored in ctx, returning nil in case it
// doesn't exist.
func Identify(r *http.Request) *Identity {
	ident, ok := r.Context().Value(identityKey).(*Identity)
	if !ok {
		return nil
	}
	return ident
}

// Identity contains information about the logged in user.
type Identity struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
	PhotoURL  string
	AuthDate  time.Time
}

// Middleware is a middleware for handling Telegram authentication.
type Middleware struct {
	// CheckFunc is a function that checks if the authenticated user is allowed to
	// access the resource. If CheckFunc is nil, all authenticated users are
	// allowed.
	CheckFunc func(*Identity) bool
	// Token is a Telegram bot token.
	Token string
	// TTL is the time-to-live for a user session. If set, the session expires
	// after this time; otherwise, it doesn't.
	TTL time.Duration
}

// LoginHandler returns a handler that handles Telegram authentication.
//
// It expects to receive authentication data as query parameters from Telegram,
// validates it, and sets cookies.
//
// If authentication is successful, the user is redirected to redirectTarget.
func (mw *Middleware) LoginHandler(redirectTarget string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mw.LoggedIn(r) {
			http.Redirect(w, r, redirectTarget, http.StatusFound)
			return
		}

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

		mw.setCookie(w, "auth_data", base64.URLEncoding.EncodeToString([]byte(checkString)))
		mw.setCookie(w, "auth_data_hash", hash)

		http.Redirect(w, r, redirectTarget, http.StatusFound)
	})
}

// LogoutHandler returns a handler that logs the user out. After that, the user
// is redirected to redirectTarget.
func (mw *Middleware) LogoutHandler(redirectTarget string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delCookie(w, "auth_data")
		delCookie(w, "auth_data_hash")
		http.Redirect(w, r, redirectTarget, http.StatusFound)
	})
}

// LoggedIn reports if the user is logged in.
func (mw *Middleware) LoggedIn(r *http.Request) bool {
	ident := Identify(r)
	if ident == nil {
		return false
	}
	if mw.CheckFunc != nil {
		return mw.CheckFunc(ident)
	}
	return true
}

func (mw *Middleware) setIdentity(r *http.Request) *http.Request {
	if len(r.Cookies()) == 0 {
		return r
	}

	var data, hash string
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case "auth_data":
			bdata, err := base64.URLEncoding.DecodeString(cookie.Value)
			if err != nil {
				return r
			}
			data = string(bdata)
		case "auth_data_hash":
			hash = cookie.Value
		}
	}

	if !mw.validateAuthData(data, hash) {
		return r
	}

	ident, err := authDataToIdentity(data)
	if err != nil {
		return r
	}
	if mw.TTL > 0 && time.Since(ident.AuthDate) > mw.TTL {
		return r
	}

	return r.WithContext(context.WithValue(r.Context(), identityKey, ident))
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

func (mw *Middleware) setCookie(w http.ResponseWriter, key, val string) {
	cookie := &http.Cookie{
		Name:     key,
		Value:    val,
		HttpOnly: true,
	}
	if mw.TTL > 0 {
		cookie.Expires = time.Now().Add(mw.TTL)
	}
	http.SetCookie(w, cookie)
}

func delCookie(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{
		Name:     key,
		Value:    "",
		MaxAge:   0,
		HttpOnly: true,
	})
}

func authDataToIdentity(data string) (*Identity, error) {
	dataMap := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			dataMap[parts[0]] = parts[1]
		}
	}

	ident := &Identity{
		Username:  dataMap["username"],
		FirstName: dataMap["first_name"],
		LastName:  dataMap["last_name"],
		PhotoURL:  dataMap["photo_url"],
	}

	id, err := strconv.ParseInt(dataMap["id"], 10, 64)
	if err != nil {
		return nil, err
	}
	ident.ID = id

	date, err := strconv.ParseInt(dataMap["auth_date"], 10, 64)
	if err != nil {
		return nil, err
	}
	ident.AuthDate = time.Unix(date, 0)

	return ident, nil
}

// Middleware returns a middleware that identifies the user and optionally
// checks if the user is logged in.
//
// If the user is not logged in, it responds with an error [web.ErrUnauthorized].
//
// Otherwise, it calls the next handler.
func (mw *Middleware) Middleware(enforceAuth bool) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = mw.setIdentity(r)
			if enforceAuth {
				if !mw.LoggedIn(r) {
					web.RespondError(w, r, web.ErrUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
