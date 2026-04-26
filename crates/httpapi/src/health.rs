// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `/healthz` and `/readyz` handlers. Mirrors
//! `internal/httpapi/health_handler.go`.

use std::time::Duration;

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde_json::{json, Value};

use crate::router::SharedState;

const READINESS_TIMEOUT: Duration = Duration::from_secs(3);

/// `GET /healthz` — returns `{"status":"ok"}`.
pub(crate) async fn handle_healthz() -> impl IntoResponse {
    Json(json!({"status": "ok"}))
}

/// `GET /readyz` — performs a bounded readiness check against the active
/// upstream provider, plus an optional Ollama probe when local-model is
/// required.
pub(crate) async fn handle_readyz(State(state): State<SharedState>) -> Response {
    let snapshot = state.snapshot();
    let provider = snapshot.provider.clone();
    let ollama = snapshot.ollama_checker.clone();
    let local_required = snapshot.readiness.local_model_required;

    let mut payload = json!({
        "status": "ready",
        "runtime": {
            "retrieval_mode": snapshot.readiness.retrieval_mode,
            "system_config_loaded": snapshot.readiness.system_config_loaded,
            "audit_ledger_path": snapshot.readiness.audit_ledger_path,
            "local_model_required": snapshot.readiness.local_model_required,
            "modules_discovered": snapshot.readiness.modules_discovered,
            "modules_enabled": snapshot.readiness.modules_enabled,
        }
    });

    let ready_check = tokio::time::timeout(READINESS_TIMEOUT, provider.ready()).await;
    match ready_check {
        Ok(Ok(())) => {}
        Ok(Err(err)) => {
            payload["status"] = Value::String("degraded".into());
            payload["provider"] = json!({
                "status": "unavailable",
                "error": err.to_string(),
            });
            return (StatusCode::SERVICE_UNAVAILABLE, Json(payload)).into_response();
        }
        Err(_timeout) => {
            payload["status"] = Value::String("degraded".into());
            payload["provider"] = json!({
                "status": "unavailable",
                "error": "readiness check timed out",
            });
            return (StatusCode::SERVICE_UNAVAILABLE, Json(payload)).into_response();
        }
    }

    if let Some(checker) = ollama {
        let probe = tokio::time::timeout(READINESS_TIMEOUT, checker.ready()).await;
        match probe {
            Ok(Ok(())) => {
                payload["local_model"] = json!({"status": "available"});
            }
            Ok(Err(err)) => {
                payload["local_model"] = json!({
                    "status": "unavailable",
                    "error": err,
                });
                if local_required {
                    payload["status"] = Value::String("degraded".into());
                    return (StatusCode::SERVICE_UNAVAILABLE, Json(payload)).into_response();
                }
            }
            Err(_timeout) => {
                payload["local_model"] = json!({
                    "status": "unavailable",
                    "error": "readiness check timed out",
                });
                if local_required {
                    payload["status"] = Value::String("degraded".into());
                    return (StatusCode::SERVICE_UNAVAILABLE, Json(payload)).into_response();
                }
            }
        }
    }

    (StatusCode::OK, Json(payload)).into_response()
}
