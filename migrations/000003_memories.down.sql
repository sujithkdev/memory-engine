-- ============================================================
-- Migration 000003 DOWN
-- ============================================================

DROP TABLE IF EXISTS memory_relationships;
DROP TABLE IF EXISTS memory_audit_log;
DROP TABLE IF EXISTS memories;

DROP TYPE IF EXISTS memory_relationship_type;
DROP TYPE IF EXISTS audit_action;
DROP TYPE IF EXISTS memory_level;
DROP TYPE IF EXISTS memory_privacy;
DROP TYPE IF EXISTS memory_source_type;
DROP TYPE IF EXISTS memory_status;
DROP TYPE IF EXISTS memory_type;
