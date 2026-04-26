// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// OpenAI-compatible chat completion request.
///
/// Unknown top-level fields (`tools`, `tool_choice`, `response_format`,
/// reasoning controls, …) are preserved in `extra` so they can round-trip to
/// the upstream provider unchanged. Serializing emits the unknown fields after
/// the well-known ones in alphabetic order — same behavior as the Go daemon.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ChatCompletionRequest {
    pub model: String,
    pub messages: Vec<Message>,
    #[serde(default, skip_serializing_if = "is_false")]
    pub stream: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub retrieval: Option<RetrievalOptions>,
    /// Forwarded provider-specific fields, sorted alphabetically on encode.
    #[serde(flatten, skip_serializing_if = "BTreeMap::is_empty")]
    pub extra: BTreeMap<String, Value>,
}

impl ChatCompletionRequest {
    /// Returns true when the request payload includes the named extra field.
    #[must_use]
    pub fn has_extra(&self, name: &str) -> bool {
        self.extra.contains_key(name)
    }
}

/// Optional per-request retrieval overrides applied before the policy stage.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct RetrievalOptions {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub top_k: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub max_context_chars: Option<i64>,
}

/// One chat message. Content is stored as a raw [`Value`] so structured
/// (multimodal) inputs and `null` payloads (used with `tool_calls`) round-trip
/// unchanged.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Message {
    pub role: String,
    #[serde(default = "Value::default")]
    pub content: Value,
    #[serde(flatten, skip_serializing_if = "BTreeMap::is_empty")]
    pub extra: BTreeMap<String, Value>,
}

impl Message {
    /// Returns true when the message carries the named extra field.
    #[must_use]
    pub fn has_extra(&self, name: &str) -> bool {
        self.extra.contains_key(name)
    }
}

/// OpenAI-compatible error envelope.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ErrorResponse {
    pub error: ApiError,
}

/// Inner error payload.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ApiError {
    pub message: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub param: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub code: String,
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(value: &bool) -> bool {
    !*value
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_preserves_unknown_fields_in_alphabetic_order() {
        let raw = r#"{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto"}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).expect("parse");
        let encoded = serde_json::to_string(&req).expect("encode");
        assert_eq!(
            encoded,
            r#"{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tool_choice":"auto","tools":[{"type":"function","function":{"name":"lookup"}}]}"#,
        );
    }

    #[test]
    fn structured_content_round_trips() {
        let raw = r#"{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).expect("parse");
        let encoded = serde_json::to_string(&req).expect("encode");
        assert_eq!(encoded, raw);
    }

    #[test]
    fn stream_false_is_omitted() {
        let req = ChatCompletionRequest {
            model: "m".into(),
            messages: vec![Message {
                role: "user".into(),
                content: Value::String("hello".into()),
                extra: BTreeMap::new(),
            }],
            stream: false,
            retrieval: None,
            extra: BTreeMap::new(),
        };
        let encoded = serde_json::to_string(&req).expect("encode");
        assert!(!encoded.contains("\"stream\""), "stream omitted: {encoded}");
    }

    #[test]
    fn message_has_extra_detects_tool_calls() {
        let raw = r#"{"role":"assistant","content":null,"tool_calls":[]}"#;
        let message: Message = serde_json::from_str(raw).expect("parse");
        assert!(message.has_extra("tool_calls"));
    }
}
