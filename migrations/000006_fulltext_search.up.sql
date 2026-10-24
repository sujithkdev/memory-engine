-- ============================================================
-- Migration 000006: Full-Text Search
-- ============================================================
-- Adds:
--   1. search_vector (tsvector) column — pre-computed FTS document
--   2. GIN index on search_vector for fast FTS queries
--   3. Trigger to auto-update search_vector on INSERT/UPDATE
--   4. search_rank helper function
-- ============================================================

-- ── 1. Add tsvector column ────────────────────────────────────────────────────
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS search_vector tsvector;

-- ── 2. GIN index for fast full-text search ───────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_memories_fts
    ON memories USING GIN (search_vector)
    WHERE status = 'ACTIVE';

-- Partial index: active memories only (most common search path).
CREATE INDEX IF NOT EXISTS idx_memories_fts_active
    ON memories USING GIN (search_vector)
    WHERE status IN ('ACTIVE', 'VALIDATED');

-- ── 3. Trigger function to keep search_vector current ────────────────────────
-- Weights:
--   A = memory_type (highest — matches on type are very relevant)
--   B = namespace   (high relevance)
--   C = content     (standard — the main search target)
--
-- Uses english dictionary for stemming (stop words, stemming).
-- Switch to 'simple' if non-English content is expected.
CREATE OR REPLACE FUNCTION trigger_update_search_vector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', COALESCE(NEW.memory_type::text, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(NEW.namespace, '')),          'B') ||
        setweight(to_tsvector('english', COALESCE(NEW.content, '')),            'C');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_search_vector
    BEFORE INSERT OR UPDATE OF content, memory_type, namespace
    ON memories
    FOR EACH ROW EXECUTE FUNCTION trigger_update_search_vector();

-- ── 4. Back-fill existing rows ────────────────────────────────────────────────
UPDATE memories SET search_vector =
    setweight(to_tsvector('english', COALESCE(memory_type::text, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(namespace, '')),          'B') ||
    setweight(to_tsvector('english', COALESCE(content, '')),            'C')
WHERE search_vector IS NULL;

-- ── 5. Ranked search helper ───────────────────────────────────────────────────
-- Returns ts_rank_cd score for a query against a memory's search vector.
-- cd = cover density ranking (better for longer documents).
CREATE OR REPLACE FUNCTION fts_rank(vec tsvector, query tsquery) RETURNS FLOAT AS $$
    SELECT ts_rank_cd(vec, query, 32)  -- 32 = normalise by document length
$$ LANGUAGE SQL IMMUTABLE;

COMMENT ON COLUMN memories.search_vector IS 'Pre-computed tsvector for full-text search. Auto-updated by trigger.';
