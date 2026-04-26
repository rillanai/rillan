// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! SQL schema. Mirrors `internal/index/schema.go` byte-for-byte (modulo
//! comments) so an existing Go-built database opens cleanly.

pub(crate) const CURRENT_SCHEMA_VERSION: i64 = 2;

pub(crate) const BOOTSTRAP_SQL: &str = r#"
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS index_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    root_path TEXT,
    status TEXT NOT NULL,
    documents_count INTEGER NOT NULL DEFAULT 0,
    chunks_count INTEGER NOT NULL DEFAULT 0,
    vectors_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS documents (
    path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    indexed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chunks (
    id TEXT PRIMARY KEY,
    document_path TEXT NOT NULL REFERENCES documents(path) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    chunk_id UNINDEXED,
    document_path UNINDEXED,
    ordinal UNINDEXED,
    start_line UNINDEXED,
    end_line UNINDEXED,
    content,
    tokenize = 'unicode61'
);

CREATE TABLE IF NOT EXISTS vectors (
    chunk_id TEXT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    dimensions INTEGER NOT NULL,
    embedding BLOB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chunks_document_path ON chunks(document_path);
CREATE INDEX IF NOT EXISTS idx_runs_completed_at ON index_runs(completed_at);
"#;
