// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package syncx contains useful synchronization primitives.
package syncx

import "sync"

// Protect wraps T into [Protected].
func Protect[T any](val T) *Protected[T] { return &Protected[T]{val: val} }

// Protected provides synchronized access to a value of type T.
type Protected[T any] struct {
	mu  sync.RWMutex
	val T
}

// RAccess provides read access to the protected value.
// It executes the provided function f with the value under a read lock.
func (p *Protected[T]) RAccess(f func(T)) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	f(p.val)
}

// Access provides write access to the protected value.
// It executes the provided function f with the value under a write lock.
func (p *Protected[T]) Access(f func(T)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p.val)
}

// Lazy represents a lazily computed value.
type Lazy[T any] struct {
	once sync.Once
	val  T
	err  error
}

// Get returns T, calling f to compute it, if necessary.
func (l *Lazy[T]) Get(f func() T) T {
	l.once.Do(func() { l.val = f() })
	return l.val
}

// GetErr returns T and an error, calling f to compute them, if necessary.
func (l *Lazy[T]) GetErr(f func() (T, error)) (T, error) {
	l.once.Do(func() { l.val, l.err = f() })
	return l.val, l.err
}

// LimitedWaitGroup is a version of [sync.WaitGroup] that limits the
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
	for range delta {
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
func (lwg *LimitedWaitGroup) Wait() { lwg.wg.Wait() }
