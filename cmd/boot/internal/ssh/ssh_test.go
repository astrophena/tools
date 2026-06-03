// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package ssh

import (
	"os"
	"path/filepath"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"

	"go.starlark.net/starlark"
)

func TestSSHKeySkipsExistingKey(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "key")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt := &boot.Runtime{Root: root}
	task, thread := testutil.TaskThread("test")

	m := &impl{rt: rt}
	_, err := m.key(thread, starlark.NewBuiltin("ssh.key", m.key), nil, []starlark.Tuple{
		{starlark.String("path"), starlark.String(keyPath)},
	})
	if err != nil {
		t.Fatalf("ssh.key failed: %v", err)
	}
	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	res, err := task.Actions[0].Apply(t.Context(), false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res != boot.ResultSkip {
		t.Errorf("got result %v, want %v", res, boot.ResultSkip)
	}
}
