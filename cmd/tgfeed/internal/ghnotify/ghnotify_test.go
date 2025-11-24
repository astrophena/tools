// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package ghnotify

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/rr"
	"go.astrophena.name/base/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

// Updating this test:
//
//	$ GITHUB_TOKEN="$(gh auth token)" go test -update -httprecord testdata/handler.httprr
//

func TestHandler(t *testing.T) {
	rec, err := rr.Open(filepath.Join("testdata", "handler.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.ScrubReq(func(r *http.Request) error {
		r.Header.Del("Authorization")
		return nil
	})

	tok := "example"
	if rec.Recording() {
		tok = os.Getenv("GITHUB_TOKEN")
	}

	h := Handler(tok, nil, rec.Client())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)

	testutil.AssertEqual(t, w.Code, http.StatusOK)

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, w.Body.Bytes(), "", "  "); err != nil {
		t.Fatal(err)
	}

	goldenFile := filepath.Join("testdata", "handler.golden")
	if *update {
		if err := os.WriteFile(goldenFile, prettyJSON.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, prettyJSON.String(), string(want))
}

func TestRewriteURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/actions":                              "https://github.com/actions",
		"https://api.github.com/repos/astrophena/tools/pulls/10":  "https://github.com/astrophena/tools/pull/10",
		"https://api.github.com/repos/astrophena/tools/issues/10": "https://github.com/astrophena/tools/issues/10",
	}

	for got, want := range cases {
		if rewriteURL(got) != want {
			t.Errorf("rewriteURL(%q) != %q", got, want)
		}
	}
}
