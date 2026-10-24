package retrieval

import (
	"github.com/google/uuid"
	"github.com/memory-engine/memory-engine/internal/domain"
)

// MergeResults combines FTS and vector search results into a deduplicated
// candidate pool, preserving the best score from each source.
//
// Reciprocal Rank Fusion (RRF) blending:
//   combined_score = α × keyword_rank_score + (1-α) × vector_similarity
//
// Where α=0.5 gives equal weight to both signals.
// Memories found by both sources get a bonus (they are more likely relevant).
func MergeResults(
	keywordResults []KeywordResult,
	vectorResults []VectorResult,
	alpha float64, // weight for keyword vs vector (0.5 = equal)
) []RankedCandidate {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.5
	}

	// Index by memory ID to deduplicate.
	type entry struct {
		memory        *domain.Memory
		keywordScore  float64
		vectorScore   float64
		sources       []string
	}
	seen := make(map[uuid.UUID]*entry)

	for _, kr := range keywordResults {
		if e, ok := seen[kr.Memory.ID]; ok {
			e.keywordScore = kr.Rank
			e.sources = appendUniq(e.sources, "keyword")
		} else {
			seen[kr.Memory.ID] = &entry{
				memory:       kr.Memory,
				keywordScore: kr.Rank,
				sources:      []string{"keyword"},
			}
		}
	}

	for _, vr := range vectorResults {
		if e, ok := seen[vr.Memory.ID]; ok {
			e.vectorScore = vr.Similarity
			e.sources = appendUniq(e.sources, "vector")
		} else {
			seen[vr.Memory.ID] = &entry{
				memory:      vr.Memory,
				vectorScore: vr.Similarity,
				sources:     []string{"vector"},
			}
		}
	}

	candidates := make([]RankedCandidate, 0, len(seen))
	for _, e := range seen {
		// Normalise keyword score to [0,1] — ts_rank_cd is already [0,1].
		kw := normalise(e.keywordScore)
		vs := normalise(e.vectorScore)

		// Bonus for appearing in both sources.
		dualBonus := 0.0
		if len(e.sources) == 2 {
			dualBonus = 0.05
		}

		// Blend: if only one source contributed, the other is 0, so alpha still applies correctly.
		blended := alpha*kw + (1-alpha)*vs + dualBonus
		if blended > 1.0 {
			blended = 1.0
		}

		candidates = append(candidates, RankedCandidate{
			Memory:       e.memory,
			KeywordScore: kw,
			VectorScore:  vs,
			FinalScore:   blended, // pre-rerank blend; final score set by reranker
			Sources:      e.sources,
		})
	}

	return candidates
}

// KeywordResult is one FTS search result.
type KeywordResult struct {
	Memory *domain.Memory
	Rank   float64 // ts_rank_cd score
}

// VectorResult is one vector similarity search result.
type VectorResult struct {
	Memory     *domain.Memory
	Similarity float64 // cosine similarity [0,1]
}

func normalise(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func appendUniq(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
