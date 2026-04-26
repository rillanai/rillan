// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Upstream provider seam. Mirrors `internal/providers` from the Go repo.
//!
//! Built-in adapters:
//! * [`OpenAiProvider`] — OpenAI-compatible HTTP API.
//! * [`AnthropicProvider`] — Messages API translation layer.
//! * [`OllamaProvider`] — local Ollama adapter that wraps `/api/generate`.
//! * [`StdioProvider`] — out-of-process module providers via stdin/stdout.

mod anthropic_client;
mod host;
mod ollama_client;
mod openai_client;
mod stdio_client;

pub use anthropic_client::AnthropicProvider;
pub use host::{Host, HostError};
pub use ollama_client::OllamaProvider;
pub use openai_client::OpenAiProvider;
pub use stdio_client::StdioProvider;

use std::fmt::Debug;
use std::pin::Pin;

use async_trait::async_trait;
use bytes::Bytes;
use futures::stream::{Stream, StreamExt};
use http::HeaderMap;
use rillan_chat::ProviderRequest;
use thiserror::Error;

/// Trait implemented by every upstream-provider adapter.
#[async_trait]
pub trait Provider: Debug + Send + Sync {
    /// Adapter name. Used in metrics and tracing fields.
    fn name(&self) -> &str;

    /// Lightweight readiness probe. Returns `Ok(())` when the upstream is
    /// reachable and authenticated.
    async fn ready(&self) -> Result<(), ProviderError>;

    /// Forwards a chat-completions request and returns the upstream response.
    async fn chat_completions(
        &self,
        request: ProviderRequest,
    ) -> Result<ProviderResponse, ProviderError>;
}

/// Decoded upstream response. Body is either an in-memory `Bytes` (legacy
/// buffered path) or a chunked byte stream forwarded straight to the caller
/// for SSE / streaming responses.
pub struct ProviderResponse {
    pub status: http::StatusCode,
    pub headers: HeaderMap,
    pub body: ProviderBody,
}

impl Debug for ProviderResponse {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ProviderResponse")
            .field("status", &self.status)
            .field("headers", &self.headers)
            .field("body", &self.body)
            .finish()
    }
}

/// Stream of body chunks produced by an upstream provider.
pub type ProviderByteStream =
    Pin<Box<dyn Stream<Item = Result<Bytes, ProviderError>> + Send + 'static>>;

/// Upstream response body. Buffered for adapters that pre-buffer (Anthropic
/// translation, Ollama, stdio); streamed for HTTP adapters that hand back the
/// raw upstream bytes.
pub enum ProviderBody {
    Buffered(Bytes),
    Stream(ProviderByteStream),
}

impl ProviderBody {
    /// Reads the entire body into memory. Used by callers that still need a
    /// fully-buffered payload (non-streaming responses).
    pub async fn collect(self) -> Result<Bytes, ProviderError> {
        match self {
            Self::Buffered(bytes) => Ok(bytes),
            Self::Stream(mut stream) => {
                let mut buf = bytes::BytesMut::new();
                while let Some(chunk) = stream.next().await {
                    buf.extend_from_slice(&chunk?);
                }
                Ok(buf.freeze())
            }
        }
    }

    /// Wraps a [`reqwest::Response`] body as a [`ProviderBody::Stream`].
    pub fn from_reqwest(response: reqwest::Response) -> Self {
        let stream = response
            .bytes_stream()
            .map(|item| item.map_err(ProviderError::ReadBody));
        Self::Stream(Box::pin(stream))
    }
}

impl Debug for ProviderBody {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Buffered(b) => f.debug_tuple("Buffered").field(&b.len()).finish(),
            Self::Stream(_) => f.debug_struct("Stream").finish_non_exhaustive(),
        }
    }
}

impl From<Bytes> for ProviderBody {
    fn from(value: Bytes) -> Self {
        Self::Buffered(value)
    }
}

#[derive(Debug, Error)]
pub enum ProviderError {
    #[error("create upstream request: {0}")]
    BuildRequest(#[source] reqwest::Error),
    #[error("perform upstream request: {0}")]
    PerformRequest(#[source] reqwest::Error),
    #[error("read upstream body: {0}")]
    ReadBody(#[source] reqwest::Error),
    #[error("upstream readiness returned status {0}")]
    NotReady(http::StatusCode),
    #[error("upstream ready check failed: {0}")]
    ReadyCheck(#[source] reqwest::Error),
    #[error("translate request: {0}")]
    Translate(String),
    #[error("marshal upstream payload: {0}")]
    MarshalPayload(#[source] serde_json::Error),
    #[error("ollama: {0}")]
    Ollama(#[from] rillan_ollama::Error),
    #[error("stdio adapter: {0}")]
    Stdio(String),
}

#[cfg(test)]
mod body_tests {
    use super::*;
    use futures::stream;

    #[tokio::test]
    async fn collect_buffered_returns_bytes() {
        let body = ProviderBody::Buffered(Bytes::from_static(b"hello world"));
        let collected = body.collect().await.expect("collect");
        assert_eq!(&collected[..], b"hello world");
    }

    #[tokio::test]
    async fn collect_stream_concatenates_chunks() {
        let chunks: Vec<Result<Bytes, ProviderError>> = vec![
            Ok(Bytes::from_static(b"event: chunk\n")),
            Ok(Bytes::from_static(b"data: hi\n\n")),
        ];
        let body = ProviderBody::Stream(Box::pin(stream::iter(chunks)));
        let collected = body.collect().await.expect("collect");
        assert_eq!(&collected[..], b"event: chunk\ndata: hi\n\n");
    }
}
