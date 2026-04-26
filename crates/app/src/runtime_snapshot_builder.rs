// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Runtime-snapshot builder. Mirrors `internal/app/runtime_snapshot_builder.go`.
//!
//! Responsible for the heavy lifting that turns a parsed config + project +
//! optional system policy into a ready-to-use [`RuntimeSnapshot`]:
//!
//! 1. Discover + filter the project's module catalog.
//! 2. Augment the runtime config with module-provided LLM adapters.
//! 3. Resolve the runtime provider host (resolves credentials through the
//!    [`Store`]).
//! 4. Build the multi-provider [`Host`] and grab the default provider.
//! 5. Probe per-candidate readiness via `routing::build_status_catalog`.
//! 6. Wire up an Ollama-backed classifier + embedder + rewriter when
//!    `local_model.enabled` is true.
//! 7. Construct the retrieval pipeline (always present; effectively a
//!    pass-through when `cfg.retrieval.enabled` is false).

use std::path::{Path, PathBuf};
use std::sync::Arc;

use async_trait::async_trait;
use rillan_classify::{OllamaClassifier, SafeClassifier};
use rillan_config::{Config, LlmProviderConfig, ProjectConfig, SystemConfig, SCHEMA_VERSION_V2};
use rillan_httpapi::{OllamaChecker, ReadinessInfo, RuntimeSnapshot};
use rillan_modules::{
    filter_enabled, filter_trusted, load_project_catalog, Catalog as ModuleCatalog,
};
use rillan_providers::{Host, Provider};
use rillan_retrieval::{
    FallbackEmbedder, OllamaEmbedder, OllamaQueryRewriter, Pipeline, PlaceholderEmbedder,
    QueryEmbedder,
};
use rillan_routing::{build_catalog as build_route_catalog, build_status_catalog, StatusInput};
use rillan_secretstore::Store;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum BuildError {
    #[error("read config: {0}")]
    ConfigLoad(#[from] rillan_config::Error),
    #[error("resolve runtime providers: {0}")]
    Resolve(#[from] rillan_config::ResolveError),
    #[error("build provider host: {0}")]
    Host(#[from] rillan_providers::HostError),
    #[error("module catalog: {0}")]
    Modules(#[from] rillan_modules::Error),
    #[error("module llm adapter id collision: {id} already declared in {first}")]
    AdapterCollision { id: String, first: String },
}

/// Builder used by both the initial daemon start and the
/// `RuntimeManager.refresh()` callback. Holds the disk paths it needs to
/// re-read config from disk; in-memory rebuilds use [`build_from_loaded`].
#[derive(Debug, Clone)]
pub struct SnapshotBuilder {
    pub config_path: PathBuf,
    pub system_config_path: PathBuf,
    pub audit_ledger_path: String,
    pub store: Store,
}

impl SnapshotBuilder {
    /// Builds a snapshot from already-parsed config values. Used for the
    /// initial daemon construction so the caller sees validation errors at
    /// startup rather than on the first `/admin/runtime/refresh`.
    pub async fn build_from_loaded(
        &self,
        cfg: Config,
        project: ProjectConfig,
        system: Option<SystemConfig>,
        project_config_path: &Path,
    ) -> Result<RuntimeSnapshot, BuildError> {
        build_runtime_snapshot(
            cfg,
            project,
            system,
            &self.audit_ledger_path,
            project_config_path,
            &self.store,
        )
        .await
    }

    /// Re-reads config + project + system from disk, then builds a new
    /// snapshot. Called by the `RuntimeManager.refresh()` callback.
    pub async fn build_from_disk(&self) -> Result<RuntimeSnapshot, BuildError> {
        let cfg = rillan_config::load(&self.config_path)?;
        let project_config_path = rillan_config::resolve_project_config_path(&cfg.index.root);
        let project = match rillan_config::load_project(&project_config_path) {
            Ok(value) => value,
            Err(rillan_config::Error::Read(io)) if io.kind() == std::io::ErrorKind::NotFound => {
                rillan_config::default_project_config()
            }
            Err(err) => return Err(err.into()),
        };
        let system = match rillan_config::load_system(&self.system_config_path) {
            Ok(value) => Some(value),
            Err(rillan_config::Error::Read(io)) if io.kind() == std::io::ErrorKind::NotFound => {
                None
            }
            Err(err) => return Err(err.into()),
        };
        self.build_from_loaded(cfg, project, system, &project_config_path)
            .await
    }
}

async fn build_runtime_snapshot(
    cfg: Config,
    project: ProjectConfig,
    system: Option<SystemConfig>,
    audit_ledger_path: &str,
    project_config_path: &Path,
    store: &Store,
) -> Result<RuntimeSnapshot, BuildError> {
    let discovered = load_project_catalog(project_config_path)?;
    let module_catalog = filter_enabled(discovered.clone(), &project.modules.enabled)?;
    let module_catalog = filter_trusted(module_catalog, project_config_path, system.as_ref())?;
    let runtime_config =
        augment_runtime_config_with_module_llm_adapters(cfg.clone(), &module_catalog)?;

    let host_cfg = rillan_config::resolve_runtime_provider_host(&runtime_config, &project, store)?;
    let host = Arc::new(Host::new(&host_cfg)?);
    let default_provider: Arc<dyn Provider> = host.default_provider();

    let route_catalog = build_route_catalog(&runtime_config, &project);
    let route_status = build_status_catalog(StatusInput {
        catalog: route_catalog.clone(),
        config: &runtime_config,
        store: store.clone(),
    })
    .await;

    let mut classifier: Option<Arc<SafeClassifier>> = None;
    let mut ollama_checker: Option<Arc<dyn OllamaChecker>> = None;
    let mut query_embedder: Option<Arc<dyn QueryEmbedder>> = None;
    let mut query_rewriter: Option<Arc<dyn rillan_retrieval::QueryRewriter>> = None;

    if cfg.local_model.enabled {
        let ollama_client = rillan_ollama::Client::new(cfg.local_model.base_url.clone());
        ollama_checker = Some(Arc::new(OllamaPing {
            client: ollama_client.clone(),
        }));
        classifier = Some(Arc::new(SafeClassifier::new(Some(Box::new(
            OllamaClassifier::new(
                ollama_client.clone(),
                cfg.local_model.query_rewrite.model.clone(),
            ),
        )))));
        let primary: Box<dyn QueryEmbedder> = Box::new(OllamaEmbedder::new(
            ollama_client.clone(),
            cfg.local_model.embed_model.clone(),
        ));
        let fallback: Box<dyn QueryEmbedder> = Box::new(PlaceholderEmbedder);
        query_embedder = Some(Arc::new(FallbackEmbedder::new(primary, Some(fallback))));
        if cfg.local_model.query_rewrite.enabled {
            query_rewriter = Some(Arc::new(OllamaQueryRewriter::new(
                ollama_client,
                cfg.local_model.query_rewrite.model.clone(),
            )));
        }
    }

    let mut pipeline = Pipeline::new(cfg.retrieval.clone(), rillan_index::default_db_path());
    if let Some(embedder) = query_embedder {
        pipeline = pipeline.with_query_embedder(embedder);
    }
    if let Some(rewriter) = query_rewriter {
        pipeline = pipeline.with_query_rewriter(rewriter);
    }

    let retrieval_mode = if cfg.retrieval.enabled {
        "targeted_remote".to_string()
    } else {
        "disabled".to_string()
    };
    let modules_discovered = discovered.modules.len();
    let modules_enabled = module_catalog.modules.len();
    let system_loaded = system.is_some();
    let local_required = cfg.local_model.enabled;

    Ok(RuntimeSnapshot {
        provider: default_provider,
        provider_host: host,
        pipeline: Arc::new(pipeline),
        config: runtime_config,
        project_config: project,
        system_config: system,
        modules: module_catalog,
        classifier,
        route_catalog,
        route_status,
        readiness: ReadinessInfo {
            retrieval_mode,
            system_config_loaded: system_loaded,
            audit_ledger_path: audit_ledger_path.to_string(),
            local_model_required: local_required,
            modules_discovered,
            modules_enabled,
        },
        ollama_checker,
    })
}

/// Mirrors `augmentRuntimeConfigWithModuleLLMAdapters` in Go: appends LLM
/// adapter entries declared by trusted modules into the runtime config,
/// rejecting collisions with existing provider ids.
pub fn augment_runtime_config_with_module_llm_adapters(
    cfg: Config,
    module_catalog: &ModuleCatalog,
) -> Result<Config, BuildError> {
    if cfg.schema_version < SCHEMA_VERSION_V2 || module_catalog.modules.is_empty() {
        return Ok(cfg);
    }

    let mut augmented = cfg;
    let mut seen: std::collections::BTreeMap<String, String> = augmented
        .llms
        .providers
        .iter()
        .filter_map(|provider| {
            let id = provider.id.trim();
            if id.is_empty() {
                None
            } else {
                Some((id.to_string(), "runtime config".to_string()))
            }
        })
        .collect();

    for module in &module_catalog.modules {
        for adapter in &module.llm_adapters {
            let id = adapter.id.trim().to_string();
            if id.is_empty() {
                continue;
            }
            if let Some(first) = seen.get(&id) {
                return Err(BuildError::AdapterCollision {
                    id,
                    first: first.clone(),
                });
            }
            seen.insert(id.clone(), format!("module {}", module.id));
            augmented.llms.providers.push(LlmProviderConfig {
                id,
                ..adapter.clone()
            });
        }
    }
    Ok(augmented)
}

/// Adapter from `rillan_ollama::Client` to the `OllamaChecker` trait used by
/// the readyz handler.
struct OllamaPing {
    client: rillan_ollama::Client,
}

#[async_trait]
impl OllamaChecker for OllamaPing {
    async fn ready(&self) -> Result<(), String> {
        self.client.ping().await.map_err(|err| err.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::{LlmRegistryConfig, SCHEMA_VERSION_V2};
    use rillan_modules::{Catalog as ModuleCatalog, LoadedModule};
    use std::collections::BTreeMap;

    fn module_with_adapter(id: &str, adapter_id: &str) -> LoadedModule {
        LoadedModule {
            id: id.to_string(),
            display_name: String::new(),
            version: "0.1.0".into(),
            root_path: PathBuf::new(),
            manifest_sha256: String::new(),
            manifest_path: PathBuf::new(),
            entrypoint: vec![],
            llm_adapters: vec![LlmProviderConfig {
                id: adapter_id.into(),
                backend: "openai_compatible".into(),
                transport: rillan_config::LLM_TRANSPORT_HTTP.into(),
                endpoint: "https://example.com/v1".into(),
                ..LlmProviderConfig::default()
            }],
            mcp_servers: vec![],
            lsp_servers: vec![],
        }
    }

    fn cfg_with_provider(id: &str) -> Config {
        Config {
            schema_version: SCHEMA_VERSION_V2,
            llms: LlmRegistryConfig {
                default: id.to_string(),
                providers: vec![LlmProviderConfig {
                    id: id.into(),
                    backend: "openai_compatible".into(),
                    transport: rillan_config::LLM_TRANSPORT_HTTP.into(),
                    endpoint: "https://api.openai.com/v1".into(),
                    ..LlmProviderConfig::default()
                }],
            },
            ..Config::default()
        }
    }

    #[test]
    fn augment_passes_through_when_no_modules() {
        let cfg = cfg_with_provider("openai");
        let catalog = ModuleCatalog::default();
        let augmented = augment_runtime_config_with_module_llm_adapters(cfg.clone(), &catalog)
            .expect("augment");
        assert_eq!(augmented.llms.providers.len(), cfg.llms.providers.len());
    }

    #[test]
    fn augment_appends_module_adapters() {
        let cfg = cfg_with_provider("openai");
        let catalog = ModuleCatalog {
            modules_dir: PathBuf::new(),
            modules: vec![module_with_adapter("demo", "demo-llm")],
        };
        let augmented = augment_runtime_config_with_module_llm_adapters(cfg, &catalog).unwrap();
        let ids: Vec<&str> = augmented
            .llms
            .providers
            .iter()
            .map(|p| p.id.as_str())
            .collect();
        assert_eq!(ids, vec!["openai", "demo-llm"]);
    }

    #[test]
    fn augment_rejects_id_collisions() {
        let cfg = cfg_with_provider("demo-llm");
        let catalog = ModuleCatalog {
            modules_dir: PathBuf::new(),
            modules: vec![module_with_adapter("demo", "demo-llm")],
        };
        let err =
            augment_runtime_config_with_module_llm_adapters(cfg, &catalog).expect_err("collision");
        assert!(matches!(err, BuildError::AdapterCollision { .. }));
    }

    #[tokio::test]
    async fn build_from_loaded_produces_default_snapshot() {
        let dir = tempfile::tempdir().unwrap();
        let project_path = dir.path().join(".rillan").join("project.yaml");
        std::fs::create_dir_all(project_path.parent().unwrap()).unwrap();
        std::fs::write(&project_path, b"name: demo\nclassification: open_source\n").unwrap();

        // Use a single-provider config with auth_strategy=none so we don't
        // need to prime the keyring.
        let mut cfg = cfg_with_provider("openai");
        cfg.llms.providers[0].auth_strategy = rillan_config::AUTH_STRATEGY_NONE.into();
        cfg.llms.providers[0].credential_ref.clear();
        cfg.runtime.vector_store_mode = "embedded".into();

        let project = rillan_config::default_project_config();
        let store = Store::in_memory();
        let builder = SnapshotBuilder {
            config_path: dir.path().join("config.yaml"),
            system_config_path: dir.path().join("system.yaml"),
            audit_ledger_path: dir
                .path()
                .join("ledger.jsonl")
                .to_string_lossy()
                .to_string(),
            store,
        };
        let snapshot = builder
            .build_from_loaded(cfg, project, None, &project_path)
            .await
            .expect("build");
        assert_eq!(snapshot.readiness.modules_enabled, 0);
        assert!(snapshot.classifier.is_none());
        assert!(snapshot.ollama_checker.is_none());
        // Pipeline is always present; retrieval mode reflects the disabled default.
        assert_eq!(snapshot.readiness.retrieval_mode, "disabled");
        assert_eq!(snapshot.provider.name(), "openai");
    }

    // Suppress an unused-import warning when no test references this map.
    #[allow(dead_code)]
    fn _types() {
        let _: BTreeMap<String, String> = BTreeMap::new();
    }
}
