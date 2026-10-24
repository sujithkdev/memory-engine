-- ============================================================
-- Migration 000003: Memory System
-- ============================================================
-- Core tables:
--   memories            — the main memory store
--   memory_audit_log    — immutable audit trail of every state transition
--   memory_relationships — links between memories (Phase 5+, created here)
-- ============================================================

-- ============================================================
-- ENUM TYPES
-- ============================================================

-- Memory type: what kind of information this memory holds.
CREATE TYPE memory_type AS ENUM (
    'WORKING',       -- temporary, short-lived context
    'FACT',          -- objective factual information
    'PREFERENCE',    -- user preferences and tastes
    'PROJECT',       -- project-specific knowledge
    'GOAL',          -- user goals and objectives
    'INSTRUCTION',   -- explicit instructions to follow
    'SKILL',         -- skills and capabilities
    'EXPERIENCE',    -- past experiences and events (episodic)
    'DECISION',      -- decisions made and their rationale
    'KNOWLEDGE',     -- general domain knowledge
    'RELATIONSHIP'   -- information about relationships
);

-- Memory status: the lifecycle state machine.
-- NEW → CANDIDATE → VALIDATED → ACTIVE → UPDATED → STALE → ARCHIVED → DELETED
CREATE TYPE memory_status AS ENUM (
    'NEW',          -- just created, not yet processed
    'CANDIDATE',    -- extracted automatically, awaiting validation
    'VALIDATED',    -- confirmed valid, ready for activation
    'ACTIVE',       -- live memory, used in retrieval
    'UPDATED',      -- content changed, supersedes a previous version
    'STALE',        -- not accessed in a long time, candidate for archival
    'ARCHIVED',     -- no longer active but preserved
    'DELETED'       -- soft-deleted, excluded from all operations
);

-- Source type: where the memory came from.
CREATE TYPE memory_source_type AS ENUM (
    'EXPLICIT',      -- user said "remember this"
    'EXTRACTED',     -- LLM extracted from conversation (Phase 11)
    'INFERRED',      -- system inferred from patterns
    'IMPORTED',      -- imported from external source
    'SYSTEM'         -- created by the system itself
);

-- Privacy level: controls sharing scope.
CREATE TYPE memory_privacy AS ENUM (
    'PRIVATE',   -- only visible to the owning user
    'SHARED',    -- can be shared with specific parties
    'PUBLIC'     -- visible to all (future use)
);

-- Memory level: abstraction hierarchy.
CREATE TYPE memory_level AS ENUM (
    'RAW',          -- directly from conversation
    'EXTRACTED',    -- extracted and cleaned
    'CONSOLIDATED', -- merged/consolidated from multiple sources
    'CORE'          -- long-term stable core belief/fact
);

-- Relationship type between memories.
CREATE TYPE memory_relationship_type AS ENUM (
    'CAUSED_BY',
    'LED_TO',
    'SUPPORTS',
    'CONTRADICTS',
    'RELATED_TO',
    'SUPERSEDES',
    'DERIVED_FROM'
);

-- Audit action type.
CREATE TYPE audit_action AS ENUM (
    'CREATED',
    'STATUS_CHANGED',
    'CONTENT_UPDATED',
    'IMPORTANCE_UPDATED',
    'CONFIDENCE_UPDATED',
    'ARCHIVED',
    'DELETED',
    'RESTORED'
);

