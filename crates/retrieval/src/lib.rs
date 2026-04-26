// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Hybrid retrieval pipeline. Mirrors `internal/retrieval` from the Go repo.
//! ADR-009.
//!
//! Modules:
//! * [`embedder`] — `QueryEmbedder` trait + placeholder/Ollama/fallback impls.
//! * [`rewriter`] — `QueryRewriter` trait + Ollama-backed rewriter.
//! * [`pipeline`] — top-level `Pipeline` that runs rewrite → embed → vector
//!   search → keyword search → RRF fusion → context compilation → request
//!   sanitization.

pub mod embedder;
pub mod pipeline;
pub mod rewriter;

pub use embedder::{
    FallbackEmbedder, OllamaEmbedder, PlaceholderEmbedder, QueryEmbedder, QueryEmbedderError,
};
pub use pipeline::{
    build_query, compile_context, resolve_settings, CompiledContext, DebugMetadata, Pipeline,
    PipelineError, Settings, SourceReference,
};
pub use rewriter::{OllamaQueryRewriter, QueryRewriter, RewriterError};
