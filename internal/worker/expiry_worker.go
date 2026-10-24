package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// ExpiryWorker soft-deletes memories past their delete_after timestamp.
// Also transitions ARCHIVED memories older than retentionDays to DELETED.
// Runs every hour by default.
type ExpiryWorker struct {
	memRepo      *repository.MemoryRepository
	batchSize    int
	interval     time.Duration
	logger       zerolog.Logger
}

// NewExpiryWorker creates a new ExpiryWorker.
func NewExpiryWorker(
	memRepo *repository.MemoryRepository,
	interval time.Duration,
	batchSize int,
	logger zerolog.Logger,
) *ExpiryWorker {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return &ExpiryWorker{
		memRepo:   memRepo,
		batchSize: batchSize,
		interval:  interval,
		logger:    logger,
	}
}

func (w *ExpiryWorker) Name() string            { return "expiry-cleanup" }
func (w *ExpiryWorker) Interval() time.Duration { return w.interval }

// Run finds and soft-deletes expired memories.
func (w *ExpiryWorker) Run(ctx context.Context) error {
	expired, err := w.memRepo.ListExpired(ctx, time.Now(), w.batchSize)
	if err != nil {
		return fmt.Errorf("listing expired memories: %w", err)
	}

	deleted := 0
	for _, mem := range expired {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reason := fmt.Sprintf("auto-expired: delete_after=%v", mem.DeleteAfter)
		_, err := w.memRepo.TransitionStatus(ctx, mem.ID, mem.UserID,
			domain.MemoryStatusDeleted, "system", reason,
		)
		if err != nil {
			w.logger.Warn().Err(err).Str("id", mem.ID.String()).Msg("expiry: delete failed")
			continue
		}
		deleted++
	}

	if deleted > 0 {
		w.logger.Info().Int("deleted", deleted).Msg("expiry worker complete")
	} else {
		w.logger.Debug().Msg("expiry worker: nothing to expire")
	}
	return nil
}
