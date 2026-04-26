// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Validation logic. Mirrors `internal/config/validate.go`.

use std::net::IpAddr;

use thiserror::Error;

use crate::constants::{
    self, AUTH_STRATEGY_API_KEY, AUTH_STRATEGY_BROWSER_OIDC, AUTH_STRATEGY_DEVICE_OIDC,
    LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO, PROJECT_CLASSIFICATION_INTERNAL,
    PROJECT_CLASSIFICATION_OPEN_SOURCE, PROJECT_CLASSIFICATION_PROPRIETARY,
    PROJECT_CLASSIFICATION_TRADE_SECRET, PROVIDER_ANTHROPIC, PROVIDER_OLLAMA, PROVIDER_OPENAI,
    ROUTE_PREFERENCE_AUTO, ROUTE_PREFERENCE_LOCAL_ONLY, ROUTE_PREFERENCE_PREFER_CLOUD,
    ROUTE_PREFERENCE_PREFER_LOCAL, SCHEMA_VERSION_V2, SYSTEM_ENCRYPTION_KEYRING_AES_GCM,
};
use crate::resolve::resolve_active_llm_provider;
use crate::types::{
    bundled_llm_provider_preset, Config, ProjectConfig, ProjectRoutingConfig, SystemConfig,
};

/// CLI-exposed validation modes. `serve` is the strictest (provider must
/// exist, auth must be wired up); `index` only requires `index.root`;
/// `status` only validates structural defaults.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Validation {
    Serve,
    Index,
    Status,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ValidateError {
    #[error("server.host must not be empty")]
    EmptyHost,
    #[error("server.port must be between 1 and 65535")]
    BadPort,
    #[error("server.log_level must be one of debug, info, warn, or error")]
    BadLogLevel,
    #[error("runtime.vector_store_mode must be \"embedded\" in milestone two")]
    BadVectorStoreMode,
    #[error("server.auth.auth_strategy must be one of {:?}, {:?}, or {:?} when server.auth.enabled is true", AUTH_STRATEGY_API_KEY, AUTH_STRATEGY_BROWSER_OIDC, AUTH_STRATEGY_DEVICE_OIDC)]
    BadAuthStrategy,
    #[error("server.auth.session_ref must not be empty when server.auth.enabled is true")]
    MissingSessionRef,
    #[error("server.allow_non_loopback_bind must be true when server.host is not loopback")]
    NonLoopbackBindNotAllowed,
    #[error("server.auth.enabled must be true when server.host is not loopback")]
    NonLoopbackAuthRequired,
    #[error("server.host must be a loopback address or wildcard bind when non-loopback binds are enabled")]
    NonLoopbackHostNotWildcard,
    #[error("index.chunk_size_lines must be greater than zero")]
    BadChunkSize,
    #[error("knowledge_graph.auto_update must be one of none, poll, or watch")]
    BadAutoUpdate,
    #[error("knowledge_graph.traversal_depth must be zero or greater")]
    BadTraversalDepth,
    #[error("knowledge_graph.max_nodes must be greater than zero")]
    BadMaxNodes,
    #[error("retrieval.top_k must be greater than zero")]
    BadRetrievalTopK,
    #[error("retrieval.max_context_chars must be greater than zero")]
    BadRetrievalMaxContext,
    #[error("index.includes must not contain empty patterns")]
    EmptyIncludePattern,
    #[error("index.excludes must not contain empty patterns")]
    EmptyExcludePattern,
    #[error("local_model.base_url must not be empty when local_model is enabled")]
    LocalModelBaseUrlEmpty,
    #[error("local_model.embed_model must not be empty when local_model is enabled")]
    LocalModelEmbedEmpty,
    #[error("local_model.enabled must be true when query_rewrite is enabled")]
    LocalModelDisabledForRewrite,
    #[error("local_model.query_rewrite.model must not be empty when query_rewrite is enabled")]
    LocalModelRewriteModelEmpty,
    #[error("agent.mcp.read_only must be true in milestone seven")]
    McpNotReadOnly,
    #[error("agent.mcp.max_open_files must be greater than zero when MCP is enabled")]
    McpMaxOpenFilesZero,
    #[error("agent.mcp.max_diagnostics must be greater than zero when MCP is enabled")]
    McpMaxDiagnosticsZero,
    #[error("llms.default must not be empty")]
    LlmDefaultEmpty,
    #[error("llm provider {provider:?} preset {preset:?} is not bundled")]
    UnbundledPreset { provider: String, preset: String },
    #[error("llm provider {0:?} backend must not be empty")]
    LlmBackendEmpty(String),
    #[error("llm provider {0:?} endpoint must not be empty when transport is \"http\"")]
    LlmEndpointEmpty(String),
    #[error(
        "llm provider {0:?} endpoint must not be empty when backend is \"ollama\" and local_model.base_url is empty"
    )]
    LlmOllamaEndpointEmpty(String),
    #[error("llm provider {0:?} command must not be empty when transport is \"stdio\"")]
    LlmStdioCommandEmpty(String),
    #[error("llm provider {0:?} transport must be \"http\" or \"stdio\"")]
    LlmTransportInvalid(String),
    #[error("provider.openai.api_key is required for the openai provider")]
    OpenAiApiKeyMissing,
    #[error("anthropic is never implicit; set provider.anthropic.enabled=true to opt in")]
    AnthropicNotEnabled,
    #[error("provider.anthropic.api_key is required when anthropic is selected")]
    AnthropicApiKeyMissing,
    #[error(
        "provider.type must be one of {:?} or {:?}",
        PROVIDER_OPENAI,
        PROVIDER_ANTHROPIC
    )]
    UnknownProvider,
    #[error("index.root is required for the index command")]
    IndexRootMissing,
    #[error("project.name must not be empty")]
    ProjectNameEmpty,
    #[error(
        "project.classification must be one of {:?}, {:?}, {:?}, or {:?}",
        PROJECT_CLASSIFICATION_OPEN_SOURCE,
        PROJECT_CLASSIFICATION_INTERNAL,
        PROJECT_CLASSIFICATION_PROPRIETARY,
        PROJECT_CLASSIFICATION_TRADE_SECRET
    )]
    ProjectBadClassification,
    #[error(
        "{field} must be one of {:?}, {:?}, {:?}, or {:?}",
        ROUTE_PREFERENCE_AUTO,
        ROUTE_PREFERENCE_PREFER_LOCAL,
        ROUTE_PREFERENCE_PREFER_CLOUD,
        ROUTE_PREFERENCE_LOCAL_ONLY
    )]
    BadRoutePreference { field: String },
    #[error("project.sources[{index}].path must not be empty")]
    ProjectSourcePathEmpty { index: usize },
    #[error("project.sources[{index}].type must not be empty")]
    ProjectSourceTypeEmpty { index: usize },
    #[error("project.routing.task_types must not contain empty task names")]
    ProjectEmptyTaskName,
    #[error("project.instructions[{index}] must not be empty")]
    ProjectEmptyInstruction { index: usize },
    #[error("project.modules.enabled[{index}] must not be empty")]
    ProjectEmptyModule { index: usize },
    #[error("system.version must not be empty")]
    SystemVersionEmpty,
    #[error(
        "system.encryption.method must be {:?}",
        SYSTEM_ENCRYPTION_KEYRING_AES_GCM
    )]
    SystemEncryptionMethodWrong,
    #[error("system.encryption.keyring_service must not be empty")]
    SystemKeyringServiceEmpty,
    #[error("system.encryption.keyring_account must not be empty")]
    SystemKeyringAccountEmpty,
    #[error("system.encrypted_payload must not be empty")]
    SystemEncryptedPayloadEmpty,
    #[error("active llm provider not found: {0}")]
    ResolveActive(String),
}

