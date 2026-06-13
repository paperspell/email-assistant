package workers

import (
	"context"
	"math"
	"sync"

	"golang.org/x/sync/semaphore"
)

// Triable is a Worker backed by a weighted semaphore that supports non-blocking attempts.
type Triable struct {
	sem *semaphore.Weighted
	wg  *sync.WaitGroup
}

// NewTriable creates a Triable worker with a concurrency limit of n.
// If n <= 0, the limit is set to MaxInt64 (effectively unlimited).
func NewTriable(n int64) *Triable {
	if n <= 0 {
		n = math.MaxInt64
	}
	return &Triable{
		sem: semaphore.NewWeighted(n),
		wg:  &sync.WaitGroup{},
	}
}

// Do acquires a slot (blocking) and runs fn in a new goroutine.
func (w Triable) Do(ctx context.Context, fn func(ctx context.Context)) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		if err := w.sem.Acquire(ctx, 1); err != nil {
			return
		}
		fn(ctx)
		w.sem.Release(1)
	}()
}

// DoTry attempts to acquire a slot without blocking.
// Returns false if no slot is available.
func (w Triable) DoTry(ctx context.Context, fn func(ctx context.Context)) bool {
	w.wg.Add(1)
	if !w.sem.TryAcquire(1) {
		w.wg.Done()
		return false
	}
	go func() {
		fn(ctx)
		w.sem.Release(1)
		w.wg.Done()
	}()
	return true
}

// Wait blocks until all running workloads complete.
func (w Triable) Wait() {
	w.wg.Wait()
}
