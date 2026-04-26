// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! HTTP router, handlers, and middleware for the Rillan daemon.
//!
//! Mirrors the public surface of `internal/httpapi`. Delivers
//! `GET /healthz`, `GET /readyz`, `POST /v1/chat/completions`,
//! `POST /v1/agent/tasks`, `POST /v1/agent/proposals/{id}/decision`, and the
//! loopback-only `POST /admin/runtime/refresh` admin endpoint.

mod admin_reload;
mod agent_proposal;
mod agent_task;
mod auth_middleware;
mod chat;
mod health;
mod middleware;
mod router;
mod runtime_snapshot;

pub use admin_reload::{RefreshFn, ADMIN_RUNTIME_REFRESH_PATH};
pub use agent_task::AgentTaskState;
pub use auth_middleware::{require_bearer, BearerFn, BearerResolver};
pub use middleware::{
    emit_request_id, header_name, request_id_layer, RequestIdHandler, RequestIdLayer,
};
pub use router::{build_router, RouterError, RouterOptions, RuntimeSnapshotFn};
pub use runtime_snapshot::{OllamaChecker, ReadinessInfo, RuntimeSnapshot};
