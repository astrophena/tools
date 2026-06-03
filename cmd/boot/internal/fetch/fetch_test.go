// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package fetch

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"

	"go.starlark.net/starlark"
)

func TestFetchFileRejectsInvalidChecksum(t *testing.T) {
	rt := &boot.Runtime{Root: t.TempDir()}
	_, thread := testutil.TaskThread("test")
	m := &impl{rt: rt}
	_, err := m.file(thread, starlark.NewBuiltin("fetch.file", m.file), nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String("https://example.invalid/file")},
		{starlark.String("path"), starlark.String("file")},
		{starlark.String("checksum"), starlark.String("sha256:not-hex")},
	})
	if err == nil {
		t.Fatal("fetch.file succeeded, want checksum error")
	}
}

func TestFetchFileUpdatesModeWithoutChecksum(t *testing.T) {
	content := []byte("hello\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")
	m := &impl{rt: rt}
	_, err := m.file(thread, starlark.NewBuiltin("fetch.file", m.file), nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String(server.URL)},
		{starlark.String("path"), starlark.String(path)},
		{starlark.String("mode"), starlark.MakeInt(0o600)},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := task.Actions[0].Apply(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res != boot.ResultChange {
		t.Fatalf("result = %s, want %s", res, boot.ResultChange)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestFetchFile(t *testing.T) {
	content := []byte("hello\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")

	m := &impl{rt: rt}
	path := filepath.Join(root, "dir", "file.txt")
	checksum := fmt.Sprintf("%x", sha256.Sum256(content))
	_, err := m.file(thread, starlark.NewBuiltin("fetch.file", m.file), nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String(server.URL)},
		{starlark.String("path"), starlark.String(path)},
		{starlark.String("mode"), starlark.MakeInt(0o600)},
		{starlark.String("checksum"), starlark.String(checksum)},
	})
	if err != nil {
		t.Fatalf("fetch.file failed: %v", err)
	}
	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	res, err := task.Actions[0].Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("got content %q, want %q", got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got mode %o, want 0600", info.Mode().Perm())
	}

	res, err = task.Actions[0].Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}
