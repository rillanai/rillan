// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Ollama chat-completions adapter. Mirrors
//! `internal/providers/ollama/client.go`. Translates an OpenAI-style request
//! into a single `/api/generate` call and re-frames the response as an
//! OpenAI-compatible JSON envelope.

use async_trait::async_trait;
use bytes::Bytes;
use http::HeaderMap;
use http::HeaderValue;
use reqwest::header::CONTENT_TYPE;
use rillan_chat::{message_text, ProviderRequest};
use rillan_config::LocalModelProvider;
use serde::Serialize;

use crate::{Provider, ProviderBody, ProviderError, ProviderResponse};

#[derive(Debug, Clone)]
pub struct OllamaProvider {
    client: rillan_ollama::Client,
}

impl OllamaProvider {
    /// Builds the adapter from `cfg`.
    #[must_use]
    pub fn new(cfg: &LocalModelProvider) -> Self {
        Self {
            client: rillan_ollama::Client::new(cfg.base_url.clone()),
        }
    }
}

#[async_trait]
impl Provider for OllamaProvider {
    fn name(&self) -> &str {
        "ollama"
    }

    async fn ready(&self) -> Result<(), ProviderError> {
        self.client.ping().await?;
        Ok(())
    }

    async fn chat_completions(
        &self,
        request: ProviderRequest,
    ) -> Result<ProviderResponse, ProviderError> {
        let prompt = build_prompt(&request).map_err(ProviderError::Translate)?;
        let content = self
            .client
            .generate(&request.request.model, &prompt)
            .await?;
        let response = ChatCompletionResponse {
            id: "chatcmpl-ollama".into(),
            object: "chat.completion".into(),
            created: time::OffsetDateTime::now_utc().unix_timestamp(),
            model: request.request.model.clone(),
            choices: vec![ChatCompletionChoice {
                index: 0,
                message: ChatCompletionReply {
                    role: "assistant".into(),
                    content,
                },
                finish_reason: "stop".into(),
            }],
            usage: ChatCompletionUsageStats::default(),
        };
        let body = serde_json::to_vec(&response).map_err(ProviderError::MarshalPayload)?;
        let mut headers = HeaderMap::new();
        headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
        Ok(ProviderResponse {
            status: http::StatusCode::OK,
            headers,
            body: ProviderBody::Buffered(Bytes::from(body)),
        })
    }
}

#[derive(Debug, Serialize)]
struct ChatCompletionResponse {
    id: String,
    object: String,
    created: i64,
    model: String,
    choices: Vec<ChatCompletionChoice>,
    usage: ChatCompletionUsageStats,
}

#[derive(Debug, Serialize)]
struct ChatCompletionChoice {
    index: i32,
    message: ChatCompletionReply,
    finish_reason: String,
}

#[derive(Debug, Serialize)]
struct ChatCompletionReply {
    role: String,
    content: String,
}

#[derive(Debug, Default, Serialize)]
struct ChatCompletionUsageStats {
    prompt_tokens: i32,
    completion_tokens: i32,
    total_tokens: i32,
}

fn build_prompt(request: &ProviderRequest) -> Result<String, String> {
    let mut parts = Vec::with_capacity(request.request.messages.len() + 1);
    for (idx, message) in request.request.messages.iter().enumerate() {
        let content =
            message_text(message).map_err(|err| format!("read messages[{idx}].content: {err}"))?;
        parts.push(format!("{}: {}", message.role, content));
    }
    parts.push("assistant:".into());
    Ok(parts.join("\n\n"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_chat::Message;
    use rillan_chat::Request;
    use serde_json::Value;
    use std::collections::BTreeMap;

    #[test]
    fn build_prompt_appends_assistant_marker() {
        let request = ProviderRequest {
            request: Request {
                model: "qwen3".into(),
                messages: vec![
                    Message {
                        role: "system".into(),
                        content: Value::String("be terse".into()),
                        extra: BTreeMap::new(),
                    },
                    Message {
                        role: "user".into(),
                        content: Value::String("ping".into()),
                        extra: BTreeMap::new(),
                    },
                ],
                ..Request::default()
            },
            raw_body: Bytes::new(),
        };
        let prompt = build_prompt(&request).expect("build");
        assert!(prompt.ends_with("assistant:"));
        assert!(prompt.contains("system: be terse"));
        assert!(prompt.contains("user: ping"));
    }
}
