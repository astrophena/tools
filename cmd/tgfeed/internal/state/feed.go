// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package state

import "time"

// Clone returns a deep copy of feed state.
func (f *Feed) Clone() *Feed {
	if f == nil {
		return nil
	}
	cp := *f
	if f.SeenItems != nil {
		cp.SeenItems = make(map[string]time.Time, len(f.SeenItems))
		for k, v := range f.SeenItems {
			cp.SeenItems[k] = v
		}
	}
	return &cp
}

// IsDisabled reports whether fetching is disabled for this feed.
func (f *Feed) IsDisabled() bool { return f.Disabled }

// CacheHeaders returns conditional request values to use for the next fetch.
func (f *Feed) CacheHeaders() (etag string, lastModified string) {
	return f.ETag, f.LastModified
}

// MarkNotModified applies bookkeeping for a 304 response.
func (f *Feed) MarkNotModified(now time.Time) {
	f.LastUpdated = now
	f.ErrorCount = 0
	f.LastError = ""
}

// UpdateCacheHeaders persists response cache headers.
func (f *Feed) UpdateCacheHeaders(etag string, lastModified string) {
	f.ETag = etag
	if lastModified != "" {
		f.LastModified = lastModified
	}
}

// MarkFetchSuccess applies bookkeeping for a successful fetch.
func (f *Feed) MarkFetchSuccess(now time.Time) {
	f.LastUpdated = now
	f.ErrorCount = 0
	f.LastError = ""
	f.FetchCount += 1
}

// MarkFetchFailure applies bookkeeping for a failed fetch and reports whether
// this failure transitioned the feed into disabled state.
func (f *Feed) MarkFetchFailure(err error, threshold int) (disabled bool) {
	f.FetchFailCount += 1
	f.ErrorCount += 1
	f.LastError = err.Error()
	if threshold > 0 && f.ErrorCount >= threshold && !f.Disabled {
		f.Disabled = true
		return true
	}
	return false
}

// Reenable clears failure markers and enables future fetching.
func (f *Feed) Reenable() {
	f.Disabled = false
	f.ErrorCount = 0
	f.LastError = ""
}

// PrepareSeenItems initializes seen-items storage and drops stale entries.
func (f *Feed) PrepareSeenItems(now time.Time, cleanupPeriod time.Duration) (justEnabled bool) {
	if f.SeenItems == nil {
		f.SeenItems = make(map[string]time.Time)
		justEnabled = true
	}
	for guid, seenAt := range f.SeenItems {
		if now.Sub(seenAt) > cleanupPeriod {
			delete(f.SeenItems, guid)
		}
	}
	return justEnabled
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
