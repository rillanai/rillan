// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `QueryEmbedder` trait + concrete implementations. Mirrors
//! `internal/retrieval/embedder.go`.

use async_trait::async_trait;
use rillan_index::placeholder_embedding;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum QueryEmbedderError {
    #[error("primary embedder is nil")]
    NoPrimary,
    #[error("ollama: {0}")]
    Ollama(#[from] rillan_ollama::Error),
}

/// Produces a vector embedding for a search query.
#[async_trait]
pub trait QueryEmbedder: Send + Sync {
    async fn embed_query(&self, query: &str) -> Result<Vec<f32>, QueryEmbedderError>;
}

/// Placeholder embedder driven by [`placeholder_embedding`].
#[derive(Debug, Default, Clone, Copy)]
pub struct PlaceholderEmbedder;

#[async_trait]
impl QueryEmbedder for PlaceholderEmbedder {
    async fn embed_query(&self, query: &str) -> Result<Vec<f32>, QueryEmbedderError> {
        Ok(placeholder_embedding(query))
    }
}

/// Ollama-backed embedder.
#[derive(Debug, Clone)]
pub struct OllamaEmbedder {
    client: rillan_ollama::Client,
    model: String,
}

impl OllamaEmbedder {
    pub fn new(client: rillan_ollama::Client, model: impl Into<String>) -> Self {
        Self {
            client,
            model: model.into(),
        }
    }
}

#[async_trait]
impl QueryEmbedder for OllamaEmbedder {
    async fn embed_query(&self, query: &str) -> Result<Vec<f32>, QueryEmbedderError> {
        Ok(self.client.embed(&self.model, query).await?)
    }
}

/// Embedder that retries with a fallback when the primary fails.
pub struct FallbackEmbedder {
    primary: Box<dyn QueryEmbedder>,
    fallback: Option<Box<dyn QueryEmbedder>>,
}

impl FallbackEmbedder {
    pub fn new(primary: Box<dyn QueryEmbedder>, fallback: Option<Box<dyn QueryEmbedder>>) -> Self {
        Self { primary, fallback }
    }
}

impl std::fmt::Debug for FallbackEmbedder {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("FallbackEmbedder")
            .field("has_fallback", &self.fallback.is_some())
            .finish()
    }
}

#[async_trait]
impl QueryEmbedder for FallbackEmbedder {
    async fn embed_query(&self, query: &str) -> Result<Vec<f32>, QueryEmbedderError> {
        match self.primary.embed_query(query).await {
            Ok(value) => Ok(value),
            Err(err) => match &self.fallback {
                Some(fallback) => fallback.embed_query(query).await,
                None => Err(err),
            },
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn placeholder_returns_eight_dims() {
        let embedder = PlaceholderEmbedder;
        let v = embedder.embed_query("hi").await.unwrap();
        assert_eq!(v.len(), 8);
    }

    struct AlwaysFails;

    #[async_trait]
    impl QueryEmbedder for AlwaysFails {
        async fn embed_query(&self, _q: &str) -> Result<Vec<f32>, QueryEmbedderError> {
            Err(QueryEmbedderError::NoPrimary)
        }
    }

    #[tokio::test]
    async fn fallback_embedder_uses_secondary_on_error() {
        let embedder =
            FallbackEmbedder::new(Box::new(AlwaysFails), Some(Box::new(PlaceholderEmbedder)));
        let v = embedder.embed_query("hi").await.unwrap();
        assert_eq!(v.len(), 8);
    }

    #[tokio::test]
    async fn fallback_embedder_propagates_when_no_secondary() {
        let embedder = FallbackEmbedder::new(Box::new(AlwaysFails), None);
        let err = embedder.embed_query("hi").await.expect_err("must fail");
        assert!(matches!(err, QueryEmbedderError::NoPrimary));
    }
}
