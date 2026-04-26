// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! OpenAI-compatible request/response shapes and validation.
//!
//! Mirrors `internal/openai` in the upstream Go repo. The serializers preserve
//! unknown fields verbatim and emit them in alphabetic order so that downstream
//! providers see exactly what the caller intended even when they use
//! provider-specific extensions (`tools`, `tool_choice`, response formatting,
//! reasoning controls, …).

mod request_features;
mod types;
mod validate;

pub use request_features::required_capabilities;
pub use types::{ApiError, ChatCompletionRequest, ErrorResponse, Message, RetrievalOptions};
pub use validate::{message_text, ValidateError};

pub use validate::validate_chat_completion_request;
