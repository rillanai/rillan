// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Local SQLite-backed code index. Mirrors `internal/index` from the upstream
//! Go repo. ADR-003.
//!
//! Submodules:
//!
//! * [`chunker`] — line-based chunking with stable SHA-256 chunk ids.
//! * [`discovery`] — recursive file walk with glob/include/exclude filters.
//! * [`graphify`] — optional knowledge-graph file discovery + status.
//! * [`vectors`] — float32 embedding encode/decode + a deterministic
//!   placeholder embedding for tests + offline indexing.
//! * [`vector_store`] — `VectorStore` trait + embedded/Ollama implementations.
//! * [`store`] — SQLite + FTS5 schema, replace-all writes, vector + keyword
//!   search.
//! * [`indexer`] — top-level rebuild orchestrator.

pub mod chunker;
pub mod discovery;
pub mod graphify;
pub mod indexer;
mod schema;
pub mod store;
pub mod types;
pub mod vector_store;
pub mod vectors;

pub use chunker::{build_document, chunk_file};
pub use discovery::discover_files;
pub use graphify::{discover_graphify_files, read_graphify_status, GraphifyStatus};
pub use indexer::{read_status, rebuild, RebuildOptions};
pub use store::{default_db_path, Store, StoreError};
pub use types::{
    ChunkRecord, DocumentRecord, RunStatus, SearchResult, SourceFile, Status, VectorRecord,
};
pub use vector_store::{
    EmbeddedVectorStore, Embedder, OllamaVectorStore, VectorStore, VectorStoreError,
    VECTOR_STORE_MODE_EMBEDDED, VECTOR_STORE_MODE_OLLAMA,
};
pub use vectors::{decode_embedding, encode_embedding, placeholder_embedding};
