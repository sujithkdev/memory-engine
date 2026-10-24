package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/service"
)

// ReembedWorker processes the needs_reembedding queue.
// Runs every hour by default.
// Triggered automatically when embedding model changes via ActivateModel().
type ReembedWorker struct {
	embedSvc  *service.EmbeddingService
	batchSize int
	interval  time.Duration
	logger    zerolog.Logger
}

// NewReembedWorker creates a new ReembedWorker.
func NewReembedWorker(
	embedSvc *service.EmbeddingService,
	interval time.Duration,
	batchSize int,
	logger zerolog.Logger,
) *ReembedWorker {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	return &ReembedWorker{
		embedSvc:  embedSvc,
		batchSize: batchSize,
		interval:  interval,
		logger:    logger,
	}
}

func (w *ReembedWorker) Name() string            { return "re-embedding" }
func (w *ReembedWorker) Interval() time.Duration { return w.interval }

// Run processes the re-embedding queue in batches.
func (w *ReembedWorker) Run(ctx context.Context) error {
	count, err := w.embedSvc.ProcessReembeddingQueue(ctx, w.batchSize)
	if err != nil {
		return err
	}
	if count > 0 {
		w.logger.Info().Int("embedded", count).Msg("re-embedding worker complete")
	} else {
		w.logger.Debug().Msg("re-embedding worker: queue empty")
	}
	return nil
}
