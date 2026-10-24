package domain

import (
	"time"

	"github.com/google/uuid"
)

// ── Memory Types ──────────────────────────────────────────────────────────────

// MemoryType classifies what kind of information a memory holds.
type MemoryType string

const (
	MemoryTypeWorking      MemoryType = "WORKING"
	MemoryTypeFact         MemoryType = "FACT"
	MemoryTypePreference   MemoryType = "PREFERENCE"
	MemoryTypeProject      MemoryType = "PROJECT"
	MemoryTypeGoal         MemoryType = "GOAL"
	MemoryTypeInstruction  MemoryType = "INSTRUCTION"
	MemoryTypeSkill        MemoryType = "SKILL"
	MemoryTypeExperience   MemoryType = "EXPERIENCE"
	MemoryTypeDecision     MemoryType = "DECISION"
	MemoryTypeKnowledge    MemoryType = "KNOWLEDGE"
	MemoryTypeRelationship MemoryType = "RELATIONSHIP"
)

// ValidMemoryTypes is the set of accepted memory type values.
var ValidMemoryTypes = map[MemoryType]bool{
	MemoryTypeWorking: true, MemoryTypeFact: true,
	MemoryTypePreference: true, MemoryTypeProject: true,
	MemoryTypeGoal: true, MemoryTypeInstruction: true,
	MemoryTypeSkill: true, MemoryTypeExperience: true,
	MemoryTypeDecision: true, MemoryTypeKnowledge: true,
	MemoryTypeRelationship: true,
}

// ── Memory Status (Lifecycle State Machine) ───────────────────────────────────

// MemoryStatus represents the lifecycle state of a memory.
// State machine: NEW → CANDIDATE → VALIDATED → ACTIVE → UPDATED → STALE → ARCHIVED → DELETED
type MemoryStatus string

const (
	MemoryStatusNew       MemoryStatus = "NEW"
	MemoryStatusCandidate MemoryStatus = "CANDIDATE"
	MemoryStatusValidated MemoryStatus = "VALIDATED"
	MemoryStatusActive    MemoryStatus = "ACTIVE"
	MemoryStatusUpdated   MemoryStatus = "UPDATED"
	MemoryStatusStale     MemoryStatus = "STALE"
	MemoryStatusArchived  MemoryStatus = "ARCHIVED"
	MemoryStatusDeleted   MemoryStatus = "DELETED"
)

// ValidTransitions defines which status transitions are allowed.
// Key = current status, Value = set of valid next statuses.
var ValidTransitions = map[MemoryStatus]map[MemoryStatus]bool{
	MemoryStatusNew: {
		MemoryStatusCandidate: true,
		MemoryStatusActive:    true, // explicit "remember this" skips CANDIDATE
		MemoryStatusDeleted:   true,
	},
	MemoryStatusCandidate: {
		MemoryStatusValidated: true,
		MemoryStatusDeleted:   true,
	},
	MemoryStatusValidated: {
		MemoryStatusActive:  true,
		MemoryStatusDeleted: true,
	},
	MemoryStatusActive: {
		MemoryStatusUpdated:  true,
		MemoryStatusStale:    true,
		MemoryStatusArchived: true,
		MemoryStatusDeleted:  true,
	},
	MemoryStatusUpdated: {
		MemoryStatusArchived: true, // old version gets archived
		MemoryStatusDeleted:  true,
	},
	MemoryStatusStale: {
		MemoryStatusActive:   true, // re-activated if accessed again
		MemoryStatusArchived: true,
		MemoryStatusDeleted:  true,
	},
	MemoryStatusArchived: {
		MemoryStatusActive:  true, // can be restored
		MemoryStatusDeleted: true,
	},
	MemoryStatusDeleted: {}, // terminal state
}

// CanTransitionTo checks if a status change is valid.
func (s MemoryStatus) CanTransitionTo(next MemoryStatus) bool {
	allowed, ok := ValidTransitions[s]
	if !ok {
		return false
	}
	return allowed[next]
}

// ── Source Type ───────────────────────────────────────────────────────────────

type MemorySourceType string

const (
	SourceTypeExplicit  MemorySourceType = "EXPLICIT"
	SourceTypeExtracted MemorySourceType = "EXTRACTED"
	SourceTypeInferred  MemorySourceType = "INFERRED"
	SourceTypeImported  MemorySourceType = "IMPORTED"
	SourceTypeSystem    MemorySourceType = "SYSTEM"
)

