// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `POST /v1/agent/proposals/{id}/decision` handler. Mirrors
//! `internal/httpapi/agent_proposal_handler.go`.

use std::sync::Arc;

use axum::extract::{Path, State};
use axum::http::header::CONTENT_TYPE;
use axum::http::{HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};
use bytes::Bytes;
use rillan_agent::{ApprovalGate, GatingError, ProposalError};
use rillan_openai::{ApiError, ErrorResponse};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub(crate) struct AgentProposalDecisionRequest {
    #[serde(default)]
    pub approved: bool,
}

/// Handler entry point. The proposal id is extracted from the path; the body
/// must be a JSON `{"approved": bool}` payload.
pub(crate) async fn handle_proposal_decision(
    State(gate): State<Arc<ApprovalGate>>,
    Path(proposal_id): Path<String>,
    body: Bytes,
) -> Response {
    if proposal_id.trim().is_empty() {
        return error_response(
            StatusCode::BAD_REQUEST,
            "invalid_request_error",
            "proposal id must not be empty",
        );
    }
    if body.len() > 1 << 20 {
        return error_response(
            StatusCode::PAYLOAD_TOO_LARGE,
            "invalid_request_error",
            "request body exceeds 1 MiB limit",
        );
    }
    let request: AgentProposalDecisionRequest = match serde_json::from_slice(&body) {
        Ok(value) => value,
        Err(_) => {
            return error_response(
                StatusCode::BAD_REQUEST,
                "invalid_request_error",
                "request body must be valid JSON",
            );
        }
    };
    match gate.resolve(&proposal_id, request.approved).await {
        Ok(proposal) => json_response(StatusCode::OK, &dto_for(&proposal)),
        Err(GatingError::ApprovalRequired(proposal)) => {
            // Mirror the Go behavior: a denied proposal still returns 200
            // with the proposal payload. The gate has already updated the
            // store and emitted the audit event.
            json_response(StatusCode::OK, &dto_for(&proposal))
        }
        Err(GatingError::Proposal(ProposalError::NotFound)) => error_response(
            StatusCode::NOT_FOUND,
            "not_found_error",
            "proposal not found",
        ),
        Err(err) => {
            tracing::warn!(error = %err, "agent proposal decision failed");
            error_response(
                StatusCode::BAD_REQUEST,
                "invalid_request_error",
                &err.to_string(),
            )
        }
    }
}

#[derive(serde::Serialize)]
struct AgentActionProposalDto {
    id: String,
    kind: String,
    summary: String,
    #[serde(skip_serializing_if = "std::collections::BTreeMap::is_empty")]
    payload: std::collections::BTreeMap<String, String>,
    #[serde(skip_serializing_if = "String::is_empty")]
    request_id: String,
    status: String,
}

fn dto_for(proposal: &rillan_agent::ActionProposal) -> AgentActionProposalDto {
    AgentActionProposalDto {
        id: proposal.id.clone(),
        kind: proposal
            .kind
            .map(|k| k.as_str().to_string())
            .unwrap_or_default(),
        summary: proposal.summary.clone(),
        payload: proposal.payload.clone(),
        request_id: proposal.request_id.clone(),
        status: proposal.status.clone(),
    }
}

fn json_response<T: serde::Serialize>(status: StatusCode, value: &T) -> Response {
    let body = serde_json::to_vec(value).unwrap_or_else(|_| b"{}".to_vec());
    Response::builder()
        .status(status)
        .header(CONTENT_TYPE, HeaderValue::from_static("application/json"))
        .body(axum::body::Body::from(body))
        .unwrap_or_else(|_| status.into_response())
}

fn error_response(status: StatusCode, kind: &str, message: &str) -> Response {
    let payload = ErrorResponse {
        error: ApiError {
            message: message.to_string(),
            kind: kind.to_string(),
            param: String::new(),
            code: String::new(),
        },
    };
    let body = serde_json::to_vec(&payload).unwrap_or_else(|_| b"{}".to_vec());
    Response::builder()
        .status(status)
        .header(CONTENT_TYPE, HeaderValue::from_static("application/json"))
        .body(axum::body::Body::from(body))
        .unwrap_or_else(|_| status.into_response())
}
