// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Vector-store implementations. Mirrors `internal/index/vector_store.go`.

use async_trait::async_trait;
use thiserror::Error;

use crate::types::{ChunkRecord, VectorRecord};
use crate::vectors::{encode_embedding, placeholder_embedding};

pub const VECTOR_STORE_MODE_EMBEDDED: &str = "embedded";
pub const VECTOR_STORE_MODE_OLLAMA: &str = "ollama";

#[derive(Debug, Error)]
pub enum VectorStoreError {
    #[error("unsupported vector store mode {0:?}")]
    UnsupportedMode(String),
    #[error("embed chunk {chunk_id}: {source}")]
    Embed {
        chunk_id: String,
        #[source]
        source: Box<dyn std::error::Error + Send + Sync>,
    },
}

/// Builds vector records for a slice of chunks.
#[async_trait]
pub trait VectorStore: Send + Sync {
    async fn build_records(
        &self,
        chunks: &[ChunkRecord],
    ) -> Result<Vec<VectorRecord>, VectorStoreError>;
    fn mode(&self) -> &'static str;
}

/// Generates a single embedding vector for a text input.
#[async_trait]
pub trait Embedder: Send + Sync {
    async fn embed(&self, model: &str, text: &str) -> Result<Vec<f32>, EmbedError>;
}

#[derive(Debug, Error)]
#[error("embed: {0}")]
pub struct EmbedError(pub String);

#[derive(Debug, Default, Clone, Copy)]
pub struct EmbeddedVectorStore;

#[async_trait]
impl VectorStore for EmbeddedVectorStore {
    async fn build_records(
        &self,
        chunks: &[ChunkRecord],
    ) -> Result<Vec<VectorRecord>, VectorStoreError> {
        Ok(chunks
            .iter()
            .map(|chunk| {
                let values = placeholder_embedding(&chunk.content);
                VectorRecord {
                    chunk_id: chunk.id.clone(),
                    dimensions: u32::try_from(values.len()).unwrap_or(u32::MAX),
                    embedding: encode_embedding(&values),
                }
            })
            .collect())
    }

    fn mode(&self) -> &'static str {
        VECTOR_STORE_MODE_EMBEDDED
    }
}

/// Builds the configured store given a wire-format mode string.
pub fn new_vector_store(mode: &str) -> Result<Box<dyn VectorStore>, VectorStoreError> {
    match mode {
        "" | VECTOR_STORE_MODE_EMBEDDED => Ok(Box::new(EmbeddedVectorStore)),
        other => Err(VectorStoreError::UnsupportedMode(other.to_string())),
    }
}

/// Vector store backed by a remote [`Embedder`] (Ollama-compatible).
pub struct OllamaVectorStore {
    embedder: std::sync::Arc<dyn Embedder>,
    model: String,
}

impl OllamaVectorStore {
    pub fn new(embedder: std::sync::Arc<dyn Embedder>, model: impl Into<String>) -> Self {
        Self {
            embedder,
            model: model.into(),
        }
    }
}

impl std::fmt::Debug for OllamaVectorStore {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("OllamaVectorStore")
            .field("model", &self.model)
            .finish()
    }
}

#[async_trait]
impl VectorStore for OllamaVectorStore {
    async fn build_records(
        &self,
        chunks: &[ChunkRecord],
    ) -> Result<Vec<VectorRecord>, VectorStoreError> {
        let mut vectors = Vec::with_capacity(chunks.len());
        for chunk in chunks {
            let embedding = self
                .embedder
                .embed(&self.model, &chunk.content)
                .await
                .map_err(|err| VectorStoreError::Embed {
                    chunk_id: chunk.id.clone(),
                    source: Box::new(err),
                })?;
            vectors.push(VectorRecord {
                chunk_id: chunk.id.clone(),
                dimensions: u32::try_from(embedding.len()).unwrap_or(u32::MAX),
                embedding: encode_embedding(&embedding),
            });
        }
        Ok(vectors)
    }

    fn mode(&self) -> &'static str {
        VECTOR_STORE_MODE_OLLAMA
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn chunk(id: &str, content: &str) -> ChunkRecord {
        ChunkRecord {
            id: id.into(),
            document_path: "main.go".into(),
            ordinal: 0,
            start_line: 1,
            end_line: 1,
            content: content.into(),
            content_hash: String::new(),
        }
    }

    #[tokio::test]
    async fn embedded_store_produces_one_record_per_chunk() {
        let store = EmbeddedVectorStore;
        let chunks = vec![chunk("a", "hello"), chunk("b", "world")];
        let records = store.build_records(&chunks).await.expect("build");
        assert_eq!(records.len(), 2);
        assert_eq!(records[0].dimensions, 8);
    }

    #[test]
    fn new_vector_store_rejects_unknown_mode() {
        match new_vector_store("custom") {
            Ok(_) => panic!("expected unsupported mode"),
            Err(err) => assert!(matches!(err, VectorStoreError::UnsupportedMode(_))),
        }
    }
}
