// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Provider resolution. Mirrors `ResolveActiveLLMProvider`,
//! `ResolveLLMProviderByID`, `ResolveRuntimeProviderHostConfig`, and
//! `ResolveRuntimeProviderAdapterConfig` from the Go repo.

use std::collections::BTreeSet;

use thiserror::Error;

use crate::constants::{
    self, AUTH_STRATEGY_API_KEY, AUTH_STRATEGY_NONE, DEFAULT_RUNTIME_PROVIDER_ID,
    LLM_BACKEND_OPENAI_COMPATIBLE, LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO, PROVIDER_ANTHROPIC,
    PROVIDER_OLLAMA, PROVIDER_OPENAI, SCHEMA_VERSION_V2,
};
use crate::types::{
    AnthropicConfig, Config, LocalModelProvider, OpenAiConfig, ProjectConfig, ResolvedLlmProvider,
    RuntimeProviderAdapterConfig, RuntimeProviderHostConfig,
};
use rillan_secretstore::{Binding, Store};

/// Errors raised by resolver helpers.
#[derive(Debug, Error)]
pub enum ResolveError {
    #[error("llms.default must not be empty")]
    DefaultEmpty,
    #[error("llm provider {0:?} is not allowed for this project")]
    NotAllowed(String),
    #[error("llm provider {0:?} not found")]
    NotFound(String),
    #[error("llm provider {provider:?} command must not be empty when transport is {transport:?}")]
    StdioCommandEmpty { provider: String, transport: String },
    #[error(
        "llm provider {provider:?} uses unsupported auth strategy {strategy:?} for stdio transport"
    )]
    StdioStrategyUnsupported { provider: String, strategy: String },
    #[error(
        "llm provider {provider:?} uses unsupported auth strategy {strategy:?} for anthropic; only {expected:?} is supported"
    )]
    AnthropicStrategyUnsupported {
        provider: String,
        strategy: String,
        expected: &'static str,
    },
    #[error(
        "llm provider {0:?} must include credential_ref when auth_strategy is {AUTH_STRATEGY_API_KEY:?}"
    )]
    CredentialRefMissing(String),
    #[error("credential at {0} does not contain an api key")]
    CredentialMissingApiKey(String),
    #[error(
        "llm provider {provider:?} uses unsupported backend {backend:?} in the current runtime"
    )]
    UnsupportedBackend { provider: String, backend: String },
    #[error("llm provider {0:?} endpoint must not be empty for ollama")]
    OllamaEndpointEmpty(String),
    #[error("runtime provider host must include at least one provider")]
    HostEmpty,
    #[error("secret store error: {0}")]
    Secret(#[from] rillan_secretstore::Error),
}

/// Selects the active LLM provider after applying any project-level overrides
/// and allowlists.
pub fn resolve_active_llm_provider(
    cfg: &Config,
    project: &ProjectConfig,
) -> Result<ResolvedLlmProvider, ResolveError> {
    let mut selected = cfg.llms.default.trim().to_string();
    let override_id = project.providers.llm_default.trim();
    if !override_id.is_empty() {
        selected = override_id.to_string();
    }
    if selected.is_empty() {
        return Err(ResolveError::DefaultEmpty);
    }
    if !project.providers.llm_allowed.is_empty() {
        let allowed = project
            .providers
            .llm_allowed
            .iter()
            .any(|candidate| candidate.trim() == selected);
        if !allowed {
            return Err(ResolveError::NotAllowed(selected));
        }
    }
    for provider in &cfg.llms.providers {
        if provider.id.trim() != selected {
            continue;
        }
        return Ok(ResolvedLlmProvider {
            id: provider.id.clone(),
            preset: provider.preset.trim().to_string(),
            backend: provider.backend.trim().to_string(),
            transport: provider.transport.trim().to_string(),
            endpoint: provider.endpoint.trim().to_string(),
            command: provider.command.clone(),
            auth_strategy: provider.auth_strategy.trim().to_string(),
            default_model: provider.default_model.trim().to_string(),
            model_pins: provider.model_pins.clone(),
            capabilities: provider.capabilities.clone(),
            credential_ref: provider.credential_ref.trim().to_string(),
        });
    }
    Err(ResolveError::NotFound(selected))
}

