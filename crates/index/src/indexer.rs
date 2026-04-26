// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Top-level rebuild orchestrator. Mirrors `internal/index/indexer.go`.

use std::sync::Arc;

use rillan_config::Config;
use thiserror::Error;
use tracing::info;

use crate::chunker::{build_document, chunk_file};
use crate::discovery::{discover_files, DiscoveryError};
use crate::graphify::{discover_graphify_files, GraphifyError};
use crate::store::{default_db_path, Store, StoreError};
use crate::types::{ChunkRecord, DocumentRecord, RunStatus, Status};
use crate::vector_store::{
    new_vector_store, EmbeddedVectorStore, OllamaVectorStore, VectorStore, VectorStoreError,
};

/// Optional inputs to [`rebuild`].
#[derive(Default)]
pub struct RebuildOptions {
    /// Optional embedder. When `Some(...)`, an [`OllamaVectorStore`] replaces
    /// the default [`EmbeddedVectorStore`]; the inner `Embedder` must be
    /// reachable.
    pub embedder: Option<Arc<dyn crate::vector_store::Embedder>>,
}

impl std::fmt::Debug for RebuildOptions {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RebuildOptions")
            .field("has_embedder", &self.embedder.is_some())
            .finish()
    }
}

#[derive(Debug, Error)]
pub enum IndexerError {
    #[error("store: {0}")]
    Store(#[from] StoreError),
    #[error("discovery: {0}")]
    Discovery(#[from] DiscoveryError),
    #[error("graphify: {0}")]
    Graphify(#[from] GraphifyError),
    #[error("vector store: {0}")]
    VectorStore(#[from] VectorStoreError),
}

/// Runs a full index rebuild against `cfg`. Returns the post-rebuild status.
pub async fn rebuild(cfg: &Config, options: RebuildOptions) -> Result<Status, IndexerError> {
    let store = Store::open(default_db_path())?;
    let vector_store: Box<dyn VectorStore> = match options.embedder {
        Some(embedder) => Box::new(OllamaVectorStore::new(
            embedder,
            cfg.local_model.embed_model.clone(),
        )),
        None => match new_vector_store(&cfg.runtime.vector_store_mode) {
            Ok(store) => store,
            Err(err) => return Err(err.into()),
        },
    };
    let _ = EmbeddedVectorStore; // keeps the unused import warning silent in alt branches

    let run_id = store.record_run_start(&cfg.index.root)?;

    let files = match discover_files(&cfg.index) {
        Ok(value) => value,
        Err(err) => {
            let _ =
                store.record_run_completion(run_id, RunStatus::Failed, 0, 0, 0, &err.to_string());
            return Err(err.into());
        }
    };
    let graph_files = match discover_graphify_files(&cfg.knowledge_graph) {
        Ok(value) => value,
        Err(err) => {
            let _ =
                store.record_run_completion(run_id, RunStatus::Failed, 0, 0, 0, &err.to_string());
            return Err(err.into());
        }
    };
    let mut all_files = files;
    all_files.extend(graph_files);

    let mut documents: Vec<DocumentRecord> = Vec::with_capacity(all_files.len());
    let mut chunks: Vec<ChunkRecord> = Vec::new();
    for file in &all_files {
        documents.push(build_document(file));
        let mut file_chunks = chunk_file(file, cfg.index.chunk_size_lines);
        chunks.append(&mut file_chunks);
    }

    let vectors = match vector_store.build_records(&chunks).await {
        Ok(value) => value,
        Err(err) => {
            let _ =
                store.record_run_completion(run_id, RunStatus::Failed, 0, 0, 0, &err.to_string());
            return Err(err.into());
        }
    };

    if let Err(err) = store.replace_all(&documents, &chunks, &vectors) {
        let _ = store.record_run_completion(run_id, RunStatus::Failed, 0, 0, 0, &err.to_string());
        return Err(err.into());
    }

    store.record_run_completion(
        run_id,
        RunStatus::Succeeded,
        documents.len(),
        chunks.len(),
        vectors.len(),
        "",
    )?;

    info!(
        root = %cfg.index.root,
        vector_store = vector_store.mode(),
        documents = documents.len(),
        chunks = chunks.len(),
        vectors = vectors.len(),
        "index rebuild completed",
    );

    let mut status = store.read_status()?;
    status.configured_root_path = cfg.index.root.clone();
    Ok(status)
}

/// Reads the current index status without rebuilding.
pub async fn read_status(cfg: &Config) -> Result<Status, IndexerError> {
    let store = Store::open(default_db_path())?;
    let mut status = store.read_status()?;
    status.configured_root_path = cfg.index.root.clone();
    Ok(status)
}
