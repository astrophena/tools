// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package retry

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func Retryable(host string, body []byte) (time.Duration, bool) {
	f, ok := handlers[host]
	if !ok {
		return 0, false
	}
	return f(body)
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
				d, err := time.ParseDuration(after + "s")
				if err == nil {
					return d, true
				}
			}

			const unlockPrefix = "Time to unlock access: "
			if after, ok := strings.CutPrefix(s, unlockPrefix); ok {
				parts := strings.Split(after, ":")
				if len(parts) != 3 {
					continue
				}
				h, err1 := strconv.Atoi(parts[0])
				m, err2 := strconv.Atoi(parts[1])
				sec, err3 := strconv.Atoi(parts[2])
				if err1 == nil && err2 == nil && err3 == nil {
					return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
				}
			}
		}

		return 0, false
	},
}
