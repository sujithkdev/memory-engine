-- ============================================================
-- Migration 000005 DOWN
-- ============================================================

DROP TABLE IF EXISTS dedup_log;
DROP TABLE IF EXISTS conflict_log;

DROP INDEX IF EXISTS idx_memories_content_hash;
ALTER TABLE memories DROP COLUMN IF EXISTS content_hash;

DROP TYPE IF EXISTS conflict_status;
DROP TYPE IF EXISTS resolution_strategy;
DROP TYPE IF EXISTS conflict_type;
