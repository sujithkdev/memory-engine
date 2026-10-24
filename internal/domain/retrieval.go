package domain

import "github.com/google/uuid"

// RetrievalRequest is the unified input for the hybrid retrieval pipeline.
//
// Pipeline:
//
//	Query → classify → namespace/type filter
//	  → keyword search (FTS)  ─┐
//	  → vector search (ANN)   ─┴→ merge top-20
//	  → reranker → top-K
//	  → conflict check
//	  → context compression
//	  → token budget
type RetrievalRequest struct {
	UserID    uuid.UUID `json:"user_id"`
	Query     string    `json:"query"`
	Namespace string    `json:"namespace,omitempty"`
	// MemoryTypes optionally restricts retrieval to specific types.
	MemoryTypes []MemoryType `json:"memory_types,omitempty"`
	// TopK is the number of memories to return after reranking (default 5).
	TopK int `json:"top_k,omitempty"`
	// CandidatePool is how many candidates to gather before reranking (default 20).
	CandidatePool int `json:"candidate_pool,omitempty"`
	// MaxTokens is the token budget for the returned context (default 2000).
	MaxTokens int `json:"max_tokens,omitempty"`
	// MinScore filters candidates below this final score (default 0).
	MinScore float64 `json:"min_score,omitempty"`
	// IncludeScoreBreakdown includes per-signal scoring in the response.
	IncludeScoreBreakdown bool `json:"include_score_breakdown,omitempty"`
}

// RetrievalResponse is the full output of the hybrid retrieval pipeline.
type RetrievalResponse struct {
	// Results are the final ranked memories within token budget.
	Results []RetrievalResult `json:"results"`
	// TokensUsed is the total estimated tokens in the returned context.
	TokensUsed int `json:"tokens_used"`
	// TokenBudget is the configured max tokens.
	TokenBudget int `json:"token_budget"`
	// CandidatesConsidered is the total memories evaluated before reranking.
	CandidatesConsidered int `json:"candidates_considered"`
	// Query metadata.
	Query         string `json:"query"`
	Namespace     string `json:"namespace,omitempty"`
	// Pipeline trace — explains which stages ran and what they found.
	Trace *RetrievalTrace `json:"trace,omitempty"`
}

// RetrievalResult is one memory in the retrieval response.
type RetrievalResult struct {
	Memory         *Memory        `json:"memory"`
	FinalScore     float64        `json:"final_score"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
	// Sources shows which retrieval paths found this memory.
	Sources []string `json:"sources"` // "keyword", "vector", "both"
	// TokenCount is an estimate of this memory's token usage.
	TokenCount int `json:"token_count"`
}

// RetrievalTrace records what happened at each pipeline stage.
// Used for debugging retrieval quality.
type RetrievalTrace struct {
	KeywordHits  int    `json:"keyword_hits"`
	VectorHits   int    `json:"vector_hits"`
	MergedTotal  int    `json:"merged_total"`
	AfterRerank  int    `json:"after_rerank"`
	AfterBudget  int    `json:"after_budget"`
	QueryType    string `json:"query_type"`
}