// ── Privacy Level ─────────────────────────────────────────────────────────────

type MemoryPrivacy string

const (
	PrivacyPrivate MemoryPrivacy = "PRIVATE"
	PrivacyShared  MemoryPrivacy = "SHARED"
	PrivacyPublic  MemoryPrivacy = "PUBLIC"
)

// ── Memory Level ─────────────────────────────────────────────────────────────

type MemoryLevel string

const (
	MemoryLevelRaw          MemoryLevel = "RAW"
	MemoryLevelExtracted    MemoryLevel = "EXTRACTED"
	MemoryLevelConsolidated MemoryLevel = "CONSOLIDATED"
	MemoryLevelCore         MemoryLevel = "CORE"
)

// ── Memory Relationship ───────────────────────────────────────────────────────

type RelationshipType string

const (
	RelCausedBy   RelationshipType = "CAUSED_BY"
	RelLedTo      RelationshipType = "LED_TO"
	RelSupports   RelationshipType = "SUPPORTS"
	RelContradicts RelationshipType = "CONTRADICTS"
	RelRelatedTo  RelationshipType = "RELATED_TO"
	RelSupersedes RelationshipType = "SUPERSEDES"
	RelDerivedFrom RelationshipType = "DERIVED_FROM"
)

// ── Audit Action ─────────────────────────────────────────────────────────────

type AuditAction string

const (
	AuditCreated           AuditAction = "CREATED"
	AuditStatusChanged     AuditAction = "STATUS_CHANGED"
	AuditContentUpdated    AuditAction = "CONTENT_UPDATED"
	AuditImportanceUpdated AuditAction = "IMPORTANCE_UPDATED"
	AuditConfidenceUpdated AuditAction = "CONFIDENCE_UPDATED"
	AuditArchived          AuditAction = "ARCHIVED"
	AuditDeleted           AuditAction = "DELETED"
	AuditRestored          AuditAction = "RESTORED"
)

// ── Memory ────────────────────────────────────────────────────────────────────

