package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/search"
)

const (
	defaultTopK    = 20
	defaultPageSize = 20
	defaultMinRank  = 0.0
)

// SearchService handles full-text search over memories.
type SearchService struct {
	searchRepo *repository.SearchRepository
}

// NewSearchService creates a new SearchService.
func NewSearchService(searchRepo *repository.SearchRepository) *SearchService {
	return &SearchService{searchRepo: searchRepo}
}

// Search executes a full-text search with pre-filtering and ranking.
func (s *SearchService) Search(ctx context.Context, req domain.SearchRequest) (*domain.SearchResponse, error) {
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return nil, domain.Validation(map[string]string{"user_id": "invalid UUID"})
	}

	req.Query = strings.TrimSpace(req.Query)

	queryType := parseQueryType(req.QueryType)
	var parsedQuery domain.SearchQuery

	if req.Query == "" {
		parsedQuery = domain.SearchQuery{Raw: "", QueryType: domain.SearchAnd}
	} else {
		parsedQuery = search.ParseQueryAuto(req.Query)
		if queryType != "" {
			parsedQuery = search.ParseQuery(req.Query, queryType)
		}
		// websearch_to_tsquery expects the raw user string, not pre-built operators.
		// Reset TSQuery to raw input so PostgreSQL parses it natively.
		if parsedQuery.QueryType == domain.SearchWebStyle {
			parsedQuery.TSQuery = req.Query
		}
	}

	// Build filter.
	filter := domain.MemoryFilter{
		UserID:   userID,
		Page:     req.Page,
		PageSize: req.PageSize,
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = defaultPageSize
	}
	if req.Namespace != "" {
		ns := strings.ToUpper(strings.TrimSpace(req.Namespace))
		filter.Namespace = &ns
	}
	if req.MemoryType != "" {
		mt := domain.MemoryType(strings.ToUpper(req.MemoryType))
		filter.MemoryType = &mt
	}
	if req.Status != "" {
		st := domain.MemoryStatus(strings.ToUpper(req.Status))
		filter.Status = &st
	}

	minRank := req.MinRank
	if minRank < 0 {
		minRank = defaultMinRank
	}

	return s.searchRepo.Search(ctx, userID, parsedQuery, filter, minRank)
}

// SuggestNamespaces returns namespace autocomplete suggestions.
func (s *SearchService) SuggestNamespaces(ctx context.Context, userID uuid.UUID, prefix string) ([]string, error) {
	return s.searchRepo.SuggestNamespaces(ctx, userID, prefix, 10)
}

func parseQueryType(s string) domain.SearchQueryType {
	switch strings.ToUpper(s) {
	case "OR":
		return domain.SearchOr
	case "PHRASE":
		return domain.SearchPhrase
	case "PREFIX":
		return domain.SearchPrefix
	case "WEB":
		return domain.SearchWebStyle
	case "AND":
		return domain.SearchAnd
	default:
		return ""
	}
}
