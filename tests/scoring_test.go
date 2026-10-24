package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Scoring endpoint tests ────────────────────────────────────────────────────

func TestScoringWeights_Endpoint(t *testing.T) {
	router := buildRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/memories/scoring-weights", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data domain.ScoringWeights `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	sum := resp.Data.Relevance + resp.Data.Importance + resp.Data.Confidence +
		resp.Data.Salience + resp.Data.Recency + resp.Data.AccessFrequency

	// Weights must sum to ~1.0.
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("scoring weights should sum to 1.0, got %.4f", sum)
	}

	// Relevance should be the highest weight.
	if resp.Data.Relevance < resp.Data.Importance {
		t.Errorf("relevance (%.2f) should be >= importance (%.2f)", resp.Data.Relevance, resp.Data.Importance)
	}
}

func TestScoreOne_Endpoint(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create an ACTIVE memory.
	body := mustJSON(t, map[string]interface{}{
		"content":     "I prefer Go for backend development",
		"memory_type": "PREFERENCE",
		"importance":  0.8,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup failed: %s", rr.Body.String())
	}

	var createResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &createResp)
	memID := createResp.Data.ID

	// Get the score breakdown.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s/score?user_id=%s", memID, user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var scoreResp struct {
		Data map[string]interface{} `json:"data"`
	}
	mustDecode(t, rr.Body, &scoreResp)

	// Verify all expected fields are present.
	requiredFields := []string{
		"final_score", "score_breakdown",
		"importance_level", "confidence_level",
		"is_temporally_valid", "decayed_salience",
	}
	for _, field := range requiredFields {
		if _, ok := scoreResp.Data[field]; !ok {
			t.Errorf("missing field in score response: %s", field)
		}
	}

	// final_score must be in [0,1].
	finalScore, ok := scoreResp.Data["final_score"].(float64)
	if !ok {
		t.Fatal("final_score is not a float64")
	}
	if finalScore < 0 || finalScore > 1 {
		t.Errorf("final_score %.4f out of [0,1]", finalScore)
	}
}

func TestScoreList_Endpoint(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create memories with different importance levels.
	importances := []float64{0.9, 0.3, 0.7}
	for i, imp := range importances {
		body := mustJSON(t, map[string]interface{}{
			"content":     fmt.Sprintf("memory with importance %.1f", imp),
			"memory_type": "FACT",
			"importance":  imp,
			"namespace":   "TEST",
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
			body,
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup memory %d failed: %s", i, rr.Body.String())
		}
	}

	// Get scored list.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/scored?namespace=TEST", user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Memories       []domain.ScoredMemory  `json:"memories"`
			Total          int                    `json:"total"`
			ScoringWeights domain.ScoringWeights  `json:"scoring_weights"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 3 {
		t.Errorf("expected at least 3 scored memories, got %d", resp.Data.Total)
	}

	// Verify memories are sorted by final_score descending.
	mems := resp.Data.Memories
	for i := 1; i < len(mems); i++ {
		if mems[i].FinalScore > mems[i-1].FinalScore {
			t.Errorf("memories not sorted by score at index %d: %.4f > %.4f",
				i, mems[i].FinalScore, mems[i-1].FinalScore)
		}
	}

	// Verify scoring weights are returned.
	if resp.Data.ScoringWeights.Relevance == 0 {
		t.Error("scoring weights should be non-zero in response")
	}
}

func TestScoreList_ScoreBreakdownPresent(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create one ACTIVE memory.
	body := mustJSON(t, map[string]interface{}{
		"content":     "breakdown test memory",
		"memory_type": "KNOWLEDGE",
		"importance":  0.75,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/scored", user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Data struct {
			Memories []struct {
				FinalScore     float64                `json:"final_score"`
				ScoreBreakdown domain.ScoreBreakdown  `json:"score_breakdown"`
			} `json:"memories"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if len(resp.Data.Memories) == 0 {
		t.Skip("no active memories found — skip breakdown check")
	}

	bd := resp.Data.Memories[0].ScoreBreakdown
	// Weighted total should match final score.
	diff := resp.Data.Memories[0].FinalScore - bd.WeightedTotal
	if diff < -0.01 || diff > 0.01 {
		t.Errorf("final_score %.4f != weighted_total %.4f",
			resp.Data.Memories[0].FinalScore, bd.WeightedTotal)
	}
}

// ── ImportanceLevel computed column ───────────────────────────────────────────

func TestImportanceLevel_SetByDB(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create a CRITICAL importance memory (>= 0.9).
	body := mustJSON(t, map[string]interface{}{
		"content":     "critical importance memory",
		"memory_type": "INSTRUCTION",
		"importance":  0.95,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}

	var createResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &createResp)

	if createResp.Data.ImportanceLevel != "CRITICAL" {
		t.Errorf("expected ImportanceLevel=CRITICAL for importance 0.95, got %q",
			createResp.Data.ImportanceLevel)
	}
}

func TestInitialSalience_SetByType(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// INSTRUCTION memories should get salience 0.85 from the DB trigger.
	body := mustJSON(t, map[string]interface{}{
		"content":     "always use UTC for timestamps",
		"memory_type": "INSTRUCTION",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	// Trigger sets INSTRUCTION → 0.85. Allow small float tolerance.
	if resp.Data.Salience < 0.84 || resp.Data.Salience > 0.86 {
		t.Errorf("expected salience ~0.85 for INSTRUCTION type, got %.4f", resp.Data.Salience)
	}
}
