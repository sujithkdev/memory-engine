// Package dedup implements memory deduplication logic.
//
// Deduplication strategy (Phase 5, text-based):
//
//  1. Exact match   — SHA-256 hash of normalised content → instant O(1) lookup
//  2. Near match    — Jaccard similarity on word sets (no embedding needed yet)
//     Phase 7 will upgrade near-match to cosine similarity on vectors.
//
// Thresholds (configurable):
//
//	ExactThreshold = 1.0  (identical hash)
//	NearThreshold  = 0.75 (word overlap ratio)
package dedup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strings"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

const (
	// NearDuplicateThreshold — Jaccard similarity above this triggers near-duplicate.
	NearDuplicateThreshold = 0.75
	// ConfidenceBoostOnDuplicate — when a duplicate is found, boost existing memory's confidence.
	ConfidenceBoostOnDuplicate = 0.05
	// MaxCandidatesToCheck — max existing memories to compare against (performance guard).
	MaxCandidatesToCheck = 50
)

// Deduplicator checks candidate memories against existing ones.
type Deduplicator struct {
	memories  *repository.MemoryRepository
	conflicts *repository.ConflictRepository
}

// New creates a new Deduplicator.
func New(memories *repository.MemoryRepository, conflicts *repository.ConflictRepository) *Deduplicator {
	return &Deduplicator{memories: memories, conflicts: conflicts}
}

// Check examines a candidate memory against existing memories for the user.
// Returns a DedupResult describing what action to take.
//
// Call this BEFORE inserting a new memory. Based on the result:
//   - DROPPED         → don't insert, existing memory already covers this
//   - MERGED          → don't insert, boost confidence on existing
//   - UPDATED_CONFIDENCE → don't insert, slight update to existing
//   - KEPT_BOTH       → insert candidate, log the conflict
func (d *Deduplicator) Check(ctx context.Context, candidate *domain.Memory) (*domain.DedupResult, error) {
	// Step 1: Exact hash match — fastest path.
	if candidate.ContentHash != "" {
		existing, err := d.memories.FindByContentHash(ctx, candidate.UserID, candidate.ContentHash)
		if err != nil {
			return nil, fmt.Errorf("exact hash check: %w", err)
		}
		if existing != nil {
			return &domain.DedupResult{
				Action:          domain.DedupDropped,
				MatchedMemoryID: &existing.ID,
				Similarity:      1.0,
				Reason:          "exact duplicate detected via content hash",
				FinalMemory:     existing,
			}, nil
		}
	}

	// Step 2: Near-duplicate check via Jaccard similarity on word sets.
	candidates, err := d.memories.ListActiveForUser(ctx, candidate.UserID, candidate.Namespace, MaxCandidatesToCheck)
	if err != nil {
		return nil, fmt.Errorf("fetching candidates for dedup: %w", err)
	}

	var bestMatch *domain.Memory
	var bestSim float64

	for _, existing := range candidates {
		if existing.ID == candidate.ID {
			continue
		}
		sim := jaccardSimilarity(candidate.Content, existing.Content)
		if sim > bestSim {
			bestSim = sim
			bestMatch = existing
		}
	}

	if bestMatch == nil || bestSim < NearDuplicateThreshold {
		// No duplicate found — candidate is unique.
		return &domain.DedupResult{
			Action:     domain.DedupKeptBoth,
			Similarity: bestSim,
			Reason:     "no duplicate found",
			FinalMemory: candidate,
		}, nil
	}

	// Near-duplicate found. Decide action based on similarity tier.
	return d.resolveNearDuplicate(candidate, bestMatch, bestSim), nil
}

// resolveNearDuplicate decides what to do when a near-duplicate is found.
func (d *Deduplicator) resolveNearDuplicate(candidate, existing *domain.Memory, sim float64) *domain.DedupResult {
	switch {
	case sim >= 0.95:
		// Functionally identical — drop candidate, boost existing confidence.
		return &domain.DedupResult{
			Action:          domain.DedupMerged,
			MatchedMemoryID: &existing.ID,
			Similarity:      sim,
			Reason:          fmt.Sprintf("near-identical content (%.2f similarity) — merging into existing", sim),
			FinalMemory:     existing,
		}

	case sim >= 0.85:
		// Very similar — boost existing confidence slightly.
		return &domain.DedupResult{
			Action:          domain.DedupUpdatedConfidence,
			MatchedMemoryID: &existing.ID,
			Similarity:      sim,
			Reason:          fmt.Sprintf("high similarity (%.2f) — updating confidence on existing", sim),
			FinalMemory:     existing,
		}

	default:
		// Similar but distinct enough to keep both — log as near-duplicate conflict.
		return &domain.DedupResult{
			Action:          domain.DedupKeptBoth,
			MatchedMemoryID: &existing.ID,
			Similarity:      sim,
			Reason:          fmt.Sprintf("near-duplicate detected (%.2f similarity) — keeping both", sim),
			FinalMemory:     candidate,
		}
	}
}

// HashContent computes the SHA-256 hash of normalised content.
// Normalisation: lowercase, trim whitespace, collapse internal spaces.
func HashContent(content string) string {
	normalised := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(content))), " ")
	h := sha256.Sum256([]byte(normalised))
	return fmt.Sprintf("%x", h)
}

// ── Text similarity ────────────────────────────────────────────────────────────

// jaccardSimilarity computes the Jaccard index of two text strings based on word sets.
// J(A,B) = |A ∩ B| / |A ∪ B|
// Range: [0,1] — 1 = identical word sets, 0 = no words in common.
func jaccardSimilarity(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)

	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}

	intersection := 0
	for word := range setA {
		if setB[word] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// wordSet converts text to a set of lowercase words (stop words removed).
func wordSet(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		// Strip punctuation.
		w = strings.Trim(w, ".,!?;:\"'()-")
		if w != "" && !isStopWord(w) {
			set[w] = true
		}
	}
	return set
}

// isStopWord returns true for common English stop words that dilute similarity.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"but": true, "is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true,
	"could": true, "should": true, "may": true, "might": true,
	"i": true, "me": true, "my": true, "we": true, "our": true,
	"you": true, "your": true, "he": true, "she": true, "it": true,
	"they": true, "them": true, "their": true, "this": true, "that": true,
	"to": true, "of": true, "in": true, "on": true, "at": true,
	"for": true, "with": true, "about": true, "from": true, "by": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// CosineSimilarityPlaceholder is a stub for Phase 7 when embeddings are available.
// Currently returns Jaccard similarity as approximation.
func CosineSimilarityPlaceholder(a, b string) float64 {
	return jaccardSimilarity(a, b)
}

// BoostConfidence returns a new confidence value boosted by the duplicate amount,
// clamped to [0,1].
func BoostConfidence(current float64) float64 {
	return math.Min(1.0, current+ConfidenceBoostOnDuplicate)
}
