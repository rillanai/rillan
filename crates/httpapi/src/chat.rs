// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `POST /v1/chat/completions` handler.
//!
//! Pipeline:
//! 1. Decode the request body into [`rillan_chat::Request`].
//! 2. Validate the OpenAI-compatible shape.
//! 3. Run the regex-based secret scanner.
//! 4. Evaluate policy. Block / redact / local-only verdicts return early
//!    with an OpenAI-compatible error envelope.
//! 5. Forward the (possibly redacted) request to the upstream provider.
//! 6. Relay status, content-type, and body to the caller.
//!
//! Mirrors the egress slice of `internal/httpapi/chat_completions_handler.go`.
//! Audit-ledger wiring, retrieval injection, classifier integration, and
//! decision-trace headers are roadmap items.

use axum::extract::State;
use axum::http::header::CONTENT_TYPE;
use axum::http::{HeaderMap, HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};
use bytes::Bytes;
use rillan_chat::ProviderRequest;
use rillan_openai::{validate_chat_completion_request, ApiError, ErrorResponse};
use rillan_policy::{EvaluationInput, EvaluationPhase, EvaluationResult, RuntimePolicy, Verdict};
use rillan_providers::{ProviderBody, ProviderResponse};

use crate::router::SharedState;

const MAX_REQUEST_BYTES: usize = 1024 * 1024;

/// Entry point for `POST /v1/chat/completions`.
pub(crate) async fn handle_chat_completions(
    State(state): State<SharedState>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    if body.len() > MAX_REQUEST_BYTES {
        return error_response(
            StatusCode::PAYLOAD_TOO_LARGE,
            "invalid_request_error",
            "request body exceeds 1 MiB limit",
        );
    }
    if !json_content_type(&headers) {
        return error_response(
            StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "invalid_request_error",
            "content-type must be application/json",
        );
    }

    let parsed: rillan_chat::Request = match serde_json::from_slice(&body) {
        Ok(value) => value,
        Err(err) => {
            return error_response(
                StatusCode::BAD_REQUEST,
                "invalid_request_error",
                &format!("invalid request body: {err}"),
            );
        }
    };

    if let Err(err) = validate_chat_completion_request(&parsed) {
        return error_response(
            StatusCode::BAD_REQUEST,
            "invalid_request_error",
            &err.to_string(),
        );
    }

    let snapshot = state.snapshot();
    let scanner = state.scanner().clone();
    let evaluator = state.evaluator().clone();

    let scan = scanner.scan(&body);
    let evaluation = match evaluator.evaluate(EvaluationInput {
        project: snapshot.project_config.clone(),
        runtime: RuntimePolicy::default(),
        request: Some(parsed.clone()),
        body: body.to_vec(),
        scan,
        classification: None,
        phase: Some(EvaluationPhase::Egress),
    }) {
        Ok(result) => result,
        Err(err) => {
            tracing::error!(error = %err, "policy evaluation failed");
            return error_response(
                StatusCode::INTERNAL_SERVER_ERROR,
                "policy_error",
                "policy evaluation failed",
            );
        }
    };

    if let Some(early) = early_return_for_verdict(&evaluation) {
        return early;
    }

    let provider = snapshot.provider.clone();
    let provider_request = ProviderRequest {
        request: evaluation.request.clone().unwrap_or_else(|| parsed.clone()),
        raw_body: Bytes::from(evaluation.body.clone()),
    };

    match provider.chat_completions(provider_request).await {
        Ok(response) => proxy_response(&parsed, response).await,
        Err(err) => {
            tracing::error!(error = %err, "upstream provider failed");
            error_response(
                StatusCode::BAD_GATEWAY,
                "upstream_error",
                &format!("upstream provider error: {err}"),
            )
        }
    }
}

fn early_return_for_verdict(evaluation: &EvaluationResult) -> Option<Response> {
    match evaluation.verdict {
        Verdict::Allow | Verdict::Redact => None,
        Verdict::Block => Some(error_response(
            StatusCode::BAD_REQUEST,
            "policy_block",
            &format!("request blocked by policy: {}", evaluation.reason),
        )),
        Verdict::LocalOnly => Some(error_response(
            StatusCode::FORBIDDEN,
            "policy_local_only",
            &format!(
                "request must be served locally (reason: {}); local routing is not yet implemented in the Rust port",
                evaluation.reason
            ),
        )),
    }
}

