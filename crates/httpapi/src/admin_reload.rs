// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `POST /admin/runtime/refresh` handler. Mirrors
//! `internal/httpapi/admin_reload_handler.go`.

use std::net::IpAddr;
use std::sync::Arc;

use axum::extract::{ConnectInfo, State};
use axum::http::header::CONTENT_TYPE;
use axum::http::{HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};
use rillan_openai::{ApiError, ErrorResponse};
use std::net::SocketAddr;

pub const ADMIN_RUNTIME_REFRESH_PATH: &str = "/admin/runtime/refresh";

/// Boxed async refresh callback.
pub type RefreshFn = Arc<
    dyn Fn() -> std::pin::Pin<
            Box<dyn std::future::Future<Output = Result<(), String>> + Send + 'static>,
        > + Send
        + Sync
        + 'static,
>;

/// Handler entry point.
pub(crate) async fn handle_refresh(
    State(refresh): State<Option<RefreshFn>>,
    ConnectInfo(addr): ConnectInfo<SocketAddr>,
) -> Response {
    if !is_loopback(&addr.ip()) {
        return error_response(
            StatusCode::FORBIDDEN,
            "forbidden",
            "admin refresh is restricted to localhost",
        );
    }
    let Some(refresh) = refresh else {
        return error_response(
            StatusCode::SERVICE_UNAVAILABLE,
            "service_unavailable",
            "runtime refresh is not configured",
        );
    };
    match refresh().await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(err) => {
            tracing::error!(error = %err, "runtime refresh failed");
            error_response(StatusCode::INTERNAL_SERVER_ERROR, "refresh_error", &err)
        }
    }
}

fn is_loopback(ip: &IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => v4.is_loopback(),
        IpAddr::V6(v6) => v6.is_loopback(),
    }
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

    #[test]
    fn loopback_check_accepts_v4_and_v6() {
        assert!(is_loopback(&"127.0.0.1".parse::<IpAddr>().unwrap()));
        assert!(is_loopback(&"::1".parse::<IpAddr>().unwrap()));
        assert!(!is_loopback(&"10.0.0.1".parse::<IpAddr>().unwrap()));
    }
}
