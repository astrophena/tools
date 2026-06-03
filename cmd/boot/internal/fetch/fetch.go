// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark fetch module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "fetch" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"file": starlark.NewBuiltin("fetch.file", m.file),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) file(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}

	var (
		url      string
		path     string
		mode     int = 0o644
		checksum string
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"url", &url,
		"path", &path,
		"mode?", &mode,
		"checksum?", &checksum,
	); err != nil {
		return nil, err
	}
	abs := m.rt.ResolveTarget(path)
	checksum, err := normalizeChecksum(checksum)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	targetMode, err := boot.FileMode("mode", mode)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("fetch %s to %s", url, abs),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			ok, err := fileMatches(abs, checksum, targetMode)
			if err != nil {
				return "", err
			}
			if ok {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := download(ctx, url, abs, targetMode); err != nil {
				return "", err
			}
			if checksum != "" {
				ok, err := fileMatches(abs, checksum, targetMode)
				if err != nil {
					return "", err
				}
				if !ok {
					return "", fmt.Errorf("checksum mismatch for %s", abs)
				}
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func normalizeChecksum(checksum string) (string, error) {
	checksum = strings.TrimPrefix(strings.TrimSpace(checksum), "sha256:")
	if checksum == "" {
		return "", nil
	}
	if len(checksum) != sha256.Size*2 {
		return "", fmt.Errorf("checksum must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return "", fmt.Errorf("checksum must be a SHA-256 hex digest")
	}
	return strings.ToLower(checksum), nil
}

func fileMatches(path, checksum string, mode os.FileMode) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode().Perm() != mode.Perm() {
		return false, nil
	}
	if checksum == "" {
		return true, nil
	}
	got, err := fileSHA256(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return got == checksum, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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

func download(ctx context.Context, url, path string, mode os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
