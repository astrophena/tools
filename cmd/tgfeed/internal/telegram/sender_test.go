// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
)

func TestSplitMessage(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want []string
	}{
		"short":             {in: "hello", want: []string{"hello"}},
		"exact":             {in: strings.Repeat("a", 4096), want: []string{strings.Repeat("a", 4096)}},
		"long (no newline)": {in: strings.Repeat("a", 4100), want: []string{strings.Repeat("a", 4096), "aaaa"}},
		"long (single line with spaces)": {
			in:   strings.Repeat("a", 3000) + " " + strings.Repeat("b", 1500),
			want: []string{strings.Repeat("a", 3000), strings.Repeat("b", 1500)},
		},
		"long (newline split)": {
			in:   strings.Repeat("a", 4000) + "\n" + strings.Repeat("b", 100),
			want: []string{strings.Repeat("a", 4000), strings.Repeat("b", 100)},
		},
		"multi-byte unicode": {
			in:   strings.Repeat("🙂", 4095) + "\n" + "🙂",
			want: []string{strings.Repeat("🙂", 4095), "🙂"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := splitMessage(tc.in)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestSplitMessageNewlineRich(t *testing.T) {
	t.Parallel()

	in := strings.Repeat("line\n", 900)
	got := splitMessage(in)
	if len(got) < 2 {
		t.Fatalf("want at least 2 chunks, got %d", len(got))
	}
	for i, chunk := range got {
		if strings.TrimSpace(chunk) == "" {
			t.Fatalf("chunk %d is empty or whitespace only", i)
		}
		if utf8.RuneCountInString(chunk) > 4096 {
			t.Fatalf("chunk %d exceeds rune cap: %d", i, utf8.RuneCountInString(chunk))
		}
	}

	joined := strings.Join(got, "\n")
	testutil.AssertEqual(t, joined, strings.TrimSpace(in))
}

func TestSendRateLimitRetry(t *testing.T) {
	t.Parallel()

	s := New(Config{ChatID: "chat", Token: "token"})
	var calls int
	s.makeRequest = func(context.Context, string, any) error {
		calls++
		if calls == 1 {
			return &request.StatusError{StatusCode: 429, Body: []byte(`{"parameters":{"retry_after":1}}`)}
		}
		return nil
	}
	var waits []time.Duration
	s.sleep = func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		return true
	}

	err := s.Send(t.Context(), sender.Message{Body: "hello"})
	testutil.AssertEqual(t, err, nil)
	testutil.AssertEqual(t, calls, 2)
	testutil.AssertEqual(t, waits, []time.Duration{time.Second})
}

func TestSendNonRetryableError(t *testing.T) {
	t.Parallel()

	s := New(Config{ChatID: "chat", Token: "token"})
	wantErr := errors.New("boom")
	s.makeRequest = func(context.Context, string, any) error { return wantErr }
	s.sleep = func(context.Context, time.Duration) bool {
		t.Fatal("sleep should not be called for non-retryable errors")
		return false
	}

	err := s.Send(t.Context(), sender.Message{Body: "hello"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Send() error = %v, want %v", err, wantErr)
	}
}

func TestIsRateLimited(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err      error
		retry    bool
		waitTime time.Duration
	}{
		"rate-limited": {
			err:      &request.StatusError{StatusCode: 429, Body: []byte(`{"parameters":{"retry_after":3}}`)},
			retry:    true,
			waitTime: 3 * time.Second,
		},
		"bad body": {
			err:   &request.StatusError{StatusCode: 429, Body: []byte(`oops`)},
			retry: false,
		},
		"other status": {
			err:   &request.StatusError{StatusCode: 500, Body: []byte(`{}`)},
			retry: false,
		},
		"other error": {
			err:   fmt.Errorf("network"),
			retry: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			retry, wait := isRateLimited(tc.err)
			testutil.AssertEqual(t, retry, tc.retry)
			testutil.AssertEqual(t, wait, tc.waitTime)
		})
	}
}

func TestSendInvalidTopic(t *testing.T) {
	t.Parallel()

	s := New(Config{ChatID: "chat", Token: "token"})
	err := s.Send(t.Context(), sender.Message{Body: "hello", Target: sender.Target{Topic: "not-a-number"}})
	if err == nil {
		t.Fatal("Send() error = nil, want non-nil")
	}
}
