package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// RelationshipRepository handles memory_relationships persistence.
type RelationshipRepository struct {
	db *pgxpool.Pool
}

// NewRelationshipRepository creates a new RelationshipRepository.
func NewRelationshipRepository(db *pgxpool.Pool) *RelationshipRepository {
	return &RelationshipRepository{db: db}
}

// Create inserts a new directed relationship between two memories.
// Silently succeeds if the relationship already exists (upsert).
func (r *RelationshipRepository) Create(
	ctx context.Context,
	sourceID, targetID uuid.UUID,
	relType domain.RelationshipType,
	confidence float64,
) (*domain.MemoryRelationship, error) {
	const q = `
		INSERT INTO memory_relationships (source_memory_id, target_memory_id, relationship_type, confidence)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (source_memory_id, target_memory_id, relationship_type)
		DO UPDATE SET confidence = EXCLUDED.confidence
		RETURNING id, source_memory_id, target_memory_id, relationship_type, confidence, created_at
	`
	rel := &domain.MemoryRelationship{}
	var relTypeStr string
	err := r.db.QueryRow(ctx, q, sourceID, targetID, string(relType), confidence).Scan(
		&rel.ID, &rel.SourceMemoryID, &rel.TargetMemoryID, &relTypeStr, &rel.Confidence, &rel.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating relationship: %w", err)
	}
	rel.RelationshipType = domain.RelationshipType(relTypeStr)
	return rel, nil
}

// GetForMemory returns all relationships where the memory is source or target.
func (r *RelationshipRepository) GetForMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.MemoryRelationship, error) {
	const q = `
		SELECT id, source_memory_id, target_memory_id, relationship_type, confidence, created_at
		FROM memory_relationships
		WHERE source_memory_id = $1 OR target_memory_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, q, memoryID)
	if err != nil {
		return nil, fmt.Errorf("getting relationships: %w", err)
	}
	defer rows.Close()

	return scanRelationships(rows)
}

// GetDerivedMemories returns all memories derived from a source (DERIVED_FROM edges).
func (r *RelationshipRepository) GetDerivedMemories(ctx context.Context, sourceID uuid.UUID) ([]*domain.MemoryRelationship, error) {
	const q = `
		SELECT id, source_memory_id, target_memory_id, relationship_type, confidence, created_at
		FROM memory_relationships
		WHERE source_memory_id = $1 AND relationship_type = 'DERIVED_FROM'
	`
	rows, err := r.db.Query(ctx, q, sourceID)
	if err != nil {
		return nil, fmt.Errorf("getting derived memories: %w", err)
	}
	defer rows.Close()

	return scanRelationships(rows)
}

// Delete removes a specific relationship.
func (r *RelationshipRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM memory_relationships WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting relationship: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("relationship", id.String())
	}
	return nil
}

// GetByID fetches a single relationship by ID.
func (r *RelationshipRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.MemoryRelationship, error) {
	const q = `
		SELECT id, source_memory_id, target_memory_id, relationship_type, confidence, created_at
		FROM memory_relationships WHERE id = $1
	`
	rel := &domain.MemoryRelationship{}
	var relTypeStr string
	err := r.db.QueryRow(ctx, q, id).Scan(
		&rel.ID, &rel.SourceMemoryID, &rel.TargetMemoryID, &relTypeStr, &rel.Confidence, &rel.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("relationship", id.String())
		}
		return nil, err
	}
	rel.RelationshipType = domain.RelationshipType(relTypeStr)
	return rel, nil
}

func scanRelationships(rows pgx.Rows) ([]*domain.MemoryRelationship, error) {
	var rels []*domain.MemoryRelationship
	for rows.Next() {
		rel := &domain.MemoryRelationship{}
		var relTypeStr string
		if err := rows.Scan(
			&rel.ID, &rel.SourceMemoryID, &rel.TargetMemoryID, &relTypeStr, &rel.Confidence, &rel.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		rel.RelationshipType = domain.RelationshipType(relTypeStr)
		rels = append(rels, rel)
	}
	return rels, rows.Err()
}
