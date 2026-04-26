// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `rillan mcp` — manage named MCP endpoints. Mirrors `cmd/rillan/mcp.go`.

use std::path::PathBuf;

use anyhow::{anyhow, Context, Result};
use clap::{Args, Subcommand};
use rillan_config::{
    load_for_edit, write_config, McpServerConfig, AUTH_STRATEGY_API_KEY,
    AUTH_STRATEGY_BROWSER_OIDC, AUTH_STRATEGY_DEVICE_OIDC, AUTH_STRATEGY_NONE, LLM_TRANSPORT_HTTP,
    LLM_TRANSPORT_STDIO,
};
use rillan_secretstore::{Credential, Store};

use crate::commands::daemon::refresh_daemon_after_mutation;

#[derive(Debug, Args)]
pub(crate) struct McpArgs {
    #[command(subcommand)]
    command: McpCommand,
}

#[derive(Debug, Subcommand)]
enum McpCommand {
    /// Add a named MCP endpoint entry.
    Add(AddArgs),
    /// Remove a named MCP endpoint entry.
    Remove(RemoveArgs),
    /// List configured MCP endpoint entries.
    List(ListArgs),
    /// Select the default MCP endpoint entry.
    Use(UseArgs),
    /// Authenticate an MCP endpoint entry.
    Login(LoginArgs),
    /// Clear authentication for an MCP endpoint entry.
    Logout(LogoutArgs),
}

