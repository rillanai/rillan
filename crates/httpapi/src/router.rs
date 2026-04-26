// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Router construction. Mirrors `internal/httpapi/router.go`.

use std::sync::Arc;

use axum::middleware::{from_fn, from_fn_with_state};
use axum::routing::{get, post};
use axum::Router;
use rillan_agent::ApprovalGate;
use rillan_policy::{Evaluator, Scanner};
use thiserror::Error;
use tokio::sync::RwLock;

use crate::admin_reload::{handle_refresh, RefreshFn, ADMIN_RUNTIME_REFRESH_PATH};
use crate::agent_proposal::handle_proposal_decision;
use crate::agent_task::{handle_agent_task, AgentTaskState};
use crate::auth_middleware::{require_bearer, BearerResolver};
use crate::chat::handle_chat_completions;
use crate::health::{handle_healthz, handle_readyz};
use crate::middleware::emit_request_id;
use crate::runtime_snapshot::RuntimeSnapshot;

/// Type-alias for the runtime-snapshot accessor used by the router. Returning
/// the snapshot from a closure mirrors `RuntimeSnapshotFunc` in the Go code,
/// which lets the daemon swap state without rebuilding the router.
pub type RuntimeSnapshotFn = Arc<dyn Fn() -> RuntimeSnapshot + Send + Sync + 'static>;

/// Static dependencies wired into the router.
pub struct RouterOptions {
    pub runtime_snapshot: RuntimeSnapshotFn,
    pub scanner: Arc<Scanner>,
    pub evaluator: Arc<Evaluator>,
    /// Optional agent approval gate. When `None`, agent endpoints are not
    /// mounted.
    pub approval_gate: Option<Arc<ApprovalGate>>,
    /// Approved repo roots forwarded to the agent task handler. Empty means
    /// the agent rejects every `repo_root` value.
    pub approved_repo_roots: Vec<String>,
    /// Optional refresh callback. When `None`, the admin-refresh endpoint is
    /// not mounted.
    pub refresh: Option<RefreshFn>,
    /// Optional bearer resolver. When `Some`, the middleware is attached to
    /// every protected route (chat completions, agent task, agent proposal
    /// decision, admin runtime refresh). `/healthz` and `/readyz` are never
    /// authenticated. The resolver may itself return `Ok(None)` at request
    /// time to short-circuit the check (e.g. when `server.auth.enabled` is
    /// false in the live snapshot).
    pub bearer_resolver: Option<Arc<dyn BearerResolver>>,
}

impl std::fmt::Debug for RouterOptions {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RouterOptions")
            .field("scanner", &"<scanner>")
            .field("evaluator", &"<evaluator>")
            .field("agent", &self.approval_gate.is_some())
            .field("approved_repo_roots", &self.approved_repo_roots)
            .field("refresh", &self.refresh.is_some())
            .field("bearer", &self.bearer_resolver.is_some())
            .finish()
    }
}

/// Errors raised during router construction. Reserved for future use; today
/// the router builder is infallible.
#[derive(Debug, Error)]
pub enum RouterError {}

/// Shared, cheaply-cloneable state passed to handlers.
#[derive(Clone)]
pub(crate) struct SharedState {
    pub(crate) inner: Arc<SharedInner>,
}

pub(crate) struct SharedInner {
    pub(crate) runtime_snapshot: RuntimeSnapshotFn,
    pub(crate) scanner: Arc<Scanner>,
    pub(crate) evaluator: Arc<Evaluator>,
    _placeholder: RwLock<()>,
}

impl SharedState {
    /// Returns the current runtime snapshot.
    pub(crate) fn snapshot(&self) -> RuntimeSnapshot {
        (self.inner.runtime_snapshot)()
    }

    pub(crate) fn scanner(&self) -> &Arc<Scanner> {
        &self.inner.scanner
    }

    pub(crate) fn evaluator(&self) -> &Arc<Evaluator> {
        &self.inner.evaluator
    }
}

impl std::fmt::Debug for SharedState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SharedState").finish_non_exhaustive()
    }
}