/// Validates a runtime config in serve mode.
pub fn validate(cfg: &Config) -> Result<(), ValidateError> {
    validate_for_mode(cfg, Validation::Serve)
}

/// Validates a runtime config for the chosen mode.
pub fn validate_for_mode(cfg: &Config, mode: Validation) -> Result<(), ValidateError> {
    if cfg.server.host.is_empty() {
        return Err(ValidateError::EmptyHost);
    }
    if cfg.server.port == 0 {
        return Err(ValidateError::BadPort);
    }
    match constants::normalize(&cfg.server.log_level).as_str() {
        "debug" | "info" | "warn" | "error" => {}
        _ => return Err(ValidateError::BadLogLevel),
    }
    if constants::normalize(&cfg.runtime.vector_store_mode) != "embedded" {
        return Err(ValidateError::BadVectorStoreMode);
    }
    if cfg.server.auth.enabled {
        match constants::normalize(&cfg.server.auth.auth_strategy).as_str() {
            AUTH_STRATEGY_API_KEY | AUTH_STRATEGY_BROWSER_OIDC | AUTH_STRATEGY_DEVICE_OIDC => {}
            _ => return Err(ValidateError::BadAuthStrategy),
        }
        if cfg.server.auth.session_ref.trim().is_empty() {
            return Err(ValidateError::MissingSessionRef);
        }
    }
    if host_requires_non_loopback_opt_in(&cfg.server.host) {
        if !cfg.server.allow_non_loopback_bind {
            return Err(ValidateError::NonLoopbackBindNotAllowed);
        }
        if !cfg.server.auth.enabled {
            return Err(ValidateError::NonLoopbackAuthRequired);
        }
        if !is_wildcard_bind_host(&cfg.server.host) {
            return Err(ValidateError::NonLoopbackHostNotWildcard);
        }
    }
    if cfg.index.chunk_size_lines == 0 {
        return Err(ValidateError::BadChunkSize);
    }
    match constants::normalize(&cfg.knowledge_graph.auto_update).as_str() {
        "none" | "poll" | "watch" => {}
        _ => return Err(ValidateError::BadAutoUpdate),
    }
    if cfg.knowledge_graph.traversal_depth < 0 {
        return Err(ValidateError::BadTraversalDepth);
    }
    if cfg.knowledge_graph.max_nodes < 1 {
        return Err(ValidateError::BadMaxNodes);
    }
    if cfg.retrieval.top_k == 0 {
        return Err(ValidateError::BadRetrievalTopK);
    }
    if cfg.retrieval.max_context_chars == 0 {
        return Err(ValidateError::BadRetrievalMaxContext);
    }
    for pattern in &cfg.index.includes {
        if pattern.trim().is_empty() {
            return Err(ValidateError::EmptyIncludePattern);
        }
    }
    for pattern in &cfg.index.excludes {
        if pattern.trim().is_empty() {
            return Err(ValidateError::EmptyExcludePattern);
        }
    }

    if cfg.local_model.enabled {
        if cfg.local_model.base_url.trim().is_empty() {
            return Err(ValidateError::LocalModelBaseUrlEmpty);
        }
        if cfg.local_model.embed_model.trim().is_empty() {
            return Err(ValidateError::LocalModelEmbedEmpty);
        }
    }
    if cfg.local_model.query_rewrite.enabled {
        if !cfg.local_model.enabled {
            return Err(ValidateError::LocalModelDisabledForRewrite);
        }
        if cfg.local_model.query_rewrite.model.trim().is_empty() {
            return Err(ValidateError::LocalModelRewriteModelEmpty);
        }
    }

    if cfg.agent.mcp.enabled {
        if !cfg.agent.mcp.read_only {
            return Err(ValidateError::McpNotReadOnly);
        }
        if cfg.agent.mcp.max_open_files == 0 {
            return Err(ValidateError::McpMaxOpenFilesZero);
        }
        if cfg.agent.mcp.max_diagnostics == 0 {
            return Err(ValidateError::McpMaxDiagnosticsZero);
        }
    }

    match mode {
        Validation::Serve => validate_serve_provider(cfg)?,
        Validation::Index => {
            if cfg.index.root.trim().is_empty() {
                return Err(ValidateError::IndexRootMissing);
            }
        }
        Validation::Status => {}
    }

    Ok(())
}

