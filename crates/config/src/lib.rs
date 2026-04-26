// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Three-tier configuration: runtime config, repo-local project config, and
//! per-machine system policy. Mirrors `internal/config` from the upstream Go
//! repo.
//!
//! Public surface in this crate:
//!
//! * [`Config`], [`ProjectConfig`], [`SystemConfig`] — the user-facing schema.
//! * [`load`], [`load_with_mode`], [`load_project`], [`load_system`] — disk
//!   reads with env overrides and validation.
//! * [`Validation`] — validation mode selector for the CLI (serve / index /
//!   status).
//! * [`default_config_path`] / [`default_data_dir`] / [`default_log_dir`] —
//!   per-platform path resolution.
//! * [`write_example_config`] / [`write_example_project_config`] —
//!   one-shot scaffolding used by `rillan init`.

mod constants;
mod load;
mod paths;
mod resolve;
mod system_crypto;
mod types;
mod validate;
mod write_example;

pub use constants::{
    AUTH_STRATEGY_API_KEY, AUTH_STRATEGY_BROWSER_OIDC, AUTH_STRATEGY_DEVICE_OIDC,
    AUTH_STRATEGY_NONE, DEFAULT_RUNTIME_PROVIDER_ID, DEFAULT_SERVER_HOST, DEFAULT_SERVER_PORT,
    LLM_BACKEND_OPENAI_COMPATIBLE, LLM_PRESET_ANTHROPIC, LLM_PRESET_DEEPSEEK, LLM_PRESET_KIMI,
    LLM_PRESET_OPENAI, LLM_PRESET_XAI, LLM_PRESET_ZAI, LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO,
    PROJECT_CLASSIFICATION_INTERNAL, PROJECT_CLASSIFICATION_OPEN_SOURCE,
    PROJECT_CLASSIFICATION_PROPRIETARY, PROJECT_CLASSIFICATION_TRADE_SECRET, PROVIDER_ANTHROPIC,
    PROVIDER_DEEPSEEK, PROVIDER_KIMI, PROVIDER_LOCAL, PROVIDER_OLLAMA, PROVIDER_OPENAI,
    PROVIDER_OPENAI_COMPATIBLE, PROVIDER_XAI, PROVIDER_ZAI, ROUTE_PREFERENCE_AUTO,
    ROUTE_PREFERENCE_LOCAL_ONLY, ROUTE_PREFERENCE_PREFER_CLOUD, ROUTE_PREFERENCE_PREFER_LOCAL,
    SCHEMA_VERSION_V1, SCHEMA_VERSION_V2,
};
pub use load::{
    apply_environment_overrides, default_config, default_project_config, default_system_config,
    load, load_for_edit, load_project, load_system, load_with_mode, write_config, Error,
    Validation,
};
pub use paths::{
    default_config_path, default_data_dir, default_log_dir, default_project_config_path,
    default_system_config_path, legacy_project_config_path, legacy_system_config_path,
    resolve_project_config_path, resolve_system_config_path,
};
pub use resolve::{
    resolve_active_llm_provider, resolve_llm_provider_by_id, resolve_runtime_provider_adapter,
    resolve_runtime_provider_host, resolve_server_auth_bearer, ResolveError,
};
pub use system_crypto::{decrypt_system_policy, SystemCryptoError};
pub use types::{
    bundled_llm_provider_preset, bundled_llm_provider_presets, AgentRuntimeConfig, AnthropicConfig,
    AuthConfig, Config, ControlPlaneAuthConfig, IndexConfig, KnowledgeGraphConfig,
    LlmProviderConfig, LlmProviderPreset, LlmRegistryConfig, LocalModelConfig, LocalModelProvider,
    McpConfig, McpRegistryConfig, McpServerConfig, OpenAiConfig, ProjectAgentConfig, ProjectConfig,
    ProjectModuleSelectionConfig, ProjectProviderSelectionConfig, ProjectRoutingConfig,
    ProjectSkillSelectionConfig, ProjectSource, ProviderConfig, QueryRewriteConfig,
    ResolvedLlmProvider, RetrievalConfig, RuntimeConfig, RuntimeProviderAdapterConfig,
    RuntimeProviderHostConfig, ServerAuthConfig, ServerConfig, SystemConfig,
    SystemEncryptionConfig, SystemIdentityRules, SystemPolicy, SystemPolicyRules,
    TrustedModulePolicy,
};
pub use validate::{validate, validate_for_mode, validate_project, validate_system, ValidateError};
pub use write_example::{write_example_config, write_example_project_config};

/// Returns the default [`tracing`] level for the given log-level string.
/// Mirrors `config.ParseLogLevel` from the Go repo.
#[must_use]
pub fn parse_log_level(level: &str) -> tracing::Level {
    match level.trim().to_lowercase().as_str() {
        "debug" => tracing::Level::DEBUG,
        "warn" | "warning" => tracing::Level::WARN,
        "error" => tracing::Level::ERROR,
        _ => tracing::Level::INFO,
    }
}
