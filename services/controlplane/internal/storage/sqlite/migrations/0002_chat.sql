-- Native chat is stored with the existing control-plane SQLite state and is
-- therefore included in the same PVC/backup lifecycle as users and settings.
CREATE TABLE chat_conversations (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL REFERENCES users(id),
    model_id   TEXT NOT NULL,
    title      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_chat_conversations_owner_updated ON chat_conversations(owner_id, updated_at DESC);

CREATE TABLE chat_messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content         TEXT NOT NULL,
    status          TEXT NOT NULL CHECK(status IN ('complete', 'streaming', 'stopped', 'failed')),
    sequence        INTEGER NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE(conversation_id, sequence)
);
CREATE INDEX idx_chat_messages_conversation_sequence ON chat_messages(conversation_id, sequence);
