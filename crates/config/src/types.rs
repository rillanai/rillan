// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::constants;

/// Top-level Rillan runtime configuration. Schema v2 retains the legacy
/// provider section so older config files keep loading.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Config {
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub schema_version: u32,
    #[serde(default)]
    pub server: ServerConfig,
    #[serde(default)]
    pub provider: ProviderConfig,
    #[serde(default)]
    pub index: IndexConfig,
    #[serde(default, skip_serializing_if = "KnowledgeGraphConfig::is_default")]
    pub knowledge_graph: KnowledgeGraphConfig,
    #[serde(default)]
    pub retrieval: RetrievalConfig,
    #[serde(default)]
    pub runtime: RuntimeConfig,
    #[serde(default)]
    pub local_model: LocalModelConfig,
    #[serde(default)]
    pub agent: AgentRuntimeConfig,
    #[serde(default, skip_serializing_if = "AuthConfig::is_default")]
    pub auth: AuthConfig,
    #[serde(default, skip_serializing_if = "LlmRegistryConfig::is_empty")]
    pub llms: LlmRegistryConfig,
    #[serde(default, skip_serializing_if = "McpRegistryConfig::is_empty")]
    pub mcps: McpRegistryConfig,
}

impl Default for Config {
    fn default() -> Self {
        crate::load::default_config()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AuthConfig {
    #[serde(default, skip_serializing_if = "ControlPlaneAuthConfig::is_default")]
    pub rillan: ControlPlaneAuthConfig,
}

impl AuthConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.rillan.is_default()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ControlPlaneAuthConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub endpoint: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub auth_strategy: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub session_ref: String,
}

impl ControlPlaneAuthConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.endpoint.is_empty() && self.auth_strategy.is_empty() && self.session_ref.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct LlmRegistryConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub default: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub providers: Vec<LlmProviderConfig>,
}

