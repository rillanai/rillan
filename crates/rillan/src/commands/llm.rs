// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan llm` — credential + registry management. Mirrors `cmd/rillan/llm.go`.

use std::path::PathBuf;

use anyhow::{anyhow, Context, Result};
use clap::{Args, Subcommand};
use rillan_config::{
    bundled_llm_provider_preset, write_config, LlmProviderConfig, AUTH_STRATEGY_API_KEY,
    LLM_TRANSPORT_HTTP, LLM_TRANSPORT_STDIO,
};
use rillan_secretstore::{Credential, Store};

use crate::commands::daemon::refresh_daemon_after_mutation;

#[derive(Debug, Args)]
pub(crate) struct LlmArgs {
    #[command(subcommand)]
    command: LlmCommand,
}

#[derive(Debug, Subcommand)]
enum LlmCommand {
    /// List configured LLM providers.
    List(ListArgs),
    /// Add a named LLM provider entry.
    Add(AddArgs),
    /// Remove a named LLM provider entry.
    Remove(RemoveArgs),
    /// Mark a provider as the active default.
    Use(UseArgs),
    /// Save an API-key credential to the OS keyring.
    Login(LoginArgs),
    /// Remove a stored credential from the OS keyring.
    Logout(LogoutArgs),
}

#[derive(Debug, Args)]
struct ListArgs {
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct AddArgs {
    /// Provider id (used for routing, credential refs, and audit traces).
    id: String,
    /// Bundled preset id (openai, anthropic, xai, deepseek, kimi, zai).
    #[arg(long)]
    preset: Option<String>,
    /// Provider backend identity (e.g. openai_compatible).
    #[arg(long)]
    backend: Option<String>,
    /// Transport (http or stdio).
    #[arg(long, default_value = LLM_TRANSPORT_HTTP)]
    transport: String,
    /// Provider endpoint URL.
    #[arg(long)]
    endpoint: Option<String>,
    /// Provider command for stdio transport (repeatable).
    #[arg(long = "command", value_name = "ARG")]
    command: Vec<String>,
    /// Auth strategy (none, api_key, browser_oidc, device_oidc).
    #[arg(long)]
    auth_strategy: Option<String>,
    /// Default model name for this provider.
    #[arg(long)]
    default_model: Option<String>,
    /// Capability exposed by this provider (repeatable).
    #[arg(long = "capability", value_name = "CAP")]
    capabilities: Vec<String>,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct RemoveArgs {
    id: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct UseArgs {
    id: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct LoginArgs {
    /// Provider id as configured in `llms.providers[*].id`.
    id: String,
    /// API key to store. If omitted, the value is read from `RILLAN_LOGIN_API_KEY`.
    #[arg(long, value_name = "API_KEY", env = "RILLAN_LOGIN_API_KEY")]
    api_key: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct LogoutArgs {
    /// Provider id whose credential should be removed.
    id: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

pub(crate) async fn run(args: LlmArgs, store: Store) -> Result<()> {
    match args.command {
        LlmCommand::List(args) => list(args).await,
        LlmCommand::Add(args) => add(args, store.clone()).await,
        LlmCommand::Remove(args) => remove(args, store.clone()).await,
        LlmCommand::Use(args) => use_provider(args, store.clone()).await,
        LlmCommand::Login(args) => login(args, store).await,
        LlmCommand::Logout(args) => logout(args, store).await,
    }
}

async fn list(args: ListArgs) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_for_edit(&config_path)?;
    println!("default: {}", cfg.llms.default);
    for provider in &cfg.llms.providers {
        println!(
            "  - id={id} preset={preset} backend={backend} endpoint={endpoint}",
            id = provider.id,
            preset = provider.preset,
            backend = provider.backend,
            endpoint = provider.endpoint,
        );
    }
    Ok(())
}

async fn add(args: AddArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .clone()
        .unwrap_or_else(rillan_config::default_config_path);
    let mut cfg = rillan_config::load_for_edit(&config_path)?;
    let entry = build_entry(&args)?;
    if cfg.llms.providers.iter().any(|p| p.id == entry.id) {
        return Err(anyhow!("llm provider {:?} already exists", entry.id));
    }
    let entry_id = entry.id.clone();
    cfg.llms.providers.push(entry);
    if cfg.llms.default.is_empty() {
        cfg.llms.default = entry_id.clone();
    }
    write_config(&config_path, &cfg)?;
    println!("added llm provider {entry_id}");
    refresh_daemon_after_mutation(cfg, "updated llm provider config", &store).await
}

async fn remove(args: RemoveArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let mut cfg = rillan_config::load_for_edit(&config_path)?;
    let before = cfg.llms.providers.len();
    cfg.llms.providers.retain(|p| p.id != args.id);
    if cfg.llms.providers.len() == before {
        return Err(anyhow!("llm provider {:?} not found", args.id));
    }
    if cfg.llms.default == args.id {
        cfg.llms.default = String::new();
    }
    write_config(&config_path, &cfg)?;
    println!("removed llm provider {}", args.id);
    refresh_daemon_after_mutation(cfg, "updated llm provider config", &store).await
}

async fn use_provider(args: UseArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let mut cfg = rillan_config::load_for_edit(&config_path)?;
    if !cfg.llms.providers.iter().any(|p| p.id == args.id) {
        return Err(anyhow!("llm provider {:?} not found", args.id));
    }
    cfg.llms.default = args.id.clone();
    write_config(&config_path, &cfg)?;
    println!("default llm provider set to {}", args.id);
    refresh_daemon_after_mutation(cfg, "updated llm provider config", &store).await
}

async fn login(args: LoginArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_for_edit(&config_path)?;
    let provider = cfg
        .llms
        .providers
        .iter()
        .find(|p| p.id == args.id)
        .with_context(|| {
            format!(
                "provider {:?} is not configured in {}",
                args.id,
                config_path.display()
            )
        })?;
    let credential = Credential {
        kind: AUTH_STRATEGY_API_KEY.to_string(),
        api_key: args.api_key,
        endpoint: provider.endpoint.clone(),
        auth_strategy: provider.auth_strategy.clone(),
        ..Credential::default()
    };
    store.save(&provider.credential_ref, credential)?;
    println!("stored credential at {}", provider.credential_ref);
    refresh_daemon_after_mutation(cfg, "updated llm provider auth", &store).await
}

async fn logout(args: LogoutArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_for_edit(&config_path)?;
    let provider = cfg
        .llms
        .providers
        .iter()
        .find(|p| p.id == args.id)
        .with_context(|| format!("provider {:?} is not configured", args.id))?;
    store.delete(&provider.credential_ref)?;
    println!("removed credential at {}", provider.credential_ref);
    refresh_daemon_after_mutation(cfg, "updated llm provider auth", &store).await
}

fn build_entry(args: &AddArgs) -> Result<LlmProviderConfig> {
    let mut entry = LlmProviderConfig {
        id: args.id.trim().to_string(),
        ..LlmProviderConfig::default()
    };
    if entry.id.is_empty() {
        return Err(anyhow!("llm provider id must not be empty"));
    }
    if let Some(preset_id) = args.preset.as_deref() {
        let preset = bundled_llm_provider_preset(preset_id);
        if preset.id.is_empty() {
            return Err(anyhow!("unknown preset {preset_id:?}"));
        }
        entry.preset = preset.id.to_string();
        entry.backend = preset.family.to_string();
        entry.endpoint = preset.endpoint.to_string();
        entry.auth_strategy = preset.auth_strategy.to_string();
        entry.default_model = preset.default_model.to_string();
        entry.model_pins = preset.model_pins.iter().map(|s| (*s).to_string()).collect();
        entry.capabilities = preset
            .capabilities
            .iter()
            .map(|s| (*s).to_string())
            .collect();
    }
    if let Some(backend) = &args.backend {
        entry.backend = backend.clone();
    }
    entry.transport = args.transport.clone();
    if entry.transport == LLM_TRANSPORT_STDIO {
        entry.command = args.command.clone();
    } else if entry.transport.is_empty() {
        entry.transport = LLM_TRANSPORT_HTTP.to_string();
    }
    if let Some(endpoint) = &args.endpoint {
        entry.endpoint = endpoint.clone();
    }
    if let Some(strategy) = &args.auth_strategy {
        entry.auth_strategy = strategy.clone();
    }
    if let Some(model) = &args.default_model {
        entry.default_model = model.clone();
    }
    if !args.capabilities.is_empty() {
        entry.capabilities = args.capabilities.clone();
    }
    if entry.credential_ref.is_empty() {
        entry.credential_ref = format!("keyring://rillan/llm/{}", entry.id);
    }
    Ok(entry)
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    fn config_path(dir: &tempfile::TempDir) -> PathBuf {
        dir.path().join("config.yaml")
    }

    fn write_initial(path: &std::path::Path, cfg: &rillan_config::Config) {
        rillan_config::write_config(path, cfg).expect("write config");
    }

    #[tokio::test]
    async fn add_notifies_daemon_refresh() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let (tx, rx) = tokio::sync::oneshot::channel();
        tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut buf = vec![0_u8; 4096];
            let n = stream.read(&mut buf).await.unwrap();
            let _ = tx.send(String::from_utf8_lossy(&buf[..n]).to_string());
            stream
                .write_all(b"HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
                .await
                .unwrap();
        });

        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        let mut cfg = rillan_config::Config::default();
        cfg.server.port = port;
        write_initial(&path, &cfg);

        add(
            AddArgs {
                id: "demo".into(),
                preset: Some("openai".into()),
                backend: None,
                transport: LLM_TRANSPORT_HTTP.into(),
                endpoint: None,
                command: Vec::new(),
                auth_strategy: None,
                default_model: None,
                capabilities: Vec::new(),
                config: Some(path),
            },
            Store::in_memory(),
        )
        .await
        .unwrap();

        let request = rx.await.unwrap();
        assert!(request.starts_with("POST /admin/runtime/refresh HTTP/1.1\r\n"));
    }
}
