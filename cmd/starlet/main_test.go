// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/cmd/starlet/internal/convcache"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/web"

	"go.starlark.net/starlark"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

func TestEngineMain(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		env                map[string]string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
		checkFunc          func(t *testing.T, e *engine)
	}{
		"prints usage with help flag": {
			args:         []string{"-h"},
			wantErr:      flag.ErrHelp,
			wantInStderr: "Usage: starlet",
		},
		"overrides telegram token passed from flag by env": {
			args: []string{"-tg-token", "blablabla"},
			env: map[string]string{
				"TG_TOKEN": "foobarfoo",
			},
			checkFunc: func(t *testing.T, e *engine) {
				testutil.AssertEqual(t, e.tgToken, "foobarfoo")
			},
		},
		"version": {
			args: []string{"-version"},
		},
	}

	getenvFunc := func(env map[string]string) func(string) string {
		return func(name string) string {
			if env == nil {
				return ""
			}
			return env[name]
		}
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var (
				e              = new(engine)
				stdout, stderr bytes.Buffer
			)
			e.noServerStart = true

			err := e.main(context.Background(), tc.args, getenvFunc(tc.env), &stdout, &stderr)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.wantErr != nil {
					t.Fatalf("must fail with error: %v", tc.wantErr)
				}
			}

			if err != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error: %v", err)
			}

			if tc.wantNothingPrinted {
				if stdout.String() != "" {
					t.Errorf("stdout must be empty, got: %q", stdout.String())
				}
				if stderr.String() != "" {
					t.Errorf("stderr must be empty, got: %q", stderr.String())
				}
			}

			if tc.wantInStdout != "" && !strings.Contains(stdout.String(), tc.wantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.wantInStdout, stdout.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.wantInStderr, stderr.String())
			}

			if tc.checkFunc != nil {
				tc.checkFunc(t, e)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	e := testEngine(t, testMux(t, nil))
	health, err := request.Make[web.HealthResponse](context.Background(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

var update = flag.Bool("update", false, "update golden files in testdata")

func TestHandleTelegramWebhook(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if len(ar.Files) != 2 ||
			ar.Files[0].Name != "bot.star" ||
			ar.Files[1].Name != "update.json" {
			t.Fatalf("%s txtar should contain only two files: bot.star and update.json", match)
		}

		var upd json.RawMessage
		for _, f := range ar.Files {
			if f.Name == "update.json" {
				upd = json.RawMessage(f.Data)
			}
		}

		tm := testMux(t, nil)
		tm.gist = txtarToGist(t, readFile(t, match))
		e := testEngine(t, tm)

		_, err = request.Make[any](context.Background(), request.Params{
			Method: http.MethodPost,
			URL:    "/telegram",
			Body:   upd,
			Headers: map[string]string{
				"X-Telegram-Bot-Api-Secret-Token": e.tgSecret,
			},
			HTTPClient: testutil.MockHTTPClient(e.mux),
		})
		if err != nil {
			t.Fatal(err)
		}

		calls, err := json.MarshalIndent(tm.telegramCalls, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return calls
	}, *update)
}

func TestHandleLogin(t *testing.T) {
	e := testEngine(t, testMux(t, nil))

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
				"id":         {strconv.FormatInt(e.tgOwner, 10)},
				"first_name": {"Ilya"},
				"last_name":  {"Mateyko"},
				"username":   {"astrophena"},
				"photo_url":  {"https://t.me/i/userpic/320/XyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyq.jpg"},
				"auth_date":  {strconv.FormatInt(time.Now().Unix(), 10)},
				"hash":       {computeAuthHash(e.tgToken, e.tgOwner)},
			},
			wantStatusCode: http.StatusFound,
			wantLocation:   "/debug/",
			wantCookies: map[string]string{
				"auth_data":      base64.URLEncoding.EncodeToString([]byte(constructAuthData(e.tgOwner))),
				"auth_data_hash": computeAuthHash(e.tgToken, e.tgOwner),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/login?"+tc.query.Encode(), nil)
			w := httptest.NewRecorder()
			e.handleLogin(w, r)

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
	e := testEngine(t, testMux(t, nil))

	cases := map[string]struct {
		cookies    []*http.Cookie
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
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(e.tgOwner)))},
				{Name: "auth_data_hash", Value: "invalid"},
			},
			wantLogged: false,
		},
		"wrong owner": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(1)))},
				{Name: "auth_data_hash", Value: computeAuthHash(e.tgToken, 1)},
			},
			wantLogged: false,
		},
		"expired": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructExpiredAuthData(e.tgOwner)))},
				{Name: "auth_data_hash", Value: computeAuthHash(e.tgToken, e.tgOwner, time.Now().Add(-25*time.Hour))},
			},
			wantLogged: false,
		},
		"valid": {
			cookies: []*http.Cookie{
				{Name: "auth_data", Value: base64.URLEncoding.EncodeToString([]byte(constructAuthData(e.tgOwner)))},
				{Name: "auth_data_hash", Value: computeAuthHash(e.tgToken, e.tgOwner)},
			},
			wantLogged: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, c := range tc.cookies {
				r.AddCookie(c)
			}
			testutil.AssertEqual(t, e.loggedIn(r), tc.wantLogged)
		})
	}
}

