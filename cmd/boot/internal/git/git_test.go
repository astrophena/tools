// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestGitClone(t *testing.T) {
	// Require git to be installed for tests.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root, remote, _ := newGitRemote(t)

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	// 1. Initial clone should result in Change.
	dest := filepath.Join(root, "local")
	action := h.EmitOne("git.clone", m.clone, starlark.Tuple{
		starlark.String(remote),
		starlark.String(dest),
	}, nil)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); errors.Is(err, fs.ErrNotExist) {
		t.Errorf("destination .git directory was not created")
	}

	// 2. Second clone (idempotency check) should result in Skip.
	action = h.EmitOne("git.clone", m.clone, starlark.Tuple{
		starlark.String(remote),
		starlark.String(dest),
	}, nil)
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestGitSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root, remote, _ := newGitRemote(t, "hello\n")

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	dest := filepath.Join(root, "local")
	action := h.EmitOne("git.sync", m.sync, nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String(remote)},
		{starlark.String("dest"), starlark.String(dest)},
	})
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("destination .git: %v", err)
	}

	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestGitPullAlreadyCurrent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root, remote, _ := newGitRemote(t, "hello\n")

	dest := filepath.Join(root, "local")
	testutil.RunGit(t, root, "clone", remote, dest)

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	action := h.EmitOne("git.pull", m.pull, starlark.Tuple{starlark.String(dest)}, nil)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestGitSyncRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root, remote, revisions := newGitRemote(t, "one\n", "two\n")
	first := revisions[0]

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}
	dest := filepath.Join(root, "local")
	action := h.EmitOne("git.sync", m.sync, nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String(remote)},
		{starlark.String("dest"), starlark.String(dest)},
		{starlark.String("revision"), starlark.String(first)},
	})
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	got := strings.TrimSpace(testutil.RunGitOutput(t, dest, "rev-parse", "HEAD"))
	if got != first {
		t.Fatalf("HEAD = %s, want %s", got, first)
	}
	branch := strings.TrimSpace(testutil.RunGitOutput(t, dest, "branch", "--show-current"))
	if branch != "" {
		t.Fatalf("branch = %q, want detached HEAD", branch)
	}
	res, err = action.Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("dry-run apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got dry-run result %v, want %v", res, boot.ResultSkip)
	}
}

func newGitRemote(t *testing.T, contents ...string) (root, remote string, revisions []string) {
	t.Helper()
	root = t.TempDir()
	remote = filepath.Join(root, "remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, remote, "init", "--bare")
	if len(contents) == 0 {
		return root, remote, nil
	}

	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, work, "init")
	testutil.RunGit(t, work, "config", "user.email", "test@example.com")
	testutil.RunGit(t, work, "config", "user.name", "Test User")
	for i, content := range contents {
		if err := os.WriteFile(filepath.Join(work, "README.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		testutil.RunGit(t, work, "add", "README.md")
		testutil.RunGit(t, work, "commit", "-m", fmt.Sprintf("commit %d", i+1))
		revisions = append(revisions, strings.TrimSpace(testutil.RunGitOutput(t, work, "rev-parse", "HEAD")))
	}
	testutil.RunGit(t, work, "branch", "-M", "main")
	testutil.RunGit(t, work, "remote", "add", "origin", remote)
	testutil.RunGit(t, work, "push", "-u", "origin", "main")
	testutil.RunGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return root, remote, revisions
}
