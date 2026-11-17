// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package state

import (
	"maps"
	"time"
)

// Clone returns a deep copy of feed state.
func (f *Feed) Clone() *Feed {
	if f == nil {
		return nil
	}
	cp := *f
	cp.SeenItems = maps.Clone(f.SeenItems)
	cp.PendingItems = maps.Clone(f.PendingItems)
	return &cp
}

// MarkNotModified applies bookkeeping for a 304 response.
func (f *Feed) MarkNotModified(now time.Time) {
	f.LastUpdated = now
	f.ErrorCount = 0
	f.LastError = ""
}

// UpdateCacheHeaders persists response cache headers.
func (f *Feed) UpdateCacheHeaders(etag, lastModified string) {
	f.ETag = etag
	if lastModified != "" {
		f.LastModified = lastModified
	}
}

// MarkFetchSuccess applies bookkeeping for a successful fetch.
func (f *Feed) MarkFetchSuccess(now time.Time) {
	f.MarkNotModified(now)
	f.FetchCount++
}

// MarkFetchFailure applies bookkeeping for a failed fetch and reports whether
// this failure transitioned the feed into disabled state.
func (f *Feed) MarkFetchFailure(err error, threshold int) (disabled bool) {
	f.FetchFailCount++
	f.ErrorCount++
	f.LastError = err.Error()
	if threshold > 0 && f.ErrorCount >= threshold && !f.Disabled {
		f.Disabled = true
		f.DisabledNotifyPending = true
		return true
	}
	return false
}

// Reenable clears failure markers and enables future fetching.
func (f *Feed) Reenable() {
	f.Disabled = false
	f.DisabledNotifyPending = false
	f.ErrorCount = 0
	f.LastError = ""
}

// PrepareSeenItems initializes seen-items storage and drops stale entries.
//
// It reports whether the map was initialized and how many entries were pruned.
func (f *Feed) PrepareSeenItems(now time.Time, cleanupPeriod time.Duration) (justEnabled bool, pruned int) {
	if f.SeenItems == nil {
		f.SeenItems = make(map[string]time.Time)
		justEnabled = true
	}
	for guid, seenAt := range f.SeenItems {
		if now.Sub(seenAt) > cleanupPeriod {
			delete(f.SeenItems, guid)
			pruned++
		}
	}
	for guid, pendingAt := range f.PendingItems {
		if now.Sub(pendingAt) > cleanupPeriod {
			delete(f.PendingItems, guid)
			pruned++
		}
	}
	return justEnabled, pruned
}

// IsSeen reports whether guid was already processed.
func (f *Feed) IsSeen(guid string) bool {
	_, ok := f.SeenItems[guid]
	return ok
}

// MarkSeen records guid as processed at now.
func (f *Feed) MarkSeen(guid string, now time.Time) {
	if f.SeenItems == nil {
		f.SeenItems = make(map[string]time.Time)
	}
	f.SeenItems[guid] = now
}

// IsPending reports whether guid was accepted but not completely delivered.
func (f *Feed) IsPending(guid string) bool {
	_, ok := f.PendingItems[guid]
	return ok
}

// MarkPending records guid as awaiting successful delivery.
func (f *Feed) MarkPending(guid string, now time.Time) {
	if guid == "" {
		return
	}
	if f.PendingItems == nil {
		f.PendingItems = make(map[string]time.Time)
	}
	f.PendingItems[guid] = now
}

// CommitPending records guid as seen and removes its pending delivery marker.
func (f *Feed) CommitPending(guid string, now time.Time) {
	delete(f.PendingItems, guid)
	f.MarkSeen(guid, now)
}