fn validate_serve_provider(cfg: &Config) -> Result<(), ValidateError> {
    if cfg.schema_version >= SCHEMA_VERSION_V2 && !cfg.llms.providers.is_empty() {
        if cfg.llms.default.trim().is_empty() {
            return Err(ValidateError::LlmDefaultEmpty);
        }
        for provider in &cfg.llms.providers {
            let preset_id = provider.preset.trim();
            if !preset_id.is_empty() && bundled_llm_provider_preset(preset_id).id.is_empty() {
                return Err(ValidateError::UnbundledPreset {
                    provider: provider.id.clone(),
                    preset: preset_id.to_string(),
                });
            }
        }
        let active = resolve_active_llm_provider(cfg, &ProjectConfig::default())
            .map_err(|err| ValidateError::ResolveActive(err.to_string()))?;
        if active.backend.trim().is_empty() {
            return Err(ValidateError::LlmBackendEmpty(active.id.clone()));
        }
        match active.transport.as_str() {
            LLM_TRANSPORT_HTTP => {
                if constants::normalize(&active.backend) == PROVIDER_OLLAMA {
                    if active.endpoint.trim().is_empty()
                        && cfg.local_model.base_url.trim().is_empty()
                    {
                        return Err(ValidateError::LlmOllamaEndpointEmpty(active.id.clone()));
                    }
                } else if active.endpoint.trim().is_empty() {
                    return Err(ValidateError::LlmEndpointEmpty(active.id.clone()));
                }
            }
            LLM_TRANSPORT_STDIO => {
                if active.command.is_empty() {
                    return Err(ValidateError::LlmStdioCommandEmpty(active.id.clone()));
                }
            }
            _ => return Err(ValidateError::LlmTransportInvalid(active.id.clone())),
        }
        return Ok(());
    }

    match constants::normalize(&cfg.provider.kind).as_str() {
        PROVIDER_OPENAI => {
            if cfg.provider.openai.api_key.is_empty() {
                return Err(ValidateError::OpenAiApiKeyMissing);
            }
            Ok(())
        }
        PROVIDER_ANTHROPIC => {
            if !cfg.provider.anthropic.enabled {
                return Err(ValidateError::AnthropicNotEnabled);
            }
            if cfg.provider.anthropic.api_key.is_empty() {
                return Err(ValidateError::AnthropicApiKeyMissing);
            }
            Ok(())
        }
        _ => Err(ValidateError::UnknownProvider),
    }
}

