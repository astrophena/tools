// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"crawshaw.dev/jsonfile"
)

// JSONFile is a file-backed implementation of the [Store] interface.
type JSONFile struct {
	path        string
	metricsPath string
	f           *jsonfile.JSONFile[jsonStore]
	ttl         time.Duration
	persisted   persistedStats

	gets           atomic.Uint64
	getHits        atomic.Uint64
	getMisses      atomic.Uint64
	expired        atomic.Uint64
	sets           atomic.Uint64
	rewrites       atomic.Uint64
	rewriteBytes   atomic.Uint64
	cleanupDeletes atomic.Uint64
}

// JSONFileStats describes file-backed cache activity since process start.
type JSONFileStats struct {
	Path                string
	MetricsPath         string
	TTL                 time.Duration
	Gets                uint64
	GetHits             uint64
	GetMisses           uint64
	Expired             uint64
	Sets                uint64
	Rewrites            uint64
	RewriteBytes        uint64
	CleanupDeletes      uint64
	TotalGets           uint64
	TotalGetHits        uint64
	TotalGetMisses      uint64
	TotalExpired        uint64
	TotalSets           uint64
	TotalRewrites       uint64
	TotalRewriteBytes   uint64
	TotalCleanupDeletes uint64
	FileSizeBytes       int64
}

type jsonStore struct {
	Data map[string]entry `json:"data"`
}

type entry struct {
	Value        []byte    `json:"value"`
	LastAccessed time.Time `json:"last_accessed"`
}

type persistedStats struct {
	Gets           uint64 `json:"gets"`
	GetHits        uint64 `json:"get_hits"`
	GetMisses      uint64 `json:"get_misses"`
	Expired        uint64 `json:"expired"`
	Sets           uint64 `json:"sets"`
	Rewrites       uint64 `json:"rewrites"`
	RewriteBytes   uint64 `json:"rewrite_bytes"`
	CleanupDeletes uint64 `json:"cleanup_deletes"`
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
		path:        path,
		metricsPath: path + ".metrics.json",
		f:           f,
		ttl:         ttl,
	}
	if persisted, err := loadPersistedStats(s.metricsPath); err == nil {
		s.persisted = persisted
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
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
	var deleted uint64
	err := s.f.Write(func(js *jsonStore) error {
		for key, e := range js.Data {
			if time.Since(e.LastAccessed) > s.ttl {
				delete(js.Data, key)
				deleted++
			}
		}
		return nil
	})
	if err == nil && deleted > 0 {
		s.cleanupDeletes.Add(deleted)
		s.recordRewrite()
	}
}

// Get retrieves a value for a given key.
func (s *JSONFile) Get(_ context.Context, key string) ([]byte, error) {
	s.gets.Add(1)
	var val []byte
	var (
		hit     bool
		mutated bool
	)
	err := s.f.Write(func(js *jsonStore) error {
		e, ok := js.Data[key]
		if !ok {
			return nil
		}

		hit = true
		if time.Since(e.LastAccessed) > s.ttl {
			delete(js.Data, key)
			mutated = true
			return nil
		}

		e.LastAccessed = time.Now()
		js.Data[key] = e
		mutated = true
		val = e.Value
		return nil
	})
	if hit {
		s.getHits.Add(1)
	} else {
		s.getMisses.Add(1)
	}
	if err == nil && hit && val == nil {
		s.expired.Add(1)
	}
	if err == nil && mutated {
		s.recordRewrite()
	}
	return val, err
}

// Set stores a value for a given key.
func (s *JSONFile) Set(_ context.Context, key string, val []byte) error {
	s.sets.Add(1)
	err := s.f.Write(func(js *jsonStore) error {
		if js.Data == nil {
			js.Data = make(map[string]entry)
		}
		js.Data[key] = entry{
			Value:        val,
			LastAccessed: time.Now(),
		}
		return nil
	})
	if err == nil {
		s.recordRewrite()
	}
	return err
}

// Close closes the file store.
func (s *JSONFile) Close() error {
	return writePersistedStats(s.metricsPath, s.persisted.add(s.sessionStats()))
}

// Stats returns cumulative file-store metrics since process start.
func (s *JSONFile) Stats() JSONFileStats {
	session := s.sessionStats()
	totals := s.persisted.add(session)
	stats := JSONFileStats{
		Path:                s.path,
		MetricsPath:         s.metricsPath,
		TTL:                 s.ttl,
		Gets:                session.Gets,
		GetHits:             session.GetHits,
		GetMisses:           session.GetMisses,
		Expired:             session.Expired,
		Sets:                session.Sets,
		Rewrites:            session.Rewrites,
		RewriteBytes:        session.RewriteBytes,
		CleanupDeletes:      session.CleanupDeletes,
		TotalGets:           totals.Gets,
		TotalGetHits:        totals.GetHits,
		TotalGetMisses:      totals.GetMisses,
		TotalExpired:        totals.Expired,
		TotalSets:           totals.Sets,
		TotalRewrites:       totals.Rewrites,
		TotalRewriteBytes:   totals.RewriteBytes,
		TotalCleanupDeletes: totals.CleanupDeletes,
	}
	if info, err := os.Stat(stats.Path); err == nil {
		stats.FileSizeBytes = info.Size()
	}
	return stats
}

func (s *JSONFile) recordRewrite() {
	s.rewrites.Add(1)
	if info, err := os.Stat(s.path); err == nil {
		s.rewriteBytes.Add(uint64(info.Size()))
	}
}

func (s *JSONFile) sessionStats() persistedStats {
	return persistedStats{
		Gets:           s.gets.Load(),
		GetHits:        s.getHits.Load(),
		GetMisses:      s.getMisses.Load(),
		Expired:        s.expired.Load(),
		Sets:           s.sets.Load(),
		Rewrites:       s.rewrites.Load(),
		RewriteBytes:   s.rewriteBytes.Load(),
		CleanupDeletes: s.cleanupDeletes.Load(),
	}
}

func (p persistedStats) add(other persistedStats) persistedStats {
	return persistedStats{
		Gets:           p.Gets + other.Gets,
		GetHits:        p.GetHits + other.GetHits,
		GetMisses:      p.GetMisses + other.GetMisses,
		Expired:        p.Expired + other.Expired,
		Sets:           p.Sets + other.Sets,
		Rewrites:       p.Rewrites + other.Rewrites,
		RewriteBytes:   p.RewriteBytes + other.RewriteBytes,
		CleanupDeletes: p.CleanupDeletes + other.CleanupDeletes,
	}
}

func loadPersistedStats(path string) (persistedStats, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return persistedStats{}, err
	}
	var stats persistedStats
	if err := json.Unmarshal(b, &stats); err != nil {
		return persistedStats{}, err
	}
	return stats, nil
}

func writePersistedStats(path string, stats persistedStats) error {
	b, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err1 := f.Close(); err == nil && err1 != nil {
		err = err1
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
