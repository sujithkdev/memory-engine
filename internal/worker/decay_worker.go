package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// DecayWorker computes and persists decayed salience values for ACTIVE memories.
// Runs every 24h by default.
// Formula: s(t) = max(floor, s0 × e^(−λ × days_since_access))
type DecayWorker struct {
	memRepo    *repository.MemoryRepository
	scorer     *scorer.Scorer
	batchSize  int
	interval   time.Duration
	logger     zerolog.Logger
}

// NewDecayWorker creates a new DecayWorker.
func NewDecayWorker(
	memRepo *repository.MemoryRepository,
	s *scorer.Scorer,
	interval time.Duration,
	batchSize int,
	logger zerolog.Logger,
) *DecayWorker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return &DecayWorker{
		memRepo:   memRepo,
		scorer:    s,
		batchSize: batchSize,
		interval:  interval,
		logger:    logger,
	}
}

func (w *DecayWorker) Name() string            { return "salience-decay" }
func (w *DecayWorker) Interval() time.Duration { return w.interval }

// Run processes a batch of memories, computing and persisting decayed salience.
func (w *DecayWorker) Run(ctx context.Context) error {
	// Find memories scored more than interval ago (i.e., stale salience).
	cutoff := time.Now().Add(-w.interval)
	mems, err := w.memRepo.ListForDecay(ctx, cutoff, w.batchSize)
	if err != nil {
		return err
	}
	if len(mems) == 0 {
		w.logger.Debug().Msg("decay worker: no memories need decay")
		return nil
	}

	now := time.Now()
	updated := 0
	staled := 0

	for _, mem := range mems {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		newSalience := w.scorer.DecaySalience(mem, now)
		if err := w.memRepo.UpdateScoredSalience(ctx, mem.ID, newSalience); err != nil {
			w.logger.Warn().Err(err).Str("id", mem.ID.String()).Msg("decay: update failed")
			continue
		}
		updated++

		// If salience has fallen to floor and memory hasn't been accessed
		// in 90+ days, mark it STALE for eventual archival.
		daysSince := now.Sub(mem.LastAccessed).Hours() / 24
		if daysSince > 90 && newSalience <= w.scorer.DecaySalience(mem, now)+0.001 {
			_, transErr := w.memRepo.TransitionStatus(ctx, mem.ID, mem.UserID,
				staleStatus, "system", "salience decayed to floor — auto-staled after 90 days inactivity",
			)
			if transErr == nil {
				staled++
			}
		}
	}

	w.logger.Info().
		Int("processed", len(mems)).
		Int("updated", updated).
		Int("staled", staled).
		Msg("decay worker complete")

	return nil
}
