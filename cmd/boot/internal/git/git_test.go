// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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

	root := t.TempDir()

	// Initialize a local git repository to serve as the "remote".
	remote := filepath.Join(root, "remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, remote, "init", "--bare")

	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")

	m := &impl{rt: rt}

	// 1. Initial clone should result in Change.
	dest := filepath.Join(root, "local")
	_, err := m.clone(thread, starlark.NewBuiltin("git.clone", m.clone), starlark.Tuple{
		starlark.String(remote),
		starlark.String(dest),
	}, nil)
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	res, err := task.Actions[0].Apply(context.Background(), false)
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
	task.Actions = nil
	_, err = m.clone(thread, starlark.NewBuiltin("git.clone", m.clone), starlark.Tuple{
		starlark.String(remote),
		starlark.String(dest),
	}, nil)
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	res, err = task.Actions[0].Apply(context.Background(), false)
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

	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, remote, "init", "--bare")

	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, work, "init")
	testutil.RunGit(t, work, "config", "user.email", "test@example.com")
	testutil.RunGit(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, work, "add", "README.md")
	testutil.RunGit(t, work, "commit", "-m", "initial")
	testutil.RunGit(t, work, "branch", "-M", "main")
	testutil.RunGit(t, work, "remote", "add", "origin", remote)
	testutil.RunGit(t, work, "push", "-u", "origin", "main")
	testutil.RunGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")
	m := &impl{rt: rt}

	dest := filepath.Join(root, "local")
	_, err := m.sync(thread, starlark.NewBuiltin("git.sync", m.sync), nil, []starlark.Tuple{
		{starlark.String("url"), starlark.String(remote)},
		{starlark.String("dest"), starlark.String(dest)},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	res, err := task.Actions[0].Apply(context.Background(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("destination .git: %v", err)
	}

	res, err = task.Actions[0].Apply(context.Background(), false)
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

	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, remote, "init", "--bare")

	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, work, "init")
	testutil.RunGit(t, work, "config", "user.email", "test@example.com")
	testutil.RunGit(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, work, "add", "README.md")
	testutil.RunGit(t, work, "commit", "-m", "initial")
	testutil.RunGit(t, work, "branch", "-M", "main")
	testutil.RunGit(t, work, "remote", "add", "origin", remote)
	testutil.RunGit(t, work, "push", "-u", "origin", "main")
	testutil.RunGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	dest := filepath.Join(root, "local")
	testutil.RunGit(t, root, "clone", remote, dest)

	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")
	m := &impl{rt: rt}

	_, err := m.pull(thread, starlark.NewBuiltin("git.pull", m.pull), starlark.Tuple{starlark.String(dest)}, nil)
	if err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	res, err := task.Actions[0].Apply(context.Background(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}
