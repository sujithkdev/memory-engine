package domain

// SearchQuery holds a parsed and validated FTS query.
type SearchQuery struct {
	// Raw is the original user input.
	Raw string `json:"raw"`
	// TSQuery is the PostgreSQL tsquery string derived from Raw.
	// e.g. "python & scripting" or "python | go"
	TSQuery string `json:"ts_query"`
	// QueryType describes how the query was parsed.
	QueryType SearchQueryType `json:"query_type"`
}

// SearchQueryType describes the parsing strategy used.
type SearchQueryType string

const (
	// SearchPhrase — exact phrase search (terms in order).
	SearchPhrase SearchQueryType = "PHRASE"
	// SearchAnd — all terms must match (default).
	SearchAnd SearchQueryType = "AND"
	// SearchOr — any term matches.
	SearchOr SearchQueryType = "OR"
	// SearchPrefix — prefix/autocomplete search.
	SearchPrefix SearchQueryType = "PREFIX"
	// SearchWebStyle — Google-style mixed query (+term -term "phrase").
	SearchWebStyle SearchQueryType = "WEB"
)

// SearchRequest is the input for a full-text search.
type SearchRequest struct {
	UserID      string   `json:"user_id"`
	Query       string   `json:"query"`
	Namespace   string   `json:"namespace,omitempty"`
	MemoryType  string   `json:"memory_type,omitempty"`
	Status      string   `json:"status,omitempty"`
	QueryType   string   `json:"query_type,omitempty"` // AND|OR|PHRASE|PREFIX|WEB
	TopK        int      `json:"top_k,omitempty"`
	Page        int      `json:"page,omitempty"`
	PageSize    int      `json:"page_size,omitempty"`
	// MinRank filters out results below this FTS rank threshold.
	MinRank     float64  `json:"min_rank,omitempty"`
}

// SearchResult is one memory in a search response with its rank.
type SearchResult struct {
	Memory    *Memory `json:"memory"`
	Rank      float64 `json:"rank"`       // ts_rank_cd score
	Headline  string  `json:"headline"`   // highlighted snippet
}

// SearchResponse is the full response from a search query.
type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
	Query      SearchQuery    `json:"query"`
	// Filters applied.
	Namespace  string `json:"namespace,omitempty"`
	MemoryType string `json:"memory_type,omitempty"`
}
