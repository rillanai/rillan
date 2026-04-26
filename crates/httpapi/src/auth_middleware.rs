// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Server-side bearer-token middleware. Mirrors
//! `internal/httpapi/auth_middleware.go`.

use std::sync::Arc;

use axum::body::Body;
use axum::extract::State;
use axum::http::header::{AUTHORIZATION, CONTENT_TYPE, WWW_AUTHENTICATE};
use axum::http::{HeaderValue, Request, StatusCode};
use axum::middleware::Next;
use axum::response::Response;
use rillan_openai::{ApiError, ErrorResponse};

/// Resolves the expected daemon bearer at request time. Returning `Ok(None)`
/// short-circuits the auth check (used when `server.auth.enabled` is false).
pub trait BearerResolver: Send + Sync {
    fn resolve(&self) -> Result<Option<String>, String>;
}

/// Convenience adapter for closures.
pub struct BearerFn<F>(pub F);

impl<F> std::fmt::Debug for BearerFn<F> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BearerFn").finish_non_exhaustive()
    }
}

impl<F> BearerResolver for BearerFn<F>
where
    F: Fn() -> Result<Option<String>, String> + Send + Sync,
{
    fn resolve(&self) -> Result<Option<String>, String> {
        (self.0)()
    }
}

/// Middleware that verifies an incoming `Authorization: Bearer …` header
/// against the daemon-resolved bearer.
pub async fn require_bearer(
    State(resolver): State<Arc<dyn BearerResolver>>,
    request: Request<Body>,
    next: Next,
) -> Response {
    match resolver.resolve() {
        Ok(None) => next.run(request).await,
        Ok(Some(expected)) => {
            let provided = request
                .headers()
                .get(AUTHORIZATION)
                .and_then(|value| value.to_str().ok())
                .and_then(parse_bearer);
            let Some(provided) = provided else {
                return unauthorized();
            };
            if !constant_time_eq(provided.as_bytes(), expected.as_bytes()) {
                return unauthorized();
            }
            next.run(request).await
        }
        Err(err) => {
            tracing::error!(error = %err, "server auth resolution failed");
            error_response(
                StatusCode::INTERNAL_SERVER_ERROR,
                "config_error",
                "server auth is misconfigured",
            )
        }
    }
}

fn parse_bearer(value: &str) -> Option<String> {
    let trimmed = value.trim();
    let mut parts = trimmed.splitn(2, char::is_whitespace);
    let scheme = parts.next()?;
    let token = parts.next()?.trim();
    if !scheme.eq_ignore_ascii_case("bearer") || token.is_empty() {
        return None;
    }
    Some(token.to_string())
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff: u8 = 0;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

fn unauthorized() -> Response {
    let mut response = error_response(
        StatusCode::UNAUTHORIZED,
        "authentication_error",
        "missing or invalid bearer token",
    );
    response.headers_mut().insert(
        WWW_AUTHENTICATE,
        HeaderValue::from_static("Bearer realm=\"rillan\""),
    );
    response
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
        .body(Body::from(body))
        .unwrap_or_else(|_| Response::new(Body::empty()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_bearer_accepts_valid_header() {
        assert_eq!(parse_bearer("Bearer abc"), Some("abc".to_string()));
        assert_eq!(parse_bearer("bearer  xyz "), Some("xyz".to_string()));
    }

    #[test]
    fn parse_bearer_rejects_other_schemes() {
        assert_eq!(parse_bearer("Basic abc"), None);
        assert_eq!(parse_bearer(""), None);
        assert_eq!(parse_bearer("Bearer "), None);
    }

    #[test]
    fn constant_time_eq_is_correct() {
        assert!(constant_time_eq(b"abc", b"abc"));
        assert!(!constant_time_eq(b"abc", b"abd"));
        assert!(!constant_time_eq(b"abc", b"abcd"));
    }

    use axum::body::to_bytes;
    use axum::middleware::from_fn_with_state;
    use axum::routing::get;
    use axum::Router;

    fn protected_router(resolver: Arc<dyn BearerResolver>) -> Router {
        Router::new()
            .route("/protected", get(|| async { "ok" }))
            .layer(from_fn_with_state(resolver, require_bearer))
    }

    fn make_request(uri: &str, header: Option<&str>) -> Request<Body> {
        let mut builder = Request::builder().method("GET").uri(uri);
        if let Some(value) = header {
            builder = builder.header(AUTHORIZATION, value);
        }
        builder.body(Body::empty()).unwrap()
    }

    async fn run(router: Router, request: Request<Body>) -> (StatusCode, Vec<u8>) {
        let response = tower::ServiceExt::oneshot(router, request).await.unwrap();
        let status = response.status();
        let bytes = to_bytes(response.into_body(), 1 << 16).await.unwrap();
        (status, bytes.to_vec())
    }

    #[tokio::test]
    async fn resolver_returning_none_lets_request_through() {
        let resolver: Arc<dyn BearerResolver> = Arc::new(BearerFn(|| Ok(None)));
        let (status, body) =
            run(protected_router(resolver), make_request("/protected", None)).await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body, b"ok");
    }

    #[tokio::test]
    async fn missing_header_returns_401_with_www_authenticate() {
        let resolver: Arc<dyn BearerResolver> =
            Arc::new(BearerFn(|| Ok(Some("secret".to_string()))));
        let router = protected_router(resolver);
        let request = make_request("/protected", None);
        let response = tower::ServiceExt::oneshot(router, request).await.unwrap();
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        assert_eq!(
            response.headers().get(WWW_AUTHENTICATE).unwrap(),
            "Bearer realm=\"rillan\""
        );
    }

    #[tokio::test]
    async fn wrong_token_returns_401() {
        let resolver: Arc<dyn BearerResolver> =
            Arc::new(BearerFn(|| Ok(Some("secret".to_string()))));
        let (status, _) = run(
            protected_router(resolver),
            make_request("/protected", Some("Bearer not-the-secret")),
        )
        .await;
        assert_eq!(status, StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn correct_token_lets_request_through() {
        let resolver: Arc<dyn BearerResolver> =
            Arc::new(BearerFn(|| Ok(Some("secret".to_string()))));
        let (status, body) = run(
            protected_router(resolver),
            make_request("/protected", Some("Bearer secret")),
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body, b"ok");
    }

    #[tokio::test]
    async fn resolver_error_returns_500() {
        let resolver: Arc<dyn BearerResolver> =
            Arc::new(BearerFn(|| Err("keyring is locked".to_string())));
        let (status, _) = run(
            protected_router(resolver),
            make_request("/protected", Some("Bearer anything")),
        )
        .await;
        assert_eq!(status, StatusCode::INTERNAL_SERVER_ERROR);
    }
}
