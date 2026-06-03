// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"os"
	"path/filepath"
	"testing"

	"go.starlark.net/starlark"
)

func TestHostBuiltin(t *testing.T) {
	home := t.TempDir()
	if err := writeTermuxHostname(home, "recipe-host\n"); err != nil {
		t.Fatal(err)
	}
	engine := &Engine{Runtime: &Runtime{
		Root:        "/recipe",
		Home:        home,
		Interactive: true,
		Getenv: func(key string) string {
			if key == "TERMUX_VERSION" {
				return "0.118.0"
			}
			return ""
		},
	}}

	got, err := engine.host(nil, starlark.NewBuiltin("host", engine.host), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	attrs, ok := got.(starlark.HasAttrs)
	if !ok {
		t.Fatalf("host() returned %T, want value with attributes", got)
	}
	for _, field := range []struct {
		name string
		want starlark.Value
	}{
		{name: "home", want: starlark.String(home)},
		{name: "hostname", want: starlark.String("recipe-host")},
		{name: "interactive", want: starlark.True},
		{name: "needs_sudo", want: starlark.False},
		{name: "root", want: starlark.String("/recipe")},
	} {
		attr, err := attrs.Attr(field.name)
		if err != nil {
			t.Fatalf("get %s: %v", field.name, err)
		}
		if attr != field.want {
			t.Fatalf("%s = %s, want %s", field.name, attr, field.want)
		}
	}
}

func TestHostBuiltinRejectsArguments(t *testing.T) {
	engine := &Engine{Runtime: &Runtime{}}
	_, err := engine.host(nil, starlark.NewBuiltin("host", engine.host), starlark.Tuple{starlark.String("unexpected")}, nil)
	if err == nil || err.Error() != "host: got 1 arguments, want at most 0" {
		t.Fatalf("error = %v, want unexpected arguments error", err)
	}
}

func TestHostBuiltinRequiresRuntime(t *testing.T) {
	engine := &Engine{}
	_, err := engine.host(nil, starlark.NewBuiltin("host", engine.host), nil, nil)
	if err == nil || err.Error() != "host: runtime is not configured" {
		t.Fatalf("error = %v, want runtime error", err)
	}
}

func writeTermuxHostname(home, content string) error {
	path := filepath.Join(home, "local", "data", "termux")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "hostname"), []byte(content), 0o644)
}
