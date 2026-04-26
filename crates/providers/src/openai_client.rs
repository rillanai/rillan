// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! OpenAI-compatible HTTP client. Mirrors
//! `internal/providers/openai/client.go`.

use std::time::Duration;

use async_trait::async_trait;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE};
use rillan_chat::ProviderRequest;
use rillan_config::OpenAiConfig;
use rillan_observability::REQUEST_ID_HEADER;

use crate::{Provider, ProviderBody, ProviderError, ProviderResponse};

/// Default upstream HTTP timeout. Matches `defaultHTTPTimeout` in the Go repo.
pub(crate) const DEFAULT_HTTP_TIMEOUT: Duration = Duration::from_secs(30);

/// HTTP client for any OpenAI-compatible upstream.
#[derive(Debug, Clone)]
pub struct OpenAiProvider {
    base_url: String,
    api_key: String,
    client: reqwest::Client,
}

impl OpenAiProvider {
    /// Builds a provider from `cfg` using a freshly-initialized
    /// [`reqwest::Client`] with the default timeout.
    #[must_use]
    pub fn new(cfg: &OpenAiConfig) -> Self {
        let client = reqwest::Client::builder()
            .timeout(DEFAULT_HTTP_TIMEOUT)
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self::with_client(cfg, client)
    }

    /// Builds a provider from `cfg` using an existing `reqwest::Client`.
    #[must_use]
    pub fn with_client(cfg: &OpenAiConfig, client: reqwest::Client) -> Self {
        let base_url = cfg.base_url.trim_end_matches('/').to_string();
        Self {
            base_url,
            api_key: cfg.api_key.clone(),
            client,
        }
    }

    fn url(&self, path: &str) -> String {
        format!("{}{}", self.base_url, path)
    }

    fn auth_value(&self) -> String {
        format!("Bearer {}", self.api_key)
    }
}

#[async_trait]
impl Provider for OpenAiProvider {
    fn name(&self) -> &str {
        "openai"
    }

    async fn ready(&self) -> Result<(), ProviderError> {
        let request = self
            .client
            .get(self.url("/models"))
            .header(AUTHORIZATION, self.auth_value())
            .header(ACCEPT, "application/json")
            .build()
            .map_err(ProviderError::BuildRequest)?;

        let response = self
            .client
            .execute(request)
            .await
            .map_err(ProviderError::ReadyCheck)?;
        if !response.status().is_success() {
            return Err(ProviderError::NotReady(response.status()));
        }
        Ok(())
    }

    async fn chat_completions(
        &self,
        request: ProviderRequest,
    ) -> Result<ProviderResponse, ProviderError> {
        let mut builder = self
            .client
            .post(self.url("/chat/completions"))
            .header(AUTHORIZATION, self.auth_value())
            .header(CONTENT_TYPE, "application/json")
            .header(ACCEPT, "application/json")
            .body(request.raw_body);

        if let Some(request_id) = current_request_id() {
            builder = builder.header(REQUEST_ID_HEADER, request_id);
        }

        let response = builder
            .send()
            .await
            .map_err(ProviderError::PerformRequest)?;
        let status = response.status();
        let headers = response.headers().clone();
        Ok(ProviderResponse {
            status,
            headers,
            body: ProviderBody::from_reqwest(response),
        })
    }
}

/// Stub. The full request-id machinery is wired through tracing fields in the
/// HTTP layer; until that lands the provider does not forward an X-Request-ID
/// header. Returning `None` keeps the upstream wire format byte-identical to
/// the Go daemon when no id is present.
fn current_request_id() -> Option<String> {
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;
    use rillan_chat::{ProviderRequest, Request};
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[tokio::test]
    async fn chat_completions_returns_stream_body() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/chat/completions"))
            .respond_with(
                ResponseTemplate::new(200)
                    .insert_header("content-type", "text/event-stream")
                    .set_body_raw(
                        b"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
                            .to_vec(),
                        "text/event-stream",
                    ),
            )
            .mount(&server)
            .await;

        let cfg = rillan_config::OpenAiConfig {
            base_url: server.uri(),
            api_key: "test".into(),
        };
        let provider = OpenAiProvider::new(&cfg);
        let response = provider
            .chat_completions(ProviderRequest {
                request: Request {
                    model: "gpt-4o-mini".into(),
                    stream: true,
                    ..Request::default()
                },
                raw_body: Bytes::from_static(b"{}"),
            })
            .await
            .expect("call");
        assert_eq!(response.status, http::StatusCode::OK);
        assert!(matches!(response.body, ProviderBody::Stream(_)));
        let bytes = response.body.collect().await.expect("collect");
        let text = std::str::from_utf8(&bytes).unwrap();
        assert!(text.contains("data: [DONE]"));
        assert!(text.contains("\"content\":\"hi\""));
    }
}
