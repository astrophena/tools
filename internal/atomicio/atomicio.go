// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package atomicio provides atomic file writing with backups.
package atomicio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	backupTimeFormat = "20060102150405.999999999"
	maxBackups       = 10
)

// WriteFile writes data to a file atomically. It creates a backup of the
// original file if it exists, and prunes old backups.
func WriteFile(name string, data []byte, perm fs.FileMode) (err error) {
	// Create a temporary file in the same directory to ensure that it's on the
	// same filesystem, which is a requirement for an atomic os.Rename.
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".tmp")
	if err != nil {
		return err
	}
	defer func() {
		// Clean up the temporary file if something goes wrong.
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()

	// Write data to the temporary file.
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// If the original file exists, create a backup.
	if _, err := os.Stat(name); err == nil {
		backupName := name + "." + time.Now().UTC().Format(backupTimeFormat) + ".bak"
		if err := os.Rename(name, backupName); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// Atomically move the temporary file to the final destination.
	if err := os.Rename(f.Name(), name); err != nil {
		return err
	}

	// Prune old backups.
	return pruneBackups(name)
}

func pruneBackups(name string) error {
	backups, err := filepath.Glob(name + ".*.bak")
	if err != nil {
		return err
	}

	if len(backups) <= maxBackups {
		return nil
	}

	slices.Sort(backups)

	for i := 0; i < len(backups)-maxBackups; i++ {
		if err := os.Remove(backups[i]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	return nil
}
