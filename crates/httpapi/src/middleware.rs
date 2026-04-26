// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Request-id middleware. Mirrors `internal/httpapi/middleware.go`.

use std::time::Instant;

use axum::body::Body;
use axum::http::{HeaderName, HeaderValue, Request};
use axum::middleware::Next;
use axum::response::Response;
use rillan_observability::new_request_id;
use tracing::info;

pub(crate) const HEADER_NAME: HeaderName = HeaderName::from_static("x-request-id");

/// Wraps `next` with request-id assignment plus a `request completed` log
/// line. Exposed publicly so callers (or integration tests) can attach the
/// middleware to their own router.
pub async fn emit_request_id(mut request: Request<Body>, next: Next) -> Response {
    let request_id = new_request_id();
    let header_value =
        HeaderValue::from_str(&request_id).unwrap_or_else(|_| HeaderValue::from_static("unknown"));
    request
        .headers_mut()
        .insert(HEADER_NAME, header_value.clone());

    let method = request.method().clone();
    let path = request.uri().path().to_string();
    let start = Instant::now();
    let mut response = next.run(request).await;
    response.headers_mut().insert(HEADER_NAME, header_value);

    let duration_ms = u64::try_from(start.elapsed().as_millis()).unwrap_or(u64::MAX);
    info!(
        request_id = %request_id,
        method = %method,
        path = %path,
        status = response.status().as_u16(),
        duration_ms,
        "request completed"
    );
    response
}

/// Returns the canonical lowercase header name used by the daemon.
#[must_use]
pub fn header_name() -> HeaderName {
    HEADER_NAME
}

/// Returns a layer suitable for `Router::layer` that runs [`emit_request_id`].
///
/// Defined as a `from_fn` adapter; callers attach it via
/// `Router::layer(request_id_layer())` exactly the way the daemon's own
/// router does.
pub fn request_id_layer() -> RequestIdLayer {
    axum::middleware::from_fn(wrap)
}

/// Concrete layer type returned by [`request_id_layer`]. Hidden alias keeps
/// the `from_fn` machinery off the public surface.
pub type RequestIdLayer = axum::middleware::FromFnLayer<RequestIdHandler, (), RequestIdHandler>;

type BoxedFuture = std::pin::Pin<Box<dyn std::future::Future<Output = Response> + Send + 'static>>;

/// Function-pointer signature accepted by [`axum::middleware::from_fn`].
pub type RequestIdHandler = fn(Request<Body>, Next) -> BoxedFuture;

fn wrap(request: Request<Body>, next: Next) -> BoxedFuture {
    Box::pin(emit_request_id(request, next))
}
