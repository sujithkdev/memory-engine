package retrieval

import (
	"strings"
	"unicode"

	"github.com/memory-engine/memory-engine/internal/domain"
)

const (
	// DefaultTokenBudget is the default max tokens for retrieval context.
	DefaultTokenBudget = 2000
	// DefaultTopK is the default number of memories to return.
	DefaultTopK = 5
	// DefaultCandidatePool is how many candidates to gather before reranking.
	DefaultCandidatePool = 20
	// TokensPerWord is a rough approximation for token counting without a tokenizer.
	// GPT-style models average ~1.3 tokens per word for English text.
	TokensPerWord = 1.33
)

// TokenBudget enforces a maximum token count on retrieved memories.
type TokenBudget struct {
	maxTokens int
}

// NewTokenBudget creates a TokenBudget with the given limit.
func NewTokenBudget(maxTokens int) *TokenBudget {
	if maxTokens <= 0 {
		maxTokens = DefaultTokenBudget
	}
	return &TokenBudget{maxTokens: maxTokens}
}

// Apply filters candidates to fit within the token budget.
// Higher-scored candidates are kept; lower-scored ones are dropped.
// Candidates must already be sorted by score descending.
func (tb *TokenBudget) Apply(candidates []RankedCandidate) ([]RankedCandidate, int) {
	var kept []RankedCandidate
	totalTokens := 0

	for _, c := range candidates {
		tokens := EstimateTokens(c.Memory.Content)
		if totalTokens+tokens > tb.maxTokens {
			// Check if remaining budget can fit any smaller memory.
			remaining := tb.maxTokens - totalTokens
			if remaining < 50 { // less than 50 tokens left — stop
				break
			}
			// Try to fit a compressed version or skip.
			continue
		}
		kept = append(kept, c)
		totalTokens += tokens
	}

	return kept, totalTokens
}

// EstimateTokens approximates token count for a string.
// Uses word count * TokensPerWord as a fast approximation.
// Replace with tiktoken or a proper tokenizer for production accuracy.
func EstimateTokens(text string) int {
	words := countWords(text)
	tokens := int(float64(words) * TokensPerWord)
	if tokens < 1 && len(text) > 0 {
		return 1
	}
	return tokens
}

// countWords counts whitespace-separated words.
func countWords(text string) int {
	inWord := false
	count := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			count++
		}
	}
	return count
}

// ── Context compressor ────────────────────────────────────────────────────────

// ContextCompressor trims memory content to fit within per-memory token limits.
// Used when a memory's content is long but its score warrants inclusion.
type ContextCompressor struct {
	maxTokensPerMemory int
}

// NewContextCompressor creates a ContextCompressor.
func NewContextCompressor(maxTokensPerMemory int) *ContextCompressor {
	if maxTokensPerMemory <= 0 {
		maxTokensPerMemory = 200
	}
	return &ContextCompressor{maxTokensPerMemory: maxTokensPerMemory}
}

// Compress truncates memory content to fit within the per-memory token limit.
// Adds an ellipsis if truncated so the LLM knows content was cut.
func (c *ContextCompressor) Compress(mem *domain.Memory) *domain.Memory {
	if mem == nil {
		return nil
	}

	tokens := EstimateTokens(mem.Content)
	if tokens <= c.maxTokensPerMemory {
		return mem // no compression needed
	}

	// Truncate by word count.
	targetWords := int(float64(c.maxTokensPerMemory) / TokensPerWord)
	truncated := truncateWords(mem.Content, targetWords)

	// Return a shallow copy with truncated content.
	compressed := *mem
	compressed.Content = truncated + " […]"
	return &compressed
}

// truncateWords returns the first n words of text.
func truncateWords(text string, n int) string {
	words := strings.Fields(text)
	if len(words) <= n {
		return text
	}
	return strings.Join(words[:n], " ")
}