/// Resolves a provider by id, falling back to the legacy `provider` block when
/// schema v2 isn't in use.
pub fn resolve_llm_provider_by_id(
    cfg: &Config,
    provider_id: &str,
) -> Result<ResolvedLlmProvider, ResolveError> {
    let selected = provider_id.trim();
    if cfg.schema_version < SCHEMA_VERSION_V2 || cfg.llms.providers.is_empty() {
        if selected.is_empty() || selected == DEFAULT_RUNTIME_PROVIDER_ID {
            return Ok(ResolvedLlmProvider {
                id: DEFAULT_RUNTIME_PROVIDER_ID.to_string(),
                backend: cfg.provider.kind.trim().to_string(),
                transport: LLM_TRANSPORT_HTTP.to_string(),
                endpoint: legacy_endpoint(cfg).to_string(),
                auth_strategy: legacy_auth_strategy(cfg).to_string(),
                ..ResolvedLlmProvider::default()
            });
        }
        return Err(ResolveError::NotFound(provider_id.to_string()));
    }
    for provider in &cfg.llms.providers {
        if provider.id.trim() != selected {
            continue;
        }
        return Ok(ResolvedLlmProvider {
            id: provider.id.clone(),
            preset: provider.preset.trim().to_string(),
            backend: provider.backend.trim().to_string(),
            transport: provider.transport.trim().to_string(),
            endpoint: provider.endpoint.trim().to_string(),
            command: provider.command.clone(),
            auth_strategy: provider.auth_strategy.trim().to_string(),
            default_model: provider.default_model.trim().to_string(),
            model_pins: provider.model_pins.clone(),
            capabilities: provider.capabilities.clone(),
            credential_ref: provider.credential_ref.trim().to_string(),
        });
    }
    Err(ResolveError::NotFound(provider_id.to_string()))
}

/// Builds a runtime provider host config: every allowed provider, with
/// credentials resolved through `store`.
pub fn resolve_runtime_provider_host(
    cfg: &Config,
    project: &ProjectConfig,
    store: &Store,
) -> Result<RuntimeProviderHostConfig, ResolveError> {
    if cfg.schema_version < SCHEMA_VERSION_V2 || cfg.llms.providers.is_empty() {
        return Ok(RuntimeProviderHostConfig {
            default: DEFAULT_RUNTIME_PROVIDER_ID.to_string(),
            providers: vec![RuntimeProviderAdapterConfig {
                id: DEFAULT_RUNTIME_PROVIDER_ID.to_string(),
                kind: cfg.provider.kind.clone(),
                openai: cfg.provider.openai.clone(),
                anthropic: cfg.provider.anthropic.clone(),
                local_model: cfg.provider.local.clone(),
                ..RuntimeProviderAdapterConfig::default()
            }],
        });
    }

    let selected = resolve_active_llm_provider(cfg, project)?;
    let allowed: BTreeSet<String> = project
        .providers
        .llm_allowed
        .iter()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect();
    let mut providers = Vec::with_capacity(cfg.llms.providers.len());
    for provider in &cfg.llms.providers {
        let provider_id = provider.id.trim().to_string();
        if provider_id.is_empty() {
            continue;
        }
        if !allowed.is_empty() && !allowed.contains(&provider_id) {
            continue;
        }
        let resolved = match resolve_llm_provider_by_id(cfg, &provider_id) {
            Ok(value) => value,
            Err(err) if provider_id == selected.id => return Err(err),
            Err(_) => continue,
        };
        let adapter = match resolve_runtime_provider_adapter(cfg, &resolved, store) {
            Ok(value) => value,
            Err(err) if provider_id == selected.id => return Err(err),
            Err(_) => continue,
        };
        providers.push(adapter);
    }
    if providers.is_empty() {
        return Err(ResolveError::HostEmpty);
    }
    Ok(RuntimeProviderHostConfig {
        default: selected.id,
        providers,
    })
}

/// Builds the runtime adapter config for one selected provider, resolving its
/// credential through `store`.
pub fn resolve_runtime_provider_adapter(
    cfg: &Config,
    selected: &ResolvedLlmProvider,
    store: &Store,
) -> Result<RuntimeProviderAdapterConfig, ResolveError> {
    let mut adapter = RuntimeProviderAdapterConfig {
        id: selected.id.clone(),
        preset: selected.preset.clone(),
        kind: selected.backend.clone(),
        transport: selected.transport.clone(),
        command: selected.command.clone(),
        ..RuntimeProviderAdapterConfig::default()
    };

    if selected.transport == LLM_TRANSPORT_STDIO {
        if selected.command.is_empty() {
            return Err(ResolveError::StdioCommandEmpty {
                provider: selected.id.clone(),
                transport: LLM_TRANSPORT_STDIO.to_string(),
            });
        }
        let strategy = constants::normalize(&selected.auth_strategy);
        if !strategy.is_empty() && strategy != AUTH_STRATEGY_NONE {
            return Err(ResolveError::StdioStrategyUnsupported {
                provider: selected.id.clone(),
                strategy: selected.auth_strategy.clone(),
            });
        }
        return Ok(adapter);
    }

    match selected.backend.as_str() {
        LLM_BACKEND_OPENAI_COMPATIBLE => {
            let secret = resolve_runtime_provider_bearer(selected, store)?;
            adapter.openai = OpenAiConfig {
                base_url: selected.endpoint.clone(),
                api_key: secret,
            };
            Ok(adapter)
        }
        PROVIDER_ANTHROPIC => {
            let api_key = resolve_runtime_provider_api_key(selected, store)?;
            adapter.anthropic = AnthropicConfig {
                enabled: true,
                base_url: selected.endpoint.clone(),
                api_key,
            };
            Ok(adapter)
        }
        PROVIDER_OLLAMA => {
            let mut base_url = selected.endpoint.trim().to_string();
            if base_url.is_empty() {
                base_url = cfg.local_model.base_url.trim().to_string();
            }
            if base_url.is_empty() {
                return Err(ResolveError::OllamaEndpointEmpty(selected.id.clone()));
            }
            adapter.local_model = LocalModelProvider { base_url };
            Ok(adapter)
        }
        _ => Err(ResolveError::UnsupportedBackend {
            provider: selected.id.clone(),
            backend: selected.backend.clone(),
        }),
    }
}