/// Validates a project configuration.
pub fn validate_project(cfg: &ProjectConfig) -> Result<(), ValidateError> {
    if cfg.name.trim().is_empty() {
        return Err(ValidateError::ProjectNameEmpty);
    }
    match constants::normalize(&cfg.classification).as_str() {
        PROJECT_CLASSIFICATION_OPEN_SOURCE
        | PROJECT_CLASSIFICATION_INTERNAL
        | PROJECT_CLASSIFICATION_PROPRIETARY
        | PROJECT_CLASSIFICATION_TRADE_SECRET => {}
        _ => return Err(ValidateError::ProjectBadClassification),
    }
    validate_route_preference("project.routing.default", &cfg.routing)?;
    for (index, source) in cfg.sources.iter().enumerate() {
        if source.path.trim().is_empty() {
            return Err(ValidateError::ProjectSourcePathEmpty { index });
        }
        if source.kind.trim().is_empty() {
            return Err(ValidateError::ProjectSourceTypeEmpty { index });
        }
    }
    for (key, value) in &cfg.routing.task_types {
        if key.trim().is_empty() {
            return Err(ValidateError::ProjectEmptyTaskName);
        }
        validate_route_value(&format!("project.routing.task_types[{key:?}]"), value)?;
    }
    for (index, instruction) in cfg.instructions.iter().enumerate() {
        if instruction.trim().is_empty() {
            return Err(ValidateError::ProjectEmptyInstruction { index });
        }
    }
    for (index, module_id) in cfg.modules.enabled.iter().enumerate() {
        if module_id.trim().is_empty() {
            return Err(ValidateError::ProjectEmptyModule { index });
        }
    }
    Ok(())
}

