-- ============================================================
-- Migration 000005: Deduplication & Conflict Detection
-- ============================================================
-- Adds:
--   1. content_hash column for fast exact-duplicate detection
--   2. conflict_log table — immutable record of every conflict found
--   3. dedup_log table — tracks deduplication decisions
--   4. Index on content_hash for O(1) exact duplicate lookup
--   5. conflict_type enum
-- ============================================================

-- ── Conflict type enum ────────────────────────────────────────────────────────
CREATE TYPE conflict_type AS ENUM (
    'EXACT_DUPLICATE',      -- identical content
    'NEAR_DUPLICATE',       -- very similar content (cosine sim > threshold)
    'DIRECT_CONTRADICTION', -- "I use Python" vs "I use Go"
    'TEMPORAL_UPDATE',      -- "I live in NYC" → "I live in London"
    'PARTIAL_OVERLAP'       -- overlapping but not contradictory
);

-- ── Conflict resolution strategy enum ────────────────────────────────────────
CREATE TYPE resolution_strategy AS ENUM (
    'KEEP_NEWER',        -- keep the newer memory, archive the older
    'KEEP_HIGHER_CONF',  -- keep the one with higher confidence
    'MERGE',             -- merge content from both
    'KEEP_BOTH',         -- keep both, mark as conflicting
    'MANUAL'             -- flagged for human review
);

-- ── Conflict resolution status ────────────────────────────────────────────────
CREATE TYPE conflict_status AS ENUM (
    'DETECTED',   -- conflict found, not yet resolved
    'RESOLVED',   -- resolution applied
    'IGNORED',    -- acknowledged, no action taken
    'MANUAL'      -- escalated for manual review
);

-- ── 1. content_hash on memories ───────────────────────────────────────────────
-- SHA-256 hash of normalised content for exact duplicate detection.
-- Normalisation: lowercase + trim + collapse whitespace (done in Go).
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS content_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_memories_content_hash
    ON memories (user_id, content_hash)
    WHERE content_hash IS NOT NULL AND status != 'DELETED';

-- ── 2. conflict_log ───────────────────────────────────────────────────────────
-- Immutable log of every conflict detected between memories.
CREATE TABLE IF NOT EXISTS conflict_log (
    id                  UUID                PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID                NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The two memories involved.
    memory_a_id         UUID                NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    memory_b_id         UUID                NOT NULL REFERENCES memories(id) ON DELETE CASCADE,

    conflict_type       conflict_type       NOT NULL,
    resolution_strategy resolution_strategy,
    status              conflict_status     NOT NULL DEFAULT 'DETECTED',

    -- Similarity score that triggered conflict detection [0,1].
    similarity_score    FLOAT               CHECK (similarity_score BETWEEN 0 AND 1),

    -- Which memory "won" the resolution (NULL if KEEP_BOTH or MANUAL).
    winning_memory_id   UUID                REFERENCES memories(id) ON DELETE SET NULL,

    -- Explanation of why this conflict was detected and how it was resolved.
    detection_reason    TEXT,
    resolution_reason   TEXT,

    -- Who resolved it.
    resolved_by         TEXT,               -- 'system', 'user', 'llm'
    resolved_at         TIMESTAMPTZ,

    created_at          TIMESTAMPTZ         NOT NULL DEFAULT NOW(),

    -- Prevent logging the same pair twice in DETECTED state.
    UNIQUE (memory_a_id, memory_b_id, conflict_type)
);

CREATE INDEX IF NOT EXISTS idx_conflict_user         ON conflict_log (user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conflict_memory_a     ON conflict_log (memory_a_id);
CREATE INDEX IF NOT EXISTS idx_conflict_memory_b     ON conflict_log (memory_b_id);
CREATE INDEX IF NOT EXISTS idx_conflict_unresolved   ON conflict_log (user_id, status) WHERE status = 'DETECTED';

COMMENT ON TABLE conflict_log IS 'Immutable log of every conflict detected. Never UPDATE rows — only resolve status.';

-- ── 3. dedup_log ──────────────────────────────────────────────────────────────
-- Tracks deduplication decisions for audit and debugging.
CREATE TABLE IF NOT EXISTS dedup_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The candidate (new) memory that was being inserted.
    candidate_id    UUID        NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    -- The existing memory it matched.
    matched_id      UUID        NOT NULL REFERENCES memories(id) ON DELETE CASCADE,

    -- How similar they were [0,1].
    similarity      FLOAT       NOT NULL CHECK (similarity BETWEEN 0 AND 1),

    -- What the deduplicator decided.
    action          TEXT        NOT NULL, -- 'MERGED', 'DROPPED', 'KEPT_BOTH', 'UPDATED_CONFIDENCE'

    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dedup_user      ON dedup_log (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_dedup_candidate ON dedup_log (candidate_id);

COMMENT ON TABLE dedup_log IS 'Audit trail for deduplication decisions.';
