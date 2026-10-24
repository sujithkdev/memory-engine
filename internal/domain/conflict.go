package domain

import (
	"time"

	"github.com/google/uuid"
)

// ── Conflict types ────────────────────────────────────────────────────────────

type ConflictType string

const (
	ConflictExactDuplicate     ConflictType = "EXACT_DUPLICATE"
	ConflictNearDuplicate      ConflictType = "NEAR_DUPLICATE"
	ConflictDirectContradiction ConflictType = "DIRECT_CONTRADICTION"
	ConflictTemporalUpdate     ConflictType = "TEMPORAL_UPDATE"
	ConflictPartialOverlap     ConflictType = "PARTIAL_OVERLAP"
)

type ResolutionStrategy string

const (
	ResolutionKeepNewer     ResolutionStrategy = "KEEP_NEWER"
	ResolutionKeepHigherConf ResolutionStrategy = "KEEP_HIGHER_CONF"
	ResolutionMerge         ResolutionStrategy = "MERGE"
	ResolutionKeepBoth      ResolutionStrategy = "KEEP_BOTH"
	ResolutionManual        ResolutionStrategy = "MANUAL"
)

type ConflictStatus string

const (
	ConflictDetected ConflictStatus = "DETECTED"
	ConflictResolved ConflictStatus = "RESOLVED"
	ConflictIgnored  ConflictStatus = "IGNORED"
	ConflictManual   ConflictStatus = "MANUAL"
)

// ConflictLogEntry is one record in the conflict log.
type ConflictLogEntry struct {
	ID                 uuid.UUID          `json:"id"`
	UserID             uuid.UUID          `json:"user_id"`
	MemoryAID          uuid.UUID          `json:"memory_a_id"`
	MemoryBID          uuid.UUID          `json:"memory_b_id"`
	ConflictType       ConflictType       `json:"conflict_type"`
	ResolutionStrategy *ResolutionStrategy `json:"resolution_strategy,omitempty"`
	Status             ConflictStatus     `json:"status"`
	SimilarityScore    *float64           `json:"similarity_score,omitempty"`
	WinningMemoryID    *uuid.UUID         `json:"winning_memory_id,omitempty"`
	DetectionReason    *string            `json:"detection_reason,omitempty"`
	ResolutionReason   *string            `json:"resolution_reason,omitempty"`
	ResolvedBy         *string            `json:"resolved_by,omitempty"`
	ResolvedAt         *time.Time         `json:"resolved_at,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
}

// ConflictCheckResult is the output of a conflict check for a candidate memory.
type ConflictCheckResult struct {
	HasConflicts bool               `json:"has_conflicts"`
	Conflicts    []DetectedConflict `json:"conflicts,omitempty"`
}

// DetectedConflict holds details about one found conflict before it is logged.
type DetectedConflict struct {
	ExistingMemory     *Memory            `json:"existing_memory"`
	ConflictType       ConflictType       `json:"conflict_type"`
	SimilarityScore    float64            `json:"similarity_score"`
	SuggestedStrategy  ResolutionStrategy `json:"suggested_strategy"`
	DetectionReason    string             `json:"detection_reason"`
}

// ── Dedup types ────────────────────────────────────────────────────────────────

// DedupAction is what the deduplicator decided to do.
type DedupAction string

const (
	DedupDropped           DedupAction = "DROPPED"            // candidate dropped, existing kept
	DedupMerged            DedupAction = "MERGED"             // confidence boosted on existing
	DedupKeptBoth          DedupAction = "KEPT_BOTH"          // both kept, conflict logged
	DedupUpdatedConfidence DedupAction = "UPDATED_CONFIDENCE" // existing confidence updated
)

// DedupResult is the outcome of running the deduplicator on a candidate memory.
type DedupResult struct {
	Action          DedupAction `json:"action"`
	MatchedMemoryID *uuid.UUID  `json:"matched_memory_id,omitempty"`
	Similarity      float64     `json:"similarity"`
	Reason          string      `json:"reason"`
	// FinalMemory is the memory to use after dedup — may be existing or candidate.
	FinalMemory *Memory `json:"final_memory,omitempty"`
}

// DedupLogEntry is one record in the dedup audit log.
type DedupLogEntry struct {
	ID          uuid.UUID   `json:"id"`
	UserID      uuid.UUID   `json:"user_id"`
	CandidateID uuid.UUID   `json:"candidate_id"`
	MatchedID   uuid.UUID   `json:"matched_id"`
	Similarity  float64     `json:"similarity"`
	Action      DedupAction `json:"action"`
	Reason      *string     `json:"reason,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}
