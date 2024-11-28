// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package gemini

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/tools/internal/util/rr"

	"go.astrophena.name/base/testutil"
)

// Updating this test:
//
//	$  GEMINI_API_KEY=... go test -httprecord testdata/generate_content.httprr
//
// (notice an extra space before command to prevent recording it in shell
// history)

func TestGenerateContent(t *testing.T) {
	rec, err := rr.Open(filepath.Join("testdata", "generate_content.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.Scrub(func(r *http.Request) error {
		r.Header.Del("x-goog-api-key")
		return nil
	})

	c := &Client{
		HTTPClient: rec.Client(),
	}
	if rec.Recording() {
		c.APIKey = os.Getenv("GEMINI_API_KEY")
	}

	const prompt = "Write a poem about broken door handle."

	content, err := c.GenerateContent(context.Background(), "gemini-1.5-flash", GenerateContentParams{
		Contents: []*Content{{Parts: []*Part{{Text: prompt}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(content.Candidates), 1)
}

func TestEmptyModel(t *testing.T) {
	c := &Client{}
	_, err := c.GenerateContent(context.Background(), "", GenerateContentParams{})
	if err == nil {
		t.Fatalf("GenerateContent should fail when called with empty model")
	}
}
