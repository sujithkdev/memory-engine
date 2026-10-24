-- ============================================================
-- Migration 000007 DOWN
-- ============================================================

DROP FUNCTION  IF EXISTS cosine_similarity(vector, vector);
DROP INDEX     IF EXISTS idx_memories_needs_reembedding;
ALTER TABLE memories DROP COLUMN IF EXISTS needs_reembedding;
DROP TABLE     IF EXISTS embedding_model_registry;
DROP INDEX     IF EXISTS idx_memory_embeddings_model;
DROP INDEX     IF EXISTS idx_memory_embeddings_hnsw;
DROP TABLE     IF EXISTS memory_embeddings;
DROP EXTENSION IF EXISTS vector;
