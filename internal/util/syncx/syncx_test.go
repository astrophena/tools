package syncx

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

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
