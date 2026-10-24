package repository

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// SearchRepository handles full-text search queries against memories.
type SearchRepository struct {
	db *pgxpool.Pool
}

// NewSearchRepository creates a new SearchRepository.
func NewSearchRepository(db *pgxpool.Pool) *SearchRepository {
	return &SearchRepository{db: db}
}

// searchSelectCols is the explicit aliased column list for search queries.
// Must stay in sync with memorySelectCols in memory_repository.go.
const searchSelectCols = `
	m.id, m.user_id, m.content, m.memory_type, m.status,
	m.importance, m.confidence, m.salience,
	m.valid_from, m.valid_until,
	m.source_type, m.source_id,
	m.namespace, m.memory_level,
	m.privacy_level, m.retention_days, m.delete_after,
	m.version, m.parent_id,
	m.last_accessed, m.access_count,
	m.importance_level, m.scored_at,
	m.content_hash,
	m.created_at, m.updated_at
`

// Search executes a full-text search and returns ranked results.
//
// Pipeline:
//  1. Hard filters (user_id, status, namespace, type) — uses B-tree indexes first
//  2. FTS filter (search_vector @@ tsquery) — uses GIN index
//  3. ORDER BY ts_rank_cd DESC, importance DESC
//  4. Headline extraction for UI highlighting
func (r *SearchRepository) Search(
	ctx context.Context,
	userID uuid.UUID,
	query domain.SearchQuery,
	filter domain.MemoryFilter,
	minRank float64,
) (*domain.SearchResponse, error) {
	if query.TSQuery == "" {
		return r.listAsSearchResults(ctx, userID, filter)
	}

	// Determine the correct PostgreSQL tsquery function based on query type.
	tsQueryFn := tsqueryFunction(query.QueryType)

	args := []any{userID, query.TSQuery}
	argIdx := 3
	whereClauses := []string{
		"m.user_id = $1",
		fmt.Sprintf("m.search_vector @@ %s('english', $2)", tsQueryFn),
	}

	// Status filter — default ACTIVE.
	statusVal := "ACTIVE"
	if filter.Status != nil {
		statusVal = string(*filter.Status)
	}
	whereClauses = append(whereClauses, fmt.Sprintf("m.status = $%d", argIdx))
	args = append(args, statusVal)
	argIdx++

	// Namespace pre-filter (scalability lever).
	if filter.Namespace != nil && *filter.Namespace != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("m.namespace = $%d", argIdx))
		args = append(args, *filter.Namespace)
		argIdx++
	}

	// Memory type filter.
	if filter.MemoryType != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("m.memory_type = $%d", argIdx))
		args = append(args, string(*filter.MemoryType))
		argIdx++
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	// Count (for pagination metadata).
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM memories m %s`, whereSQL)
	var total int64
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting search results: %w", err)
	}

	// tsquery arg index for rank + headline (appended after the WHERE args).
	rankArgIdx := argIdx
	limitArgIdx := argIdx + 1
	offsetArgIdx := argIdx + 2

	offset := (filter.Page - 1) * filter.PageSize
	dataArgs := append(args, query.TSQuery, filter.PageSize, offset)

	dataQ := fmt.Sprintf(`
		SELECT
			%s,
			ts_rank_cd(m.search_vector, %s('english', $%d)) AS rank,
			ts_headline(
				'english',
				m.content,
				%s('english', $%d),
				'MaxWords=20, MinWords=5, ShortWord=3, HighlightAll=false, MaxFragments=2, FragmentDelimiter=" … "'
			) AS headline
		FROM memories m
		%s
		ORDER BY rank DESC, m.importance DESC, m.created_at DESC
		LIMIT $%d OFFSET $%d
	`,
		searchSelectCols,
		tsQueryFn, rankArgIdx,
		tsQueryFn, rankArgIdx,
		whereSQL,
		limitArgIdx, offsetArgIdx,
	)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("executing FTS search: %w", err)
	}
	defer rows.Close()

	var results []domain.SearchResult
	for rows.Next() {
		mem := &domain.Memory{}
		var rank float64
		var headline string

		if err := rows.Scan(
			&mem.ID, &mem.UserID, &mem.Content, &mem.MemoryType, &mem.Status,
			&mem.Importance, &mem.Confidence, &mem.Salience,
			&mem.ValidFrom, &mem.ValidUntil,
			&mem.SourceType, &mem.SourceID,
			&mem.Namespace, &mem.MemoryLevel,
			&mem.PrivacyLevel, &mem.RetentionDays, &mem.DeleteAfter,
			&mem.Version, &mem.ParentID,
			&mem.LastAccessed, &mem.AccessCount,
			&mem.ImportanceLevel, &mem.ScoredAt,
			&mem.ContentHash,
			&mem.CreatedAt, &mem.UpdatedAt,
			&rank, &headline,
		); err != nil {
			return nil, fmt.Errorf("scanning search result: %w", err)
		}

		if rank < minRank {
			continue
		}

		results = append(results, domain.SearchResult{
			Memory:   mem,
			Rank:     rank,
			Headline: headline,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating search results: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.SearchResponse{
		Results:    results,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
		Query:      query,
		Namespace:  ptrStr(filter.Namespace),
		MemoryType: memTypeStr(filter.MemoryType),
	}, nil
}

// SuggestNamespaces returns distinct namespaces matching a prefix (autocomplete).
func (r *SearchRepository) SuggestNamespaces(ctx context.Context, userID uuid.UUID, prefix string, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT namespace FROM memories
		WHERE user_id = $1
		  AND namespace ILIKE $2
		  AND status = 'ACTIVE'
		ORDER BY namespace
		LIMIT $3
	`, userID, prefix+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("suggesting namespaces: %w", err)
	}
	defer rows.Close()

	var ns []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		ns = append(ns, n)
	}
	return ns, rows.Err()
}

