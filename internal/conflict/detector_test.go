package conflict_test

import (
	"testing"

	"github.com/memory-engine/memory-engine/internal/conflict"
)

// ── Uncertainty propagation ───────────────────────────────────────────────────

func TestPropagateUncertainty_CapsDerived(t *testing.T) {
	// Derived confidence (0.9) > source confidence (0.6) → should be capped.
	result := conflict.PropagateUncertainty(0.6, 0.9)
	if result != 0.6 {
		t.Errorf("expected derived to be capped to source (0.6), got %.4f", result)
	}
}

func TestPropagateUncertainty_DerivedLower_Unchanged(t *testing.T) {
	// Derived confidence (0.4) < source confidence (0.8) → no change.
	result := conflict.PropagateUncertainty(0.8, 0.4)
	if result != 0.4 {
		t.Errorf("expected derived to remain 0.4, got %.4f", result)
	}
}

func TestPropagateUncertainty_Equal_Unchanged(t *testing.T) {
	result := conflict.PropagateUncertainty(0.7, 0.7)
	if result != 0.7 {
		t.Errorf("expected 0.7, got %.4f", result)
	}
}

func TestPropagateUncertainty_ZeroSource_CapsToZero(t *testing.T) {
	result := conflict.PropagateUncertainty(0.0, 0.9)
	if result != 0.0 {
		t.Errorf("zero source should cap derived to 0.0, got %.4f", result)
	}
}

func TestPropagateUncertainty_OneSource_NoChange(t *testing.T) {
	result := conflict.PropagateUncertainty(1.0, 0.5)
	if result != 0.5 {
		t.Errorf("source=1.0 should not cap anything, got %.4f", result)
	}
}
