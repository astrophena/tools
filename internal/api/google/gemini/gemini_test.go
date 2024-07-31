package gemini

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/request/rr"
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
	rec.Scrub(func(r *http.Request) error {
		r.Header.Del("x-goog-api-key")
		return nil
	})

	c := &Client{
		Model:      "gemini-1.5-flash",
		HTTPClient: rec.Client(),
	}
	if rec.Recording() {
		c.APIKey = os.Getenv("GEMINI_API_KEY")
	}

	const prompt = "Write a poem about broken door handle."

	content, err := c.GenerateContent(context.Background(), GenerateContentParams{
		Contents: []*Content{{Parts: []*Part{{Text: prompt}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(content.Candidates), 1)
}
