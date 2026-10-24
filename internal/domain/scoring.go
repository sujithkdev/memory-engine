package domain

// ScoredMemory pairs a Memory with its computed score breakdown.
// Used in retrieval responses so callers can understand why a memory was ranked.
type ScoredMemory struct {
	Memory         *Memory        `json:"memory"`
	FinalScore     float64        `json:"final_score"`
	ScoreBreakdown ScoreBreakdown `json:"score_breakdown"`
}

// ScoreBreakdown shows each signal's contribution to the final score.
type ScoreBreakdown struct {
	Relevance       float64 `json:"relevance"`
	Importance      float64 `json:"importance"`
	Confidence      float64 `json:"confidence"`
	Salience        float64 `json:"salience"`
	Recency         float64 `json:"recency"`
	AccessFrequency float64 `json:"access_frequency"`
	WeightedTotal   float64 `json:"weighted_total"`
}

// ScoredMemoryList is a ranked list with score context.
type ScoredMemoryList struct {
	Memories       []ScoredMemory `json:"memories"`
	Total          int            `json:"total"`
	ScoringWeights ScoringWeights `json:"scoring_weights"`
}

// ScoringWeights is a public view of the active weight configuration.
type ScoringWeights struct {
	Relevance       float64 `json:"relevance"`
	Importance      float64 `json:"importance"`
	Confidence      float64 `json:"confidence"`
	Salience        float64 `json:"salience"`
	Recency         float64 `json:"recency"`
	AccessFrequency float64 `json:"access_frequency"`
}

// ScoreRequest is the input for the score endpoint.
type ScoreRequest struct {
	UserID    string  `json:"user_id"`
	Namespace string  `json:"namespace,omitempty"`
	Query     string  `json:"query,omitempty"`
	TopK      int     `json:"top_k,omitempty"`
}
