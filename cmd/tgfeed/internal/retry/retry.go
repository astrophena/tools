// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package retry implements logic for parsing feed source errors to determine
// whether a request should be retried later, and if so, what backoff to use.
package retry

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Retryable analyzes the response body from the given host and returns
// the duration to wait before retrying, and a boolean indicating whether
// the request should be retried at all.
func Retryable(host string, body []byte) (time.Duration, bool) {
	f, ok := handlers[host]
	if !ok {
		return 0, false
	}
	return f(body)
}

// RetryAfter parses the standard HTTP Retry-After header and returns the duration
// to wait before retrying, and a boolean indicating whether the header was valid.
func RetryAfter(header string) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}

	// Try parsing as delay-seconds.
	if delay, err := strconv.Atoi(header); err == nil && delay >= 0 {
		return time.Duration(delay) * time.Second, true
	}

	// Try parsing as HTTP-date.
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		if d := time.Until(t); d >= 0 {
			return d, true
		}
		return 0, true // Past date means retry immediately.
	}

	return 0, false
}

var handlers = map[string]func([]byte) (time.Duration, bool){
	"tg.i-c-a.su": func(body []byte) (time.Duration, bool) {
		var response struct {
			Errors []any `json:"errors"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return 0, false
		}

		for _, e := range response.Errors {
			s, ok := e.(string)
			if !ok {
				continue
			}

			const floodPrefix = "FLOOD_WAIT_"
			if after, ok := strings.CutPrefix(s, floodPrefix); ok {
				d, err := strconv.Atoi(after)
				if err == nil {
					return time.Duration(d) * time.Second, true
				}
			}

			const unlockPrefix = "Time to unlock access: "
			if after, ok := strings.CutPrefix(s, unlockPrefix); ok {
				t, err := time.Parse(time.TimeOnly, after)
				if err == nil {
					return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second, true
				}
			}
		}

		return 0, false
	},
}
