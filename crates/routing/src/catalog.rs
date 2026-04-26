// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Catalog construction. Mirrors `internal/routing/catalog.go`.

use std::collections::{BTreeMap, BTreeSet};

use rillan_config::{
    bundled_llm_provider_preset, Config, LlmProviderConfig, ProjectConfig,
    DEFAULT_RUNTIME_PROVIDER_ID, LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO, PROVIDER_OLLAMA,
    SCHEMA_VERSION_V2,
};

use crate::types::{Candidate, Catalog, Location};

/// Builds a [`Catalog`] from a runtime + project config.
#[must_use]
pub fn build_catalog(cfg: &Config, project: &ProjectConfig) -> Catalog {
    let mut candidates = build_candidates(cfg);
    let allowed: BTreeSet<String> = project
        .providers
        .llm_allowed
        .iter()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect();
    let allowed_active = !allowed.is_empty();
    if allowed_active {
        candidates.retain(|c| allowed.contains(&c.id));
    }
    candidates.sort_by(|a, b| a.id.cmp(&b.id));
    let mut by_id: BTreeMap<String, Candidate> = BTreeMap::new();
    for candidate in &candidates {
        by_id.insert(candidate.id.clone(), candidate.clone());
    }
    Catalog {
        candidates,
        by_id,
        allowed: allowed_active,
    }
}

fn build_candidates(cfg: &Config) -> Vec<Candidate> {
    if cfg.schema_version < SCHEMA_VERSION_V2 || cfg.llms.providers.is_empty() {
        return vec![legacy_candidate(cfg)];
    }
    cfg.llms
        .providers
        .iter()
        .map(|provider| {
            let family = provider_family(provider);
            let capabilities = provider_capabilities(provider);
            let model_pins = provider_model_pins(provider);
            let transport = provider.transport.trim().to_string();
            let location = location_for_provider(&family, &transport);
            Candidate {
                id: provider.id.trim().to_string(),
                backend: family,
                preset: provider.preset.trim().to_string(),
                transport,
                endpoint: provider.endpoint.trim().to_string(),
                default_model: provider.default_model.trim().to_string(),
                model_pins,
                capabilities,
                location: Some(location),
            }
        })
        .collect()
}

fn legacy_candidate(cfg: &Config) -> Candidate {
    let backend = cfg.provider.kind.trim().to_lowercase();
    let location = location_for_provider(&backend, LLM_TRANSPORT_HTTP);
    Candidate {
        id: DEFAULT_RUNTIME_PROVIDER_ID.to_string(),
        backend,
        transport: LLM_TRANSPORT_HTTP.to_string(),
        capabilities: vec!["chat".to_string()],
        location: Some(location),
        ..Candidate::default()
    }
}

fn provider_family(provider: &LlmProviderConfig) -> String {
    let backend = provider.backend.trim().to_lowercase();
    if !backend.is_empty() {
        return backend;
    }
    let preset = bundled_llm_provider_preset(&provider.preset);
    if preset.id.is_empty() {
        return String::new();
    }
    preset.family.trim().to_lowercase()
}

fn provider_capabilities(provider: &LlmProviderConfig) -> Vec<String> {
    if !provider.capabilities.is_empty() {
        return provider.capabilities.clone();
    }
    let preset = bundled_llm_provider_preset(&provider.preset);
    if !preset.id.is_empty() {
        return preset
            .capabilities
            .iter()
            .map(|s| (*s).to_string())
            .collect();
    }
    vec!["chat".to_string()]
}

fn provider_model_pins(provider: &LlmProviderConfig) -> Vec<String> {
    if !provider.model_pins.is_empty() {
        return provider.model_pins.clone();
    }
    let preset = bundled_llm_provider_preset(&provider.preset);
    if !preset.id.is_empty() && !preset.model_pins.is_empty() {
        return preset.model_pins.iter().map(|s| (*s).to_string()).collect();
    }
    let model = provider.default_model.trim();
    if !model.is_empty() {
        return vec![model.to_string()];
    }
    Vec::new()
}

fn location_for_provider(family: &str, transport: &str) -> Location {
    if transport.trim().to_lowercase() == LLM_TRANSPORT_STDIO {
        return Location::Local;
    }
    if family.trim().to_lowercase() == PROVIDER_OLLAMA {
        return Location::Local;
    }
    Location::Remote
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::AgentRuntimeConfig;
    use rillan_config::{
        AnthropicConfig, AuthConfig, IndexConfig, KnowledgeGraphConfig, LlmRegistryConfig,
        LocalModelConfig, McpRegistryConfig, OpenAiConfig, ProjectConfig, ProviderConfig,
        RetrievalConfig, RuntimeConfig, ServerConfig,
    };

    fn empty_config() -> Config {
        Config {
            schema_version: SCHEMA_VERSION_V2,
            server: ServerConfig::default(),
            provider: ProviderConfig {
                openai: OpenAiConfig::default(),
                anthropic: AnthropicConfig::default(),
                ..ProviderConfig::default()
            },
            index: IndexConfig::default(),
            knowledge_graph: KnowledgeGraphConfig::default(),
            retrieval: RetrievalConfig::default(),
            runtime: RuntimeConfig::default(),
            local_model: LocalModelConfig::default(),
            agent: AgentRuntimeConfig::default(),
            auth: AuthConfig::default(),
            llms: LlmRegistryConfig::default(),
            mcps: McpRegistryConfig {
                default: String::new(),
                servers: Vec::new(),
            },
        }
    }

    #[test]
    fn legacy_path_returns_single_default_candidate() {
        let cfg = empty_config();
        let catalog = build_catalog(&cfg, &ProjectConfig::default());
        assert_eq!(catalog.candidates.len(), 1);
        assert_eq!(catalog.candidates[0].id, DEFAULT_RUNTIME_PROVIDER_ID);
    }

    #[test]
    fn allowlist_filters_candidates() {
        let mut cfg = empty_config();
        cfg.llms.providers = vec![
            LlmProviderConfig {
                id: "alpha".into(),
                backend: "openai_compatible".into(),
                transport: LLM_TRANSPORT_HTTP.into(),
                ..LlmProviderConfig::default()
            },
            LlmProviderConfig {
                id: "beta".into(),
                backend: "openai_compatible".into(),
                transport: LLM_TRANSPORT_HTTP.into(),
                ..LlmProviderConfig::default()
            },
        ];
        let project = ProjectConfig {
            providers: rillan_config::ProjectProviderSelectionConfig {
                llm_allowed: vec!["beta".into()],
                ..rillan_config::ProjectProviderSelectionConfig::default()
            },
            ..ProjectConfig::default()
        };
        let catalog = build_catalog(&cfg, &project);
        assert_eq!(catalog.candidates.len(), 1);
        assert_eq!(catalog.candidates[0].id, "beta");
        assert!(catalog.allowed);
    }

    #[test]
    fn stdio_transport_is_local_location() {
        let mut cfg = empty_config();
        cfg.llms.providers = vec![LlmProviderConfig {
            id: "stdio".into(),
            backend: "openai_compatible".into(),
            transport: LLM_TRANSPORT_STDIO.into(),
            command: vec!["./bin".into()],
            ..LlmProviderConfig::default()
        }];
        let catalog = build_catalog(&cfg, &ProjectConfig::default());
        assert_eq!(catalog.candidates[0].location, Some(Location::Local));
    }
}