#[derive(Debug, Args)]
struct AddArgs {
    /// Endpoint id (kebab-case).
    id: String,
    /// MCP endpoint URL (required for http transport).
    #[arg(long)]
    endpoint: Option<String>,
    /// Transport type (http or stdio).
    #[arg(long, default_value = LLM_TRANSPORT_HTTP)]
    transport: String,
    /// MCP command for stdio transport (repeatable).
    #[arg(long = "command", value_name = "ARG")]
    command: Vec<String>,
    /// Auth strategy (none, api_key, browser_oidc, device_oidc).
    #[arg(long, default_value = AUTH_STRATEGY_NONE)]
    auth_strategy: String,
    /// Whether this MCP endpoint is read-only.
    #[arg(long, default_value_t = true, action = clap::ArgAction::Set)]
    read_only: bool,
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
struct ListArgs {
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
    /// Endpoint id as configured in `mcps.servers[*].id`.
    id: String,
    /// API key to store. If omitted, the value is read from
    /// `RILLAN_LOGIN_API_KEY`.
    #[arg(long, value_name = "API_KEY", env = "RILLAN_LOGIN_API_KEY")]
    api_key: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct LogoutArgs {
    id: String,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

pub(crate) async fn run(args: McpArgs, store: Store) -> Result<()> {
    match args.command {
        McpCommand::Add(args) => add(args, store.clone()).await,
        McpCommand::Remove(args) => remove(args, store.clone()).await,
        McpCommand::List(args) => list(args).await,
        McpCommand::Use(args) => use_server(args, store.clone()).await,
        McpCommand::Login(args) => login(args, store).await,
        McpCommand::Logout(args) => logout(args, store).await,
    }
}

async fn add(args: AddArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config.clone());
    let entry = build_entry(&args)?;
    let mut cfg = load_for_edit(&config_path)?;
    if cfg.mcps.servers.iter().any(|s| s.id == entry.id) {
        return Err(anyhow!("mcp server {:?} already exists", entry.id));
    }
    let entry_id = entry.id.clone();
    cfg.mcps.servers.push(entry);
    if cfg.mcps.default.is_empty() {
        cfg.mcps.default = entry_id.clone();
    }
    write_config(&config_path, &cfg)?;
    println!("added mcp server {entry_id}");
    refresh_daemon_after_mutation(cfg, "updated mcp config", &store).await
}

async fn remove(args: RemoveArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let mut cfg = load_for_edit(&config_path)?;
    let id = args.id.trim().to_string();
    let before = cfg.mcps.servers.len();
    cfg.mcps.servers.retain(|s| s.id != id);
    if cfg.mcps.servers.len() == before {
        return Err(anyhow!("mcp server {:?} not found", id));
    }
    if cfg.mcps.default == id {
        cfg.mcps.default = String::new();
    }
    write_config(&config_path, &cfg)?;
    println!("removed mcp server {id}");
    refresh_daemon_after_mutation(cfg, "updated mcp config", &store).await
}

async fn list(args: ListArgs) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let cfg = load_for_edit(&config_path)?;
    let mut servers = cfg.mcps.servers.clone();
    servers.sort_by(|a, b| a.id.cmp(&b.id));
    println!("default: {}", cfg.mcps.default);
    for server in servers {
        println!("- id: {}", server.id);
        println!("  endpoint: {}", server.endpoint);
        println!("  transport: {}", server.transport);
        println!("  auth_strategy: {}", server.auth_strategy);
        println!("  read_only: {}", server.read_only);
        if !server.command.is_empty() {
            println!("  command: {}", server.command.join(" "));
        }
    }
    Ok(())
}

async fn use_server(args: UseArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let mut cfg = load_for_edit(&config_path)?;
    let id = args.id.trim().to_string();
    if !cfg.mcps.servers.iter().any(|s| s.id == id) {
        return Err(anyhow!("mcp server {:?} not found", id));
    }
    cfg.mcps.default = id.clone();
    write_config(&config_path, &cfg)?;
    println!("default mcp server set to {id}");
    refresh_daemon_after_mutation(cfg, "updated mcp config", &store).await
}

async fn login(args: LoginArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let cfg = load_for_edit(&config_path)?;
    let server = cfg
        .mcps
        .servers
        .iter()
        .find(|s| s.id == args.id)
        .with_context(|| {
            format!(
                "mcp server {:?} is not configured in {}",
                args.id,
                config_path.display()
            )
        })?;
    let credential = Credential {
        kind: AUTH_STRATEGY_API_KEY.to_string(),
        api_key: args.api_key,
        endpoint: server.endpoint.clone(),
        auth_strategy: server.auth_strategy.clone(),
        ..Credential::default()
    };
    store.save(&server.credential_ref, credential)?;
    println!("authenticated mcp endpoint {}", server.id);
    refresh_daemon_after_mutation(cfg, "updated mcp auth", &store).await
}

async fn logout(args: LogoutArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let cfg = load_for_edit(&config_path)?;
    let server = cfg
        .mcps
        .servers
        .iter()
        .find(|s| s.id == args.id)
        .with_context(|| format!("mcp server {:?} is not configured", args.id))?;
    store.delete(&server.credential_ref)?;
    println!("cleared mcp auth for {}", server.id);
    refresh_daemon_after_mutation(cfg, "updated mcp auth", &store).await
}

fn resolve_config_path(override_path: Option<PathBuf>) -> PathBuf {
    override_path.unwrap_or_else(rillan_config::default_config_path)
}

fn build_entry(args: &AddArgs) -> Result<McpServerConfig> {
    let id = args.id.trim().to_string();
    if id.is_empty() {
        return Err(anyhow!("mcp server name must not be empty"));
    }
    let transport = normalize_transport(&args.transport);
    let endpoint = args.endpoint.clone().unwrap_or_default();
    let command = args
        .command
        .iter()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect::<Vec<_>>();
    let auth_strategy = normalize_auth_strategy(&args.auth_strategy);

    match transport.as_str() {
        LLM_TRANSPORT_HTTP => {
            if endpoint.trim().is_empty() {
                return Err(anyhow!(
                    "mcp server endpoint must not be empty when transport is {:?}",
                    transport,
                ));
            }
        }
        LLM_TRANSPORT_STDIO => {
            if command.is_empty() {
                return Err(anyhow!(
                    "mcp server command must not be empty when transport is {:?}",
                    transport,
                ));
            }
        }
        other => return Err(anyhow!("unsupported mcp transport {other:?}")),
    }
    match auth_strategy.as_str() {
        AUTH_STRATEGY_NONE
        | AUTH_STRATEGY_API_KEY
        | AUTH_STRATEGY_BROWSER_OIDC
        | AUTH_STRATEGY_DEVICE_OIDC => {}
        other => return Err(anyhow!("unsupported mcp auth strategy {other:?}")),
    }

    Ok(McpServerConfig {
        credential_ref: credential_ref_for_mcp(&id),
        id,
        endpoint,
        transport,
        command,
        auth_strategy,
        read_only: args.read_only,
    })
}

fn credential_ref_for_mcp(id: &str) -> String {
    format!("keyring://rillan/mcp/{}", id.trim())
}

fn normalize_transport(value: &str) -> String {
    let trimmed = value.trim().to_ascii_lowercase();
    if trimmed.is_empty() {
        LLM_TRANSPORT_HTTP.to_string()
    } else {
        trimmed
    }
}

fn normalize_auth_strategy(value: &str) -> String {
    let trimmed = value.trim().to_ascii_lowercase();
    if trimmed.is_empty() {
        AUTH_STRATEGY_NONE.to_string()
    } else {
        trimmed
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;
    use rillan_config::Config;

    fn config_path(dir: &tempfile::TempDir) -> PathBuf {
        dir.path().join("config.yaml")
    }

    fn write_initial(path: &std::path::Path, cfg: &Config) {
        rillan_config::write_config(path, cfg).expect("write config");
    }

    fn add_args(id: &str, path: PathBuf) -> AddArgs {
        AddArgs {
            id: id.into(),
            endpoint: Some("http://127.0.0.1:8765".into()),
            transport: LLM_TRANSPORT_HTTP.into(),
            command: Vec::new(),
            auth_strategy: AUTH_STRATEGY_NONE.into(),
            read_only: true,
            config: Some(path),
        }
    }

    #[tokio::test]
    async fn add_creates_server_entry() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());

        add(add_args("ide-local", path.clone()), Store::in_memory())
            .await
            .unwrap();

        let cfg = load_for_edit(&path).unwrap();
        assert_eq!(cfg.mcps.default, "ide-local");
        assert_eq!(cfg.mcps.servers.len(), 1);
        assert!(cfg.mcps.servers[0].read_only);
        assert_eq!(
            cfg.mcps.servers[0].credential_ref,
            "keyring://rillan/mcp/ide-local"
        );
    }

