// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Disk loading, env overrides, and `applyDerivedDefaults`. Mirrors
//! `internal/config/load.go` from the Go repo.

use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use thiserror::Error;

use crate::constants::{
    self, AUTH_STRATEGY_API_KEY, DEFAULT_SERVER_HOST, DEFAULT_SERVER_PORT,
    DEFAULT_SYSTEM_KEYRING_ACCOUNT, DEFAULT_SYSTEM_KEYRING_SERVICE, LLM_PRESET_OPENAI,
    LLM_TRANSPORT_HTTP, PROJECT_CLASSIFICATION_OPEN_SOURCE, PROVIDER_OPENAI, ROUTE_PREFERENCE_AUTO,
    SCHEMA_VERSION_V2, SYSTEM_CONFIG_VERSION, SYSTEM_ENCRYPTION_KEYRING_AES_GCM,
};
use crate::types::{
    bundled_llm_provider_preset, AgentRuntimeConfig, AnthropicConfig, AuthConfig, Config,
    IndexConfig, KnowledgeGraphConfig, LlmProviderConfig, LlmRegistryConfig, LocalModelConfig,
    LocalModelProvider, McpConfig, McpRegistryConfig, OpenAiConfig, ProjectAgentConfig,
    ProjectConfig, ProjectModuleSelectionConfig, ProjectProviderSelectionConfig,
    ProjectRoutingConfig, ProjectSkillSelectionConfig, ProviderConfig, QueryRewriteConfig,
    RetrievalConfig, RuntimeConfig, ServerAuthConfig, ServerConfig, SystemConfig,
    SystemEncryptionConfig,
};
pub use crate::validate::Validation;
use crate::validate::{validate_for_mode, validate_project, validate_system, ValidateError};