func computeAuthHash(token string, owner int64, authTimes ...time.Time) string {
	var authTime time.Time
	if len(authTimes) > 0 {
		authTime = authTimes[0]
	} else {
		authTime = time.Now()
	}
	data := constructAuthData(owner, authTime)
	h := sha256.New()
	h.Write([]byte(token))
	tokenHash := h.Sum(nil)

	hm := hmac.New(sha256.New, tokenHash)
	hm.Write([]byte(data))
	return hex.EncodeToString(hm.Sum(nil))
}

func constructAuthData(owner int64, authTimes ...time.Time) string {
	var authTime time.Time
	if len(authTimes) > 0 {
		authTime = authTimes[0]
	} else {
		authTime = time.Now()
	}
	return "auth_date=" + strconv.FormatInt(authTime.Unix(), 10) +
		"\nfirst_name=Ilya\nid=" + strconv.FormatInt(owner, 10) +
		"\nlast_name=Mateyko\nphoto_url=https://t.me/i/userpic/320/XyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyqYqXyq.jpg\nusername=astrophena"
}

func constructExpiredAuthData(owner int64) string {
	return constructAuthData(owner, time.Now().Add(-25*time.Hour))
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

func TestReadFile(t *testing.T) {
	e := testEngine(t, testMux(t, nil))
	e.files = map[string]string{
		"test.txt": "test",
	}

	cases := map[string]struct {
		name      string
		wantValue string
		wantErr   error
	}{
		"file exists": {
			name:      "test.txt",
			wantValue: "test",
		},
		"file does not exist": {
			name:    "nonexistent.txt",
			wantErr: fmt.Errorf("file %s not found in Gist", "nonexistent.txt"),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			value, err := e.readFile(
				e.newStarlarkThread(context.Background()),
				starlark.NewBuiltin("read", e.readFile),
				starlark.Tuple{starlark.String(tc.name)}, nil)
			if tc.wantErr != nil {
				testutil.AssertEqual(t, err.Error(), tc.wantErr.Error())
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, value.(starlark.String).GoString(), tc.wantValue)
		})
	}
}

func TestEscapeHTML(t *testing.T) {
	e := testEngine(t, testMux(t, nil))

	cases := map[string]struct {
		in   string
		want string
	}{
		"no escaping": {
			in:   "plain text",
			want: "plain text",
		},
		"basic escaping": {
			in:   "<b>bold</b>",
			want: "&lt;b&gt;bold&lt;/b&gt;",
		},
		"complex escaping": {
			in:   "<script>alert('hello')</script>",
			want: "&lt;script&gt;alert(&#39;hello&#39;)&lt;/script&gt;",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			value, err := escapeHTML(
				e.newStarlarkThread(context.Background()),
				starlark.NewBuiltin("escape", escapeHTML),
				starlark.Tuple{starlark.String(tc.in)},
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, value.(starlark.String).GoString(), tc.want)
		})
	}
}

func testEngine(t *testing.T, m *mux) *engine {
	e := &engine{
		ghToken:  "test",
		gistID:   "test",
		httpc:    testutil.MockHTTPClient(m.mux),
		tgOwner:  123456789,
		stderr:   logger.Logf(t.Logf),
		tgSecret: "test",
		tgToken:  tgToken,
	}
	e.convCache = convcache.Module(context.Background(), 24*time.Hour)
	e.init.Do(e.doInit)
	return e
}

type mux struct {
	mux           *http.ServeMux
	mu            sync.Mutex
	gist          []byte
	telegramCalls []call
}

type call struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

const (
	getGist      = "GET api.github.com/gists/test"
	postTelegram = "POST api.telegram.org/{token}/{method}"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
		}
	}))
	m.mux.HandleFunc(postTelegram, orHandler(overrides[postTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		b := read(t, r.Body)
		m.telegramCalls = append(m.telegramCalls, call{
			Method: r.PathValue("method"),
			Args:   testutil.UnmarshalJSON[map[string]any](t, b),
		})
		jsonOK(w)
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == postTelegram {
			continue
		}
		m.mux.HandleFunc(pat, h)
	}
	return m
}

func orHandler(hh ...http.HandlerFunc) http.HandlerFunc {
	for _, h := range hh {
		if h != nil {
			return h
		}
	}
	return nil
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func read(t *testing.T, r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func txtarToGist(t *testing.T, b []byte) []byte {
	ar := txtar.Parse(b)

	g := &gist.Gist{
		Files: make(map[string]gist.File),
	}

	for _, f := range ar.Files {
		g.Files[f.Name] = gist.File{Content: string(f.Data)}
	}

	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return b
}
