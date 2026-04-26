// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Thin shared-type crate that mirrors `internal/chat` in the Go repo.
//!
//! Re-exports the OpenAI-compatible request types and adds the
//! [`ProviderRequest`] envelope passed from the HTTP layer down to providers.

use bytes::Bytes;
pub use rillan_openai::{
    message_text, ChatCompletionRequest as Request, Message, RetrievalOptions, ValidateError,
};

/// Request envelope handed to a provider adapter. Carries both the parsed
/// request shape and the original (possibly redacted) request body bytes.
#[derive(Debug, Clone)]
pub struct ProviderRequest {
    pub request: Request,
    pub raw_body: Bytes,
}