    #[tokio::test]
    async fn add_supports_stdio_transport_with_command() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());

        let args = AddArgs {
            id: "repo-plugin".into(),
            endpoint: None,
            transport: LLM_TRANSPORT_STDIO.into(),
            command: vec!["rillan-mcp-demo".into()],
            auth_strategy: AUTH_STRATEGY_NONE.into(),
            read_only: true,
            config: Some(path.clone()),
        };
        add(args, Store::in_memory()).await.unwrap();

        let cfg = load_for_edit(&path).unwrap();
        assert_eq!(cfg.mcps.servers[0].transport, LLM_TRANSPORT_STDIO);
        assert_eq!(cfg.mcps.servers[0].command, vec!["rillan-mcp-demo"]);
    }

    #[tokio::test]
    async fn add_rejects_http_without_endpoint() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());

        let args = AddArgs {
            id: "broken".into(),
            endpoint: None,
            transport: LLM_TRANSPORT_HTTP.into(),
            command: Vec::new(),
            auth_strategy: AUTH_STRATEGY_NONE.into(),
            read_only: true,
            config: Some(path),
        };
        let err = add(args, Store::in_memory()).await.expect_err("must fail");
        assert!(err.to_string().contains("endpoint must not be empty"));
    }

    #[tokio::test]
    async fn add_rejects_unknown_auth_strategy() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());

        let args = AddArgs {
            id: "broken".into(),
            endpoint: Some("http://127.0.0.1:8765".into()),
            transport: LLM_TRANSPORT_HTTP.into(),
            command: Vec::new(),
            auth_strategy: "totp".into(),
            read_only: true,
            config: Some(path),
        };
        let err = add(args, Store::in_memory()).await.expect_err("must fail");
        assert!(err.to_string().contains("unsupported mcp auth strategy"));
    }

    #[tokio::test]
    async fn use_switches_default_server() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        let mut cfg = Config::default();
        cfg.mcps.servers = vec![
            McpServerConfig {
                id: "ide-local".into(),
                endpoint: "http://127.0.0.1:8765".into(),
                transport: LLM_TRANSPORT_HTTP.into(),
                auth_strategy: AUTH_STRATEGY_NONE.into(),
                read_only: true,
                ..McpServerConfig::default()
            },
            McpServerConfig {
                id: "repo-gateway".into(),
                endpoint: "http://127.0.0.1:8766".into(),
                transport: LLM_TRANSPORT_HTTP.into(),
                auth_strategy: AUTH_STRATEGY_API_KEY.into(),
                read_only: true,
                credential_ref: credential_ref_for_mcp("repo-gateway"),
                ..McpServerConfig::default()
            },
        ];
        write_initial(&path, &cfg);

        use_server(
            UseArgs {
                id: "repo-gateway".into(),
                config: Some(path.clone()),
            },
            Store::in_memory(),
        )
        .await
        .unwrap();

        let reloaded = load_for_edit(&path).unwrap();
        assert_eq!(reloaded.mcps.default, "repo-gateway");
    }

    #[tokio::test]
    async fn remove_deletes_server_and_clears_default() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        let mut cfg = Config::default();
        cfg.mcps.default = "ide-local".into();
        cfg.mcps.servers = vec![McpServerConfig {
            id: "ide-local".into(),
            endpoint: "http://127.0.0.1:8765".into(),
            transport: LLM_TRANSPORT_HTTP.into(),
            auth_strategy: AUTH_STRATEGY_NONE.into(),
            read_only: true,
            ..McpServerConfig::default()
        }];
        write_initial(&path, &cfg);

        remove(
            RemoveArgs {
                id: "ide-local".into(),
                config: Some(path.clone()),
            },
            Store::in_memory(),
        )
        .await
        .unwrap();

        let reloaded = load_for_edit(&path).unwrap();
        assert!(reloaded.mcps.servers.is_empty());
        assert!(reloaded.mcps.default.is_empty());
    }

    #[tokio::test]
    async fn login_and_logout_round_trip_credential() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        let mut cfg = Config::default();
        cfg.mcps.servers = vec![McpServerConfig {
            id: "ide-local".into(),
            endpoint: "http://127.0.0.1:8765".into(),
            transport: LLM_TRANSPORT_HTTP.into(),
            auth_strategy: AUTH_STRATEGY_API_KEY.into(),
            read_only: true,
            credential_ref: credential_ref_for_mcp("ide-local"),
            ..McpServerConfig::default()
        }];
        write_initial(&path, &cfg);

        let store = Store::in_memory();
        login(
            LoginArgs {
                id: "ide-local".into(),
                api_key: "secret-key".into(),
                config: Some(path.clone()),
            },
            store.clone(),
        )
        .await
        .unwrap();
        assert!(store.exists(&credential_ref_for_mcp("ide-local")));

        logout(
            LogoutArgs {
                id: "ide-local".into(),
                config: Some(path.clone()),
            },
            store.clone(),
        )
        .await
        .unwrap();
        assert!(!store.exists(&credential_ref_for_mcp("ide-local")));
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
        let mut cfg = Config::default();
        cfg.server.port = port;
        write_initial(&path, &cfg);

        add(add_args("ide-local", path), Store::in_memory())
            .await
            .unwrap();

        let request = rx.await.unwrap();
        assert!(request.starts_with("POST /admin/runtime/refresh HTTP/1.1\r\n"));
    }
}