// Memory is the core domain object — one atomic piece of user knowledge.
type Memory struct {
	ID         uuid.UUID    `json:"id"`
	UserID     uuid.UUID    `json:"user_id"`
	Content    string       `json:"content"`
	MemoryType MemoryType   `json:"memory_type"`
	Status     MemoryStatus `json:"status"`

	// Scoring signals.
	Importance float64 `json:"importance"`
	Confidence float64 `json:"confidence"`
	Salience   float64 `json:"salience"`

	// Temporal validity.
	ValidFrom  *time.Time `json:"valid_from,omitempty"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`

	// Provenance.
	SourceType MemorySourceType `json:"source_type"`
	SourceID   *uuid.UUID       `json:"source_id,omitempty"`

	// Hierarchy and namespacing.
	Namespace   string      `json:"namespace"`
	MemoryLevel MemoryLevel `json:"memory_level"`

	// Privacy and retention.
	PrivacyLevel  MemoryPrivacy `json:"privacy_level"`
	RetentionDays *int          `json:"retention_days,omitempty"`
	DeleteAfter   *time.Time    `json:"delete_after,omitempty"`

	// Versioning.
	Version  int        `json:"version"`
	ParentID *uuid.UUID `json:"parent_id,omitempty"`

	// Access tracking.
	LastAccessed time.Time `json:"last_accessed"`
	AccessCount  int       `json:"access_count"`

	// Scoring metadata (Phase 4).
	ImportanceLevel string     `json:"importance_level,omitempty"` // computed by DB
	ScoredAt        *time.Time `json:"scored_at,omitempty"`

	// Deduplication (Phase 5).
	ContentHash string `json:"content_hash,omitempty"` // SHA-256 of normalised content

	// Timestamps.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsActive returns true if this memory participates in retrieval.
func (m *Memory) IsActive() bool {
	return m.Status == MemoryStatusActive
}

// IsExpired returns true if the memory has passed its valid_until date.
func (m *Memory) IsExpired() bool {
	if m.ValidUntil == nil {
		return false
	}
	return time.Now().After(*m.ValidUntil)
}

// ── Input Types ───────────────────────────────────────────────────────────────

// CreateMemoryInput is the validated input for creating a memory.
type CreateMemoryInput struct {
	UserID     uuid.UUID        `json:"user_id"`
	Content    string           `json:"content"`
	MemoryType MemoryType       `json:"memory_type"`
	Importance float64          `json:"importance"`
	Confidence float64          `json:"confidence"`
	Namespace  string           `json:"namespace"`
	SourceType MemorySourceType `json:"source_type"`
	SourceID   *uuid.UUID       `json:"source_id,omitempty"`
	// Explicit = skip CANDIDATE, go straight to ACTIVE.
	Explicit      bool           `json:"explicit"`
	ValidUntil    *time.Time     `json:"valid_until,omitempty"`
	RetentionDays *int           `json:"retention_days,omitempty"`
	PrivacyLevel  MemoryPrivacy  `json:"privacy_level"`
	MemoryLevel   MemoryLevel    `json:"memory_level"`
}

// RememberInput is the input for the explicit "remember this" command.
// Always creates an ACTIVE memory, skipping the CANDIDATE stage.
type RememberInput struct {
	UserID        uuid.UUID     `json:"user_id"`
	Content       string        `json:"content"`
	MemoryType    MemoryType    `json:"memory_type"`
	Importance    float64       `json:"importance"`
	Namespace     string        `json:"namespace"`
	SourceID      *uuid.UUID    `json:"source_id,omitempty"`
	ValidUntil    *time.Time    `json:"valid_until,omitempty"`
	RetentionDays *int          `json:"retention_days,omitempty"`
	PrivacyLevel  MemoryPrivacy `json:"privacy_level,omitempty"`
}

// ForgetInput is the input for the explicit "forget this" command.
type ForgetInput struct {
	UserID uuid.UUID `json:"user_id"`
	Reason string    `json:"reason,omitempty"`
}

// UpdateMemoryInput is the input for updating a memory's content or metadata.
// All fields are optional — only non-nil fields are applied.
type UpdateMemoryInput struct {
	Content    *string        `json:"content,omitempty"`
	Importance *float64       `json:"importance,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Namespace  *string        `json:"namespace,omitempty"`
	ValidUntil *time.Time     `json:"valid_until,omitempty"`
	Privacy    *MemoryPrivacy `json:"privacy_level,omitempty"`
}

// MemoryFilter controls listing and search queries.
type MemoryFilter struct {
	UserID      uuid.UUID
	Status      *MemoryStatus
	MemoryType  *MemoryType
	Namespace   *string
	MemoryLevel *MemoryLevel
	// Only return memories valid at this time.
	ValidAt  *time.Time
	Page     int
	PageSize int
}

// MemoryList is a paginated list of memories.
type MemoryList struct {
	Memories   []*Memory `json:"memories"`
	Total      int64     `json:"total"`
	Page       int       `json:"page"`
	PageSize   int       `json:"page_size"`
	TotalPages int       `json:"total_pages"`
}

// ── Audit Log ─────────────────────────────────────────────────────────────────

// MemoryAuditEntry is one record in the immutable audit trail.
type MemoryAuditEntry struct {
	ID           uuid.UUID     `json:"id"`
	MemoryID     uuid.UUID     `json:"memory_id"`
	UserID       uuid.UUID     `json:"user_id"`
	Action       AuditAction   `json:"action"`
	OldStatus    *MemoryStatus `json:"old_status,omitempty"`
	NewStatus    *MemoryStatus `json:"new_status,omitempty"`
	OldContent   *string       `json:"old_content,omitempty"`
	NewContent   *string       `json:"new_content,omitempty"`
	OldValue     *float64      `json:"old_value,omitempty"`
	NewValue     *float64      `json:"new_value,omitempty"`
	FieldChanged *string       `json:"field_changed,omitempty"`
	ChangedBy    string        `json:"changed_by"`
	Reason       *string       `json:"reason,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

// ── Relationship ──────────────────────────────────────────────────────────────

// MemoryRelationship is a directed edge between two memories.
type MemoryRelationship struct {
	ID               uuid.UUID        `json:"id"`
	SourceMemoryID   uuid.UUID        `json:"source_memory_id"`
	TargetMemoryID   uuid.UUID        `json:"target_memory_id"`
	RelationshipType RelationshipType `json:"relationship_type"`
	Confidence       float64          `json:"confidence"`
	CreatedAt        time.Time        `json:"created_at"`
}