/// Resolves the bearer token for the daemon's own auth header, when
/// `server.auth.enabled` is true.
pub fn resolve_server_auth_bearer(cfg: &Config, store: &Store) -> Result<String, ResolveError> {
    let binding = Binding {
        auth_strategy: cfg.server.auth.auth_strategy.trim().to_string(),
        ..Binding::default()
    };
    Ok(store.resolve_bearer(&cfg.server.auth.session_ref, &binding)?)
}

fn resolve_runtime_provider_bearer(
    selected: &ResolvedLlmProvider,
    store: &Store,
) -> Result<String, ResolveError> {
    if selected.auth_strategy == AUTH_STRATEGY_NONE || selected.credential_ref.is_empty() {
        return Ok(String::new());
    }
    let binding = Binding {
        endpoint: selected.endpoint.clone(),
        auth_strategy: selected.auth_strategy.clone(),
        ..Binding::default()
    };
    Ok(store.resolve_bearer(&selected.credential_ref, &binding)?)
}

fn resolve_runtime_provider_api_key(
    selected: &ResolvedLlmProvider,
    store: &Store,
) -> Result<String, ResolveError> {
    if selected.auth_strategy != AUTH_STRATEGY_API_KEY {
        return Err(ResolveError::AnthropicStrategyUnsupported {
            provider: selected.id.clone(),
            strategy: selected.auth_strategy.clone(),
            expected: AUTH_STRATEGY_API_KEY,
        });
    }
    if selected.credential_ref.trim().is_empty() {
        return Err(ResolveError::CredentialRefMissing(selected.id.clone()));
    }
    let credential = store.load(&selected.credential_ref)?;
    let binding = Binding {
        endpoint: selected.endpoint.clone(),
        auth_strategy: selected.auth_strategy.clone(),
        ..Binding::default()
    };
    rillan_secretstore::check_binding(&credential, &binding)?;
    if credential.api_key.trim().is_empty() {
        return Err(ResolveError::CredentialMissingApiKey(
            selected.credential_ref.clone(),
        ));
    }
    Ok(credential.api_key)
}

fn legacy_endpoint(cfg: &Config) -> &str {
    match constants::normalize(&cfg.provider.kind).as_str() {
        PROVIDER_OPENAI => cfg.provider.openai.base_url.trim(),
        PROVIDER_ANTHROPIC => cfg.provider.anthropic.base_url.trim(),
        _ => cfg.provider.local.base_url.trim(),
    }
}

fn legacy_auth_strategy(cfg: &Config) -> &'static str {
    match constants::normalize(&cfg.provider.kind).as_str() {
        PROVIDER_OPENAI | PROVIDER_ANTHROPIC => AUTH_STRATEGY_API_KEY,
        _ => AUTH_STRATEGY_NONE,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::load::default_config;

    #[test]
    fn resolve_active_returns_default_for_default_config() {
        let cfg = default_config();
        let project = ProjectConfig::default();
        let resolved = resolve_active_llm_provider(&cfg, &project).expect("resolve");
        assert_eq!(resolved.id, "openai");
    }

    #[test]
    fn resolve_active_respects_project_override() {
        let cfg = default_config();
        let project = ProjectConfig {
            providers: crate::types::ProjectProviderSelectionConfig {
                llm_default: "anthropic".into(),
                ..crate::types::ProjectProviderSelectionConfig::default()
            },
            ..ProjectConfig::default()
        };
        let resolved = resolve_active_llm_provider(&cfg, &project).expect("resolve");
        assert_eq!(resolved.id, "anthropic");
    }

    #[test]
    fn resolve_active_rejects_when_not_allowed() {
        let cfg = default_config();
        let project = ProjectConfig {
            providers: crate::types::ProjectProviderSelectionConfig {
                llm_allowed: vec!["xai".into()],
                ..crate::types::ProjectProviderSelectionConfig::default()
            },
            ..ProjectConfig::default()
        };
        let err = resolve_active_llm_provider(&cfg, &project).expect_err("not allowed");
        assert!(matches!(err, ResolveError::NotAllowed(_)));
    }
}
