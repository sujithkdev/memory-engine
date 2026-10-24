package dedup_test

import (
	"testing"

	"github.com/memory-engine/memory-engine/internal/dedup"
)

// ── HashContent ───────────────────────────────────────────────────────────────

func TestHashContent_SameText_SameHash(t *testing.T) {
	h1 := dedup.HashContent("I prefer Python for scripting")
	h2 := dedup.HashContent("I prefer Python for scripting")
	if h1 != h2 {
		t.Errorf("same text should produce same hash")
	}
}

func TestHashContent_NormalisedEquality(t *testing.T) {
	// Different whitespace and case should hash the same.
	h1 := dedup.HashContent("  I prefer Python  ")
	h2 := dedup.HashContent("i prefer python")
	if h1 != h2 {
		t.Errorf("normalised text should produce same hash: %s vs %s", h1, h2)
	}
}

func TestHashContent_DifferentText_DifferentHash(t *testing.T) {
	h1 := dedup.HashContent("I prefer Python")
	h2 := dedup.HashContent("I prefer Go")
	if h1 == h2 {
		t.Errorf("different text should produce different hash")
	}
}

func TestHashContent_Empty_StableHash(t *testing.T) {
	h := dedup.HashContent("")
	if h == "" {
		t.Error("empty string should still produce a hash")
	}
	// Should be deterministic.
	if dedup.HashContent("") != h {
		t.Error("empty hash should be stable")
	}
}

// ── Jaccard similarity (via BoostConfidence as proxy for pkg-level test) ──────

func TestBoostConfidence_BelowOne(t *testing.T) {
	result := dedup.BoostConfidence(0.8)
	expected := 0.85
	if result < expected-0.001 || result > expected+0.001 {
		t.Errorf("expected ~0.85, got %.6f", result)
	}
}

func TestBoostConfidence_AtMax_StaysOne(t *testing.T) {
	result := dedup.BoostConfidence(1.0)
	if result != 1.0 {
		t.Errorf("boost at 1.0 should stay 1.0, got %.4f", result)
	}
}

func TestBoostConfidence_NearMax_ClampsToOne(t *testing.T) {
	result := dedup.BoostConfidence(0.98)
	if result > 1.0 {
		t.Errorf("boosted confidence should not exceed 1.0, got %.4f", result)
	}
}
