// Package syncutil contains useful synchronization primitives.
package syncutil

import "sync"

// LimitedWaitGroup is a version of [sync.WaitGroup] that that limits the
// number of concurrently working goroutines by using a buffered channel
// as a semaphore.
type LimitedWaitGroup struct {
	wg      sync.WaitGroup
	workers chan struct{}
}

// NewLimitedWaitGroup returns a new LimitedWaitGroup that limits the number of
// concurrently working goroutines to limit.
func NewLimitedWaitGroup(limit int) *LimitedWaitGroup {
	return &LimitedWaitGroup{
		workers: make(chan struct{}, limit),
	}
}

// Add increments the counter of the LimitedWaitGroup by the specified delta.
// It blocks if the number of active goroutines reaches the concurrency limit.
func (lwg *LimitedWaitGroup) Add(delta int) {
	for i := 0; i < delta; i++ {
		lwg.workers <- struct{}{}
		lwg.wg.Add(1)
	}
}

// Done decrements the counter of the LimitedWaitGroup by one and releases a slot
// in the semaphore, allowing another goroutine to start.
func (lwg *LimitedWaitGroup) Done() {
	<-lwg.workers
	lwg.wg.Done()
}

// Wait blocks until the counter of the LimitedWaitGroup becomes zero.
func (lwg *LimitedWaitGroup) Wait() {
	lwg.wg.Wait()
}
