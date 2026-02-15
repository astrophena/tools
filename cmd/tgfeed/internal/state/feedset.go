// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package state

import (
	"context"
	"sync"
	"time"
)

// FeedSet wraps all loaded feed state and persists mutations automatically.
//
// Callers should use Update for mutations at meaningful checkpoints (for
// example: fetch success, not-modified responses, fetch failures, and
// administrative actions) so state is durably written after each transition.
type FeedSet struct {
	mu    sync.RWMutex
	feeds map[string]*Feed
	store Store
}

// NewFeedSet builds a mutable state set from an existing snapshot.
func NewFeedSet(store Store, feeds map[string]*Feed) *FeedSet {
	if feeds == nil {
		feeds = map[string]*Feed{}
	}
	return &FeedSet{store: store, feeds: feeds}
}

// Get returns a copy of feed state for read-only use.
func (s *FeedSet) Get(url string) (*Feed, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fd, ok := s.feeds[url]
	if !ok {
		return nil, false
	}
	return fd.Clone(), true
}

// Update mutates one feed state and persists the full map when changed is true.
func (s *FeedSet) Update(ctx context.Context, url string, fn func(fd *Feed, exists bool) (changed bool, err error)) error {
	s.mu.Lock()
	fd, exists := s.feeds[url]
	if !exists {
		fd = NewFeed(time.Now())
		s.feeds[url] = fd
	}

	changed, err := fn(fd, exists)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !changed {
		s.mu.Unlock()
		return nil
	}

	snapshot := cloneFeedMap(s.feeds)
	s.mu.Unlock()

	return s.store.SaveState(ctx, snapshot)
}

// PruneMissing removes state entries for feeds that no longer exist and
// persists the result when any entry was removed.
func (s *FeedSet) PruneMissing(ctx context.Context, keep map[string]struct{}) error {
	s.mu.Lock()
	changed := false
	for url := range s.feeds {
		if _, ok := keep[url]; ok {
			continue
		}
		delete(s.feeds, url)
		changed = true
	}
	if !changed {
		s.mu.Unlock()
		return nil
	}
	snapshot := cloneFeedMap(s.feeds)
	s.mu.Unlock()

	return s.store.SaveState(ctx, snapshot)
}

// Save persists current state snapshot.
func (s *FeedSet) Save(ctx context.Context) error {
	s.mu.RLock()
	snapshot := cloneFeedMap(s.feeds)
	s.mu.RUnlock()
	return s.store.SaveState(ctx, snapshot)
}

// Snapshot returns a deep copy of all current feed state entries.
func (s *FeedSet) Snapshot() map[string]*Feed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFeedMap(s.feeds)
}

func cloneFeedMap(input map[string]*Feed) map[string]*Feed {
	out := make(map[string]*Feed, len(input))
	for k, v := range input {
		if v == nil {
			out[k] = nil
			continue
		}
		out[k] = v.Clone()
	}
	return out
}