-- ============================================================
-- MEMORIES
-- ============================================================
CREATE TABLE IF NOT EXISTS memories (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID            NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Core content.
    content         TEXT            NOT NULL,
    memory_type     memory_type     NOT NULL,
    status          memory_status   NOT NULL DEFAULT 'NEW',

    -- Scoring signals (Phase 4 will add logic; fields live here from the start).
    importance      FLOAT           NOT NULL DEFAULT 0.5  CHECK (importance  BETWEEN 0 AND 1),
    confidence      FLOAT           NOT NULL DEFAULT 0.8  CHECK (confidence  BETWEEN 0 AND 1),
    salience        FLOAT           NOT NULL DEFAULT 0.5  CHECK (salience    BETWEEN 0 AND 1),

    -- Temporal validity.
    valid_from      TIMESTAMPTZ     DEFAULT NOW(),
    valid_until     TIMESTAMPTZ,    -- NULL = indefinitely valid

    -- Provenance: where did this memory come from?
    source_type     memory_source_type NOT NULL DEFAULT 'EXPLICIT',
    source_id       UUID,           -- conversation_id or message_id that produced this

    -- Hierarchy and namespacing.
    namespace       TEXT            NOT NULL DEFAULT 'DEFAULT',
    memory_level    memory_level    NOT NULL DEFAULT 'RAW',

    -- Privacy and retention.
    privacy_level   memory_privacy  NOT NULL DEFAULT 'PRIVATE',
    retention_days  INT,            -- NULL = keep forever; set for auto-expiry
    delete_after    TIMESTAMPTZ,    -- explicit expiry timestamp

    -- Versioning: each update creates a new version, old one is UPDATED.
    version         INT             NOT NULL DEFAULT 1,
    parent_id       UUID            REFERENCES memories(id) ON DELETE SET NULL,

    -- Access tracking for salience decay (Phase 9).
    last_accessed   TIMESTAMPTZ     DEFAULT NOW(),
    access_count    INT             NOT NULL DEFAULT 0,

    -- Timestamps.
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- ── Indexes ────────────────────────────────────────────────────────────────────

-- Primary retrieval path: user + status + type (always filter these first).
CREATE INDEX idx_memories_user_status
    ON memories (user_id, status)
    WHERE status NOT IN ('DELETED', 'ARCHIVED');

-- Namespace pre-filter (the scalability lever).
CREATE INDEX idx_memories_namespace
    ON memories (user_id, namespace, status)
    WHERE status = 'ACTIVE';

-- Memory type filter.
CREATE INDEX idx_memories_type
    ON memories (user_id, memory_type, status);

-- Temporal queries.
CREATE INDEX idx_memories_valid_from
    ON memories (user_id, valid_from DESC);

-- Salience decay job.
CREATE INDEX idx_memories_last_accessed
    ON memories (last_accessed ASC)
    WHERE status = 'ACTIVE';

-- Retention/expiry job.
CREATE INDEX idx_memories_delete_after
    ON memories (delete_after)
    WHERE delete_after IS NOT NULL AND status != 'DELETED';

-- Versioning chain.
CREATE INDEX idx_memories_parent
    ON memories (parent_id)
    WHERE parent_id IS NOT NULL;

-- updated_at trigger.
CREATE TRIGGER set_updated_at_memories
    BEFORE UPDATE ON memories
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

COMMENT ON TABLE memories IS 'Core memory store. Every row is one atomic piece of user memory.';
COMMENT ON COLUMN memories.salience   IS 'Decays over time if not accessed. Updated by background worker.';
COMMENT ON COLUMN memories.parent_id  IS 'Points to the previous version when a memory is updated.';
COMMENT ON COLUMN memories.version    IS 'Monotonically increasing version counter per memory lineage.';
COMMENT ON COLUMN memories.delete_after IS 'Hard expiry timestamp. Background worker deletes after this date.';

-- ============================================================
-- MEMORY AUDIT LOG
-- ============================================================
-- Immutable append-only log of every state transition and change.
-- Never update or delete rows in this table.
CREATE TABLE IF NOT EXISTS memory_audit_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id       UUID        NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    action          audit_action NOT NULL,

    -- State transition fields.
    old_status      memory_status,
    new_status      memory_status,

    -- Content snapshot at time of change (for content updates).
    old_content     TEXT,
    new_content     TEXT,

    -- Numeric field changes.
    old_value       FLOAT,
    new_value       FLOAT,
    field_changed   TEXT,       -- which field changed (importance, confidence, etc.)

    -- Who/what caused this change.
    changed_by      TEXT        NOT NULL DEFAULT 'system', -- 'user', 'system', 'llm'
    reason          TEXT,       -- human-readable reason for the change

    -- Immutable timestamp.
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_memory_id
    ON memory_audit_log (memory_id, created_at DESC);

CREATE INDEX idx_audit_user_id
    ON memory_audit_log (user_id, created_at DESC);

CREATE INDEX idx_audit_action
    ON memory_audit_log (action, created_at DESC);

COMMENT ON TABLE memory_audit_log IS 'Immutable audit trail. Never UPDATE or DELETE rows here.';

-- ============================================================
-- MEMORY RELATIONSHIPS
-- ============================================================
-- Links between memories: causality, contradiction, support, etc.
-- Populated in Phase 5, table created now for schema completeness.
CREATE TABLE IF NOT EXISTS memory_relationships (
    id                  UUID                    PRIMARY KEY DEFAULT gen_random_uuid(),
    source_memory_id    UUID                    NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    target_memory_id    UUID                    NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    relationship_type   memory_relationship_type NOT NULL,
    confidence          FLOAT                   NOT NULL DEFAULT 1.0 CHECK (confidence BETWEEN 0 AND 1),
    created_at          TIMESTAMPTZ             NOT NULL DEFAULT NOW(),

    -- Prevent duplicate relationships.
    UNIQUE (source_memory_id, target_memory_id, relationship_type)
);

CREATE INDEX idx_memory_rel_source ON memory_relationships (source_memory_id);
CREATE INDEX idx_memory_rel_target ON memory_relationships (target_memory_id);

COMMENT ON TABLE memory_relationships IS 'Directed graph edges between memories. Used for knowledge graph in Phase 5+.';
