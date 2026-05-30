// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package env

import (
	"os"
	"path/filepath"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

func TestGet(t *testing.T) {
	rt := &boot.Runtime{
		Getenv: func(key string) string {
			if key == "FOO" {
				return "bar"
			}
			return ""
		},
	}

	m := Module()
	members := m.Members(rt)
	get, ok := members["get"].(*starlark.Builtin)
	if !ok {
		t.Fatal("env.get is not a builtin")
	}

	cases := map[string]struct {
		args    starlark.Tuple
		kwargs  []starlark.Tuple
		want    string
		wantErr string
	}{
		"exists": {
			args: starlark.Tuple{starlark.String("FOO")},
			want: "bar",
		},
		"missing": {
			args: starlark.Tuple{starlark.String("BAR")},
			want: "",
		},
		"missing with default": {
			args: starlark.Tuple{starlark.String("BAR"), starlark.String("baz")},
			want: "baz",
		},
		"missing with default kwarg": {
			args:   starlark.Tuple{starlark.String("BAR")},
			kwargs: []starlark.Tuple{{starlark.String("default"), starlark.String("qux")}},
			want:   "qux",
		},
		"too many args": {
			args:    starlark.Tuple{starlark.String("FOO"), starlark.String("bar"), starlark.String("baz")},
			wantErr: "env.get: got 3 arguments, want at most 2",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			thread := &starlark.Thread{Name: "test"}
			got, err := get.CallInternal(thread, tc.args, tc.kwargs)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("got error %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != `"`+tc.want+`"` {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHostname(t *testing.T) {
	m := Module()
	members := m.Members(&boot.Runtime{})
	hostname, ok := members["hostname"].(*starlark.Builtin)
	if !ok {
		t.Fatal("env.hostname is not a builtin")
	}

	got, err := hostname.CallInternal(&starlark.Thread{Name: "test"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == starlark.String("") {
		t.Fatal("env.hostname returned empty string")
	}
}

func TestLoadDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "environment.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10-base.conf"), []byte("BASE=$HOME/base\nQUOTED=\"$BASE/quoted\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/home/test")

	rt := &boot.Runtime{Root: root}
	members := Module().Members(rt)
	loadDir := members["load_dir"].(*starlark.Builtin)
	if _, err := loadDir.CallInternal(&starlark.Thread{Name: "test"}, starlark.Tuple{starlark.String("environment.d")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("BASE"); got != "/home/test/base" {
		t.Fatalf("BASE = %q, want /home/test/base", got)
	}
	if got := os.Getenv("QUOTED"); got != "/home/test/base/quoted" {
		t.Fatalf("QUOTED = %q, want /home/test/base/quoted", got)
	}
}
