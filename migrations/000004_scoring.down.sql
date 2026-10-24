-- ============================================================
-- Migration 000004 DOWN
-- ============================================================

DROP INDEX IF EXISTS idx_memories_importance_level;
DROP INDEX IF EXISTS idx_memories_scored_retrieval;

ALTER TABLE memories DROP COLUMN IF EXISTS importance_level;
ALTER TABLE memories DROP COLUMN IF EXISTS scored_at;

DROP TRIGGER IF EXISTS set_initial_salience ON memories;
DROP FUNCTION IF EXISTS trigger_set_initial_salience();
DROP FUNCTION IF EXISTS recency_score(TIMESTAMPTZ, TIMESTAMPTZ);
DROP FUNCTION IF EXISTS decayed_salience(FLOAT, TIMESTAMPTZ, FLOAT, FLOAT);