/// Builds the axum [`Router`] backing the daemon.
pub fn build_router(options: RouterOptions) -> Result<Router, RouterError> {
    let state = SharedState {
        inner: Arc::new(SharedInner {
            runtime_snapshot: options.runtime_snapshot,
            scanner: options.scanner,
            evaluator: options.evaluator,
            _placeholder: RwLock::new(()),
        }),
    };

    let pipeline = (state.inner.runtime_snapshot)().pipeline.clone();

    // Unprotected: liveness + readiness probes, mirroring Go's router which
    // exempts /healthz and /readyz from `protectedHandler`.
    let public_router = Router::new()
        .route("/healthz", get(handle_healthz))
        .route("/readyz", get(handle_readyz))
        .with_state(state.clone());

    // Protected: chat completions, agent endpoints, admin refresh.
    let mut protected_router = Router::new()
        .route("/v1/chat/completions", post(handle_chat_completions))
        .with_state(state);

    if let Some(gate) = options.approval_gate {
        let task_state = AgentTaskState {
            gate: gate.clone(),
            approved_repo_roots: Arc::new(options.approved_repo_roots),
            pipeline: Some(pipeline),
        };
        protected_router = protected_router.merge(
            Router::new()
                .route("/v1/agent/tasks", post(handle_agent_task))
                .with_state(task_state),
        );
        protected_router = protected_router.merge(
            Router::new()
                .route(
                    "/v1/agent/proposals/:id/decision",
                    post(handle_proposal_decision),
                )
                .with_state(gate),
        );
    }

    protected_router = protected_router.merge(
        Router::new()
            .route(ADMIN_RUNTIME_REFRESH_PATH, post(handle_refresh))
            .with_state(options.refresh),
    );

    if let Some(resolver) = options.bearer_resolver {
        protected_router = protected_router.layer(from_fn_with_state(resolver, require_bearer));
    }

    Ok(public_router
        .merge(protected_router)
        .layer(from_fn(emit_request_id)))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::auth_middleware::BearerFn;
    use crate::runtime_snapshot::ReadinessInfo;
    use axum::body::{to_bytes, Body};
    use axum::http::{Request, StatusCode};
    use rillan_config::{
        Config, RuntimeProviderAdapterConfig, RuntimeProviderHostConfig, PROVIDER_OPENAI_COMPATIBLE,
    };
    use rillan_providers::Host;

    fn stub_snapshot() -> RuntimeSnapshot {
        let dir = tempfile::tempdir().unwrap();
        let host_cfg = RuntimeProviderHostConfig {
            default: "stub".into(),
            providers: vec![RuntimeProviderAdapterConfig {
                id: "stub".into(),
                kind: PROVIDER_OPENAI_COMPATIBLE.into(),
                ..RuntimeProviderAdapterConfig::default()
            }],
        };
        let host = Host::new(&host_cfg).expect("host");
        let provider = host.provider("stub").expect("provider");
        RuntimeSnapshot {
            provider,
            provider_host: Arc::new(host),
            pipeline: Arc::new(rillan_retrieval::Pipeline::new(
                rillan_config::RetrievalConfig::default(),
                dir.path().join("idx.db"),
            )),
            config: Config::default(),
            project_config: Default::default(),
            system_config: None,
            modules: Default::default(),
            classifier: None,
            route_catalog: Default::default(),
            route_status: Default::default(),
            readiness: ReadinessInfo::default(),
            ollama_checker: None,
        }
    }

    fn snapshot_fn(snapshot: RuntimeSnapshot) -> RuntimeSnapshotFn {
        Arc::new(move || snapshot.clone())
    }

    fn build_test_router(bearer: Option<Arc<dyn BearerResolver>>) -> Router {
        build_router(RouterOptions {
            runtime_snapshot: snapshot_fn(stub_snapshot()),
            scanner: Arc::new(rillan_policy::Scanner::default_scanner()),
            evaluator: Arc::new(rillan_policy::Evaluator::new()),
            approval_gate: None,
            approved_repo_roots: Vec::new(),
            refresh: None,
            bearer_resolver: bearer,
        })
        .expect("router")
    }

    async fn run(router: Router, request: Request<Body>) -> StatusCode {
        let response = tower::ServiceExt::oneshot(router, request).await.unwrap();
        let status = response.status();
        let _ = to_bytes(response.into_body(), 1 << 16).await;
        status
    }

    fn get(uri: &str, header: Option<&str>) -> Request<Body> {
        let mut builder = Request::builder().method("GET").uri(uri);
        if let Some(value) = header {
            builder = builder.header("authorization", value);
        }
        builder.body(Body::empty()).unwrap()
    }

    fn post_chat(header: Option<&str>) -> Request<Body> {
        let mut builder = Request::builder()
            .method("POST")
            .uri("/v1/chat/completions")
            .header("content-type", "application/json");
        if let Some(value) = header {
            builder = builder.header("authorization", value);
        }
        builder
            .body(Body::from(r#"{"model":"x","messages":[]}"#.to_string()))
            .unwrap()
    }

    #[tokio::test]
    async fn no_resolver_leaves_routes_unauthenticated() {
        let router = build_test_router(None);
        // /healthz still 200, /v1/chat/completions still reaches handler
        // (returns 400 because messages is empty, but specifically not 401).
        assert_eq!(
            run(router.clone(), get("/healthz", None)).await,
            StatusCode::OK
        );
        let chat_status = tower::ServiceExt::oneshot(router, post_chat(None))
            .await
            .unwrap()
            .status();
        assert_ne!(chat_status, StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn protected_routes_require_bearer_when_resolver_demands() {
        let resolver: Arc<dyn BearerResolver> =
            Arc::new(BearerFn(|| Ok(Some("secret".to_string()))));
        let router = build_test_router(Some(resolver));

        // /healthz must remain reachable without a token. /readyz also runs
        // unauthenticated; the stub provider can't actually reach an upstream
        // so it returns 503, but the important property is that the auth
        // middleware did *not* short-circuit it to 401.
        assert_eq!(
            run(router.clone(), get("/healthz", None)).await,
            StatusCode::OK,
        );
        assert_ne!(
            run(router.clone(), get("/readyz", None)).await,
            StatusCode::UNAUTHORIZED,
        );

        // /v1/chat/completions without bearer → 401.
        let unauthed = tower::ServiceExt::oneshot(router.clone(), post_chat(None))
            .await
            .unwrap();
        assert_eq!(unauthed.status(), StatusCode::UNAUTHORIZED);

        // With the right token, middleware passes; the handler responds for
        // its own reasons (validation rejects empty messages → 400) but we
        // only care that auth no longer blocks.
        let authed = tower::ServiceExt::oneshot(router, post_chat(Some("Bearer secret")))
            .await
            .unwrap();
        assert_ne!(authed.status(), StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn resolver_returning_none_leaves_routes_open() {
        let resolver: Arc<dyn BearerResolver> = Arc::new(BearerFn(|| Ok(None)));
        let router = build_test_router(Some(resolver));
        let status = tower::ServiceExt::oneshot(router, post_chat(None))
            .await
            .unwrap()
            .status();
        assert_ne!(status, StatusCode::UNAUTHORIZED);
    }
}
