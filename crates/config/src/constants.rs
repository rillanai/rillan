// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Configuration constants. Names mirror the Go repo's `internal/config`
//! package so that ADRs and diffs cross-reference cleanly.

pub const SCHEMA_VERSION_V1: u32 = 1;
pub const SCHEMA_VERSION_V2: u32 = 2;

pub const LLM_BACKEND_OPENAI_COMPATIBLE: &str = "openai_compatible";
pub const LLM_TRANSPORT_HTTP: &str = "http";
pub const LLM_TRANSPORT_STDIO: &str = "stdio";

pub const PROVIDER_OPENAI: &str = "openai";
pub const PROVIDER_OPENAI_COMPATIBLE: &str = "openai_compatible";
pub const PROVIDER_ANTHROPIC: &str = "anthropic";
pub const PROVIDER_OLLAMA: &str = "ollama";
pub const PROVIDER_DEEPSEEK: &str = "deepseek";
pub const PROVIDER_KIMI: &str = "kimi";
pub const PROVIDER_LOCAL: &str = "local";
pub const PROVIDER_XAI: &str = "xai";
pub const PROVIDER_ZAI: &str = "zai";

pub const AUTH_STRATEGY_NONE: &str = "none";
pub const AUTH_STRATEGY_API_KEY: &str = "api_key";
pub const AUTH_STRATEGY_BROWSER_OIDC: &str = "browser_oidc";
pub const AUTH_STRATEGY_DEVICE_OIDC: &str = "device_oidc";

pub(crate) const SYSTEM_CONFIG_VERSION: &str = "m06";
pub(crate) const SYSTEM_ENCRYPTION_KEYRING_AES_GCM: &str = "keyring_aes_gcm";
pub(crate) const DEFAULT_SYSTEM_KEYRING_SERVICE: &str = "rillan/system-policy";
pub(crate) const DEFAULT_SYSTEM_KEYRING_ACCOUNT: &str = "machine-default";

pub const PROJECT_CLASSIFICATION_OPEN_SOURCE: &str = "open_source";
pub const PROJECT_CLASSIFICATION_INTERNAL: &str = "internal";
pub const PROJECT_CLASSIFICATION_PROPRIETARY: &str = "proprietary";
pub const PROJECT_CLASSIFICATION_TRADE_SECRET: &str = "trade_secret";

pub const ROUTE_PREFERENCE_AUTO: &str = "auto";
pub const ROUTE_PREFERENCE_PREFER_LOCAL: &str = "prefer_local";
pub const ROUTE_PREFERENCE_PREFER_CLOUD: &str = "prefer_cloud";
pub const ROUTE_PREFERENCE_LOCAL_ONLY: &str = "local_only";

pub const LLM_PRESET_OPENAI: &str = "openai";
pub const LLM_PRESET_ANTHROPIC: &str = "anthropic";
pub const LLM_PRESET_XAI: &str = "xai";
pub const LLM_PRESET_DEEPSEEK: &str = "deepseek";
pub const LLM_PRESET_KIMI: &str = "kimi";
pub const LLM_PRESET_ZAI: &str = "zai";

pub const DEFAULT_RUNTIME_PROVIDER_ID: &str = "default";

pub const DEFAULT_SERVER_HOST: &str = "127.0.0.1";
pub const DEFAULT_SERVER_PORT: u16 = 8420;

pub(crate) fn normalize(value: &str) -> String {
    value.trim().to_lowercase()
}
