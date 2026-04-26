// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! SQLite + FTS5 store. Mirrors `internal/index/store.go`.

use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

use rusqlite::{params, Connection, OptionalExtension};
use thiserror::Error;
use time::format_description::well_known::Iso8601;
use time::macros::format_description;
use time::OffsetDateTime;

use crate::schema::{BOOTSTRAP_SQL, CURRENT_SCHEMA_VERSION};
use crate::types::{ChunkRecord, DocumentRecord, RunStatus, SearchResult, Status, VectorRecord};
use crate::vectors::{decode_embedding, VectorError};

#[derive(Debug, Error)]
pub enum StoreError {
    #[error("create index directory: {0}")]
    CreateDir(#[source] std::io::Error),
    #[error("open sqlite store: {0}")]
    Open(#[source] rusqlite::Error),
    #[error("bootstrap schema: {0}")]
    Bootstrap(#[source] rusqlite::Error),
    #[error("record schema version: {0}")]
    Version(#[source] rusqlite::Error),
    #[error("record index run start: {0}")]
    StartRun(#[source] rusqlite::Error),
    #[error("record index run completion: {0}")]
    CompleteRun(#[source] rusqlite::Error),
    #[error("replace index transaction: {0}")]
    Transaction(#[source] rusqlite::Error),
    #[error("read index status: {0}")]
    ReadStatus(#[source] rusqlite::Error),
    #[error("count rows in {0}: {1}")]
    CountRows(&'static str, #[source] rusqlite::Error),
    #[error("query chunk search rows: {0}")]
    SearchVector(#[source] rusqlite::Error),
    #[error("query keyword search rows: {0}")]
    SearchKeyword(#[source] rusqlite::Error),
    #[error("decode embedding for {chunk_id}: {source}")]
    Embedding {
        chunk_id: String,
        #[source]
        source: VectorError,
    },
    #[error(
        "query embedding dimensions {query} do not match stored chunk {chunk_id} dimensions {actual}"
    )]
    DimensionMismatch {
        chunk_id: String,
        query: usize,
        actual: usize,
    },
    #[error("search limit must be greater than zero")]
    InvalidLimit,
}

/// Returns the default index DB path:
/// `<data_dir>/index/index.db`.
#[must_use]
pub fn default_db_path() -> PathBuf {
    rillan_config::default_data_dir()
        .join("index")
        .join("index.db")
}

/// SQLite-backed index store. The connection is wrapped in a `Mutex` so the
/// Tokio runtime can use it across tasks; SQLite itself is single-writer with
/// our pragmas.
#[derive(Clone)]
pub struct Store {
    inner: Arc<StoreInner>,
}

struct StoreInner {
    path: PathBuf,
    conn: Mutex<Connection>,
}

impl std::fmt::Debug for Store {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Store")
            .field("path", &self.inner.path)
            .finish_non_exhaustive()
    }
}

impl Store {
    /// Opens (or creates) the SQLite store at `db_path`.
    pub fn open(db_path: impl Into<PathBuf>) -> Result<Self, StoreError> {
        let db_path = db_path.into();
        if let Some(parent) = db_path.parent() {
            if !parent.as_os_str().is_empty() {
                std::fs::create_dir_all(parent).map_err(StoreError::CreateDir)?;
            }
        }
        let conn = Connection::open(&db_path).map_err(StoreError::Open)?;
        conn.execute_batch(
            "PRAGMA journal_mode=WAL;\n             PRAGMA synchronous=NORMAL;\n             PRAGMA foreign_keys=1;\n             PRAGMA busy_timeout=5000;",
        )
        .map_err(StoreError::Bootstrap)?;
        let store = Self {
            inner: Arc::new(StoreInner {
                path: db_path,
                conn: Mutex::new(conn),
            }),
        };
        store.bootstrap()?;
        Ok(store)
    }

    /// Returns the on-disk path.
    #[must_use]
    pub fn path(&self) -> &Path {
        &self.inner.path
    }

    fn bootstrap(&self) -> Result<(), StoreError> {
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        conn.execute_batch(BOOTSTRAP_SQL)
            .map_err(StoreError::Bootstrap)?;
        let version: Option<i64> = conn
            .query_row("SELECT MAX(version) FROM schema_version", [], |row| {
                row.get::<_, Option<i64>>(0)
            })
            .map_err(StoreError::Version)?;
        if version.is_some_and(|v| v >= CURRENT_SCHEMA_VERSION) {
            return Ok(());
        }
        conn.execute(
            "INSERT INTO schema_version(version) VALUES (?1)",
            params![CURRENT_SCHEMA_VERSION],
        )
        .map_err(StoreError::Version)?;
        Ok(())
    }

    /// Inserts a `running` row in `index_runs` and returns the new id.
    pub fn record_run_start(&self, root: &str) -> Result<i64, StoreError> {
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        conn.execute(
            "INSERT INTO index_runs(root_path, status) VALUES (?1, ?2)",
            params![root, RunStatus::Running.as_str()],
        )
        .map_err(StoreError::StartRun)?;
        Ok(conn.last_insert_rowid())
    }

    /// Updates an existing run row with the final counts and status.
    pub fn record_run_completion(
        &self,
        run_id: i64,
        status: RunStatus,
        documents: usize,
        chunks: usize,
        vectors: usize,
        err_message: &str,
    ) -> Result<(), StoreError> {
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        let err_value: Option<&str> = if err_message.is_empty() {
            None
        } else {
            Some(err_message)
        };
        conn.execute(
            "UPDATE index_runs\n             SET status = ?1, documents_count = ?2, chunks_count = ?3, vectors_count = ?4, error_message = ?5, completed_at = CURRENT_TIMESTAMP\n             WHERE id = ?6",
            params![
                status.as_str(),
                u64::try_from(documents).unwrap_or(u64::MAX),
                u64::try_from(chunks).unwrap_or(u64::MAX),
                u64::try_from(vectors).unwrap_or(u64::MAX),
                err_value,
                run_id,
            ],
        )
        .map_err(StoreError::CompleteRun)?;
        Ok(())
    }

    /// Replaces every document/chunk/vector row with the provided slice. Atomic.
    pub fn replace_all(
        &self,
        documents: &[DocumentRecord],
        chunks: &[ChunkRecord],
        vectors: &[VectorRecord],
    ) -> Result<(), StoreError> {
        let mut conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        let tx = conn.transaction().map_err(StoreError::Transaction)?;
        for stmt in [
            "DELETE FROM vectors",
            "DELETE FROM chunks_fts",
            "DELETE FROM chunks",
            "DELETE FROM documents",
        ] {
            tx.execute(stmt, []).map_err(StoreError::Transaction)?;
        }
        for document in documents {
            tx.execute(
                "INSERT INTO documents(path, content_hash, size_bytes) VALUES (?1, ?2, ?3)",
                params![document.path, document.content_hash, document.size_bytes],
            )
            .map_err(StoreError::Transaction)?;
        }
        for chunk in chunks {
            tx.execute(
                "INSERT INTO chunks(id, document_path, ordinal, start_line, end_line, content, content_hash) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
                params![
                    chunk.id,
                    chunk.document_path,
                    chunk.ordinal,
                    chunk.start_line,
                    chunk.end_line,
                    chunk.content,
                    chunk.content_hash,
                ],
            )
            .map_err(StoreError::Transaction)?;
            tx.execute(
                "INSERT INTO chunks_fts(chunk_id, document_path, ordinal, start_line, end_line, content) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params![
                    chunk.id,
                    chunk.document_path,
                    chunk.ordinal,
                    chunk.start_line,
                    chunk.end_line,
                    chunk.content,
                ],
            )
            .map_err(StoreError::Transaction)?;
        }
        for vector in vectors {
            tx.execute(
                "INSERT INTO vectors(chunk_id, dimensions, embedding) VALUES (?1, ?2, ?3)",
                params![vector.chunk_id, vector.dimensions, vector.embedding],
            )
            .map_err(StoreError::Transaction)?;
        }
        tx.commit().map_err(StoreError::Transaction)?;
        Ok(())
    }

    /// Returns the current state from disk.
    pub fn read_status(&self) -> Result<Status, StoreError> {
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        let mut status = Status {
            db_path: self.inner.path.clone(),
            ..Status::default()
        };

        type LastRunRow = (Option<String>, String, Option<String>, Option<String>);
        let last_row: Option<LastRunRow> = conn
            .query_row(
                "SELECT root_path, status, error_message, completed_at\n                 FROM index_runs ORDER BY id DESC LIMIT 1",
                [],
                |row| {
                    Ok((
                        row.get::<_, Option<String>>(0)?,
                        row.get::<_, String>(1)?,
                        row.get::<_, Option<String>>(2)?,
                        row.get::<_, Option<String>>(3)?,
                    ))
                },
            )
            .optional()
            .map_err(StoreError::ReadStatus)?;
        if let Some((root, state, err, completed)) = last_row {
            status.last_attempt_state = RunStatus::from_str_or_never(&state);
            if let Some(root) = root {
                status.last_attempt_root_path = root;
            }
            if let Some(err) = err {
                status.last_attempt_error = err;
            }
            if let Some(completed) = completed {
                status.last_attempt_at = parse_sqlite_timestamp(&completed);
            }
        }

        status.documents = count_rows(&conn, "documents")?;
        status.chunks = count_rows(&conn, "chunks")?;
        status.vectors = count_rows(&conn, "vectors")?;

        let success_row: Option<(Option<String>, Option<String>)> = conn
            .query_row(
                "SELECT root_path, completed_at FROM index_runs WHERE status = ?1 ORDER BY id DESC LIMIT 1",
                params![RunStatus::Succeeded.as_str()],
                |row| Ok((row.get::<_, Option<String>>(0)?, row.get::<_, Option<String>>(1)?)),
            )
            .optional()
            .map_err(StoreError::ReadStatus)?;
        if let Some((root, completed)) = success_row {
            if let Some(root) = root {
                status.committed_root_path = root;
            }
            if let Some(completed) = completed {
                status.committed_indexed_at = parse_sqlite_timestamp(&completed);
            }
        }
        Ok(status)
    }

    /// Vector similarity search over stored chunks.
    pub fn search_chunks(
        &self,
        query_embedding: &[f32],
        limit: usize,
    ) -> Result<Vec<SearchResult>, StoreError> {
        if limit == 0 {
            return Err(StoreError::InvalidLimit);
        }
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        let mut stmt = conn
            .prepare(
                "SELECT c.id, c.document_path, c.ordinal, c.start_line, c.end_line, c.content, v.embedding\n                 FROM chunks c JOIN vectors v ON v.chunk_id = c.id",
            )
            .map_err(StoreError::SearchVector)?;
        let mut results: Vec<SearchResult> = Vec::new();
        let rows = stmt
            .query_map([], |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, u32>(2)?,
                    row.get::<_, u32>(3)?,
                    row.get::<_, u32>(4)?,
                    row.get::<_, String>(5)?,
                    row.get::<_, Vec<u8>>(6)?,
                ))
            })
            .map_err(StoreError::SearchVector)?;
        for row in rows {
            let (chunk_id, document_path, ordinal, start_line, end_line, content, blob) =
                row.map_err(StoreError::SearchVector)?;
            let embedding = decode_embedding(&blob).map_err(|err| StoreError::Embedding {
                chunk_id: chunk_id.clone(),
                source: err,
            })?;
            if embedding.len() != query_embedding.len() {
                return Err(StoreError::DimensionMismatch {
                    chunk_id,
                    query: query_embedding.len(),
                    actual: embedding.len(),
                });
            }
            let score = cosine_similarity(query_embedding, &embedding);
            results.push(SearchResult {
                chunk_id,
                document_path,
                ordinal,
                start_line,
                end_line,
                content,
                score,
            });
        }
        results.sort_by(|a, b| {
            b.score
                .partial_cmp(&a.score)
                .unwrap_or(std::cmp::Ordering::Equal)
                .then_with(|| a.document_path.cmp(&b.document_path))
                .then_with(|| a.ordinal.cmp(&b.ordinal))
                .then_with(|| a.chunk_id.cmp(&b.chunk_id))
        });
        if results.len() > limit {
            results.truncate(limit);
        }
        Ok(results)
    }

    /// Keyword search via the `chunks_fts` virtual table.
    pub fn search_chunks_keyword(
        &self,
        query: &str,
        limit: usize,
    ) -> Result<Vec<SearchResult>, StoreError> {
        if limit == 0 {
            return Err(StoreError::InvalidLimit);
        }
        let match_query = build_fts_query(query);
        if match_query.is_empty() {
            return Ok(Vec::new());
        }
        let conn = self.inner.conn.lock().expect("store conn mutex poisoned");
        let mut stmt = conn
            .prepare(
                "SELECT chunk_id, document_path, ordinal, start_line, end_line, content\n                 FROM chunks_fts WHERE chunks_fts MATCH ?1 ORDER BY rank LIMIT ?2",
            )
            .map_err(StoreError::SearchKeyword)?;
        let rows = stmt
            .query_map(
                params![match_query, u32::try_from(limit).unwrap_or(u32::MAX)],
                |row| {
                    Ok((
                        row.get::<_, String>(0)?,
                        row.get::<_, String>(1)?,
                        row.get::<_, u32>(2)?,
                        row.get::<_, u32>(3)?,
                        row.get::<_, u32>(4)?,
                        row.get::<_, String>(5)?,
                    ))
                },
            )
            .map_err(StoreError::SearchKeyword)?;
        let mut results: Vec<SearchResult> = Vec::new();
        for (position, row) in rows.enumerate() {
            let (chunk_id, document_path, ordinal, start_line, end_line, content) =
                row.map_err(StoreError::SearchKeyword)?;
            results.push(SearchResult {
                chunk_id,
                document_path,
                ordinal,
                start_line,
                end_line,
                content,
                score: 1.0 / ((position as f64) + 1.0),
            });
        }
        Ok(results)
    }
}

fn count_rows(conn: &Connection, table: &'static str) -> Result<u64, StoreError> {
    conn.query_row(&format!("SELECT COUNT(*) FROM {table}"), [], |row| {
        row.get(0)
    })
    .map_err(|err| StoreError::CountRows(table, err))
}

fn parse_sqlite_timestamp(value: &str) -> Option<OffsetDateTime> {
    let format = format_description!("[year]-[month]-[day] [hour]:[minute]:[second]");
    let primitive = time::PrimitiveDateTime::parse(value, &format).ok()?;
    Some(primitive.assume_utc())
}

#[allow(dead_code)]
fn parse_iso_timestamp(value: &str) -> Option<OffsetDateTime> {
    OffsetDateTime::parse(value, &Iso8601::DEFAULT).ok()
}

fn build_fts_query(query: &str) -> String {
    let mut quoted: Vec<String> = Vec::new();
    let mut seen: std::collections::BTreeSet<String> = std::collections::BTreeSet::new();
    let lowered = query.to_lowercase();
    for part in lowered
        .split(|c: char| !c.is_alphanumeric() && c != '_')
        .map(str::trim)
        .filter(|s| s.len() >= 2)
    {
        if seen.insert(part.to_string()) {
            quoted.push(format!("\"{part}\""));
        }
    }
    quoted.join(" OR ")
}

fn cosine_similarity(left: &[f32], right: &[f32]) -> f64 {
    if left.is_empty() || right.is_empty() || left.len() != right.len() {
        return 0.0;
    }
    let mut dot = 0f64;
    let mut left_norm = 0f64;
    let mut right_norm = 0f64;
    for (l, r) in left.iter().zip(right.iter()) {
        let l = f64::from(*l);
        let r = f64::from(*r);
        dot += l * r;
        left_norm += l * l;
        right_norm += r * r;
    }
    if left_norm == 0.0 || right_norm == 0.0 {
        return 0.0;
    }
    dot / (left_norm.sqrt() * right_norm.sqrt())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn document(path: &str, content: &str) -> DocumentRecord {
        DocumentRecord {
            path: path.into(),
            content_hash: format!("hash:{path}"),
            size_bytes: content.len() as u64,
        }
    }

    fn chunk(path: &str, id: &str, content: &str) -> ChunkRecord {
        ChunkRecord {
            id: id.into(),
            document_path: path.into(),
            ordinal: 0,
            start_line: 1,
            end_line: 1,
            content: content.into(),
            content_hash: format!("hash:{id}"),
        }
    }

    fn vector(id: &str, values: &[f32]) -> VectorRecord {
        VectorRecord {
            chunk_id: id.into(),
            dimensions: values.len() as u32,
            embedding: crate::vectors::encode_embedding(values),
        }
    }

    #[test]
    fn replace_all_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::open(dir.path().join("idx.db")).unwrap();
        store
            .replace_all(
                &[document("a.go", "hello")],
                &[chunk("a.go", "c1", "hello world")],
                &[vector("c1", &[0.1, 0.2, 0.3])],
            )
            .expect("replace");
        let status = store.read_status().expect("status");
        assert_eq!(status.documents, 1);
        assert_eq!(status.chunks, 1);
        assert_eq!(status.vectors, 1);
    }

    #[test]
    fn search_chunks_keyword_finds_matches() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::open(dir.path().join("idx.db")).unwrap();
        store
            .replace_all(
                &[document("a.go", "hello rillan")],
                &[chunk("a.go", "c1", "hello rillan tokenizer")],
                &[],
            )
            .unwrap();
        let results = store.search_chunks_keyword("hello", 5).unwrap();
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].chunk_id, "c1");
    }

    #[test]
    fn build_fts_query_quotes_distinct_terms() {
        let q = build_fts_query("hello, hello WORLD");
        assert_eq!(q, "\"hello\" OR \"world\"");
    }

    #[test]
    fn cosine_similarity_handles_zero_vector() {
        let result = cosine_similarity(&[0.0, 0.0], &[1.0, 1.0]);
        assert_eq!(result, 0.0);
    }
}
