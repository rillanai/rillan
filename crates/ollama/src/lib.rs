// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Thin async client for the Ollama native HTTP API. Mirrors
//! `internal/ollama/client.go`.

use std::time::Duration;

use reqwest::header::CONTENT_TYPE;
use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Default upstream HTTP timeout. Matches `defaultHTTPTimeout` in the Go repo.
pub const DEFAULT_HTTP_TIMEOUT: Duration = Duration::from_secs(30);

/// HTTP client.
#[derive(Debug, Clone)]
pub struct Client {
    base_url: String,
    http: reqwest::Client,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("marshal request: {0}")]
    Marshal(#[source] serde_json::Error),
    #[error("perform ollama request: {0}")]
    Perform(#[source] reqwest::Error),
    #[error("decode ollama response: {0}")]
    Decode(#[source] reqwest::Error),
    #[error("read ollama response: {0}")]
    Read(#[source] reqwest::Error),
    #[error("ollama returned status {status}: {body}")]
    BadStatus { status: u16, body: String },
    #[error("embed response contained no embeddings")]
    EmptyEmbedding,
}

impl Client {
    /// Builds a client targeting `base_url`.
    #[must_use]
    pub fn new(base_url: impl Into<String>) -> Self {
        let base_url = base_url.into();
        let http = reqwest::Client::builder()
            .timeout(DEFAULT_HTTP_TIMEOUT)
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self::with_client(base_url, http)
    }

    /// Builds a client targeting `base_url` using an existing reqwest client.
    pub fn with_client(base_url: impl Into<String>, http: reqwest::Client) -> Self {
        let base_url = base_url.into().trim_end_matches('/').to_string();
        Self { base_url, http }
    }

    /// Returns the embedding vector produced for `input` by `model`.
    pub async fn embed(&self, model: &str, input: &str) -> Result<Vec<f32>, Error> {
        let payload = EmbedRequest {
            model: model.to_string(),
            input: input.to_string(),
        };
        let url = format!("{}/api/embed", self.base_url);
        let body = serde_json::to_vec(&payload).map_err(Error::Marshal)?;
        let response = self
            .http
            .post(url)
            .header(CONTENT_TYPE, "application/json")
            .body(body)
            .send()
            .await
            .map_err(Error::Perform)?;
        if !response.status().is_success() {
            return Err(read_error(response).await);
        }
        let parsed: EmbedResponse = response.json().await.map_err(Error::Decode)?;
        let first = parsed.embeddings.into_iter().next();
        match first {
            Some(values) if !values.is_empty() => {
                Ok(values.into_iter().map(|v| v as f32).collect())
            }
            _ => Err(Error::EmptyEmbedding),
        }
    }

    /// Runs a single-turn text generation without streaming.
    pub async fn generate(&self, model: &str, prompt: &str) -> Result<String, Error> {
        let payload = GenerateRequest {
            model: model.to_string(),
            prompt: prompt.to_string(),
            stream: false,
        };
        let url = format!("{}/api/generate", self.base_url);
        let body = serde_json::to_vec(&payload).map_err(Error::Marshal)?;
        let response = self
            .http
            .post(url)
            .header(CONTENT_TYPE, "application/json")
            .body(body)
            .send()
            .await
            .map_err(Error::Perform)?;
        if !response.status().is_success() {
            return Err(read_error(response).await);
        }
        let parsed: GenerateResponse = response.json().await.map_err(Error::Decode)?;
        Ok(parsed.response)
    }

    /// Returns Ok when the Ollama server is reachable.
    pub async fn ping(&self) -> Result<(), Error> {
        let response = self
            .http
            .get(format!("{}/", self.base_url))
            .send()
            .await
            .map_err(Error::Perform)?;
        if !response.status().is_success() {
            return Err(read_error(response).await);
        }
        Ok(())
    }
}

#[derive(Debug, Serialize)]
struct EmbedRequest {
    model: String,
    input: String,
}

#[derive(Debug, Deserialize)]
struct EmbedResponse {
    #[serde(default)]
    embeddings: Vec<Vec<f64>>,
}

#[derive(Debug, Serialize)]
struct GenerateRequest {
    model: String,
    prompt: String,
    stream: bool,
}

#[derive(Debug, Deserialize)]
struct GenerateResponse {
    #[serde(default)]
    response: String,
}

async fn read_error(response: reqwest::Response) -> Error {
    let status = response.status().as_u16();
    let body = response.text().await.unwrap_or_default();
    Error::BadStatus {
        status,
        body: body.chars().take(1024).collect(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[tokio::test]
    async fn embed_returns_first_vector() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/embed"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "embeddings": [[0.1, -0.2, 0.3]]
            })))
            .mount(&server)
            .await;

        let client = Client::new(server.uri());
        let vec = client.embed("nomic", "hi").await.expect("embed");
        assert!((vec[0] - 0.1).abs() < 1e-6);
        assert_eq!(vec.len(), 3);
    }

    #[tokio::test]
    async fn generate_decodes_response_field() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/generate"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "response": "hello"
            })))
            .mount(&server)
            .await;

        let client = Client::new(server.uri());
        let text = client.generate("qwen3", "hi").await.expect("generate");
        assert_eq!(text, "hello");
    }

    #[tokio::test]
    async fn ping_requires_2xx() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/"))
            .respond_with(ResponseTemplate::new(503))
            .mount(&server)
            .await;

        let client = Client::new(server.uri());
        let err = client.ping().await.expect_err("ping fail");
        assert!(matches!(err, Error::BadStatus { status: 503, .. }));
    }

    #[tokio::test]
    async fn empty_embedding_response_is_error() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/embed"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "embeddings": []
            })))
            .mount(&server)
            .await;

        let client = Client::new(server.uri());
        let err = client.embed("m", "x").await.expect_err("empty");
        assert!(matches!(err, Error::EmptyEmbedding));
    }
}
