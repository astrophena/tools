// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package llm

import (
	"errors"
	"io/fs"
	"time"

	"crawshaw.dev/jsonfile"
)

type usageStats struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type usageData struct {
	ByKey map[string]usageKeyStats `json:"by_key"`
}

type usageKeyStats struct {
	Total usageStats            `json:"total"`
	ByDay map[string]usageStats `json:"by_day"`
}

type usageStore struct {
	f *jsonfile.JSONFile[usageData]
}

func newUsageStore(path string) (*usageStore, error) {
	if path == "" {
		return nil, nil
	}

	f, err := jsonfile.Load[usageData](path)
	if err != nil {
		if !isNotExist(err) {
			return nil, err
		}
		f, err = jsonfile.New[usageData](path)
		if err != nil {
			return nil, err
		}
	}

	err = f.Write(func(d *usageData) error {
		if d.ByKey == nil {
			d.ByKey = make(map[string]usageKeyStats)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &usageStore{f: f}, nil
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func (s *usageStore) add(key string, now time.Time, input, output int64) error {
	if key == "" {
		return nil
	}
	day := now.UTC().Format(time.DateOnly)
	return s.f.Write(func(d *usageData) error {
		if d.ByKey == nil {
			d.ByKey = make(map[string]usageKeyStats)
		}
		v := d.ByKey[key]
		if v.ByDay == nil {
			v.ByDay = make(map[string]usageStats)
		}
		v.Total.InputTokens += input
		v.Total.OutputTokens += output
		daily := v.ByDay[day]
		daily.InputTokens += input
		daily.OutputTokens += output
		v.ByDay[day] = daily
		d.ByKey[key] = v
		return nil
	})
}

func (s *usageStore) get(key string, date string) usageStats {
	var out usageStats
	s.f.Read(func(d *usageData) {
		v, ok := d.ByKey[key]
		if !ok {
			return
		}
		if date == "" {
			out = v.Total
			return
		}
		out = v.ByDay[date]
	})
	return out
}
