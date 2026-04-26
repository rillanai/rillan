// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Daemon wiring and lifecycle. Mirrors `internal/app/app.go` plus the
//! hot-swappable runtime manager from `internal/app/runtime_manager.go` and
//! the snapshot builder from `internal/app/runtime_snapshot_builder.go`.

pub mod runtime_manager;
pub mod runtime_snapshot_builder;

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use rillan_agent::ApprovalGate;
use rillan_audit::Recorder;
use rillan_config::{Config, ProjectConfig, SystemConfig};
use rillan_httpapi::{build_router, BearerFn, BearerResolver, RouterOptions};
use rillan_policy::{Evaluator, Scanner};
use rillan_secretstore::Store;
use thiserror::Error;
use tokio::net::TcpListener;
use tracing::info;

pub use runtime_manager::{
    BuildError as RuntimeBuildError, Builder as RuntimeBuilder, RuntimeManager,
};
pub use runtime_snapshot_builder::{BuildError, SnapshotBuilder};

/// Daemon-construction errors.
#[derive(Debug, Error)]
pub enum AppError {
    #[error("server auth bearer: {0}")]
    AuthBearer(#[source] rillan_config::ResolveError),
    #[error("snapshot builder: {0}")]
    Snapshot(#[from] BuildError),
    #[error("bind {addr}: {source}")]
    Bind {
        addr: SocketAddr,
        #[source]
        source: std::io::Error,
    },
    #[error("address parse failed: {0}")]
    AddressParse(#[from] std::net::AddrParseError),
    #[error("router build: {0}")]
    Router(#[from] rillan_httpapi::RouterError),
    #[error("audit ledger: {0}")]
    Audit(#[from] rillan_audit::Error),
    #[error("server crashed: {0}")]
    Serve(#[source] std::io::Error),
}

/// Top-level daemon entry point.
#[derive(Clone, Debug)]
pub struct App {
    addr: SocketAddr,
    router: axum::Router,
}

impl App {
    /// Wires up the daemon: resolves providers, builds the runtime snapshot,
    /// and assembles the HTTP router.
    ///
    /// `config_path`, `project_config_path`, and `system_config_path` are
    /// remembered by the [`SnapshotBuilder`] so subsequent
    /// `RuntimeManager::refresh()` calls can re-read them from disk.
    pub async fn new(
        cfg: Config,
        project: ProjectConfig,
        system: Option<SystemConfig>,
        store: Store,
        config_path: PathBuf,
        project_config_path: PathBuf,
        system_config_path: PathBuf,
    ) -> Result<Self, AppError> {
        if cfg.server.auth.enabled {
            rillan_config::resolve_server_auth_bearer(&cfg, &store)
                .map_err(AppError::AuthBearer)?;
        }

        let audit_store = rillan_audit::Store::new(rillan_audit::Store::default_path())?;
        let audit_path = audit_store.path().to_string_lossy().to_string();
        let recorder: Arc<dyn Recorder> = Arc::new(audit_store);

        let builder = SnapshotBuilder {
            config_path,
            system_config_path,
            audit_ledger_path: audit_path,
            store: store.clone(),
        };
        let snapshot = builder
            .build_from_loaded(cfg.clone(), project, system, &project_config_path)
            .await?;

        let manager_builder: RuntimeBuilder = {
            let builder = builder.clone();
            Arc::new(move || {
                let builder = builder.clone();
                Box::pin(async move {
                    builder
                        .build_from_disk()
                        .await
                        .map_err(|err| RuntimeBuildError(err.to_string()))
                })
            })
        };
        let manager = Arc::new(RuntimeManager::new(snapshot, manager_builder));

        let scanner = Arc::new(Scanner::default_scanner());
        let evaluator = Arc::new(Evaluator::new());
        let approval_gate = Arc::new(ApprovalGate::new(Some(recorder)));

        let approved_repo_roots = build_approved_repo_roots(&cfg);

        let auth_snapshot_fn = manager.snapshot_fn();
        let auth_store = store.clone();
        let bearer_resolver: Arc<dyn BearerResolver> = Arc::new(BearerFn(move || {
            let snapshot = (auth_snapshot_fn)();
            if !snapshot.config.server.auth.enabled {
                return Ok(None);
            }
            let bearer = rillan_config::resolve_server_auth_bearer(&snapshot.config, &auth_store)
                .map_err(|err| err.to_string())?;
            Ok(Some(bearer))
        }));

        let router = build_router(RouterOptions {
            runtime_snapshot: manager.snapshot_fn(),
            scanner,
            evaluator,
            approval_gate: Some(approval_gate),
            approved_repo_roots,
            refresh: Some(manager.refresh_fn()),
            bearer_resolver: Some(bearer_resolver),
        })?;

        let addr: SocketAddr = format!("{}:{}", cfg.server.host, cfg.server.port).parse()?;
        Ok(Self { addr, router })
    }

    /// Runs the daemon until `shutdown` resolves, then performs a graceful
    /// shutdown.
    pub async fn run<F: std::future::Future<Output = ()> + Send + 'static>(
        self,
        shutdown: F,
    ) -> Result<(), AppError> {
        info!(addr = %self.addr, "starting rillan server");
        let listener = TcpListener::bind(self.addr)
            .await
            .map_err(|err| AppError::Bind {
                addr: self.addr,
                source: err,
            })?;
        let graceful = async move {
            shutdown.await;
            info!("server shutdown started");
        };
        axum::serve(
            listener,
            self.router
                .into_make_service_with_connect_info::<SocketAddr>(),
        )
        .with_graceful_shutdown(graceful)
        .await
        .map_err(AppError::Serve)
    }
}

/// Mirrors `buildApprovedRepoRoots` from `internal/httpapi/router.go`. Combines
/// `index.root` and `agent.approved_repo_roots` into a deduped list that
/// preserves insertion order.
fn build_approved_repo_roots(cfg: &Config) -> Vec<String> {
    let mut seen: std::collections::BTreeSet<String> = std::collections::BTreeSet::new();
    let mut roots: Vec<String> = Vec::new();
    let mut append = |root: &str| {
        let trimmed = root.trim();
        if trimmed.is_empty() {
            return;
        }
        if !seen.insert(trimmed.to_string()) {
            return;
        }
        roots.push(trimmed.to_string());
    };
    append(&cfg.index.root);
    for root in &cfg.agent.approved_repo_roots {
        append(root);
    }
    roots
}

/// Helper used by the binary CLI to resolve the system config path. Wraps
/// `rillan_config::resolve_system_config_path` so callers don't need to depend
/// on `rillan_config` directly.
#[must_use]
pub fn system_config_path() -> PathBuf {
    rillan_config::resolve_system_config_path()
}

/// Helper to resolve the project-config path against `cfg.index.root`.
#[must_use]
pub fn project_config_path(index_root: &str) -> PathBuf {
    rillan_config::resolve_project_config_path(index_root)
}

/// Re-export so binary callers can pass paths in the right shape.
#[must_use]
pub fn config_path_default() -> &'static Path {
    static DEFAULT: std::sync::OnceLock<PathBuf> = std::sync::OnceLock::new();
    DEFAULT
        .get_or_init(rillan_config::default_config_path)
        .as_path()
}
