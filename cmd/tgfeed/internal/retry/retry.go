// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package retry reads retry delays from feed responses.
package retry

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Retryable reads a retry delay from a known feed source's error body.
func Retryable(host string, body []byte) (time.Duration, bool) {
	if host != "tg.i-c-a.su" {
		return 0, false
	}
	var response struct {
		Errors []any `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, false
	}

	for _, entry := range response.Errors {
		message, ok := entry.(string)
		if !ok {
			continue
		}
		if raw, ok := strings.CutPrefix(message, "FLOOD_WAIT_"); ok {
			if delay, ok := delaySeconds(raw); ok {
				return delay, true
			}
		}
		if raw, ok := strings.CutPrefix(message, "Time to unlock access: "); ok {
			if t, err := time.Parse(time.TimeOnly, raw); err == nil {
				seconds := t.Hour()*3600 + t.Minute()*60 + t.Second()
				return time.Duration(seconds) * time.Second, true
			}
		}
	}
	return 0, false
}

// RetryAfter parses an HTTP Retry-After value. A past date returns zero.
func RetryAfter(header string) (time.Duration, bool) {
	if delay, ok := delaySeconds(header); ok {
		return delay, true
	}
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

func delaySeconds(s string) (time.Duration, bool) {
	seconds, err := strconv.ParseInt(s, 10, 64)
	if err != nil || seconds < 0 || seconds > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}
