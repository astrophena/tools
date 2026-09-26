// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package golang

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"

	"go.starlark.net/starlark"
)

func TestGoInstallCreatesOneActionPerPackage(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")

	m := &impl{rt: rt}
	_, err := m.install(h.Thread, starlark.NewBuiltin("go.install", m.install), starlark.Tuple{
		starlark.NewList([]starlark.Value{
			starlark.String("example.com/one@latest"),
			starlark.String("example.com/two@latest"),
		}),
	}, nil)
	if err != nil {
		t.Fatalf("go.install failed: %v", err)
	}
	if len(h.Task.Actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(h.Task.Actions))
	}
	if got := h.Task.Actions[0].Summary; !strings.Contains(got, "example.com/one@latest") {
		t.Errorf("first action summary = %q", got)
	}
	if got := h.Task.Actions[1].Summary; !strings.Contains(got, "example.com/two@latest") {
		t.Errorf("second action summary = %q", got)
	}
}

func TestGoInstallLocalConditions(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "src")
	if err := os.Mkdir(localDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")

	m := &impl{rt: rt}
	_, err := m.installLocal(h.Thread, starlark.NewBuiltin("go.install_local", m.installLocal), nil, []starlark.Tuple{
		{starlark.String("package"), starlark.String("example.com/tool")},
		{starlark.String("cwd"), starlark.String(localDir)},
	})
	if err != nil {
		t.Fatalf("go.install_local failed: %v", err)
	}
	if len(h.Task.Actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(h.Task.Actions))
	}

	res, err := h.Task.Actions[0].Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	res, err = h.Task.Actions[1].Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestProxyUpToDateFixedVersion(t *testing.T) {
	info := goBuildInfo{
		Path:    "example.com/tool",
		Module:  "example.com/tool",
		Version: "v1.2.3",
	}
	upToDate, err := proxyUpToDate(t.Context(), "example.com/tool@v1.2.3", info)
	if err != nil {
		t.Fatal(err)
	}
	if !upToDate {
		t.Fatal("fixed version should be up to date")
	}
}

func TestLatestModuleUsesProxy(t *testing.T) {
	oldProxy := goProxyURL
	defer func() {
		goProxyURL = oldProxy
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/hello/@latest" {
			t.Errorf("request path = %q, want /example.com/hello/@latest", r.URL.Path)
		}
		fmt.Fprint(w, `{"Version":"v1.2.3","Time":"2026-05-30T00:00:00Z"}`)
	}))
	defer server.Close()
	goProxyURL = server.URL

	info, err := latestModule(t.Context(), "example.com/hello")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want v1.2.3", info.Version)
	}
}

func TestLocalUpToDate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root := t.TempDir()
	testutil.RunGit(t, root, "init")
	testutil.RunGit(t, root, "config", "user.email", "test@example.com")
	testutil.RunGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, root, "add", "go.mod")
	testutil.RunGit(t, root, "commit", "-m", "initial")
	head := strings.TrimSpace(testutil.RunGitOutput(t, root, "rev-parse", "HEAD"))

	upToDate, err := localUpToDate(t.Context(), root, goBuildInfo{
		Settings: map[string]string{
			"vcs.revision": head,
			"vcs.modified": "false",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !upToDate {
		t.Fatal("clean checkout with matching revision should be up to date")
	}

	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	upToDate, err = localUpToDate(t.Context(), root, goBuildInfo{
		Settings: map[string]string{
			"vcs.revision": head,
			"vcs.modified": "false",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upToDate {
		t.Fatal("dirty checkout should not be up to date")
	}
}
