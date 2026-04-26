// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

use serde_json::Value;
use thiserror::Error;

use crate::types::{ChatCompletionRequest, Message};

/// Errors surfaced by [`validate_chat_completion_request`].
#[derive(Debug, Error, PartialEq, Eq)]
pub enum ValidateError {
    #[error("model must not be empty")]
    EmptyModel,
    #[error("messages must contain at least one item")]
    EmptyMessages,
    #[error("messages[{index}].role must be one of system, developer, user, assistant, or tool")]
    InvalidRole { index: usize },
    #[error("messages[{index}].content {reason}")]
    InvalidContent { index: usize, reason: &'static str },
    #[error("retrieval.top_k must be greater than zero")]
    RetrievalTopKNonPositive,
    #[error("retrieval.max_context_chars must be greater than zero")]
    RetrievalMaxContextNonPositive,
}

/// Validates a chat completion request against the same rules the Go daemon
/// enforces at the HTTP boundary.
pub fn validate_chat_completion_request(req: &ChatCompletionRequest) -> Result<(), ValidateError> {
    if req.model.trim().is_empty() {
        return Err(ValidateError::EmptyModel);
    }
    if req.messages.is_empty() {
        return Err(ValidateError::EmptyMessages);
    }

    for (index, message) in req.messages.iter().enumerate() {
        if !valid_role(&message.role) {
            return Err(ValidateError::InvalidRole { index });
        }
        if let Err(reason) = validate_message_content(message) {
            return Err(ValidateError::InvalidContent { index, reason });
        }
    }

    if let Some(retrieval) = &req.retrieval {
        if retrieval.top_k.is_some_and(|v| v < 1) {
            return Err(ValidateError::RetrievalTopKNonPositive);
        }
        if retrieval.max_context_chars.is_some_and(|v| v < 1) {
            return Err(ValidateError::RetrievalMaxContextNonPositive);
        }
    }

    Ok(())
}

fn valid_role(role: &str) -> bool {
    matches!(role, "system" | "developer" | "user" | "assistant" | "tool")
}

fn validate_message_content(message: &Message) -> Result<(), &'static str> {
    match &message.content {
        Value::Null => {
            if message.has_extra("tool_calls") {
                Ok(())
            } else {
                Err("must not be null")
            }
        }
        Value::String(text) => {
            if text.trim().is_empty() {
                Err("must not be empty")
            } else {
                Ok(())
            }
        }
        Value::Array(parts) => {
            if parts.is_empty() {
                Err("must not be empty")
            } else {
                Ok(())
            }
        }
        Value::Bool(_) | Value::Number(_) | Value::Object(_) => Ok(()),
    }
}

/// Returns the message text when the content is a plain string. Returns an
/// empty string when the content is structured (multimodal or tool-call).
pub fn message_text(message: &Message) -> Result<String, ValidateError> {
    match &message.content {
        Value::String(text) => Ok(text.clone()),
        Value::Null => Ok(String::new()),
        _ => Ok(String::new()),
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;

    use super::*;
    use crate::types::{Message, RetrievalOptions};

    fn user_text(text: &str) -> Message {
        Message {
            role: "user".into(),
            content: Value::String(text.into()),
            extra: BTreeMap::new(),
        }
    }

    #[test]
    fn accepts_minimal_request() {
        let req = ChatCompletionRequest {
            model: "gpt-4o-mini".into(),
            messages: vec![user_text("hello")],
            ..ChatCompletionRequest::default()
        };
        assert!(validate_chat_completion_request(&req).is_ok());
    }

    #[test]
    fn rejects_missing_model() {
        let req = ChatCompletionRequest {
            messages: vec![user_text("hi")],
            ..ChatCompletionRequest::default()
        };
        assert_eq!(
            validate_chat_completion_request(&req),
            Err(ValidateError::EmptyModel),
        );
    }

    #[test]
    fn accepts_structured_content() {
        let raw = r#"{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert!(validate_chat_completion_request(&req).is_ok());
    }

    #[test]
    fn accepts_assistant_tool_call_envelope() {
        let raw = r#"{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto"}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert!(validate_chat_completion_request(&req).is_ok());
    }

    #[test]
    fn rejects_invalid_retrieval_override() {
        let req = ChatCompletionRequest {
            model: "gpt-4o-mini".into(),
            messages: vec![user_text("hello")],
            retrieval: Some(RetrievalOptions {
                top_k: Some(0),
                ..RetrievalOptions::default()
            }),
            ..ChatCompletionRequest::default()
        };
        assert_eq!(
            validate_chat_completion_request(&req),
            Err(ValidateError::RetrievalTopKNonPositive),
        );
    }

    #[test]
    fn rejects_unknown_role() {
        let req = ChatCompletionRequest {
            model: "m".into(),
            messages: vec![Message {
                role: "robot".into(),
                content: Value::String("hi".into()),
                extra: BTreeMap::new(),
            }],
            ..ChatCompletionRequest::default()
        };
        assert_eq!(
            validate_chat_completion_request(&req),
            Err(ValidateError::InvalidRole { index: 0 }),
        );
    }

    #[test]
    fn rejects_empty_string_content() {
        let req = ChatCompletionRequest {
            model: "m".into(),
            messages: vec![user_text("   ")],
            ..ChatCompletionRequest::default()
        };
        assert!(matches!(
            validate_chat_completion_request(&req),
            Err(ValidateError::InvalidContent { index: 0, .. })
        ));
    }
}
