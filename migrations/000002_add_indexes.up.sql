-- ============================================================
-- Migration 000002: Additional indexes for Phase 2 queries
-- ============================================================

-- Composite index for message pagination (most common query pattern).
CREATE INDEX IF NOT EXISTS idx_messages_conv_seq
    ON messages (conversation_id, sequence_number DESC);

-- Index for conversation listing ordered by latest update.
CREATE INDEX IF NOT EXISTS idx_conversations_updated
    ON conversations (user_id, updated_at DESC)
    WHERE is_archived = FALSE;

-- Partial index: active users only.
CREATE INDEX IF NOT EXISTS idx_users_active_created
    ON users (created_at DESC)
    WHERE is_active = TRUE;
