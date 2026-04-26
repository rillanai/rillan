// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use std::sync::Arc;

use async_trait::async_trait;
use rillan_classify::SafeClassifier;
use rillan_config::{Config, ProjectConfig, SystemConfig};
use rillan_modules::Catalog as ModuleCatalog;
use rillan_providers::{Host, Provider};
use rillan_retrieval::Pipeline;
use rillan_routing::{Catalog as RouteCatalog, StatusCatalog as RouteStatusCatalog};

/// Static readiness info baked into the router. Mirrors the Go
/// `ReadinessInfo` struct.
#[derive(Debug, Clone, Default)]
pub struct ReadinessInfo {
    pub retrieval_mode: String,
    pub system_config_loaded: bool,
    pub audit_ledger_path: String,
    pub local_model_required: bool,
    pub modules_discovered: usize,
    pub modules_enabled: usize,
}

/// Async readiness probe used by `GET /readyz` to verify the local
/// Ollama-style model server, when one is configured.
#[async_trait]
pub trait OllamaChecker: Send + Sync {
    async fn ready(&self) -> Result<(), String>;
}

/// Snapshot of the daemon's hot-swappable runtime state. Mirrors the Go
/// `httpapi.RuntimeSnapshot` field-for-field. Every heavy field lives behind
/// `Arc` so the snapshot can be cloned (and stored in `ArcSwap`) cheaply.
#[derive(Clone)]
pub struct RuntimeSnapshot {
    /// Default upstream provider for chat completions.
    pub provider: Arc<dyn Provider>,
    /// Multi-provider host. `provider` is the host's default.
    pub provider_host: Arc<Host>,
    /// Retrieval pipeline. Always set; disabled when `cfg.retrieval.enabled`
    /// is false but still safe to call.
    pub pipeline: Arc<Pipeline>,
    /// Resolved runtime config, including module-augmented LLM adapters.
    pub config: Config,
    /// Repo-local project config.
    pub project_config: ProjectConfig,
    /// Optional system config (encrypted policy bundle decrypted in place).
    pub system_config: Option<SystemConfig>,
    /// Trusted module catalog after enable + trust filtering.
    pub modules: ModuleCatalog,
    /// Optional intent classifier wired up when `local_model.enabled` is true.
    pub classifier: Option<Arc<SafeClassifier>>,
    /// Routing decision-engine inputs.
    pub route_catalog: RouteCatalog,
    /// Per-candidate readiness status.
    pub route_status: RouteStatusCatalog,
    /// Static readiness info exposed via `/readyz`.
    pub readiness: ReadinessInfo,
    /// Optional ollama-style probe used by `/readyz` when local-model is
    /// required.
    pub ollama_checker: Option<Arc<dyn OllamaChecker>>,
}

impl std::fmt::Debug for RuntimeSnapshot {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RuntimeSnapshot")
            .field("provider", &self.provider.name())
            .field("modules_enabled", &self.readiness.modules_enabled)
            .field("readiness", &self.readiness)
            .finish()
    }
}
