package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// ConsolidationWorker promotes memories up the abstraction hierarchy.
//
// Hierarchy: RAW → EXTRACTED → CONSOLIDATED → CORE
//
// Phase 9: RAW memories that are ACTIVE, older than 7 days, and
// importance > 0.7 are promoted to EXTRACTED level.
// Full LLM-based consolidation is implemented in Phase 11.
type ConsolidationWorker struct {
	memRepo   *repository.MemoryRepository
	batchSize int
	interval  time.Duration
	logger    zerolog.Logger
}

// NewConsolidationWorker creates a new ConsolidationWorker.
func NewConsolidationWorker(
	memRepo *repository.MemoryRepository,
	interval time.Duration,
	batchSize int,
	logger zerolog.Logger,
) *ConsolidationWorker {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	return &ConsolidationWorker{
		memRepo:   memRepo,
		batchSize: batchSize,
		interval:  interval,
		logger:    logger,
	}
}

func (w *ConsolidationWorker) Name() string            { return "consolidation" }
func (w *ConsolidationWorker) Interval() time.Duration { return w.interval }

// Run promotes eligible RAW memories to EXTRACTED level.
func (w *ConsolidationWorker) Run(ctx context.Context) error {
	mems, err := w.memRepo.ListForConsolidation(ctx, w.batchSize)
	if err != nil {
		return fmt.Errorf("listing consolidation candidates: %w", err)
	}

	promoted := 0
	for _, mem := range mems {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := w.memRepo.SetMemoryLevel(ctx, mem.ID, domain.MemoryLevelExtracted); err != nil {
			w.logger.Warn().Err(err).Str("id", mem.ID.String()).Msg("consolidation: set level failed")
			continue
		}
		promoted++
	}

	if promoted > 0 {
		w.logger.Info().Int("promoted", promoted).Msg("consolidation worker complete")
	} else {
		w.logger.Debug().Msg("consolidation worker: nothing to promote")
	}
	return nil
}