async fn proxy_response(request: &rillan_chat::Request, response: ProviderResponse) -> Response {
    let status = StatusCode::from_u16(response.status.as_u16()).unwrap_or(StatusCode::OK);
    let upstream_ct = response.headers.get(CONTENT_TYPE).cloned();
    let is_event_stream = upstream_ct
        .as_ref()
        .and_then(|v| v.to_str().ok())
        .is_some_and(|s| s.to_ascii_lowercase().contains("text/event-stream"));
    let streaming = request.stream || is_event_stream;

    let mut builder = Response::builder().status(status);
    if let Some(content_type) = upstream_ct {
        builder = builder.header(CONTENT_TYPE, content_type);
    } else {
        builder = builder.header(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    }

    let body = if streaming {
        match response.body {
            ProviderBody::Stream(stream) => axum::body::Body::from_stream(stream),
            ProviderBody::Buffered(bytes) => axum::body::Body::from(bytes),
        }
    } else {
        match response.body.collect().await {
            Ok(bytes) => axum::body::Body::from(bytes),
            Err(err) => {
                tracing::error!(error = %err, "buffer upstream body failed");
                return error_response(
                    StatusCode::BAD_GATEWAY,
                    "upstream_error",
                    &format!("read upstream body: {err}"),
                );
            }
        }
    };

    builder.body(body).unwrap_or_else(|err| {
        tracing::error!(error = %err, "build response failed");
        StatusCode::INTERNAL_SERVER_ERROR.into_response()
    })
}

fn json_content_type(headers: &HeaderMap) -> bool {
    let Some(value) = headers.get(CONTENT_TYPE) else {
        return false;
    };
    let Ok(s) = value.to_str() else { return false };
    let primary = s.split(';').next().unwrap_or("").trim();
    primary.eq_ignore_ascii_case("application/json")
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

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;
    use http::HeaderMap;

    fn buffered_response(content_type: &'static str, body: &'static [u8]) -> ProviderResponse {
        let mut headers = HeaderMap::new();
        headers.insert(CONTENT_TYPE, HeaderValue::from_static(content_type));
        ProviderResponse {
            status: StatusCode::OK,
            headers,
            body: ProviderBody::Buffered(Bytes::from_static(body)),
        }
    }

    fn streaming_response(
        content_type: &'static str,
        chunks: Vec<&'static [u8]>,
    ) -> ProviderResponse {
        use futures::stream;
        let mut headers = HeaderMap::new();
        headers.insert(CONTENT_TYPE, HeaderValue::from_static(content_type));
        let items: Vec<Result<Bytes, rillan_providers::ProviderError>> = chunks
            .into_iter()
            .map(|c| Ok(Bytes::from_static(c)))
            .collect();
        ProviderResponse {
            status: StatusCode::OK,
            headers,
            body: ProviderBody::Stream(Box::pin(stream::iter(items))),
        }
    }

    #[tokio::test]
    async fn proxy_response_buffers_non_streaming_request() {
        let req = rillan_chat::Request::default();
        let response = proxy_response(
            &req,
            buffered_response("application/json", b"{\"ok\":true}"),
        )
        .await;
        assert_eq!(response.status(), StatusCode::OK);
        assert_eq!(
            response
                .headers()
                .get(CONTENT_TYPE)
                .and_then(|v| v.to_str().ok()),
            Some("application/json"),
        );
        let body = to_bytes(response.into_body(), 1 << 16).await.unwrap();
        assert_eq!(&body[..], b"{\"ok\":true}");
    }

    #[tokio::test]
    async fn proxy_response_streams_when_request_stream_true() {
        let req = rillan_chat::Request {
            stream: true,
            ..rillan_chat::Request::default()
        };
        let response = proxy_response(
            &req,
            streaming_response(
                "text/event-stream",
                vec![b"data: a\n\n", b"data: b\n\n", b"data: [DONE]\n\n"],
            ),
        )
        .await;
        assert_eq!(response.status(), StatusCode::OK);
        assert_eq!(
            response
                .headers()
                .get(CONTENT_TYPE)
                .and_then(|v| v.to_str().ok()),
            Some("text/event-stream"),
        );
        let body = to_bytes(response.into_body(), 1 << 16).await.unwrap();
        assert_eq!(&body[..], b"data: a\n\ndata: b\n\ndata: [DONE]\n\n");
    }

    #[tokio::test]
    async fn proxy_response_streams_when_upstream_content_type_is_sse() {
        let req = rillan_chat::Request::default();
        let response = proxy_response(
            &req,
            streaming_response("text/event-stream; charset=utf-8", vec![b"data: 1\n\n"]),
        )
        .await;
        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 1 << 16).await.unwrap();
        assert_eq!(&body[..], b"data: 1\n\n");
    }

    #[tokio::test]
    async fn proxy_response_collects_buffered_streaming_body() {
        // Even if upstream returned the body buffered, a streaming-intent
        // request should still surface the bytes verbatim under SSE.
        let req = rillan_chat::Request {
            stream: true,
            ..rillan_chat::Request::default()
        };
        let response =
            proxy_response(&req, buffered_response("text/event-stream", b"data: x\n\n")).await;
        let body = to_bytes(response.into_body(), 1 << 16).await.unwrap();
        assert_eq!(&body[..], b"data: x\n\n");
    }
}