/// Top-level error returned by config loading.
#[derive(Debug, Error)]
pub enum Error {
    #[error("config file not found at {path}; run `rillan init --output {path}` first")]
    NotFound { path: PathBuf },
    #[error("read config: {0}")]
    Read(#[source] io::Error),
    #[error("write config: {0}")]
    Write(#[source] io::Error),
    #[error("create config directory: {0}")]
    CreateDir(#[source] io::Error),
    #[error("parse config: {0}")]
    Parse(#[source] serde_yaml::Error),
    #[error("marshal config: {0}")]
    Marshal(#[source] serde_yaml::Error),
    #[error("validate: {0}")]
    Validate(#[from] ValidateError),
    #[error(
        "system config must not contain plaintext {key:?} data; store only encrypted_payload and keyring metadata"
    )]
    PlaintextSystemPolicy { key: &'static str },
    #[error("parse system config envelope: {0}")]
    SystemEnvelope(#[source] serde_yaml::Error),
}

/// Loads a runtime configuration in `serve` mode.
pub fn load(path: &Path) -> Result<Config, Error> {
    load_with_mode(path, Validation::Serve)
}

/// Loads a runtime configuration in the requested validation mode.
pub fn load_with_mode(path: &Path, mode: Validation) -> Result<Config, Error> {
    let data = match fs::read(path) {
        Ok(bytes) => bytes,
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            return Err(Error::NotFound {
                path: path.to_path_buf(),
            });
        }
        Err(err) => return Err(Error::Read(err)),
    };
    let mut cfg = parse_config(&data)?;
    apply_environment_overrides(&mut cfg);
    apply_derived_defaults(&mut cfg, path);
    validate_for_mode(&cfg, mode)?;
    Ok(cfg)
}

/// Loads a config for the edit path used by `rillan llm` / `rillan mcp` mutators.
/// Missing files yield the default config.
pub fn load_for_edit(path: &Path) -> Result<Config, Error> {
    let data = match fs::read(path) {
        Ok(bytes) => bytes,
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            let mut cfg = default_config();
            apply_derived_defaults(&mut cfg, path);
            return Ok(cfg);
        }
        Err(err) => return Err(Error::Read(err)),
    };
    let mut cfg = parse_config(&data)?;
    apply_derived_defaults(&mut cfg, path);
    Ok(cfg)
}

/// Loads a `.rillan/project.yaml`.
pub fn load_project(path: &Path) -> Result<ProjectConfig, Error> {
    let data = fs::read(path).map_err(Error::Read)?;
    let mut cfg = parse_project(&data)?;
    apply_project_derived_defaults(&mut cfg, path);
    validate_project(&cfg)?;
    Ok(cfg)
}

/// Loads a system config from disk and rejects plaintext policy fields.
pub fn load_system(path: &Path) -> Result<SystemConfig, Error> {
    let data = fs::read(path).map_err(Error::Read)?;
    reject_plaintext_system_config(&data)?;
    let mut cfg: SystemConfig = serde_yaml::from_slice(&data).map_err(Error::Parse)?;
    apply_system_derived_defaults(&mut cfg);
    validate_system(&cfg)?;
    Ok(cfg)
}

fn parse_config(data: &[u8]) -> Result<Config, Error> {
    if data.iter().all(u8::is_ascii_whitespace) {
        return Ok(default_config());
    }
    let cfg: Config = serde_yaml::from_slice(data).map_err(Error::Parse)?;
    Ok(cfg)
}

fn parse_project(data: &[u8]) -> Result<ProjectConfig, Error> {
    if data.iter().all(u8::is_ascii_whitespace) {
        return Ok(default_project_config());
    }
    let cfg: ProjectConfig = serde_yaml::from_slice(data).map_err(Error::Parse)?;
    Ok(cfg)
}

/// Writes a config back to disk, scrubbing plaintext API keys when schema v2
/// is in use (credentials live in the keyring instead).
pub fn write_config(path: &Path, cfg: &Config) -> Result<(), Error> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            fs::create_dir_all(parent).map_err(Error::CreateDir)?;
        }
    }
    let mut output = cfg.clone();
    if output.schema_version >= SCHEMA_VERSION_V2 {
        output.provider.openai.api_key.clear();
        output.provider.anthropic.api_key.clear();
    }
    let data = serde_yaml::to_string(&output).map_err(Error::Marshal)?;
    fs::write(path, data).map_err(Error::Write)?;
    Ok(())
}

/// Returns the default runtime configuration with all schema-v2 registries
/// populated.
#[must_use]
pub fn default_config() -> Config {
    Config {
        schema_version: SCHEMA_VERSION_V2,
        server: ServerConfig {
            host: DEFAULT_SERVER_HOST.to_string(),
            port: DEFAULT_SERVER_PORT,
            log_level: "info".to_string(),
            allow_non_loopback_bind: false,
            auth: ServerAuthConfig {
                enabled: false,
                auth_strategy: AUTH_STRATEGY_API_KEY.to_string(),
                session_ref: "keyring://rillan/auth/daemon".to_string(),
            },
        },
        provider: ProviderConfig {
            kind: PROVIDER_OPENAI.to_string(),
            openai: OpenAiConfig {
                base_url: "https://api.openai.com/v1".to_string(),
                api_key: String::new(),
            },
            anthropic: AnthropicConfig {
                enabled: false,
                base_url: "https://api.anthropic.com".to_string(),
                api_key: String::new(),
            },
            local: LocalModelProvider {
                base_url: "http://127.0.0.1:11434".to_string(),
            },
        },
        index: IndexConfig {
            root: String::new(),
            includes: Vec::new(),
            excludes: vec![
                ".git".to_string(),
                "node_modules".to_string(),
                ".direnv".to_string(),
                ".idea".to_string(),
            ],
            chunk_size_lines: 120,
        },
        knowledge_graph: KnowledgeGraphConfig {
            enabled: false,
            path: String::new(),
            auto_update: "none".to_string(),
            traversal_depth: 1,
            include_inferred: true,
            max_nodes: 2000,
        },
        retrieval: RetrievalConfig {
            enabled: false,
            top_k: 4,
            max_context_chars: 6000,
        },
        runtime: RuntimeConfig {
            vector_store_mode: "embedded".to_string(),
            local_model_base_url: "http://127.0.0.1:11434".to_string(),
        },
        local_model: LocalModelConfig {
            enabled: false,
            base_url: "http://127.0.0.1:11434".to_string(),
            embed_model: "nomic-embed-text".to_string(),
            query_rewrite: QueryRewriteConfig {
                enabled: false,
                model: "qwen3:0.6b".to_string(),
            },
        },
        agent: AgentRuntimeConfig {
            enabled: false,
            approved_repo_roots: Vec::new(),
            mcp: McpConfig {
                enabled: false,
                read_only: true,
                max_open_files: 8,
                max_diagnostics: 20,
            },
        },
        auth: AuthConfig::default(),
        llms: LlmRegistryConfig {
            default: LLM_PRESET_OPENAI.to_string(),
            providers: vec![
                bundled_llm_provider_preset(LLM_PRESET_OPENAI).provider_config(LLM_PRESET_OPENAI),
                bundled_llm_provider_preset(constants::LLM_PRESET_ANTHROPIC)
                    .provider_config(constants::LLM_PRESET_ANTHROPIC),
                bundled_llm_provider_preset(constants::LLM_PRESET_XAI)
                    .provider_config(constants::LLM_PRESET_XAI),
                bundled_llm_provider_preset(constants::LLM_PRESET_DEEPSEEK)
                    .provider_config(constants::LLM_PRESET_DEEPSEEK),
                bundled_llm_provider_preset(constants::LLM_PRESET_KIMI)
                    .provider_config(constants::LLM_PRESET_KIMI),
                bundled_llm_provider_preset(constants::LLM_PRESET_ZAI)
                    .provider_config(constants::LLM_PRESET_ZAI),
            ],
        },
        mcps: McpRegistryConfig {
            default: String::new(),
            servers: Vec::new(),
        },
    }
}

/// Returns the default repo-local project configuration.
#[must_use]
pub fn default_project_config() -> ProjectConfig {
    ProjectConfig {
        classification: PROJECT_CLASSIFICATION_OPEN_SOURCE.to_string(),
        sources: Vec::new(),
        routing: ProjectRoutingConfig {
            default: ROUTE_PREFERENCE_AUTO.to_string(),
            task_types: std::collections::BTreeMap::new(),
        },
        providers: ProjectProviderSelectionConfig::default(),
        modules: ProjectModuleSelectionConfig::default(),
        agent: ProjectAgentConfig {
            skills: ProjectSkillSelectionConfig::default(),
        },
        instructions: Vec::new(),
        ..ProjectConfig::default()
    }
}

/// Returns the default system config (no encrypted payload).
#[must_use]
pub fn default_system_config() -> SystemConfig {
    SystemConfig {
        version: SYSTEM_CONFIG_VERSION.to_string(),
        encryption: SystemEncryptionConfig {
            method: SYSTEM_ENCRYPTION_KEYRING_AES_GCM.to_string(),
            keyring_service: DEFAULT_SYSTEM_KEYRING_SERVICE.to_string(),
            keyring_account: DEFAULT_SYSTEM_KEYRING_ACCOUNT.to_string(),
        },
        ..SystemConfig::default()
    }
}

/// Mutates `cfg` in place, applying environment-variable overrides. Mirrors
/// `applyEnvOverrides`.
pub fn apply_environment_overrides(cfg: &mut Config) {
    apply_string_env(&mut cfg.server.host, &["RILLAN_SERVER_HOST"]);
    apply_u16_env(&mut cfg.server.port, "RILLAN_SERVER_PORT");
    apply_string_env(&mut cfg.server.log_level, &["RILLAN_SERVER_LOG_LEVEL"]);
    apply_bool_env(
        &mut cfg.server.allow_non_loopback_bind,
        "RILLAN_SERVER_ALLOW_NON_LOOPBACK_BIND",
    );
    apply_bool_env(&mut cfg.server.auth.enabled, "RILLAN_SERVER_AUTH_ENABLED");
    apply_string_env(
        &mut cfg.server.auth.auth_strategy,
        &["RILLAN_SERVER_AUTH_STRATEGY"],
    );
    apply_string_env(
        &mut cfg.server.auth.session_ref,
        &["RILLAN_SERVER_AUTH_SESSION_REF"],
    );

    apply_string_env(&mut cfg.provider.kind, &["RILLAN_PROVIDER_TYPE"]);
    apply_string_env(
        &mut cfg.provider.openai.base_url,
        &["RILLAN_OPENAI_BASE_URL"],
    );
    apply_string_env(
        &mut cfg.provider.openai.api_key,
        &["RILLAN_OPENAI_API_KEY", "OPENAI_API_KEY"],
    );
    apply_bool_env(
        &mut cfg.provider.anthropic.enabled,
        "RILLAN_ANTHROPIC_ENABLED",
    );
    apply_string_env(
        &mut cfg.provider.anthropic.base_url,
        &["RILLAN_ANTHROPIC_BASE_URL"],
    );
    apply_string_env(
        &mut cfg.provider.anthropic.api_key,
        &["RILLAN_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"],
    );
    apply_string_env(
        &mut cfg.provider.local.base_url,
        &["RILLAN_LOCAL_MODEL_BASE_URL"],
    );

    apply_string_env(&mut cfg.index.root, &["RILLAN_INDEX_ROOT"]);
    apply_csv_env(&mut cfg.index.includes, "RILLAN_INDEX_INCLUDES");
    apply_csv_env(&mut cfg.index.excludes, "RILLAN_INDEX_EXCLUDES");
    apply_usize_env(
        &mut cfg.index.chunk_size_lines,
        "RILLAN_INDEX_CHUNK_SIZE_LINES",
    );

    apply_bool_env(
        &mut cfg.knowledge_graph.enabled,
        "RILLAN_KNOWLEDGE_GRAPH_ENABLED",
    );
    apply_string_env(
        &mut cfg.knowledge_graph.path,
        &["RILLAN_KNOWLEDGE_GRAPH_PATH"],
    );
    apply_string_env(
        &mut cfg.knowledge_graph.auto_update,
        &["RILLAN_KNOWLEDGE_GRAPH_AUTO_UPDATE"],
    );
    apply_i64_env(
        &mut cfg.knowledge_graph.traversal_depth,
        "RILLAN_KNOWLEDGE_GRAPH_TRAVERSAL_DEPTH",
    );
    apply_bool_env(
        &mut cfg.knowledge_graph.include_inferred,
        "RILLAN_KNOWLEDGE_GRAPH_INCLUDE_INFERRED",
    );
    apply_i64_env(
        &mut cfg.knowledge_graph.max_nodes,
        "RILLAN_KNOWLEDGE_GRAPH_MAX_NODES",
    );

    apply_bool_env(&mut cfg.retrieval.enabled, "RILLAN_RETRIEVAL_ENABLED");
    apply_usize_env(&mut cfg.retrieval.top_k, "RILLAN_RETRIEVAL_TOP_K");
    apply_usize_env(
        &mut cfg.retrieval.max_context_chars,
        "RILLAN_RETRIEVAL_MAX_CONTEXT_CHARS",
    );

    apply_string_env(
        &mut cfg.runtime.vector_store_mode,
        &["RILLAN_VECTOR_STORE_MODE"],
    );
    apply_string_env(
        &mut cfg.runtime.local_model_base_url,
        &["RILLAN_LOCAL_MODEL_BASE_URL"],
    );

    apply_bool_env(&mut cfg.local_model.enabled, "RILLAN_LOCAL_MODEL_ENABLED");
    apply_string_env(
        &mut cfg.local_model.base_url,
        &["RILLAN_LOCAL_MODEL_BASE_URL"],
    );
    apply_string_env(
        &mut cfg.local_model.embed_model,
        &["RILLAN_LOCAL_MODEL_EMBED_MODEL"],
    );
    apply_bool_env(
        &mut cfg.local_model.query_rewrite.enabled,
        "RILLAN_LOCAL_MODEL_QUERY_REWRITE_ENABLED",
    );
    apply_string_env(
        &mut cfg.local_model.query_rewrite.model,
        &["RILLAN_LOCAL_MODEL_QUERY_REWRITE_MODEL"],
    );

    apply_bool_env(&mut cfg.agent.enabled, "RILLAN_AGENT_ENABLED");
    apply_bool_env(&mut cfg.agent.mcp.enabled, "RILLAN_AGENT_MCP_ENABLED");
    apply_bool_env(&mut cfg.agent.mcp.read_only, "RILLAN_AGENT_MCP_READ_ONLY");
    apply_usize_env(
        &mut cfg.agent.mcp.max_open_files,
        "RILLAN_AGENT_MCP_MAX_OPEN_FILES",
    );
    apply_usize_env(
        &mut cfg.agent.mcp.max_diagnostics,
        "RILLAN_AGENT_MCP_MAX_DIAGNOSTICS",
    );
}

fn apply_string_env(target: &mut String, keys: &[&str]) {
    for key in keys {
        if let Ok(value) = std::env::var(key) {
            *target = value;
            return;
        }
    }
}

fn apply_bool_env(target: &mut bool, key: &str) {
    if let Ok(value) = std::env::var(key) {
        if let Some(parsed) = parse_bool(&value) {
            *target = parsed;
        }
    }
}

fn apply_u16_env(target: &mut u16, key: &str) {
    if let Ok(value) = std::env::var(key) {
        if let Ok(parsed) = value.parse::<u16>() {
            *target = parsed;
        }
    }
}

fn apply_usize_env(target: &mut usize, key: &str) {
    if let Ok(value) = std::env::var(key) {
        if let Ok(parsed) = value.parse::<usize>() {
            *target = parsed;
        }
    }
}

fn apply_i64_env(target: &mut i64, key: &str) {
    if let Ok(value) = std::env::var(key) {
        if let Ok(parsed) = value.parse::<i64>() {
            *target = parsed;
        }
    }
}

fn apply_csv_env(target: &mut Vec<String>, key: &str) {
    if let Ok(value) = std::env::var(key) {
        let items: Vec<String> = value
            .split(',')
            .map(str::trim)
            .filter(|part| !part.is_empty())
            .map(str::to_string)
            .collect();
        *target = items;
    }
}

/// Mirrors Go's `strconv.ParseBool`: accepts `1`, `0`, `t`, `f`, `T`, `F`,
/// `true`, `false`, `TRUE`, `FALSE`, `True`, `False`. Returns None otherwise.
fn parse_bool(value: &str) -> Option<bool> {
    match value {
        "1" | "t" | "T" | "true" | "TRUE" | "True" => Some(true),
        "0" | "f" | "F" | "false" | "FALSE" | "False" => Some(false),
        _ => None,
    }
}

/// Fills in derived defaults that the Go repo applies after a config has been
/// parsed. Mirrors `applyDerivedDefaults`.
pub(crate) fn apply_derived_defaults(cfg: &mut Config, config_path: &Path) {
    if cfg.schema_version == 0 {
        cfg.schema_version = SCHEMA_VERSION_V2;
    }
    if cfg.provider.kind.is_empty() {
        cfg.provider.kind = PROVIDER_OPENAI.to_string();
    }
    if cfg.provider.openai.base_url.is_empty() {
        cfg.provider.openai.base_url = "https://api.openai.com/v1".to_string();
    }
    if cfg.provider.anthropic.base_url.is_empty() {
        cfg.provider.anthropic.base_url = "https://api.anthropic.com".to_string();
    }
    if cfg.provider.local.base_url.is_empty() {
        cfg.provider.local.base_url = "http://127.0.0.1:11434".to_string();
    }
    if cfg.runtime.local_model_base_url.is_empty() {
        cfg.runtime.local_model_base_url = cfg.provider.local.base_url.clone();
    }
    if cfg.runtime.vector_store_mode.is_empty() {
        cfg.runtime.vector_store_mode = "embedded".to_string();
    }
    if cfg.index.excludes.is_empty() {
        cfg.index.excludes = default_config().index.excludes;
    }
    if cfg.index.chunk_size_lines == 0 {
        cfg.index.chunk_size_lines = default_config().index.chunk_size_lines;
    }
    if cfg.knowledge_graph.auto_update.trim().is_empty() {
        cfg.knowledge_graph.auto_update = default_config().knowledge_graph.auto_update;
    }
    if cfg.knowledge_graph.traversal_depth == 0 {
        cfg.knowledge_graph.traversal_depth = default_config().knowledge_graph.traversal_depth;
    }
    if cfg.knowledge_graph.max_nodes == 0 {
        cfg.knowledge_graph.max_nodes = default_config().knowledge_graph.max_nodes;
    }
    if cfg.retrieval.top_k == 0 {
        cfg.retrieval.top_k = default_config().retrieval.top_k;
    }
    if cfg.retrieval.max_context_chars == 0 {
        cfg.retrieval.max_context_chars = default_config().retrieval.max_context_chars;
    }
    if cfg.server.host.is_empty() {
        cfg.server.host = DEFAULT_SERVER_HOST.to_string();
    }
    if cfg.server.port == 0 {
        cfg.server.port = DEFAULT_SERVER_PORT;
    }
    if cfg.server.log_level.is_empty() {
        cfg.server.log_level = "info".to_string();
    }
    if cfg.local_model.base_url.is_empty() {
        cfg.local_model.base_url = "http://127.0.0.1:11434".to_string();
    }
    if cfg.local_model.embed_model.is_empty() {
        cfg.local_model.embed_model = "nomic-embed-text".to_string();
    }
    if cfg.local_model.query_rewrite.model.is_empty() {
        cfg.local_model.query_rewrite.model = "qwen3:0.6b".to_string();
    }
    if cfg.agent.mcp.max_open_files == 0 {
        cfg.agent.mcp.max_open_files = default_config().agent.mcp.max_open_files;
    }
    if cfg.agent.mcp.max_diagnostics == 0 {
        cfg.agent.mcp.max_diagnostics = default_config().agent.mcp.max_diagnostics;
    }
    if !cfg.agent.mcp.enabled {
        cfg.agent.mcp.read_only = default_config().agent.mcp.read_only;
    }
    if !cfg.index.root.is_empty() {
        cfg.index.root = resolve_index_root(config_path, &cfg.index.root);
    }
    if !cfg.knowledge_graph.path.is_empty() {
        cfg.knowledge_graph.path = resolve_project_path(config_path, &cfg.knowledge_graph.path);
    }
    for entry in &mut cfg.agent.approved_repo_roots {
        let resolved = resolve_project_path(config_path, entry);
        *entry = resolved;
    }
    if cfg.llms.providers.is_empty() {
        cfg.llms.providers = default_config().llms.providers;
    }
    let providers_clone: Vec<LlmProviderConfig> = cfg.llms.providers.clone();
    for (index, provider) in cfg.llms.providers.iter_mut().enumerate() {
        apply_llm_provider_preset_defaults(provider, providers_clone.get(index));
    }
    if cfg.llms.default.is_empty() {
        if let Some(first) = cfg.llms.providers.first() {
            cfg.llms.default = first.id.clone();
        }
    }
}

fn apply_llm_provider_preset_defaults(
    provider: &mut LlmProviderConfig,
    _previous: Option<&LlmProviderConfig>,
) {
    let preset_id = constants::normalize(&provider.preset);
    if preset_id.is_empty() {
        return;
    }
    let preset = bundled_llm_provider_preset(&preset_id);
    if preset.id.is_empty() {
        return;
    }
    if provider.backend.trim().is_empty() {
        provider.backend = preset.family.to_string();
    }
    if provider.transport.trim().is_empty() {
        provider.transport = LLM_TRANSPORT_HTTP.to_string();
    }
    if provider.endpoint.trim().is_empty() {
        provider.endpoint = preset.endpoint.to_string();
    }
    if provider.auth_strategy.trim().is_empty() {
        provider.auth_strategy = preset.auth_strategy.to_string();
    }
    if provider.default_model.trim().is_empty() {
        provider.default_model = preset.default_model.to_string();
    }
    if provider.model_pins.is_empty() && !preset.model_pins.is_empty() {
        provider.model_pins = preset.model_pins.iter().map(|s| (*s).to_string()).collect();
    }
    if provider.capabilities.is_empty() {
        provider.capabilities = preset
            .capabilities
            .iter()
            .map(|s| (*s).to_string())
            .collect();
    }
    let trimmed_id = provider.id.trim().to_string();
    if provider.credential_ref.trim().is_empty() && !trimmed_id.is_empty() {
        provider.credential_ref = format!("keyring://rillan/llm/{trimmed_id}");
    }
    if provider.model_pins.is_empty() && !provider.default_model.trim().is_empty() {
        provider.model_pins = vec![provider.default_model.trim().to_string()];
    }
}

fn apply_project_derived_defaults(cfg: &mut ProjectConfig, project_path: &Path) {
    if cfg.classification.is_empty() {
        cfg.classification = default_project_config().classification;
    }
    if cfg.routing.default.is_empty() {
        cfg.routing.default = default_project_config().routing.default;
    }
    for entry in &mut cfg.sources {
        let resolved = resolve_project_path(project_path, &entry.path);
        entry.path = resolved;
    }
}

fn apply_system_derived_defaults(cfg: &mut SystemConfig) {
    if cfg.version.is_empty() {
        cfg.version = default_system_config().version;
    }
    if cfg.encryption.method.is_empty() {
        cfg.encryption.method = default_system_config().encryption.method;
    }
    if cfg.encryption.keyring_service.is_empty() {
        cfg.encryption.keyring_service = default_system_config().encryption.keyring_service;
    }
    if cfg.encryption.keyring_account.is_empty() {
        cfg.encryption.keyring_account = default_system_config().encryption.keyring_account;
    }
}

fn reject_plaintext_system_config(data: &[u8]) -> Result<(), Error> {
    let raw: serde_yaml::Value = serde_yaml::from_slice(data).map_err(Error::SystemEnvelope)?;
    if let serde_yaml::Value::Mapping(map) = raw {
        for key in ["identity", "rules", "policy"] {
            if map.contains_key(serde_yaml::Value::String(key.to_string())) {
                return Err(Error::PlaintextSystemPolicy { key });
            }
        }
    }
    Ok(())
}

fn resolve_index_root(config_path: &Path, root: &str) -> String {
    let trimmed = root.trim();
    if trimmed.is_empty() {
        return String::new();
    }
    let candidate = Path::new(trimmed);
    if candidate.is_absolute() {
        return candidate.to_string_lossy().to_string();
    }
    let base = config_path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .map_or_else(|| PathBuf::from("."), Path::to_path_buf);
    let resolved = base.join(candidate);
    resolved
        .canonicalize()
        .unwrap_or(resolved)
        .to_string_lossy()
        .to_string()
}

fn resolve_project_path(project_path: &Path, value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return String::new();
    }
    let candidate = Path::new(trimmed);
    if candidate.is_absolute() {
        return candidate.to_string_lossy().to_string();
    }
    let base = project_path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .map_or_else(|| PathBuf::from("."), Path::to_path_buf);
    let resolved = base.join(candidate);
    resolved
        .canonicalize()
        .unwrap_or(resolved)
        .to_string_lossy()
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_config_has_six_bundled_presets() {
        let cfg = default_config();
        assert_eq!(cfg.llms.providers.len(), 6);
        assert_eq!(cfg.llms.default, "openai");
        assert_eq!(cfg.server.port, DEFAULT_SERVER_PORT);
    }

