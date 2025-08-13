// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
)

func TestRun(t *testing.T) {
	clitest.Run(t, func(t *testing.T) cli.App { return cli.AppFunc(run) }, map[string]clitest.Case[cli.App]{
		"no program": {
			WantErr: cli.ErrInvalidArgs,
		},
		"hello": {
			Args:         []string{"testdata/hello.go"},
			WantInStdout: "Hello, world!\n",
		},
		"args": {
			Args:         []string{"testdata/args.go", "foo", "bar"},
			WantInStdout: "foo bar\n",
		},
	})
}

func TestCache(t *testing.T) {
	tempDir := t.TempDir()
	userCacheDir = func() (string, error) {
		return tempDir, nil
	}
	t.Cleanup(func() { userCacheDir = os.UserCacheDir })

	prog := "testdata/hello.go"
	originalContent, err := os.ReadFile(prog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.WriteFile(prog, originalContent, 0o644) })

	var stdout, stderr bytes.Buffer
	ctx := cli.WithEnv(context.Background(), &cli.Env{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	// Run gox for the first time.
	if err := gox(ctx, prog, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Hello, world!\n"; got != want {
		t.Errorf("got stdout %q, want %q", got, want)
	}
	stdout.Reset()

	// Check that the binary is cached.
	cacheDir, err := getCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(prog)
	if err != nil {
		t.Fatal(err)
	}
	cachedBin := filepath.Join(cacheDir, hash)
	stat1, err := os.Stat(cachedBin)
	if err != nil {
		t.Fatal(err)
	}

	// Run gox again and check that the cached binary is used.
	if err := gox(ctx, prog, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Hello, world!\n"; got != want {
		t.Errorf("got stdout %q, want %q", got, want)
	}
	stdout.Reset()

	stat2, err := os.Stat(cachedBin)
	if err != nil {
		t.Fatal(err)
	}
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Errorf("cached binary was rebuilt, but it should not have been")
	}

	// Modify the program and check that it is rebuilt.
	if err := os.WriteFile(prog, append(originalContent, []byte("\n// comment")...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := gox(ctx, prog, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Hello, world!\n"; got != want {
		t.Errorf("got stdout %q, want %q", got, want)
	}
	stdout.Reset()

	newHash, err := hashFile(prog)
	if err != nil {
		t.Fatal(err)
	}
	if newHash == hash {
		t.Errorf("hash of the modified file is the same as the original")
	}
	newCachedBin := filepath.Join(cacheDir, newHash)
	if _, err := os.Stat(newCachedBin); err != nil {
		t.Errorf("new cached binary was not created")
	}
}

func hashFile(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