/// Validates a system config envelope.
pub fn validate_system(cfg: &SystemConfig) -> Result<(), ValidateError> {
    if cfg.version.trim().is_empty() {
        return Err(ValidateError::SystemVersionEmpty);
    }
    if constants::normalize(&cfg.encryption.method) != SYSTEM_ENCRYPTION_KEYRING_AES_GCM {
        return Err(ValidateError::SystemEncryptionMethodWrong);
    }
    if cfg.encryption.keyring_service.trim().is_empty() {
        return Err(ValidateError::SystemKeyringServiceEmpty);
    }
    if cfg.encryption.keyring_account.trim().is_empty() {
        return Err(ValidateError::SystemKeyringAccountEmpty);
    }
    if cfg.encrypted_payload.trim().is_empty() {
        return Err(ValidateError::SystemEncryptedPayloadEmpty);
    }
    Ok(())
}

fn validate_route_preference(
    field: &str,
    routing: &ProjectRoutingConfig,
) -> Result<(), ValidateError> {
    validate_route_value(field, &routing.default)
}

fn validate_route_value(field: &str, value: &str) -> Result<(), ValidateError> {
    match constants::normalize(value).as_str() {
        ROUTE_PREFERENCE_AUTO
        | ROUTE_PREFERENCE_PREFER_LOCAL
        | ROUTE_PREFERENCE_PREFER_CLOUD
        | ROUTE_PREFERENCE_LOCAL_ONLY => Ok(()),
        _ => Err(ValidateError::BadRoutePreference {
            field: field.to_string(),
        }),
    }
}

fn host_requires_non_loopback_opt_in(host: &str) -> bool {
    let trimmed = host.trim().trim_matches(|c| c == '[' || c == ']');
    if trimmed.is_empty() || trimmed.eq_ignore_ascii_case("localhost") {
        return false;
    }
    if let Ok(ip) = trimmed.parse::<IpAddr>() {
        return !ip.is_loopback();
    }
    true
}

fn is_wildcard_bind_host(host: &str) -> bool {
    let trimmed = host.trim().trim_matches(|c| c == '[' || c == ']');
    trimmed == "0.0.0.0" || trimmed == "::"
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::load::default_config;

    #[test]
    fn default_config_serve_validates_only_with_credentials_or_v2() {
        let mut cfg = default_config();
        // schema v2 + populated providers means we don't fall back to legacy
        // checks; the active provider has no credential_ref set, so resolution
        // succeeds and the http endpoint is non-empty.
        validate_for_mode(&cfg, Validation::Serve).expect("serve mode passes");

        // status mode skips provider-specific checks regardless of provider state.
        cfg.server.log_level.clear();
        let err =
            validate_for_mode(&cfg, Validation::Status).expect_err("empty log level should fail");
        assert_eq!(err, ValidateError::BadLogLevel);
    }

    #[test]
    fn index_mode_requires_root() {
        let cfg = default_config();
        let err = validate_for_mode(&cfg, Validation::Index).expect_err("missing root");
        assert_eq!(err, ValidateError::IndexRootMissing);
    }

    #[test]
    fn project_validation_rejects_bad_classification() {
        let cfg = ProjectConfig {
            name: "demo".into(),
            classification: "external".into(),
            routing: ProjectRoutingConfig {
                default: ROUTE_PREFERENCE_AUTO.into(),
                ..ProjectRoutingConfig::default()
            },
            ..ProjectConfig::default()
        };
        assert_eq!(
            validate_project(&cfg),
            Err(ValidateError::ProjectBadClassification),
        );
    }

    #[test]
    fn host_loopback_classification_matches_go() {
        assert!(!host_requires_non_loopback_opt_in("127.0.0.1"));
        assert!(!host_requires_non_loopback_opt_in("[::1]"));
        assert!(!host_requires_non_loopback_opt_in("localhost"));
        assert!(host_requires_non_loopback_opt_in("0.0.0.0"));
        assert!(host_requires_non_loopback_opt_in("10.0.0.1"));
        assert!(is_wildcard_bind_host("0.0.0.0"));
        assert!(is_wildcard_bind_host("[::]"));
        assert!(!is_wildcard_bind_host("127.0.0.1"));
    }
}
