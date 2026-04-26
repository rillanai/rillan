// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `rillan daemon` — daemon-side CLI helpers. Mirrors
//! `cmd/rillan/daemon_refresh.go`. Today only `daemon refresh` is exposed.

use std::error::Error as StdError;
use std::path::PathBuf;

use anyhow::{anyhow, Context, Result};
use clap::{Args, Subcommand};
use reqwest::header::AUTHORIZATION;
use rillan_config::Config;
use rillan_secretstore::Store;

const ADMIN_RUNTIME_REFRESH_PATH: &str = "/admin/runtime/refresh";

#[derive(Debug, Args)]
pub(crate) struct DaemonArgs {
    #[command(subcommand)]
    command: DaemonCommand,
}

#[derive(Debug, Subcommand)]
enum DaemonCommand {
    /// Ask a running daemon to reload its config + provider host.
    Refresh(RefreshArgs),
}

#[derive(Debug, Args)]
struct RefreshArgs {
    /// Path to the runtime config file. Used to discover the daemon's bind
    /// address.
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

pub(crate) async fn run(args: DaemonArgs, store: Store) -> Result<()> {
    match args.command {
        DaemonCommand::Refresh(args) => refresh(args, store).await,
    }
}

async fn refresh(args: RefreshArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_with_mode(&config_path, rillan_config::Validation::Status)?;
    notify_daemon_runtime_refresh(&cfg, &store).await?;
    println!("daemon at {} refreshed", daemon_refresh_url(&cfg));
    Ok(())
}

pub(crate) async fn refresh_daemon_after_mutation(
    mut cfg: Config,
    mutation: &str,
    store: &Store,
) -> Result<()> {
    rillan_config::apply_environment_overrides(&mut cfg);
    notify_daemon_runtime_refresh(&cfg, store)
        .await
        .with_context(|| mutation.to_string())
}

pub(crate) async fn notify_daemon_runtime_refresh(cfg: &Config, store: &Store) -> Result<()> {
    let url = daemon_refresh_url(cfg);
    let mut request = reqwest::Client::new().post(&url);
    if cfg.server.auth.enabled {
        let bearer = rillan_config::resolve_server_auth_bearer(cfg, store)
            .context("resolve daemon auth bearer")?;
        request = request.header(AUTHORIZATION, format!("Bearer {bearer}"));
    }

    let response = match request.body(Vec::new()).send().await {
        Ok(response) => response,
        Err(err) if is_connection_refused(&err) => return Ok(()),
        Err(err) => return Err(err).context("notify daemon refresh"),
    };

    if response.status().is_success() {
        return Ok(());
    }

    let status = response.status();
    let body = response.text().await.unwrap_or_default();
    let trimmed = body.trim();
    let message = if trimmed.is_empty() {
        status.to_string()
    } else {
        trimmed.to_string()
    };
    Err(anyhow!("daemon refresh failed: {message}"))
}

fn daemon_refresh_url(cfg: &Config) -> String {
    let host = daemon_refresh_host(&cfg.server.host);
    format!(
        "http://{host}:{port}{ADMIN_RUNTIME_REFRESH_PATH}",
        port = cfg.server.port
    )
}

fn daemon_refresh_host(host: &str) -> String {
    match host.trim() {
        "" | "0.0.0.0" | "::" | "[::]" => "127.0.0.1".to_string(),
        trimmed => trimmed.to_string(),
    }
}

fn is_connection_refused(err: &reqwest::Error) -> bool {
    let mut source = err.source();
    while let Some(current) = source {
        if let Some(io_err) = current.downcast_ref::<std::io::Error>() {
            if io_err.kind() == std::io::ErrorKind::ConnectionRefused {
                return true;
            }
        }
        source = current.source();
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::SocketAddr;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    async fn spawn_server(
        response: &'static str,
    ) -> (SocketAddr, tokio::sync::oneshot::Receiver<String>) {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let (tx, rx) = tokio::sync::oneshot::channel();
        tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut buf = vec![0_u8; 4096];
            let n = stream.read(&mut buf).await.unwrap();
            let request = String::from_utf8_lossy(&buf[..n]).to_string();
            let _ = tx.send(request);
            stream.write_all(response.as_bytes()).await.unwrap();
        });
        (addr, rx)
    }

    fn base_config(port: u16) -> Config {
        let mut cfg = Config::default();
        cfg.server.host = "127.0.0.1".into();
        cfg.server.port = port;
        cfg
    }

    #[tokio::test]
    async fn notify_uses_loopback_for_wildcard_host() {
        let (addr, request_rx) =
            spawn_server("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n").await;
        let mut cfg = base_config(addr.port());
        cfg.server.host = "0.0.0.0".into();

        notify_daemon_runtime_refresh(&cfg, &Store::in_memory())
            .await
            .unwrap();

        let request = request_rx.await.unwrap();
        assert!(request.starts_with("POST /admin/runtime/refresh HTTP/1.1\r\n"));
        assert!(request.to_ascii_lowercase().contains("host: 127.0.0.1:"));
    }

    #[tokio::test]
    async fn notify_adds_bearer_when_server_auth_enabled() {
        let (addr, request_rx) =
            spawn_server("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n").await;
        let mut cfg = base_config(addr.port());
        cfg.server.auth.enabled = true;
        cfg.server.auth.auth_strategy = rillan_config::AUTH_STRATEGY_API_KEY.into();
        cfg.server.auth.session_ref = "keyring://rillan/auth/daemon".into();
        let store = Store::in_memory();
        store
            .save(
                &cfg.server.auth.session_ref,
                rillan_secretstore::Credential {
                    kind: rillan_config::AUTH_STRATEGY_API_KEY.into(),
                    api_key: "daemon-token".into(),
                    endpoint: "http://127.0.0.1".into(),
                    auth_strategy: rillan_config::AUTH_STRATEGY_API_KEY.into(),
                    ..Default::default()
                },
            )
            .unwrap();

        notify_daemon_runtime_refresh(&cfg, &store).await.unwrap();

        let request = request_rx.await.unwrap();
        assert!(request.contains("\r\nauthorization: Bearer daemon-token\r\n"));
    }

    #[tokio::test]
    async fn notify_ignores_connection_refused() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        drop(listener);

        let cfg = base_config(port);
        notify_daemon_runtime_refresh(&cfg, &Store::in_memory())
            .await
            .unwrap();
    }

    #[tokio::test]
    async fn refresh_after_mutation_wraps_error_with_mutation_context() {
        let mut cfg = base_config(1);
        cfg.server.auth.enabled = true;
        cfg.server.auth.auth_strategy = rillan_config::AUTH_STRATEGY_API_KEY.into();
        cfg.server.auth.session_ref = "keyring://rillan/auth/daemon".into();
        let err = refresh_daemon_after_mutation(cfg, "updated config", &Store::in_memory())
            .await
            .expect_err("must fail");
        let message = format!("{err:#}");
        assert!(message.contains("updated config"));
        assert!(message.contains("resolve daemon auth bearer"));
    }
}
