// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Query rewriter trait + Ollama-backed implementation. Mirrors
//! `internal/retrieval/rewriter.go`.

use async_trait::async_trait;
use thiserror::Error;

const REWRITE_PROMPT: &str = "Rewrite the following user query into a concise search query optimized for semantic similarity search over a code repository. Return only the rewritten query, nothing else.\n\nUser query:\n{query}";

#[derive(Debug, Error)]
pub enum RewriterError {
    #[error("rewrite query: {0}")]
    Ollama(#[from] rillan_ollama::Error),
}

#[async_trait]
pub trait QueryRewriter: Send + Sync {
    async fn rewrite(&self, query: &str) -> Result<String, RewriterError>;
}

#[derive(Debug, Clone)]
pub struct OllamaQueryRewriter {
    client: rillan_ollama::Client,
    model: String,
}

impl OllamaQueryRewriter {
    pub fn new(client: rillan_ollama::Client, model: impl Into<String>) -> Self {
        Self {
            client,
            model: model.into(),
        }
    }
}

#[async_trait]
impl QueryRewriter for OllamaQueryRewriter {
    async fn rewrite(&self, query: &str) -> Result<String, RewriterError> {
        let prompt = REWRITE_PROMPT.replace("{query}", query);
        let result = self.client.generate(&self.model, &prompt).await?;
        let trimmed = result.trim();
        if trimmed.is_empty() {
            Ok(query.to_string())
        } else {
            Ok(trimmed.to_string())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[tokio::test]
    async fn rewriter_returns_trimmed_response() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/generate"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(json!({"response": "  retrieve config docs  "})),
            )
            .mount(&server)
            .await;
        let client = rillan_ollama::Client::new(server.uri());
        let rewriter = OllamaQueryRewriter::new(client, "qwen3");
        let out = rewriter
            .rewrite("how do I configure rillan?")
            .await
            .unwrap();
        assert_eq!(out, "retrieve config docs");
    }

    #[tokio::test]
    async fn rewriter_falls_back_to_original_on_empty() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/generate"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({"response": ""})))
            .mount(&server)
            .await;
        let client = rillan_ollama::Client::new(server.uri());
        let rewriter = OllamaQueryRewriter::new(client, "qwen3");
        let out = rewriter.rewrite("explain index").await.unwrap();
        assert_eq!(out, "explain index");
    }
}
