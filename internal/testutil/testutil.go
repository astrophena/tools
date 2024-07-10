// Package testutil contains common testing helpers.
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/txtar"

	"github.com/google/go-cmp/cmp"
)

// UnmarshalJSON parses the JSON data into v, failing the test in case of failure.
func UnmarshalJSON[V any](t *testing.T, b []byte) V {
	var v V
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// AssertContains fails the test if v is not present in s.
func AssertContains[S ~[]V, V comparable](t *testing.T, s S, v V) {
	if !slices.Contains(s, v) {
		t.Fatalf("%v is not present in %v", v, s)
	}
}

// AssertNotContains fails the test if v is present in s.
func AssertNotContains[S ~[]V, V comparable](t *testing.T, s S, v V) {
	if slices.Contains(s, v) {
		t.Fatalf("%v is present in %v", v, s)
	}
}

// AssertEqual compares two values and if they differ, fails the test and
// prints the difference between them.
func AssertEqual(t *testing.T, got, want any) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("(-got +want):\n%s", diff)
	}
}

// Run runs a subtest for each file matching the provided glob pattern.
func Run(t *testing.T, glob string, f func(t *testing.T, match string)) {
	matches, err := filepath.Glob(glob)
	if err != nil {
		t.Fatalf("filepath.Glob(%q): %v", glob, err)
	}
	if len(matches) == 0 {
		return
	}

	for _, match := range matches {
		name, err := filepath.Rel(filepath.Dir(match), match)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q): %v", filepath.Dir(match), match, err)
		}
		name = strings.TrimSuffix(name, filepath.Ext(match))

		t.Run(name, func(t *testing.T) {
			f(t, match)
		})
	}
}

// RunGolden runs a subtest for each file matching the provided glob pattern,
// computing the result and comparing it with a golden file, or updating a
// golden file if update is true.
//
// f is a function that should compute the result and return it as a byte slice.
func RunGolden(t *testing.T, glob string, f func(t *testing.T, match string) []byte, update bool) {
	Run(t, glob, func(t *testing.T, match string) {
		got := f(t, match)

		golden := strings.TrimSuffix(match, filepath.Ext(match)) + ".golden"
		if update {
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatalf("unable to write golden file %q: %v", golden, err)
			}
			return
		}

		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("unable to read golden file %q: %v", golden, err)
		}

		AssertEqual(t, want, got)
	})
}

// ExtractTxtar extracts a txtar archive to dir.
func ExtractTxtar(t *testing.T, ar *txtar.Archive, dir string) {
	if err := txtar.Extract(ar, dir); err != nil {
		t.Fatal(err)
	}
}

// BuildTxtar constructs a txtar archive from contents of dir.
func BuildTxtar(t *testing.T, dir string) []byte {
	ar, err := txtar.FromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return txtar.Format(ar)
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// MockHTTPClient returns a [http.Client] that serves all requests made through
// it from handler h.
func MockHTTPClient(t *testing.T, h http.Handler) *http.Client {
	httpc := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w.Result(), nil
		}),
	}
	return httpc
}
