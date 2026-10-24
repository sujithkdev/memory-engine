package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// EmbeddingRepository handles memory_embeddings persistence and vector search.
type EmbeddingRepository struct {
	db *pgxpool.Pool
}

// NewEmbeddingRepository creates a new EmbeddingRepository.
func NewEmbeddingRepository(db *pgxpool.Pool) *EmbeddingRepository {
	return &EmbeddingRepository{db: db}
}

// Upsert stores or updates an embedding for a memory.
// Also clears the needs_reembedding flag on the memory.
func (r *EmbeddingRepository) Upsert(
	ctx context.Context,
	memoryID uuid.UUID,
	vector []float32,
	model, version string,
	dimensions int,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Convert []float32 to pgvector string format: "[0.1,0.2,...]"
	vecStr := float32SliceToString(vector)

	_, err = tx.Exec(ctx, `
		INSERT INTO memory_embeddings
			(memory_id, embedding, embedding_model, model_version, dimensions)
		VALUES ($1, $2::vector, $3, $4, $5)
		ON CONFLICT (memory_id, embedding_model)
		DO UPDATE SET
			embedding    = EXCLUDED.embedding,
			model_version = EXCLUDED.model_version,
			dimensions   = EXCLUDED.dimensions,
			updated_at   = NOW()
	`, memoryID, vecStr, model, version, dimensions)
	if err != nil {
		return fmt.Errorf("upserting embedding: %w", err)
	}

	// Clear reembedding flag.
	_, err = tx.Exec(ctx,
		`UPDATE memories SET needs_reembedding = FALSE WHERE id = $1`,
		memoryID,
	)
	if err != nil {
		return fmt.Errorf("clearing needs_reembedding: %w", err)
	}

	return tx.Commit(ctx)
}

// VectorSearch performs approximate nearest-neighbor search using cosine similarity.
// Returns memory IDs with their similarity scores, filtered by namespace/type/status.
//
// Search pipeline:
//  1. Pre-filter by user_id, namespace, memory_type, status (uses B-tree indexes)
//  2. Order by cosine distance to query vector (uses HNSW index)
//  3. Return top-K results with similarity scores
func (r *EmbeddingRepository) VectorSearch(
	ctx context.Context,
	userID uuid.UUID,
	queryVector []float32,
	model string,
	filter domain.MemoryFilter,
	topK int,
) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 20
	}

	vecStr := float32SliceToString(queryVector)

	args := []any{userID, vecStr, model}
	argIdx := 4
	whereClauses := []string{
		"m.user_id = $1",
		"me.embedding_model = $3",
		"m.status = 'ACTIVE'",
	}

	if filter.Namespace != nil && *filter.Namespace != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("m.namespace = $%d", argIdx))
		args = append(args, *filter.Namespace)
		argIdx++
	}
	if filter.MemoryType != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("m.memory_type = $%d", argIdx))
		args = append(args, string(*filter.MemoryType))
		argIdx++
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	// Append topK and vector args.
	args = append(args, topK)
	limitArgIdx := argIdx

	q := fmt.Sprintf(`
		SELECT
			m.id,
			1 - (me.embedding <=> $2::vector) AS similarity
		FROM memories m
		JOIN memory_embeddings me ON me.memory_id = m.id
		%s
		ORDER BY me.embedding <=> $2::vector ASC
		LIMIT $%d
	`, whereSQL, limitArgIdx)

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var res VectorSearchResult
		if err := rows.Scan(&res.MemoryID, &res.Similarity); err != nil {
			return nil, fmt.Errorf("scanning vector result: %w", err)
		}
		results = append(results, res)
	}
	return results, rows.Err()
}

// ListNeedsReembedding returns memories that need their embeddings refreshed.
func (r *EmbeddingRepository) ListNeedsReembedding(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id FROM memories
		WHERE needs_reembedding = TRUE AND status = 'ACTIVE'
		ORDER BY updated_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkNeedsReembedding flags all active memories for re-embedding.
// Called when the active embedding model changes.
func (r *EmbeddingRepository) MarkNeedsReembedding(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE memories SET needs_reembedding = TRUE WHERE status = 'ACTIVE'`,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RegisterModel records an embedding model in the registry.
// Sets is_active=true and deactivates all other models.
func (r *EmbeddingRepository) RegisterModel(ctx context.Context, name, version string, dims int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Deactivate all others.
	if _, err := tx.Exec(ctx, `UPDATE embedding_model_registry SET is_active = FALSE`); err != nil {
		return err
	}

	now := time.Now()
	_, err = tx.Exec(ctx, `
		INSERT INTO embedding_model_registry (model_name, model_version, dimensions, is_active, activated_at)
		VALUES ($1, $2, $3, TRUE, $4)
		ON CONFLICT (model_name) DO UPDATE SET
			model_version = EXCLUDED.model_version,
			dimensions    = EXCLUDED.dimensions,
			is_active     = TRUE,
			activated_at  = EXCLUDED.activated_at
	`, name, version, dims, now)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// VectorSearchResult holds one result from a vector similarity search.
type VectorSearchResult struct {
	MemoryID   uuid.UUID
	Similarity float64 // cosine similarity [0,1]
}

// float32SliceToString converts a float32 slice to PostgreSQL vector literal.
// Format: "[0.1,0.2,0.3]"
func float32SliceToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%.8f", f))
	}
	sb.WriteByte(']')
	return sb.String()
}
