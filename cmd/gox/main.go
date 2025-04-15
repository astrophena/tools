// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.astrophena.name/base/cli"
)

func main() { cli.Main(cli.AppFunc(run)) }

func run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: missing required argument 'program'", cli.ErrInvalidArgs)
	}

	var (
		prog = env.Args[0]
		args = env.Args[1:]
	)

	cacheDir, err := getCacheDir()
	if err != nil {
		return err
	}

	// Calculate the hash of the program file.
	f, err := os.Open(prog)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))

	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}

	binPath := filepath.Join(cacheDir, sum)
	if _, err := os.Stat(binPath); errors.Is(err, fs.ErrNotExist) {
		if err := run("go", "build", "-o", binPath, prog); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return run(binPath, args...)
}

func getCacheDir() (string, error) {
	goVersionOut, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", err
	}
	var goVersion string
	if fields := strings.Fields(string(goVersionOut)); len(fields) >= 3 {
		goVersion = fields[2]
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gox", goVersion), nil
}
