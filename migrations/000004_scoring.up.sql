-- ============================================================
-- Migration 000004: Scoring enhancements
-- ============================================================
-- Adds:
--   1. A DB function to compute decayed salience (for SQL-level queries)
--   2. A trigger to set initial salience by memory_type on INSERT
--   3. A composite index for scored retrieval ordering
--   4. A scored_at column to track when the score was last computed
-- ============================================================

-- ── 1. Decayed salience function ──────────────────────────────────────────────
-- Mirrors the Go scorer formula: s(t) = max(floor, s0 × e^(-λ × days))
-- Parameters match config defaults (λ=0.023, floor=0.05).
-- Used in SQL ordering before the Go scorer re-scores in memory.
CREATE OR REPLACE FUNCTION decayed_salience(
    s0      FLOAT,
    last_accessed TIMESTAMPTZ,
    lambda  FLOAT DEFAULT 0.023,
    floor   FLOAT DEFAULT 0.05
) RETURNS FLOAT AS $$
DECLARE
    days_since FLOAT;
    decayed    FLOAT;
BEGIN
    days_since := EXTRACT(EPOCH FROM (NOW() - last_accessed)) / 86400.0;
    IF days_since < 0 THEN
        days_since := 0;
    END IF;
    decayed := s0 * EXP(-lambda * days_since);
    RETURN GREATEST(floor, decayed);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- ── 2. Recency score function ─────────────────────────────────────────────────
-- Fast exponential recency score for SQL ordering (half-life ≈ 7 days).
CREATE OR REPLACE FUNCTION recency_score(
    last_accessed TIMESTAMPTZ,
    created_at    TIMESTAMPTZ
) RETURNS FLOAT AS $$
DECLARE
    reference  TIMESTAMPTZ;
    days_since FLOAT;
BEGIN
    reference := GREATEST(last_accessed, created_at);
    days_since := EXTRACT(EPOCH FROM (NOW() - reference)) / 86400.0;
    IF days_since < 0 THEN days_since := 0; END IF;
    RETURN EXP(-0.099 * days_since);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- ── 3. Initial salience trigger ───────────────────────────────────────────────
-- Sets salience based on memory_type when a memory is first inserted.
-- Matches the initialSalience map in config.yaml.
CREATE OR REPLACE FUNCTION trigger_set_initial_salience()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.salience = 0.5 THEN   -- only override the default
        NEW.salience := CASE NEW.memory_type
            WHEN 'WORKING'      THEN 0.9
            WHEN 'INSTRUCTION'  THEN 0.85
            WHEN 'GOAL'         THEN 0.75
            WHEN 'PREFERENCE'   THEN 0.7
            WHEN 'PROJECT'      THEN 0.65
            WHEN 'DECISION'     THEN 0.65
            WHEN 'FACT'         THEN 0.6
            WHEN 'SKILL'        THEN 0.6
            WHEN 'RELATIONSHIP' THEN 0.6
            WHEN 'EXPERIENCE'   THEN 0.55
            WHEN 'KNOWLEDGE'    THEN 0.5
            ELSE 0.5
        END;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_initial_salience
    BEFORE INSERT ON memories
    FOR EACH ROW EXECUTE FUNCTION trigger_set_initial_salience();

-- ── 4. scored_at column ───────────────────────────────────────────────────────
-- Records when the Go scorer last wrote a computed score back to the DB.
-- Populated by Phase 9 background workers.
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS scored_at TIMESTAMPTZ;

-- ── 5. Composite index for scored retrieval ───────────────────────────────────
-- Covers the most common retrieval query:
-- WHERE user_id=? AND namespace=? AND status='ACTIVE'
-- ORDER BY importance DESC, salience DESC
CREATE INDEX IF NOT EXISTS idx_memories_scored_retrieval
    ON memories (user_id, namespace, status, importance DESC, salience DESC)
    WHERE status = 'ACTIVE';

-- ── 6. importance_level computed column (stored) ──────────────────────────────
-- Denormalised tier for fast filtering without Go-side classification.
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS importance_level TEXT
    GENERATED ALWAYS AS (
        CASE
            WHEN importance >= 0.9 THEN 'CRITICAL'
            WHEN importance >= 0.7 THEN 'HIGH'
            WHEN importance >= 0.4 THEN 'MEDIUM'
            WHEN importance >= 0.1 THEN 'LOW'
            ELSE 'TRIVIAL'
        END
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_memories_importance_level
    ON memories (user_id, importance_level, status)
    WHERE status = 'ACTIVE';

COMMENT ON COLUMN memories.scored_at     IS 'When the scoring engine last updated this row. NULL = not yet scored.';
COMMENT ON COLUMN memories.importance_level IS 'Computed importance tier: TRIVIAL/LOW/MEDIUM/HIGH/CRITICAL.';
