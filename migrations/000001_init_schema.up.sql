-- ============================================================
-- Migration 000001: Initial Schema
-- ============================================================
-- Creates the foundational tables for the memory engine.
-- Designed to be pgvector-ready (vector column added in Phase 4).
--
-- Tables:
--   schema_version  — migration metadata (managed by golang-migrate)
--   users           — system users
--   conversations   — conversation sessions per user
--   messages        — raw message history per conversation
-- ============================================================

-- Enable UUID generation.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- USERS
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username    TEXT        NOT NULL UNIQUE,
    email       TEXT        UNIQUE,
    -- display_name is optional, nullable.
    display_name TEXT,
    -- is_active allows soft-disabling users without deletion.
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    -- Metadata
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Audit: who created this record.
    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL
);

-- Index for active user lookups.
CREATE INDEX idx_users_username   ON users (username);
CREATE INDEX idx_users_email      ON users (email) WHERE email IS NOT NULL;
CREATE INDEX idx_users_is_active  ON users (is_active) WHERE is_active = TRUE;

COMMENT ON TABLE users IS 'System users — each user has isolated memories.';
COMMENT ON COLUMN users.is_active IS 'Soft-delete flag. Inactive users are hidden but data is preserved.';

-- ============================================================
-- CONVERSATIONS
-- ============================================================
CREATE TABLE IF NOT EXISTS conversations (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT,
    -- namespace scopes conversations (e.g., TRADING, CODING, PERSONAL).
    -- Used later as a pre-filter before vector search.
    namespace   TEXT        NOT NULL DEFAULT 'DEFAULT',
    -- is_archived hides conversations from default views.
    is_archived BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Metadata
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_conversations_user_id   ON conversations (user_id);
CREATE INDEX idx_conversations_namespace ON conversations (user_id, namespace);
CREATE INDEX idx_conversations_active    ON conversations (user_id, is_archived) WHERE is_archived = FALSE;

COMMENT ON TABLE conversations IS 'Conversation sessions. Each conversation belongs to one user.';
COMMENT ON COLUMN conversations.namespace IS 'Domain namespace for pre-search filtering (TRADING, CODING, PERSONAL, etc.)';

-- ============================================================
-- MESSAGES
-- ============================================================
CREATE TABLE IF NOT EXISTS messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- role follows LLM convention: user | assistant | system | tool
    role            TEXT        NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
    content         TEXT        NOT NULL,
    -- token_count cached to avoid re-counting in token budget calculations.
    token_count     INT,
    -- sequence_number preserves ordering within a conversation.
    sequence_number INT         NOT NULL,
    -- Metadata
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- source_model records which LLM generated this message (nullable for user messages).
    source_model    TEXT
);

CREATE INDEX idx_messages_conversation_id ON messages (conversation_id, sequence_number);
CREATE INDEX idx_messages_role            ON messages (conversation_id, role);
CREATE INDEX idx_messages_created_at      ON messages (created_at);

COMMENT ON TABLE messages IS 'Raw message history. Immutable append-only log.';
COMMENT ON COLUMN messages.sequence_number IS 'Ordered position within a conversation. Client-assigned or auto-incremented.';
COMMENT ON COLUMN messages.token_count IS 'Cached token count for token budget calculations.';

-- ============================================================
-- updated_at trigger function
-- Automatically updates the updated_at column on row update.
-- ============================================================
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at_users
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_conversations
    BEFORE UPDATE ON conversations
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
