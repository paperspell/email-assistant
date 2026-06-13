package workers

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBlockingWorker_Do_RunsSynchronously(t *testing.T) {
	w := NewSequentialWorker()
	var ran bool
	w.Do(context.Background(), func(_ context.Context) { ran = true })
	assert.True(t, ran)
}

func TestBlockingWorker_DoTry_AlwaysTrue(t *testing.T) {
	w := NewSequentialWorker()
	ok := w.DoTry(context.Background(), func(_ context.Context) {})
	assert.True(t, ok)
}

func TestBlockingWorker_Wait_ReturnsImmediately(t *testing.T) {
	w := NewSequentialWorker()
	done := make(chan struct{})
	go func() {
		w.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait did not return immediately")
	}
}

func TestUnlimitedWorker_Do_AllComplete(t *testing.T) {
	w := NewUnlimitedWorker()
	var count atomic.Int64
	for range 10 {
		w.Do(context.Background(), func(_ context.Context) {
			count.Add(1)
		})
	}
	w.Wait()
	assert.Equal(t, int64(10), count.Load())
}

func TestUnlimitedWorker_DoTry_AlwaysTrue(t *testing.T) {
	w := NewUnlimitedWorker()
	ok := w.DoTry(context.Background(), func(_ context.Context) {})
	w.Wait()
	assert.True(t, ok)
}

func TestUnlimitedWorker_Wait_BlocksUntilDone(t *testing.T) {
	w := NewUnlimitedWorker()
	var done atomic.Bool
	w.Do(context.Background(), func(_ context.Context) {
		time.Sleep(20 * time.Millisecond)
		done.Store(true)
	})
	w.Wait()
	assert.True(t, done.Load())
}

func TestTriable_DoTry_ReturnsFalseWhenFull(t *testing.T) {
	w := NewTriable(1)
	block := make(chan struct{})

	ok := w.DoTry(context.Background(), func(_ context.Context) {
		<-block // hold the slot
	})
	assert.True(t, ok)

	// Second attempt should fail — slot is occupied
	ok2 := w.DoTry(context.Background(), func(_ context.Context) {})
	assert.False(t, ok2)

	close(block)
	w.Wait()
}

func TestTriable_Do_RespectsLimit(t *testing.T) {
	const limit = 3
	w := NewTriable(limit)
	var peak atomic.Int64
	var current atomic.Int64

	for range 10 {
		w.Do(context.Background(), func(_ context.Context) {
			cur := current.Add(1)
			if cur > peak.Load() {
				peak.Store(cur)
			}
			time.Sleep(10 * time.Millisecond)
			current.Add(-1)
		})
	}
	w.Wait()
	assert.LessOrEqual(t, peak.Load(), int64(limit))
}
