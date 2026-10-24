-- ============================================================
-- Migration 000006 DOWN
-- ============================================================

DROP TRIGGER  IF EXISTS update_search_vector ON memories;
DROP FUNCTION IF EXISTS trigger_update_search_vector();
DROP FUNCTION IF EXISTS fts_rank(tsvector, tsquery);

DROP INDEX IF EXISTS idx_memories_fts_active;
DROP INDEX IF EXISTS idx_memories_fts;

ALTER TABLE memories DROP COLUMN IF EXISTS search_vector;
