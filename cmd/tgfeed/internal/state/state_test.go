// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package state

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestStoreLoadSnapshotLocalFallbackTemplate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.star"), []byte("feed(url=\"https://example.com\")"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(Options{StateDir: dir, DefaultErrorTemplate: "default template"})
	snapshot, err := store.LoadSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, snapshot.ErrorTemplate, "default template")
	testutil.AssertEqual(t, len(snapshot.State), 0)
}

func TestStateMapRoundtrip(t *testing.T) {
	t.Parallel()
	input := map[string]*FeedState{"https://example.com": {LastUpdated: time.Date(2026, time.January, 1, 1, 2, 3, 0, time.UTC), ErrorCount: 2}}
	b, err := MarshalStateMap(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalStateMap(b)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, input)
}

func TestStoreRemoteErrorPropagation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad config"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	store := NewStore(Options{RemoteURL: srv.URL, HTTPClient: srv.Client()})
	_, err := store.LoadSnapshot(t.Context())
	if err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("expected propagated remote error, got %v", err)
	}
}

func TestLockerConflict(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".run.lock")
	locker := NewLocker()
	first, err := locker.Acquire(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	_, err = locker.Acquire(path, "")
	if err == nil {
		t.Fatal("expected lock conflict")
	}
	testutil.AssertEqual(t, err == ErrAlreadyRunning, true)
}