impl LlmRegistryConfig {
    pub(crate) fn is_empty(&self) -> bool {
        self.default.is_empty() && self.providers.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct LlmProviderConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub preset: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub backend: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub transport: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub endpoint: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub command: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub auth_strategy: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub default_model: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub model_pins: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub capabilities: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub credential_ref: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct McpRegistryConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub default: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub servers: Vec<McpServerConfig>,
}

impl McpRegistryConfig {
    pub(crate) fn is_empty(&self) -> bool {
        self.default.is_empty() && self.servers.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct McpServerConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub endpoint: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub transport: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub command: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub auth_strategy: String,
    #[serde(default, skip_serializing_if = "is_false")]
    pub read_only: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub credential_ref: String,
}

/// Selected provider after project overrides and allowlists have been applied.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ResolvedLlmProvider {
    pub id: String,
    pub preset: String,
    pub backend: String,
    pub transport: String,
    pub endpoint: String,
    pub command: Vec<String>,
    pub auth_strategy: String,
    pub default_model: String,
    pub model_pins: Vec<String>,
    pub capabilities: Vec<String>,
    pub credential_ref: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct RuntimeProviderHostConfig {
    pub default: String,
    pub providers: Vec<RuntimeProviderAdapterConfig>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct RuntimeProviderAdapterConfig {
    pub id: String,
    pub preset: String,
    pub kind: String,
    pub transport: String,
    pub command: Vec<String>,
    pub openai: OpenAiConfig,
    pub anthropic: AnthropicConfig,
    pub local_model: LocalModelProvider,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct LlmProviderPreset {
    pub id: &'static str,
    pub family: &'static str,
    pub endpoint: &'static str,
    pub auth_strategy: &'static str,
    pub default_model: &'static str,
    pub model_pins: &'static [&'static str],
    pub capabilities: &'static [&'static str],
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SystemConfig {
    #[serde(default)]
    pub version: String,
    #[serde(default)]
    pub encryption: SystemEncryptionConfig,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub encrypted_payload: String,
    #[serde(default, skip)]
    pub policy: SystemPolicy,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SystemEncryptionConfig {
    #[serde(default)]
    pub method: String,
    #[serde(default)]
    pub keyring_service: String,
    #[serde(default)]
    pub keyring_account: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SystemPolicy {
    #[serde(default)]
    pub identity: SystemIdentityRules,
    #[serde(default)]
    pub rules: SystemPolicyRules,
    #[serde(default)]
    pub trusted_modules: Vec<TrustedModulePolicy>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct TrustedModulePolicy {
    #[serde(default)]
    pub repo_root: String,
    #[serde(default)]
    pub module_id: String,
    #[serde(default)]
    pub manifest_sha256: String,
    #[serde(default)]
    pub allow_stdio: bool,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SystemIdentityRules {
    #[serde(default)]
    pub people: Vec<String>,
    #[serde(default)]
    pub employers: Vec<String>,
    #[serde(default)]
    pub pii_patterns: Vec<String>,
    #[serde(default)]
    pub credential_patterns: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SystemPolicyRules {
    #[serde(default)]
    pub mask_pii_for_remote: bool,
    #[serde(default)]
    pub strip_employer_references: bool,
    #[serde(default)]
    pub force_local_for_trade_secret: bool,
    #[serde(default)]
    pub block_remote_on_pci_artifacts: bool,
}

/// Repo-local `.rillan/project.yaml` configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectConfig {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub classification: String,
    #[serde(default)]
    pub sources: Vec<ProjectSource>,
    #[serde(default)]
    pub routing: ProjectRoutingConfig,
    #[serde(
        default,
        skip_serializing_if = "ProjectProviderSelectionConfig::is_default"
    )]
    pub providers: ProjectProviderSelectionConfig,
    #[serde(
        default,
        skip_serializing_if = "ProjectModuleSelectionConfig::is_default"
    )]
    pub modules: ProjectModuleSelectionConfig,
    #[serde(default, skip_serializing_if = "ProjectAgentConfig::is_default")]
    pub agent: ProjectAgentConfig,
    #[serde(default)]
    pub system_prompt: String,
    #[serde(default)]
    pub instructions: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectProviderSelectionConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub llm_default: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub llm_allowed: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub mcp_enabled: Vec<String>,
}

impl ProjectProviderSelectionConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.llm_default.is_empty() && self.llm_allowed.is_empty() && self.mcp_enabled.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectModuleSelectionConfig {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub enabled: Vec<String>,
}

impl ProjectModuleSelectionConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.enabled.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectAgentConfig {
    #[serde(
        default,
        skip_serializing_if = "ProjectSkillSelectionConfig::is_default"
    )]
    pub skills: ProjectSkillSelectionConfig,
}

impl ProjectAgentConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.skills.is_default()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectSkillSelectionConfig {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub enabled: Vec<String>,
}

impl ProjectSkillSelectionConfig {
    pub(crate) fn is_default(&self) -> bool {
        self.enabled.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectSource {
    #[serde(default)]
    pub path: String,
    #[serde(rename = "type", default)]
    pub kind: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProjectRoutingConfig {
    #[serde(default)]
    pub default: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub task_types: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct LocalModelConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub base_url: String,
    #[serde(default)]
    pub embed_model: String,
    #[serde(default)]
    pub query_rewrite: QueryRewriteConfig,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct QueryRewriteConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub model: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ServerConfig {
    #[serde(default)]
    pub host: String,
    #[serde(default)]
    pub port: u16,
    #[serde(default)]
    pub log_level: String,
    #[serde(default, skip_serializing_if = "is_false")]
    pub allow_non_loopback_bind: bool,
    #[serde(default, skip_serializing_if = "ServerAuthConfig::is_default")]
    pub auth: ServerAuthConfig,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ServerAuthConfig {
    #[serde(default, skip_serializing_if = "is_false")]
    pub enabled: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub auth_strategy: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub session_ref: String,
}

impl ServerAuthConfig {
    pub(crate) fn is_default(&self) -> bool {
        !self.enabled && self.auth_strategy.is_empty() && self.session_ref.is_empty()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProviderConfig {
    #[serde(rename = "type", default)]
    pub kind: String,
    #[serde(default)]
    pub openai: OpenAiConfig,
    #[serde(default)]
    pub anthropic: AnthropicConfig,
    #[serde(default)]
    pub local: LocalModelProvider,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct OpenAiConfig {
    #[serde(default)]
    pub base_url: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub api_key: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AnthropicConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub base_url: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub api_key: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct LocalModelProvider {
    #[serde(default)]
    pub base_url: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct IndexConfig {
    #[serde(default)]
    pub root: String,
    #[serde(default)]
    pub includes: Vec<String>,
    #[serde(default)]
    pub excludes: Vec<String>,
    #[serde(default)]
    pub chunk_size_lines: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct KnowledgeGraphConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub auto_update: String,
    #[serde(default)]
    pub traversal_depth: i64,
    #[serde(default)]
    pub include_inferred: bool,
    #[serde(default)]
    pub max_nodes: i64,
}

impl KnowledgeGraphConfig {
    pub(crate) fn is_default(&self) -> bool {
        !self.enabled
            && self.path.is_empty()
            && self.auto_update.is_empty()
            && self.traversal_depth == 0
            && !self.include_inferred
            && self.max_nodes == 0
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct RuntimeConfig {
    #[serde(default)]
    pub vector_store_mode: String,
    #[serde(default)]
    pub local_model_base_url: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct RetrievalConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub top_k: usize,
    #[serde(default)]
    pub max_context_chars: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentRuntimeConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub approved_repo_roots: Vec<String>,
    #[serde(default)]
    pub mcp: McpConfig,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct McpConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub read_only: bool,
    #[serde(default)]
    pub max_open_files: usize,
    #[serde(default)]
    pub max_diagnostics: usize,
}

/// Returns the bundled provider preset for the given preset id, normalized to
/// lowercase.
#[must_use]
pub fn bundled_llm_provider_preset(id: &str) -> LlmProviderPreset {
    let normalized = constants::normalize(id);
    bundled_llm_provider_presets()
        .iter()
        .find(|preset| preset.id == normalized)
        .copied()
        .unwrap_or_default()
}

/// Returns the full set of bundled LLM provider presets in declaration order.
#[must_use]
pub fn bundled_llm_provider_presets() -> &'static [LlmProviderPreset] {
    &PRESETS
}

const fn provider_preset(
    id: &'static str,
    family: &'static str,
    endpoint: &'static str,
    auth_strategy: &'static str,
    default_model: &'static str,
    model_pins: &'static [&'static str],
    capabilities: &'static [&'static str],
) -> LlmProviderPreset {
    LlmProviderPreset {
        id,
        family,
        endpoint,
        auth_strategy,
        default_model,
        model_pins,
        capabilities,
    }
}

const PRESETS: [LlmProviderPreset; 6] = [
    provider_preset(
        constants::LLM_PRESET_OPENAI,
        constants::PROVIDER_OPENAI_COMPATIBLE,
        "https://api.openai.com/v1",
        constants::AUTH_STRATEGY_API_KEY,
        "gpt-5",
        &["gpt-5"],
        &["chat", "reasoning", "tool_calling"],
    ),
    provider_preset(
        constants::LLM_PRESET_ANTHROPIC,
        constants::PROVIDER_ANTHROPIC,
        "https://api.anthropic.com",
        constants::AUTH_STRATEGY_API_KEY,
        "claude-sonnet-4-5",
        &["claude-sonnet-4-5"],
        &["chat", "reasoning", "tool_calling"],
    ),
    provider_preset(
        constants::LLM_PRESET_XAI,
        constants::PROVIDER_OPENAI_COMPATIBLE,
        "https://api.x.ai/v1",
        constants::AUTH_STRATEGY_API_KEY,
        "grok-4",
        &["grok-4"],
        &["chat", "reasoning", "tool_calling"],
    ),
    provider_preset(
        constants::LLM_PRESET_DEEPSEEK,
        constants::PROVIDER_OPENAI_COMPATIBLE,
        "https://api.deepseek.com/v1",
        constants::AUTH_STRATEGY_API_KEY,
        "deepseek-chat",
        &["deepseek-chat"],
        &["chat", "reasoning", "tool_calling"],
    ),
    provider_preset(
        constants::LLM_PRESET_KIMI,
        constants::PROVIDER_OPENAI_COMPATIBLE,
        "https://api.moonshot.ai/v1",
        constants::AUTH_STRATEGY_API_KEY,
        "kimi-k2-0711-preview",
        &["kimi-k2-0711-preview"],
        &["chat", "reasoning", "tool_calling"],
    ),
    provider_preset(
        constants::LLM_PRESET_ZAI,
        constants::PROVIDER_OPENAI_COMPATIBLE,
        "https://api.z.ai/api/paas/v4",
        constants::AUTH_STRATEGY_API_KEY,
        "glm-4.5",
        &["glm-4.5"],
        &["chat", "reasoning", "tool_calling"],
    ),
];

impl LlmProviderPreset {
    /// Materializes a [`LlmProviderConfig`] entry for this preset, addressed
    /// by `id`. Mirrors `LlmProviderPreset.ProviderConfig` from the Go repo.
    #[must_use]
    pub fn provider_config(&self, id: &str) -> LlmProviderConfig {
        LlmProviderConfig {
            id: id.to_string(),
            preset: self.id.to_string(),
            backend: self.family.to_string(),
            transport: constants::LLM_TRANSPORT_HTTP.to_string(),
            endpoint: self.endpoint.to_string(),
            auth_strategy: self.auth_strategy.to_string(),
            default_model: self.default_model.to_string(),
            model_pins: self.model_pins.iter().map(|s| (*s).to_string()).collect(),
            capabilities: self.capabilities.iter().map(|s| (*s).to_string()).collect(),
            credential_ref: format!("keyring://rillan/llm/{id}"),
            command: Vec::new(),
        }
    }
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(value: &bool) -> bool {
    !*value
}

fn is_zero_u32(value: &u32) -> bool {
    *value == 0
}