    #[test]
    fn parse_bool_matches_go_strconv() {
        for input in ["1", "t", "T", "true", "TRUE", "True"] {
            assert_eq!(parse_bool(input), Some(true), "expected true for {input}");
        }
        for input in ["0", "f", "F", "false", "FALSE", "False"] {
            assert_eq!(parse_bool(input), Some(false), "expected false for {input}");
        }
        assert_eq!(parse_bool("yes"), None);
        assert_eq!(parse_bool(""), None);
    }

    #[test]
    fn apply_derived_defaults_fills_index_excludes() {
        let mut cfg = Config {
            schema_version: 0,
            ..default_config()
        };
        cfg.index.excludes.clear();
        cfg.knowledge_graph.auto_update.clear();
        cfg.knowledge_graph.traversal_depth = 0;
        cfg.knowledge_graph.max_nodes = 0;
        cfg.retrieval.top_k = 0;
        cfg.retrieval.max_context_chars = 0;
        cfg.server.host.clear();
        cfg.server.port = 0;
        cfg.server.log_level.clear();

        apply_derived_defaults(&mut cfg, Path::new("/tmp/rillan.yaml"));

        assert_eq!(cfg.schema_version, SCHEMA_VERSION_V2);
        assert!(!cfg.index.excludes.is_empty());
        assert_eq!(cfg.server.host, DEFAULT_SERVER_HOST);
        assert_eq!(cfg.server.port, DEFAULT_SERVER_PORT);
        assert_eq!(cfg.server.log_level, "info");
        assert_eq!(cfg.knowledge_graph.auto_update, "none");
    }

    #[test]
    fn round_trip_yaml() {
        let cfg = default_config();
        let yaml = serde_yaml::to_string(&cfg).expect("serialize");
        let back: Config = serde_yaml::from_str(&yaml).expect("deserialize");
        assert_eq!(back.llms.providers.len(), cfg.llms.providers.len());
        assert_eq!(back.server.port, cfg.server.port);
    }

    #[test]
    fn rejects_plaintext_system_config() {
        let yaml = "version: m06\npolicy:\n  identity:\n    people: []\n";
        let err = reject_plaintext_system_config(yaml.as_bytes()).expect_err("must reject");
        match err {
            Error::PlaintextSystemPolicy { key } => assert_eq!(key, "policy"),
            other => panic!("wrong error: {other:?}"),
        }
    }

    #[test]
    fn load_propagates_not_found() {
        let path = PathBuf::from("/definitely-not-here.yaml");
        let err = load(&path).expect_err("not found");
        assert!(matches!(err, Error::NotFound { .. }));
    }
}
