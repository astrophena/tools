// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package syncx

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestProtected(t *testing.T) {
	t.Parallel()

	t.Run("read access", func(t *testing.T) {
		p := Protect(42)
		var result int
		p.RAccess(func(val int) {
			result = val
		})
		testutil.AssertEqual(t, result, 42)
	})

	t.Run("write access", func(t *testing.T) {
		var i int
		p := Protect(&i)
		p.Access(func(val *int) {
			*val = 43 // Modify the value.
		})
		var result int
		p.RAccess(func(val *int) { result = *val }) // Verify change.
		testutil.AssertEqual(t, result, 43)
	})

	t.Run("concurrent access", func(t *testing.T) {
		var i int
		p := Protect(&i)
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.Access(func(val *int) {
					*val += 1
				})
			}()
		}
		wg.Wait()

		var result int
		p.RAccess(func(val *int) { result = *val })
		testutil.AssertEqual(t, result, 100)
	})
}

func TestLazy(t *testing.T) {
	t.Parallel()

	var l Lazy[int]
	var count int
	var mu sync.Mutex

	f := func() int {
		mu.Lock()
		defer mu.Unlock()
		count++
		return count
	}

	v1 := l.Get(f)
	testutil.AssertEqual(t, v1, 1)

	v2 := l.Get(f)
	testutil.AssertEqual(t, v2, 1)

	testutil.AssertEqual(t, count, 1)

	var l2 Lazy[string]

	f2 := func() (string, error) {
		return "", errors.New("something went wrong")
	}

	notnil := func(err error) {
		if err == nil {
			t.Fatalf("err must not be nil")
		}
	}

	ev1, err := l2.GetErr(f2)
	testutil.AssertEqual(t, ev1, "")
	notnil(err)

	ev2, err := l2.GetErr(f2)
	testutil.AssertEqual(t, ev2, "")
	notnil(err)
}

func TestLimitedWaitGroup(t *testing.T) {
	t.Parallel()

	const concurrency = 5

	t.Run("add and wait", func(t *testing.T) {
		lwg := NewLimitedWaitGroup(concurrency)
		for range 10 {
			lwg.Add(1)
			go func() {
				defer lwg.Done()
				// Simulate some work.
				time.Sleep(100 * time.Millisecond)
			}()
		}
		lwg.Wait()
	})

	t.Run("done", func(t *testing.T) {
		lwg := NewLimitedWaitGroup(concurrency)
		var wg sync.WaitGroup
		for range 10 {
			lwg.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer lwg.Done()
				// Simulate some work.
				time.Sleep(100 * time.Millisecond)
			}()
		}
		wg.Wait()
		lwg.Wait()
	})

	t.Run("limits concurrency", func(t *testing.T) {
		lwg := NewLimitedWaitGroup(concurrency)
		var running int32
		var maxConcurrent int32

		for range 20 {
			lwg.Add(1)
			go func() {
				defer lwg.Done()
				// Simulate some work.
				atomic.AddInt32(&running, 1)
				defer atomic.AddInt32(&running, -1)
				for {
					current := atomic.LoadInt32(&running)
					if current > atomic.LoadInt32(&maxConcurrent) {
						atomic.StoreInt32(&maxConcurrent, current)
					}
					if current <= int32(concurrency) {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				time.Sleep(100 * time.Millisecond)
			}()
		}
		lwg.Wait()

		testutil.AssertEqual(t, int(maxConcurrent), concurrency)
	})
}
