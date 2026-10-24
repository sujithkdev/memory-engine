-- ============================================================
-- Migration 000007: pgvector + Embedding Storage
-- ============================================================
-- Adds:
--   1. pgvector extension
--   2. memory_embeddings table with HNSW index
--   3. embedding_model_version table for tracking model changes
--   4. needs_reembedding flag on memories for re-embed jobs
-- ============================================================

-- ── 1. Enable pgvector ────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS vector;

-- ── 2. memory_embeddings ─────────────────────────────────────────────────────
-- Stores one embedding per memory per model.
-- Separated from memories table to keep the main table lean and
-- allow multiple embeddings per memory (model migrations).
CREATE TABLE IF NOT EXISTS memory_embeddings (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id       UUID        NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    -- The embedding vector. Dimension 1536 covers most models
    -- (OpenAI ada-002, nomic-embed-text). Qwen3-Embedding uses 1024.
    -- We use 1536 as the max; smaller models zero-pad or truncate.
    -- Phase 7: dimension is configurable at embedding time.
    embedding       vector(1536),
    embedding_model TEXT        NOT NULL,   -- e.g. "nomic-embed-text", "qwen3-embedding"
    model_version   TEXT,                   -- model version/hash for re-embed detection
    dimensions      INT         NOT NULL,   -- actual vector dimensions used
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One active embedding per memory per model.
    UNIQUE (memory_id, embedding_model)
);

-- ── HNSW index for approximate nearest-neighbor search ───────────────────────
-- HNSW is faster at query time than IVFFlat, better for our use case.
-- ef_construction=64, m=16 are good defaults for <1M vectors.
-- cosine distance: best for text embeddings (length-invariant).
CREATE INDEX IF NOT EXISTS idx_memory_embeddings_hnsw
    ON memory_embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Index for model-based lookups (re-embedding job).
CREATE INDEX IF NOT EXISTS idx_memory_embeddings_model
    ON memory_embeddings (embedding_model, created_at DESC);

CREATE TRIGGER set_updated_at_embeddings
    BEFORE UPDATE ON memory_embeddings
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

COMMENT ON TABLE memory_embeddings IS 'Vector embeddings for memories. One row per memory per model.';
COMMENT ON COLUMN memory_embeddings.embedding IS 'Vector embedding. Dimension matches the model used.';

-- ── 3. embedding_model_registry ──────────────────────────────────────────────
-- Tracks which embedding models have been used.
-- The background re-embedding job uses this to find stale embeddings.
CREATE TABLE IF NOT EXISTS embedding_model_registry (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    model_name      TEXT        NOT NULL UNIQUE,
    model_version   TEXT,
    dimensions      INT         NOT NULL,
    is_active       BOOLEAN     NOT NULL DEFAULT FALSE,  -- only one active at a time
    activated_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE embedding_model_registry IS 'Registry of embedding models. is_active=true = current model.';

-- ── 4. needs_reembedding flag on memories ────────────────────────────────────
-- Set to TRUE when content changes or active model changes.
-- Background worker processes these.
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS needs_reembedding BOOLEAN NOT NULL DEFAULT TRUE;

-- Index for re-embedding job.
CREATE INDEX IF NOT EXISTS idx_memories_needs_reembedding
    ON memories (needs_reembedding, status)
    WHERE needs_reembedding = TRUE AND status = 'ACTIVE';

-- ── 5. Cosine similarity helper ───────────────────────────────────────────────
-- Returns cosine similarity [0,1] between two vectors.
-- Used in hybrid search ranking.
CREATE OR REPLACE FUNCTION cosine_similarity(a vector, b vector) RETURNS FLOAT AS $$
    SELECT 1 - (a <=> b)
$$ LANGUAGE SQL IMMUTABLE;

COMMENT ON FUNCTION cosine_similarity IS 'Cosine similarity between two vectors: 1 - cosine_distance.';
