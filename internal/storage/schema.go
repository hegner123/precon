// Package storage provides SQLite-backed L2 hot storage for the Precon memory multiplex.
package storage

// Schema is the SQLite schema for L2 hot storage.
// It creates the memories table, FTS5 virtual table, and topics table.
//
// NOTE: Existing databases created before this change may still have a
// vestigial "role" column in the memories table. That column was never
// written by the Store() method (it always defaulted to 'user') and is
// not read by any query. SQLite's CREATE TABLE IF NOT EXISTS means the
// schema migration is a no-op on databases that already have the table,
// so the old column persists harmlessly. New databases omit it.
const Schema = `
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    topic_id TEXT DEFAULT '',
    content TEXT NOT NULL,
    is_summary INTEGER DEFAULT 0,
    full_content_ref TEXT DEFAULT '',
    token_count INTEGER DEFAULT 0,
    relevance REAL DEFAULT 1.0,
    tier INTEGER NOT NULL DEFAULT 2,
    keywords TEXT DEFAULT '[]',
    created_at TEXT NOT NULL,
    last_accessed_at TEXT NOT NULL,
    promoted_from INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_memories_conversation ON memories(conversation_id);
CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(topic_id);
CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories(tier);
CREATE INDEX IF NOT EXISTS idx_memories_relevance ON memories(relevance);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    memory_id UNINDEXED,
    content,
    keywords
);

CREATE TABLE IF NOT EXISTS topics (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    name TEXT NOT NULL,
    keywords TEXT DEFAULT '[]',
    relevance REAL DEFAULT 1.0,
    is_current INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_topics_conversation ON topics(conversation_id);
`
