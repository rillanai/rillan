// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Multi-provider host. Mirrors `internal/providers/host.go`.

use std::collections::BTreeMap;
use std::sync::Arc;

use rillan_config::{
    RuntimeProviderAdapterConfig, RuntimeProviderHostConfig, LLM_BACKEND_OPENAI_COMPATIBLE,
    LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO, PROVIDER_ANTHROPIC, PROVIDER_OLLAMA, PROVIDER_OPENAI,
    PROVIDER_OPENAI_COMPATIBLE,
};
use thiserror::Error;

use crate::{AnthropicProvider, OllamaProvider, OpenAiProvider, Provider, StdioProvider};

/// Errors returned while wiring up a [`Host`].
#[derive(Debug, Error)]
pub enum HostError {
    #[error("runtime provider host default must not be empty")]
    DefaultEmpty,
    #[error("runtime provider host must include at least one provider")]
    NoProviders,
    #[error("runtime provider id must not be empty")]
    EmptyId,
    #[error("runtime provider {0:?} declared more than once")]
    Duplicate(String),
    #[error("runtime provider host default {0:?} not found")]
    DefaultNotFound(String),
    #[error("unsupported provider transport {0:?}")]
    UnsupportedTransport(String),
    #[error("unsupported provider type {0:?}")]
    UnsupportedKind(String),
}

/// Holds the constructed [`Provider`] instances keyed by provider id.
#[derive(Clone)]
pub struct Host {
    default: String,
    providers: BTreeMap<String, Arc<dyn Provider>>,
}

impl std::fmt::Debug for Host {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Host")
            .field("default", &self.default)
            .field("providers", &self.providers.keys().collect::<Vec<_>>())
            .finish()
    }
}

impl Host {
    /// Builds a host from the runtime-provider config.
    pub fn new(cfg: &RuntimeProviderHostConfig) -> Result<Self, HostError> {
        let default_id = cfg.default.trim();
        if default_id.is_empty() {
            return Err(HostError::DefaultEmpty);
        }
        if cfg.providers.is_empty() {
            return Err(HostError::NoProviders);
        }
        let mut providers: BTreeMap<String, Arc<dyn Provider>> = BTreeMap::new();
        for adapter in &cfg.providers {
            let id = adapter.id.trim();
            if id.is_empty() {
                return Err(HostError::EmptyId);
            }
            if providers.contains_key(id) {
                return Err(HostError::Duplicate(id.to_string()));
            }
            providers.insert(id.to_string(), build_adapter(adapter)?);
        }
        if !providers.contains_key(default_id) {
            return Err(HostError::DefaultNotFound(default_id.to_string()));
        }
        Ok(Self {
            default: default_id.to_string(),
            providers,
        })
    }

    /// Returns the provider registered under `id`.
    pub fn provider(&self, id: &str) -> Option<Arc<dyn Provider>> {
        self.providers.get(id.trim()).cloned()
    }

    /// Returns the default provider.
    pub fn default_provider(&self) -> Arc<dyn Provider> {
        self.providers
            .get(&self.default)
            .cloned()
            .expect("default provider always populated by Host::new")
    }

    /// Returns the registered provider ids.
    #[must_use]
    pub fn ids(&self) -> Vec<String> {
        self.providers.keys().cloned().collect()
    }
}

fn build_adapter(cfg: &RuntimeProviderAdapterConfig) -> Result<Arc<dyn Provider>, HostError> {
    let transport = cfg.transport.trim().to_lowercase();
    if transport == LLM_TRANSPORT_STDIO {
        return Ok(Arc::new(StdioProvider::new(cfg.command.clone())) as Arc<dyn Provider>);
    }
    if !matches!(transport.as_str(), "" | LLM_TRANSPORT_HTTP) {
        return Err(HostError::UnsupportedTransport(cfg.transport.clone()));
    }

    // PROVIDER_OPENAI_COMPATIBLE and LLM_BACKEND_OPENAI_COMPATIBLE are the
    // same wire string ("openai_compatible"); listing both here documents that
    // the daemon accepts either spelling at the Config layer.
    let _ = (PROVIDER_OPENAI_COMPATIBLE, LLM_BACKEND_OPENAI_COMPATIBLE);
    match cfg.kind.trim().to_lowercase().as_str() {
        PROVIDER_OPENAI | PROVIDER_OPENAI_COMPATIBLE => {
            Ok(Arc::new(OpenAiProvider::new(&cfg.openai)) as Arc<dyn Provider>)
        }
        PROVIDER_ANTHROPIC => {
            Ok(Arc::new(AnthropicProvider::new(&cfg.anthropic)) as Arc<dyn Provider>)
        }
        PROVIDER_OLLAMA => Ok(Arc::new(OllamaProvider::new(&cfg.local_model)) as Arc<dyn Provider>),
        other => Err(HostError::UnsupportedKind(other.to_string())),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::{OpenAiConfig, RuntimeProviderAdapterConfig};

    fn adapter(id: &str) -> RuntimeProviderAdapterConfig {
        RuntimeProviderAdapterConfig {
            id: id.into(),
            kind: "openai".into(),
            transport: "http".into(),
            openai: OpenAiConfig {
                base_url: "https://api.openai.com/v1".into(),
                api_key: "secret".into(),
            },
            ..RuntimeProviderAdapterConfig::default()
        }
    }

    #[test]
    fn rejects_duplicate_provider_ids() {
        let cfg = RuntimeProviderHostConfig {
            default: "openai".into(),
            providers: vec![adapter("openai"), adapter("openai")],
        };
        let err = Host::new(&cfg).expect_err("duplicate must fail");
        assert!(matches!(err, HostError::Duplicate(_)));
    }

    #[test]
    fn rejects_missing_default() {
        let cfg = RuntimeProviderHostConfig {
            default: "missing".into(),
            providers: vec![adapter("openai")],
        };
        let err = Host::new(&cfg).expect_err("missing default");
        assert!(matches!(err, HostError::DefaultNotFound(_)));
    }

    #[test]
    fn returns_default_provider() {
        let cfg = RuntimeProviderHostConfig {
            default: "openai".into(),
            providers: vec![adapter("openai")],
        };
        let host = Host::new(&cfg).expect("host");
        assert_eq!(host.default_provider().name(), "openai");
        assert!(host.provider("openai").is_some());
        assert!(host.provider("missing").is_none());
    }
}
