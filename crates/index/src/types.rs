// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use std::path::PathBuf;

/// Run status values stored in the `index_runs` table.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RunStatus {
    NeverIndexed,
    Running,
    Succeeded,
    Failed,
}

impl RunStatus {
    /// Wire string used in the SQLite schema.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::NeverIndexed => "never_indexed",
            Self::Running => "running",
            Self::Succeeded => "succeeded",
            Self::Failed => "failed",
        }
    }

    /// Parses a stored value back into a `RunStatus`. Unknown strings map to
    /// `NeverIndexed` so old DBs don't crash the loader.
    #[must_use]
    pub fn from_str_or_never(value: &str) -> Self {
        match value {
            "running" => Self::Running,
            "succeeded" => Self::Succeeded,
            "failed" => Self::Failed,
            _ => Self::NeverIndexed,
        }
    }
}

/// Indexable file payload.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceFile {
    pub absolute_path: PathBuf,
    pub relative_path: String,
    pub content: String,
    pub size_bytes: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DocumentRecord {
    pub path: String,
    pub content_hash: String,
    pub size_bytes: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ChunkRecord {
    pub id: String,
    pub document_path: String,
    pub ordinal: u32,
    pub start_line: u32,
    pub end_line: u32,
    pub content: String,
    pub content_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VectorRecord {
    pub chunk_id: String,
    pub dimensions: u32,
    pub embedding: Vec<u8>,
}

#[derive(Debug, Clone)]
pub struct SearchResult {
    pub chunk_id: String,
    pub document_path: String,
    pub ordinal: u32,
    pub start_line: u32,
    pub end_line: u32,
    pub content: String,
    pub score: f64,
}

#[derive(Debug, Clone)]
pub struct Status {
    pub configured_root_path: String,
    pub last_attempt_state: RunStatus,
    pub last_attempt_root_path: String,
    pub last_attempt_at: Option<time::OffsetDateTime>,
    pub last_attempt_error: String,
    pub committed_root_path: String,
    pub committed_indexed_at: Option<time::OffsetDateTime>,
    pub documents: u64,
    pub chunks: u64,
    pub vectors: u64,
    pub db_path: PathBuf,
}

impl Default for Status {
    fn default() -> Self {
        Self {
            configured_root_path: String::new(),
            last_attempt_state: RunStatus::NeverIndexed,
            last_attempt_root_path: String::new(),
            last_attempt_at: None,
            last_attempt_error: String::new(),
            committed_root_path: String::new(),
            committed_indexed_at: None,
            documents: 0,
            chunks: 0,
            vectors: 0,
            db_path: PathBuf::new(),
        }
    }
}
