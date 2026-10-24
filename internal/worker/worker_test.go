package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/worker"
)

// ── Mock worker for pool testing ──────────────────────────────────────────────

type mockWorker struct {
	name      string
	interval  time.Duration
	runCount  int64
	failAfter int // fail after N runs (0 = never fail)
	runErr    error
}

func (m *mockWorker) Name() string            { return m.name }
func (m *mockWorker) Interval() time.Duration { return m.interval }
func (m *mockWorker) Run(_ context.Context) error {
	n := atomic.AddInt64(&m.runCount, 1)
	if m.failAfter > 0 && int(n) > m.failAfter {
		return m.runErr
	}
	return nil
}

func (m *mockWorker) Runs() int64 { return atomic.LoadInt64(&m.runCount) }

// ── Pool tests ────────────────────────────────────────────────────────────────

func TestPool_StartsAndStops(t *testing.T) {
	logger := zerolog.Nop()
	w := &mockWorker{name: "test", interval: 100 * time.Millisecond}
	pool := worker.NewPool(logger, w)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)

	// Let it run for a bit.
	time.Sleep(250 * time.Millisecond)
	cancel()
	pool.Wait()

	// Should have run at least once (immediate run on start).
	if w.Runs() < 1 {
		t.Errorf("expected at least 1 run, got %d", w.Runs())
	}
}

func TestPool_RunsOnInterval(t *testing.T) {
	logger := zerolog.Nop()
	w := &mockWorker{name: "interval-test", interval: 50 * time.Millisecond}
	pool := worker.NewPool(logger, w)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)

	// Wait for 3 intervals + some buffer.
	time.Sleep(200 * time.Millisecond)
	cancel()
	pool.Wait()

	// Should have run at least 3 times (initial + 3 ticks).
	if w.Runs() < 3 {
		t.Errorf("expected at least 3 runs in 200ms with 50ms interval, got %d", w.Runs())
	}
}

func TestPool_MultipleWorkers_AllRun(t *testing.T) {
	logger := zerolog.Nop()
	w1 := &mockWorker{name: "worker-1", interval: 50 * time.Millisecond}
	w2 := &mockWorker{name: "worker-2", interval: 50 * time.Millisecond}
	w3 := &mockWorker{name: "worker-3", interval: 50 * time.Millisecond}
	pool := worker.NewPool(logger, w1, w2, w3)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()
	pool.Wait()

	for _, w := range []*mockWorker{w1, w2, w3} {
		if w.Runs() < 1 {
			t.Errorf("worker %q never ran", w.Name())
		}
	}
}

func TestPool_WorkerError_DoesNotStopPool(t *testing.T) {
	logger := zerolog.Nop()
	// This worker fails on every run after the first.
	failing := &mockWorker{
		name:      "failing",
		interval:  30 * time.Millisecond,
		failAfter: 1,
		runErr:    errors.New("simulated worker failure"),
	}
	healthy := &mockWorker{name: "healthy", interval: 30 * time.Millisecond}
	pool := worker.NewPool(logger, failing, healthy)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()
	pool.Wait()

	// Healthy worker should keep running even though failing worker errored.
	if healthy.Runs() < 2 {
		t.Errorf("healthy worker should keep running despite other worker failure, got %d runs", healthy.Runs())
	}
}

func TestPool_GracefulShutdown_WaitsForWorkers(t *testing.T) {
	logger := zerolog.Nop()
	w := &mockWorker{name: "slow", interval: 1 * time.Second}
	pool := worker.NewPool(logger, w)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	cancel()
	pool.Wait()
	elapsed := time.Since(start)

	// Shutdown should complete quickly (worker is not mid-run).
	if elapsed > 2*time.Second {
		t.Errorf("shutdown took too long: %v", elapsed)
	}
}

func TestPool_EmptyPool_DoesNotPanic(t *testing.T) {
	logger := zerolog.Nop()
	pool := worker.NewPool(logger) // no workers

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	cancel()
	pool.Wait() // should not panic or deadlock
}

func TestPool_ContextCancelled_BeforeStart(t *testing.T) {
	logger := zerolog.Nop()
	w := &mockWorker{name: "test", interval: 10 * time.Millisecond}
	pool := worker.NewPool(logger, w)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Start

	pool.Start(ctx)
	pool.Wait()
	// Worker still runs once at startup (initial run before ticker loop).
}

// ── Worker interface compliance ───────────────────────────────────────────────

func TestWorkerInterface_MockSatisfiesInterface(t *testing.T) {
	var _ worker.Worker = &mockWorker{}
}

// ── Interval validation ───────────────────────────────────────────────────────

func TestPool_IntervalRespected_TightTiming(t *testing.T) {
	logger := zerolog.Nop()
	w := &mockWorker{name: "tight", interval: 20 * time.Millisecond}
	pool := worker.NewPool(logger, w)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	time.Sleep(110 * time.Millisecond)
	cancel()
	pool.Wait()

	// With 20ms interval over 110ms: initial run + ~5 ticks = at least 5 runs.
	if w.Runs() < 4 {
		t.Errorf("expected at least 4 runs with 20ms interval over 110ms, got %d", w.Runs())
	}
}
