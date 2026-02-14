// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package filelock provides non-blocking advisory file locks.
package filelock

import (
	"errors"
	"os"
	"syscall"
)

// ErrAlreadyLocked indicates the lock is currently held by another process.
var ErrAlreadyLocked = errors.New("already locked")

// Lock represents a held file lock.
type Lock interface{ Release() error }

type fileLock struct{ file *os.File }

// Acquire obtains a non-blocking exclusive lock for path and optionally writes payload.
func Acquire(path string, payload string) (Lock, error) {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := lockFile.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyLocked
		}
		return nil, err
	}
	if payload != "" {
		if err := lockFile.Truncate(0); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
		if _, err := lockFile.Seek(0, 0); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
		if _, err := lockFile.WriteString(payload); err != nil {
			_ = (&fileLock{file: lockFile}).Release()
			return nil, err
		}
	}
	return &fileLock{file: lockFile}, nil
}

// IsLocked reports whether path is currently locked by another process.
func IsLocked(path string) bool {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer lockFile.Close()

	err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		return false
	}

	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func (l *fileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		if closeErr := l.file.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return l.file.Close()
}
