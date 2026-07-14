// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package fs

import (
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

func TestFSDir(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}
	dirPath := filepath.Join(root, "testdir")
	action := h.EmitOne("fs.dir", m.dir, starlark.Tuple{starlark.String(dirPath)}, nil)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory, got file")
	}

	action = h.EmitOne("fs.dir", m.dir, starlark.Tuple{starlark.String(dirPath)}, nil)
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestFSFile(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	filePath := filepath.Join(root, "testfile.txt")
	content := "hello world"
	mode := 0o600

	kwargs := []starlark.Tuple{
		{starlark.String("path"), starlark.String(filePath)},
		{starlark.String("content"), starlark.String(content)},
		{starlark.String("mode"), starlark.MakeInt(mode)},
	}

	action := h.EmitOne("fs.file", m.file, nil, kwargs)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}

	gotContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(gotContent) != content {
		t.Errorf("got content %q, want %q", gotContent, content)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(mode).Perm() {
		t.Errorf("got mode %o, want %o", info.Mode().Perm(), os.FileMode(mode).Perm())
	}

	action = h.EmitOne("fs.file", m.file, nil, kwargs)
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}

	newMode := 0o644
	kwargs[2] = starlark.Tuple{starlark.String("mode"), starlark.MakeInt(newMode)}
	action = h.EmitOne("fs.file", m.file, nil, kwargs)
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}

	info, err = os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(newMode).Perm() {
		t.Errorf("got mode %o, want %o", info.Mode().Perm(), os.FileMode(newMode).Perm())
	}
}

func TestFSTemplate(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	values := starlark.NewDict(1)
	if err := values.SetKey(starlark.String("name"), starlark.String("boot")); err != nil {
		t.Fatal(err)
	}
	action := h.EmitOne("fs.template", m.template, nil, []starlark.Tuple{
		{starlark.String("path"), starlark.String("out.txt")},
		{starlark.String("template"), starlark.String("hello {{name}}\n")},
		{starlark.String("values"), values},
	})
	if _, err := action.Apply(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello boot\n" {
		t.Fatalf("content = %q, want hello boot", got)
	}
}

func TestFSChmod(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}
	filePath := filepath.Join(root, "testfile.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := h.EmitOne("fs.chmod", m.chmod, nil, []starlark.Tuple{
		{starlark.String("path"), starlark.String(filePath)},
		{starlark.String("mode"), starlark.MakeInt(0o600)},
	})
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got mode %o, want 0600", info.Mode().Perm())
	}

	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestFSRemoveDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}
	linkPath := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "missing"), linkPath); err != nil {
		t.Fatal(err)
	}

	action := h.EmitOne("fs.remove", m.remove, starlark.Tuple{starlark.String(linkPath)}, nil)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}
	if _, err := os.Lstat(linkPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dangling symlink still exists: %v", err)
	}
}

func TestFSSymlink(t *testing.T) {
	root := t.TempDir()
	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}

	sourcePath := filepath.Join(root, "source.txt")
	targetPath := filepath.Join(root, "target.txt")

	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	kwargs := []starlark.Tuple{
		{starlark.String("source"), starlark.String(sourcePath)},
		{starlark.String("target"), starlark.String(targetPath)},
	}

	action := h.EmitOne("fs.symlink", m.symlink, nil, kwargs)
	res, err := action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Errorf("got result %v, want %v", res, boot.ResultChange)
	}

	link, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink failed: %v", err)
	}
	if link != sourcePath {
		t.Errorf("got link %q, want %q", link, sourcePath)
	}

	action = h.EmitOne("fs.symlink", m.symlink, nil, kwargs)
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}

func TestFSSyncTreeChangeCheck(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not found in PATH")
	}

	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := &boot.Runtime{Root: root}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt}
	action := h.EmitOne("fs.sync_tree", m.syncTree, nil, []starlark.Tuple{
		{starlark.String("source"), starlark.String(source)},
		{starlark.String("target"), starlark.String(target)},
	})
	res, err := action.Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Fatalf("dry-run before sync = %v, want %v", res, boot.ResultChange)
	}
	res, err = action.Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Fatalf("apply = %v, want %v", res, boot.ResultChange)
	}
	res, err = action.Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("dry run after sync failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Fatalf("dry-run after sync = %v, want %v", res, boot.ResultSkip)
	}

	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("goodbye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = action.Apply(t.Context(), true)
	if err != nil {
		t.Fatalf("dry run after source change failed: %v", err)
	}
	if res != boot.ResultChange {
		t.Fatalf("dry-run after source change = %v, want %v", res, boot.ResultChange)
	}
}