// listAsSearchResults returns active memories as SearchResults with rank=0.
// Used when query is empty — respects all other filters.
func (r *SearchRepository) listAsSearchResults(
	ctx context.Context,
	userID uuid.UUID,
	filter domain.MemoryFilter,
) (*domain.SearchResponse, error) {
	args := []any{userID}
	argIdx := 2
	wheres := []string{"user_id = $1", "status = 'ACTIVE'"}

	if filter.Namespace != nil && *filter.Namespace != "" {
		wheres = append(wheres, fmt.Sprintf("namespace = $%d", argIdx))
		args = append(args, *filter.Namespace)
		argIdx++
	}
	if filter.MemoryType != nil {
		wheres = append(wheres, fmt.Sprintf("memory_type = $%d", argIdx))
		args = append(args, string(*filter.MemoryType))
		argIdx++
	}

	whereSQL := "WHERE " + strings.Join(wheres, " AND ")

	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM memories "+whereSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting list results: %w", err)
	}

	offset := (filter.Page - 1) * filter.PageSize
	dataArgs := append(args, filter.PageSize, offset)
	dataQ := fmt.Sprintf(
		`SELECT %s FROM memories %s ORDER BY importance DESC, created_at DESC LIMIT $%d OFFSET $%d`,
		memorySelectCols, whereSQL, argIdx, argIdx+1,
	)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("listing search fallback: %w", err)
	}
	defer rows.Close()

	var results []domain.SearchResult
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, domain.SearchResult{Memory: m, Rank: 0})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.SearchResponse{
		Results:    results,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
		Query:      domain.SearchQuery{Raw: ""},
		Namespace:  ptrStr(filter.Namespace),
		MemoryType: memTypeStr(filter.MemoryType),
	}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// tsqueryFunction returns the correct PostgreSQL function for the query type.
//
//	PHRASE  → phraseto_tsquery (respects word order)
//	PREFIX  → to_tsquery       (handles :* operator)
//	WEB     → websearch_to_tsquery (PostgreSQL 11+ Google-style)
//	default → plainto_tsquery  (safe, no special operators needed)
func tsqueryFunction(qt domain.SearchQueryType) string {
	switch qt {
	case domain.SearchPhrase:
		return "phraseto_tsquery"
	case domain.SearchPrefix, domain.SearchAnd, domain.SearchOr:
		return "to_tsquery"
	case domain.SearchWebStyle:
		return "websearch_to_tsquery"
	default:
		return "plainto_tsquery"
	}
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func memTypeStr(mt *domain.MemoryType) string {
	if mt == nil {
		return ""
	}
	return string(*mt)
}
