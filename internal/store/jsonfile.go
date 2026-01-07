// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"crawshaw.dev/jsonfile"
)

// JSONFile is a file-backed implementation of the [Store] interface.
type JSONFile struct {
	f   *jsonfile.JSONFile[jsonStore]
	ttl time.Duration
}

type jsonStore struct {
	Data map[string]entry `json:"data"`
}

type entry struct {
	Value        []byte    `json:"value"`
	LastAccessed time.Time `json:"last_accessed"`
}

// NewJSONFile creates a new [JSONFile] backed by the file at path with the given TTL.
func NewJSONFile(ctx context.Context, path string, ttl time.Duration) (*JSONFile, error) {
	f, err := jsonfile.Load[jsonStore](path)
	if errors.Is(err, fs.ErrNotExist) {
		f, err = jsonfile.New[jsonStore](path)
		if err == nil {
			if err := f.Write(func(js *jsonStore) error {
				js.Data = make(map[string]entry)
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	if err != nil {
		return nil, err
	}

	s := &JSONFile{
		f:   f,
		ttl: ttl,
	}
	s.cleanup(ctx, true)
	go s.cleanup(ctx, false)

	return s, nil
}

func (s *JSONFile) cleanup(ctx context.Context, firstRun bool) {
	if firstRun {
		s.performCleanup()
		return
	}

	sleepDuration := min(s.ttl/2, 24*time.Hour)

	for {
		select {
		case <-time.After(sleepDuration):
			s.performCleanup()
		case <-ctx.Done():
			return
		}
	}
}

func (s *JSONFile) performCleanup() {
	s.f.Write(func(js *jsonStore) error {
		for key, e := range js.Data {
			if time.Since(e.LastAccessed) > s.ttl {
				delete(js.Data, key)
			}
		}
		return nil
	})
}

// Get retrieves a value for a given key.
func (s *JSONFile) Get(_ context.Context, key string) ([]byte, error) {
	var val []byte
	err := s.f.Write(func(js *jsonStore) error {
		e, ok := js.Data[key]
		if !ok {
			return nil
		}

		if time.Since(e.LastAccessed) > s.ttl {
			delete(js.Data, key)
			return nil
		}

		e.LastAccessed = time.Now()
		js.Data[key] = e
		val = e.Value
		return nil
	})
	return val, err
}

// Set stores a value for a given key.
func (s *JSONFile) Set(_ context.Context, key string, val []byte) error {
	return s.f.Write(func(js *jsonStore) error {
		if js.Data == nil {
			js.Data = make(map[string]entry)
		}
		js.Data[key] = entry{
			Value:        val,
			LastAccessed: time.Now(),
		}
		return nil
	})
}

// Close closes the file store.
func (s *JSONFile) Close() error { return nil }
