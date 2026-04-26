// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use serde_json::Value;

use crate::types::{ChatCompletionRequest, Message};

/// Returns the upstream provider capabilities the request requires.
///
/// Today we surface `tool_calling` (when the request carries `tools` or a
/// non-`none` `tool_choice`) and `multimodal` (when any message uses
/// structured-content parts whose type is not `text`).
#[must_use]
pub fn required_capabilities(req: &ChatCompletionRequest) -> Vec<String> {
    let mut capabilities = Vec::new();
    if uses_tools(req) {
        capabilities.push("tool_calling".to_string());
    }
    if uses_multimodal(&req.messages) {
        capabilities.push("multimodal".to_string());
    }
    capabilities.sort();
    capabilities
}

fn uses_tools(req: &ChatCompletionRequest) -> bool {
    if let Some(tools) = req.extra.get("tools") {
        if matches!(tools, Value::Array(items) if !items.is_empty()) {
            return true;
        }
    }
    if let Some(choice) = req.extra.get("tool_choice") {
        return match choice {
            Value::String(s) => {
                let trimmed = s.trim();
                !trimmed.is_empty() && !trimmed.eq_ignore_ascii_case("none")
            }
            Value::Null => false,
            _ => true,
        };
    }
    false
}

fn uses_multimodal(messages: &[Message]) -> bool {
    for message in messages {
        if let Value::Array(parts) = &message.content {
            for part in parts {
                if let Some(part_type) = part.get("type").and_then(Value::as_str) {
                    if !part_type.trim().eq_ignore_ascii_case("text") {
                        return true;
                    }
                }
            }
        }
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_tool_calling_when_tools_provided() {
        let raw = r#"{"model":"m","messages":[{"role":"user","content":"ping"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto"}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert_eq!(
            required_capabilities(&req),
            vec!["tool_calling".to_string()]
        );
    }

    #[test]
    fn detects_multimodal_for_non_text_parts() {
        let raw = r#"{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert_eq!(required_capabilities(&req), vec!["multimodal".to_string()]);
    }

    #[test]
    fn ignores_text_only_structured_content() {
        let raw = r#"{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert!(required_capabilities(&req).is_empty());
    }

    #[test]
    fn ignores_tool_choice_none() {
        let raw =
            r#"{"model":"m","messages":[{"role":"user","content":"ping"}],"tool_choice":"none"}"#;
        let req: ChatCompletionRequest = serde_json::from_str(raw).unwrap();
        assert!(required_capabilities(&req).is_empty());
    }
}
