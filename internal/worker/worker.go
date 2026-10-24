// Package worker implements background workers that maintain memory health.
//
// Workers run on a schedule and perform:
//   - Salience decay         — reduces salience of unaccessed memories
//   - Re-embedding           — processes the needs_reembedding queue
//   - Stale detection        — marks old memories as STALE
//   - Expiry cleanup         — deletes memories past their retention/delete_after
//   - Access frequency decay — decays access counts over time
package worker

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Worker is a background job that runs on a schedule.
type Worker interface {
	// Name returns a human-readable identifier.
	Name() string
	// Run executes the worker job. Context is cancelled on shutdown.
	Run(ctx context.Context) error
	// Interval returns how often this worker should run.
	Interval() time.Duration
}

// Pool manages a set of workers, running each on its own schedule.
// Supports graceful shutdown.
type Pool struct {
	workers []Worker
	logger  zerolog.Logger
	wg      sync.WaitGroup
}

// NewPool creates a new worker Pool.
func NewPool(logger zerolog.Logger, workers ...Worker) *Pool {
	return &Pool{
		workers: workers,
		logger:  logger,
	}
}

// Start launches all workers in separate goroutines.
// Returns immediately — workers run in the background.
func (p *Pool) Start(ctx context.Context) {
	for _, w := range p.workers {
		p.wg.Add(1)
		go p.runWorker(ctx, w)
	}
	p.logger.Info().
		Int("workers", len(p.workers)).
		Msg("worker pool started")
}

// Wait blocks until all workers have stopped (after context cancellation).
func (p *Pool) Wait() {
	p.wg.Wait()
	p.logger.Info().Msg("worker pool stopped")
}

// runWorker runs a single worker on its configured interval.
func (p *Pool) runWorker(ctx context.Context, w Worker) {
	defer p.wg.Done()

	log := p.logger.With().Str("worker", w.Name()).Logger()
	log.Info().Dur("interval", w.Interval()).Msg("worker started")

	// Run once immediately at startup.
	p.execute(ctx, w, log)

	ticker := time.NewTicker(w.Interval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("worker shutting down")
			return
		case <-ticker.C:
			p.execute(ctx, w, log)
		}
	}
}

// execute runs the worker job with error handling and timing.
func (p *Pool) execute(ctx context.Context, w Worker, log zerolog.Logger) {
	start := time.Now()
	if err := w.Run(ctx); err != nil {
		log.Error().Err(err).Msg("worker run failed")
	} else {
		log.Debug().Dur("duration", time.Since(start)).Msg("worker run complete")
	}
}
