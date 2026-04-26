// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Anthropic Messages API adapter. Mirrors
//! `internal/providers/anthropic/client.go`.
//!
//! Translates an OpenAI-compatible chat-completions request into Anthropic's
//! `/v1/messages` payload, drops `system`/`developer` messages into the
//! `system` field, and proxies the response back unchanged.

use std::time::Duration;

use async_trait::async_trait;
use reqwest::header::{HeaderName, HeaderValue, ACCEPT, CONTENT_TYPE};
use rillan_chat::{message_text, ProviderRequest};
use rillan_config::AnthropicConfig;
use serde::Serialize;

use crate::{Provider, ProviderBody, ProviderError, ProviderResponse};

const API_VERSION: &str = "2023-06-01";
const DEFAULT_MAX_TOKENS: u32 = 1024;
const DEFAULT_HTTP_TIMEOUT: Duration = Duration::from_secs(30);
const HEADER_API_KEY: HeaderName = HeaderName::from_static("x-api-key");
const HEADER_VERSION: HeaderName = HeaderName::from_static("anthropic-version");

#[derive(Debug, Clone)]
pub struct AnthropicProvider {
    base_url: String,
    api_key: String,
    client: reqwest::Client,
}

impl AnthropicProvider {
    /// Builds a provider from `cfg` using a freshly-initialized
    /// [`reqwest::Client`] with the default timeout.
    #[must_use]
    pub fn new(cfg: &AnthropicConfig) -> Self {
        let client = reqwest::Client::builder()
            .timeout(DEFAULT_HTTP_TIMEOUT)
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self::with_client(cfg, client)
    }

    /// Builds a provider from `cfg` using an existing reqwest client.
    pub fn with_client(cfg: &AnthropicConfig, client: reqwest::Client) -> Self {
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

    fn api_key_value(&self) -> HeaderValue {
        HeaderValue::from_str(&self.api_key).unwrap_or_else(|_| HeaderValue::from_static(""))
    }
}

#[async_trait]
impl Provider for AnthropicProvider {
    fn name(&self) -> &str {
        "anthropic"
    }

    async fn ready(&self) -> Result<(), ProviderError> {
        let response = self
            .client
            .get(self.url("/v1/models"))
            .header(HEADER_API_KEY, self.api_key_value())
            .header(HEADER_VERSION, HeaderValue::from_static(API_VERSION))
            .header(ACCEPT, "application/json")
            .send()
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
        let translated = translate_request(&request).map_err(ProviderError::Translate)?;
        let body = serde_json::to_vec(&translated).map_err(ProviderError::MarshalPayload)?;
        let response = self
            .client
            .post(self.url("/v1/messages"))
            .header(HEADER_API_KEY, self.api_key_value())
            .header(HEADER_VERSION, HeaderValue::from_static(API_VERSION))
            .header(CONTENT_TYPE, "application/json")
            .header(ACCEPT, "application/json")
            .body(body)
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

#[derive(Debug, Serialize)]
struct MessagesRequest {
    model: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    system: String,
    messages: Vec<MessagesRequestMessage>,
    max_tokens: u32,
    #[serde(skip_serializing_if = "is_false")]
    stream: bool,
}

#[derive(Debug, Serialize)]
struct MessagesRequestMessage {
    role: String,
    content: String,
}

fn translate_request(request: &ProviderRequest) -> Result<MessagesRequest, String> {
    let req = &request.request;
    let mut translated = MessagesRequest {
        model: req.model.clone(),
        system: String::new(),
        messages: Vec::with_capacity(req.messages.len()),
        max_tokens: DEFAULT_MAX_TOKENS,
        stream: req.stream,
    };
    let mut system_parts: Vec<String> = Vec::new();
    for (idx, message) in req.messages.iter().enumerate() {
        let content =
            message_text(message).map_err(|err| format!("read messages[{idx}].content: {err}"))?;
        match message.role.as_str() {
            "system" | "developer" => system_parts.push(content),
            "user" | "assistant" => translated.messages.push(MessagesRequestMessage {
                role: message.role.clone(),
                content,
            }),
            other => {
                return Err(format!(
                    "messages[{idx}].role {other:?} is unsupported for anthropic"
                ));
            }
        }
    }
    if !system_parts.is_empty() {
        translated.system = system_parts.join("\n\n");
    }
    if translated.messages.is_empty() {
        return Err(
            "anthropic requests must include at least one user or assistant message".into(),
        );
    }
    Ok(translated)
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(value: &bool) -> bool {
    !*value
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;
    use rillan_chat::Message;
    use rillan_chat::Request;
    use serde_json::Value;
    use std::collections::BTreeMap;

    fn message(role: &str, content: &str) -> Message {
        Message {
            role: role.to_string(),
            content: Value::String(content.to_string()),
            extra: BTreeMap::new(),
        }
    }

    #[test]
    fn translate_collapses_system_messages() {
        let request = ProviderRequest {
            request: Request {
                model: "claude-sonnet".into(),
                messages: vec![
                    message("system", "be terse"),
                    message("developer", "no markdown"),
                    message("user", "hello"),
                ],
                ..Request::default()
            },
            raw_body: Bytes::new(),
        };
        let translated = translate_request(&request).expect("translate");
        assert_eq!(translated.system, "be terse\n\nno markdown");
        assert_eq!(translated.messages.len(), 1);
        assert_eq!(translated.messages[0].role, "user");
        assert_eq!(translated.max_tokens, DEFAULT_MAX_TOKENS);
    }

    #[test]
    fn translate_rejects_no_user_messages() {
        let request = ProviderRequest {
            request: Request {
                model: "claude-sonnet".into(),
                messages: vec![message("system", "be terse")],
                ..Request::default()
            },
            raw_body: Bytes::new(),
        };
        let err = translate_request(&request).expect_err("must fail");
        assert!(err.contains("at least one user"));
    }

    #[test]
    fn translate_rejects_unknown_role() {
        let request = ProviderRequest {
            request: Request {
                model: "claude-sonnet".into(),
                messages: vec![message("tool", "result")],
                ..Request::default()
            },
            raw_body: Bytes::new(),
        };
        let err = translate_request(&request).expect_err("must fail");
        assert!(err.contains("unsupported for anthropic"));
    }
}
