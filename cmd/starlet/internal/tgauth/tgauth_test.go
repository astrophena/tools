// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package tgauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

const (
	// Typical Telegram Bot API token, copied from docs.
	tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	tgUser  = 123456789
)

func TestMiddleware(t *testing.T) {
	t.Parallel()

	mw := Middleware{
		Token: tgToken,
	}

	t.Run("not logged in", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("next handler should not be called")
		})

		mw.Middleware(next).ServeHTTP(w, r)

		testutil.AssertEqual(t, w.Code, http.StatusUnauthorized)
	})

	t.Run("logged in", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{
			Name:  "auth_data",
			Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(tgUser))),
		})
		r.AddCookie(&http.Cookie{
			Name:  "auth_data_hash",
			Value: computeAuthHash(tgToken, tgUser),
		})
		w := httptest.NewRecorder()
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})

		mw.Middleware(next).ServeHTTP(w, r)

		testutil.AssertEqual(t, w.Code, http.StatusAccepted)
	})
}

func TestHandleLogin(t *testing.T) {
	t.Parallel()

	mw := Middleware{
		Token: tgToken,
	}

	cases := map[string]struct {
		query          url.Values
		wantStatusCode int
		wantLocation   string
		wantCookies    map[string]string
	}{
		"missing hash": {
			query:          url.Values{},
			wantStatusCode: http.StatusBadRequest,
		},
		"invalid hash": {
			query: url.Values{
				"hash": {"invalid"},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		"valid": {
			query: url.Values{
				"id":         {strconv.FormatInt(tgUser, 10)},
				"first_name": {"John"},
				"last_name":  {"Doe"},
				"username":   {"jdoe123"},
				"photo_url":  {"https://t.me/i/userpic/320/XyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyq.jpg"},
				"auth_date":  {strconv.FormatInt(time.Now().Unix(), 10)},
				"hash":       {computeAuthHash(tgToken, tgUser)},
			},
			wantStatusCode: http.StatusFound,
			wantLocation:   "/debug/",
			wantCookies: map[string]string{
				"auth_data":      base64.URLEncoding.EncodeToString([]byte(constructAuthData(tgUser))),
				"auth_data_hash": computeAuthHash(tgToken, tgUser),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, "/login?"+tc.query.Encode(), nil)
			w := httptest.NewRecorder()
			mw.LoginHandler("/debug/").ServeHTTP(w, r)

			testutil.AssertEqual(t, w.Code, tc.wantStatusCode)
			testutil.AssertEqual(t, w.Header().Get("Location"), tc.wantLocation)

			if tc.wantCookies != nil {
				for name, wantVal := range tc.wantCookies {
					gotVal := getCookieValue(t, w.Result().Cookies(), name)
					testutil.AssertEqual(t, gotVal, wantVal)
				}
			}
		})
	}
}

func TestLoggedIn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cookies    []*http.Cookie
		checkFunc  func(*Identity) bool
		wantLogged bool
	}{
		"no cookies": {
			wantLogged: false,
		},
		"missing auth_data": {
			cookies: []*http.Cookie{
				{Name: "auth_data_hash", Value: "hash"},
			},
			wantLogged: false,
		},
		"missing auth_data_hash": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: "data"},
			},
			wantLogged: false,
		},
		"invalid auth_data": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: "invalid"},
				{Name: "auth_data_hash", Value: "hash"},
			},
			wantLogged: false,
		},
		"invalid auth_data_hash": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(tgUser)))},
				{Name: "auth_data_hash", Value: "invalid"},
			},
			wantLogged: false,
		},
		"valid": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(tgUser)))},
				{Name: "auth_data_hash", Value: computeAuthHash(tgToken, tgUser)},
			},
			wantLogged: true,
		},
		"with check func": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(tgUser)))},
				{Name: "auth_data_hash", Value: computeAuthHash(tgToken, tgUser)},
			},
			checkFunc:  func(_ *Identity) bool { return true },
			wantLogged: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mw := Middleware{
				Token: tgToken,
			}
			if tc.checkFunc != nil {
				mw.CheckFunc = tc.checkFunc
			}

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, c := range tc.cookies {
				r.AddCookie(c)
			}
			r = mw.setIdentity(r)
			testutil.AssertEqual(t, mw.LoggedIn(r), tc.wantLogged)
		})
	}
}

func computeAuthHash(token string, userID int64) string {
	data := constructAuthData(userID)
	h := sha256.New()
	h.Write([]byte(token))
	tokenHash := h.Sum(nil)

	hm := hmac.New(sha256.New, tokenHash)
	hm.Write([]byte(data))
	return hex.EncodeToString(hm.Sum(nil))
}

func constructAuthData(userID int64) string {
	var sb strings.Builder
	// Order here is important.
	sb.WriteString("auth_date=")
	sb.WriteString(strconv.FormatInt(time.Now().Unix(), 10) + "\n")
	sb.WriteString("first_name=John\n")
	sb.WriteString("id=")
	sb.WriteString(strconv.FormatInt(userID, 10) + "\n")
	sb.WriteString("last_name=Doe\n")
	sb.WriteString("photo_url=https://t.me/i/userpic/320/XyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyq.jpg\n")
	sb.WriteString("username=jdoe123")
	return sb.String()
}

func getCookieValue(t *testing.T, cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	t.Fatalf("cookie %q not found", name)
	return ""
}
