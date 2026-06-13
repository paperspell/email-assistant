package workers

import (
	"context"
	"sync"
)

// Worker executes workloads and manages their lifecycle.
type Worker interface {
	Do(context.Context, func(context.Context))
	DoTry(context.Context, func(context.Context)) bool
	Wait()
}

// WorkerFactory is a function that creates a Worker instance.
type WorkerFactory func() Worker

// BlockingWorker executes workloads in the calling goroutine (sequential).
type BlockingWorker struct{}

// NewSequentialWorker creates a BlockingWorker.
func NewSequentialWorker() Worker {
	return BlockingWorker{}
}

// Do calls fn synchronously in the current goroutine.
func (BlockingWorker) Do(ctx context.Context, fn func(context.Context)) {
	fn(ctx)
}

// DoTry calls fn synchronously and always succeeds.
func (BlockingWorker) DoTry(ctx context.Context, fn func(context.Context)) bool {
	fn(ctx)
	return true
}

// Wait returns immediately.
func (BlockingWorker) Wait() {}

// UnlimitedWorker executes each workload in a new goroutine with no concurrency limit.
type UnlimitedWorker struct {
	wg sync.WaitGroup
}

// NewUnlimitedWorker creates an UnlimitedWorker.
func NewUnlimitedWorker() Worker {
	return &UnlimitedWorker{}
}

// Do launches fn in a new goroutine.
func (uw *UnlimitedWorker) Do(ctx context.Context, fn func(context.Context)) {
	uw.wg.Add(1)
	go func() {
		defer uw.wg.Done()
		fn(ctx)
	}()
}

// DoTry launches fn in a new goroutine and always succeeds.
func (uw *UnlimitedWorker) DoTry(ctx context.Context, fn func(context.Context)) bool {
	uw.Do(ctx, fn)
	return true
}

// Wait blocks until all launched goroutines complete.
func (uw *UnlimitedWorker) Wait() {
	uw.wg.Wait()
}
