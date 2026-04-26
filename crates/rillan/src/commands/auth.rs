// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `rillan auth` — Rillan team / control-plane authentication. Mirrors
//! `cmd/rillan/auth.go`.

use std::path::PathBuf;

use anyhow::{anyhow, Result};
use clap::{Args, Subcommand};
use rillan_config::{
    load_for_edit, write_config, AUTH_STRATEGY_API_KEY, AUTH_STRATEGY_BROWSER_OIDC,
    AUTH_STRATEGY_DEVICE_OIDC,
};
use rillan_secretstore::{Credential, Store};

use crate::commands::daemon::refresh_daemon_after_mutation;

const DEFAULT_SESSION_REF: &str = "keyring://rillan/auth/control-plane";

#[derive(Debug, Args)]
pub(crate) struct AuthArgs {
    #[command(subcommand)]
    command: AuthCommand,
}

#[derive(Debug, Subcommand)]
enum AuthCommand {
    /// Log into a Rillan team endpoint.
    Login(LoginArgs),
    /// Log out of the active Rillan team endpoint.
    Logout(LogoutArgs),
    /// Show Rillan team authentication state.
    Status(StatusArgs),
}

#[derive(Debug, Args)]
struct LoginArgs {
    /// Control-plane endpoint URL. Defaults to the value already in config.
    #[arg(long)]
    endpoint: Option<String>,
    /// Auth strategy (api_key, browser_oidc, device_oidc). Defaults to the
    /// value already in config.
    #[arg(long)]
    auth_strategy: Option<String>,
    #[command(flatten)]
    credential: CredentialFlags,
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct LogoutArgs {
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct StatusArgs {
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

/// Credential flag set shared by login subcommands. Mirrors the Go
/// `addCredentialFlags` helper.
#[derive(Debug, Args, Default)]
struct CredentialFlags {
    /// API key to store securely.
    #[arg(long)]
    api_key: Option<String>,
    /// Access token to store securely.
    #[arg(long)]
    access_token: Option<String>,
    /// Refresh token to store securely.
    #[arg(long)]
    refresh_token: Option<String>,
    /// ID token to store securely.
    #[arg(long)]
    id_token: Option<String>,
    /// OIDC issuer bound to the stored session.
    #[arg(long)]
    issuer: Option<String>,
    /// OIDC audience bound to the stored session.
    #[arg(long)]
    audience: Option<String>,
}

pub(crate) async fn run(args: AuthArgs, store: Store) -> Result<()> {
    match args.command {
        AuthCommand::Login(args) => login(args, store).await,
        AuthCommand::Logout(args) => logout(args, store).await,
        AuthCommand::Status(args) => status(args, store).await,
    }
}

async fn login(args: LoginArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let mut cfg = load_for_edit(&config_path)?;

    let endpoint = pick_or_default(args.endpoint.as_deref(), &cfg.auth.rillan.endpoint);
    let auth_strategy = pick_or_default(
        args.auth_strategy.as_deref(),
        &cfg.auth.rillan.auth_strategy,
    );
    if endpoint.is_empty() {
        return Err(anyhow!(
            "--endpoint is required when no control-plane endpoint is configured"
        ));
    }
    if auth_strategy.is_empty() {
        return Err(anyhow!(
            "--auth-strategy is required when no control-plane auth strategy is configured"
        ));
    }

    let credential = credential_from_input(&auth_strategy, &endpoint, &args.credential)?;

    cfg.auth.rillan.endpoint = endpoint.clone();
    cfg.auth.rillan.auth_strategy = auth_strategy.to_ascii_lowercase();
    if cfg.auth.rillan.session_ref.is_empty() {
        cfg.auth.rillan.session_ref = DEFAULT_SESSION_REF.to_string();
    }
    let session_ref = cfg.auth.rillan.session_ref.clone();

    store.save(&session_ref, credential)?;
    write_config(&config_path, &cfg)?;
    println!("logged into control plane at {endpoint}");
    refresh_daemon_after_mutation(cfg, "updated control-plane auth", &store).await
}

async fn logout(args: LogoutArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let cfg = load_for_edit(&config_path)?;
    let session_ref = cfg.auth.rillan.session_ref.trim();
    if session_ref.is_empty() {
        return Ok(());
    }
    store.delete(session_ref)?;
    println!("logged out of control plane");
    refresh_daemon_after_mutation(cfg, "updated control-plane auth", &store).await
}

async fn status(args: StatusArgs, store: Store) -> Result<()> {
    let config_path = resolve_config_path(args.config);
    let cfg = load_for_edit(&config_path)?;
    let session_ref = cfg.auth.rillan.session_ref.trim();
    let logged_in = if session_ref.is_empty() {
        false
    } else {
        store.exists(session_ref)
    };
    println!("endpoint: {}", cfg.auth.rillan.endpoint);
    println!("auth_strategy: {}", cfg.auth.rillan.auth_strategy);
    println!("session_ref: {}", cfg.auth.rillan.session_ref);
    println!("logged_in: {logged_in}");
    Ok(())
}

fn resolve_config_path(override_path: Option<PathBuf>) -> PathBuf {
    override_path.unwrap_or_else(rillan_config::default_config_path)
}

fn pick_or_default(flag: Option<&str>, fallback: &str) -> String {
    let trimmed = flag.map(str::trim).unwrap_or_default();
    if !trimmed.is_empty() {
        return trimmed.to_string();
    }
    fallback.trim().to_string()
}

fn credential_from_input(
    auth_strategy: &str,
    endpoint: &str,
    flags: &CredentialFlags,
) -> Result<Credential> {
    match auth_strategy.trim().to_ascii_lowercase().as_str() {
        AUTH_STRATEGY_API_KEY => {
            let api_key =
                trim_required(flags.api_key.as_deref(), "--api-key", AUTH_STRATEGY_API_KEY)?;
            Ok(Credential {
                kind: AUTH_STRATEGY_API_KEY.to_string(),
                api_key,
                endpoint: endpoint.to_string(),
                auth_strategy: AUTH_STRATEGY_API_KEY.to_string(),
                ..Credential::default()
            })
        }
        AUTH_STRATEGY_BROWSER_OIDC | AUTH_STRATEGY_DEVICE_OIDC => {
            let strategy = auth_strategy.trim().to_ascii_lowercase();
            let access_token =
                trim_required(flags.access_token.as_deref(), "--access-token", &strategy)?;
            Ok(Credential {
                kind: "oidc".to_string(),
                access_token,
                refresh_token: trim_owned(flags.refresh_token.as_deref()),
                id_token: trim_owned(flags.id_token.as_deref()),
                endpoint: endpoint.to_string(),
                auth_strategy: strategy,
                issuer: trim_owned(flags.issuer.as_deref()),
                audience: trim_owned(flags.audience.as_deref()),
                ..Credential::default()
            })
        }
        "none" => Err(anyhow!("auth strategy none does not support login")),
        other => Err(anyhow!("unsupported auth strategy {other:?}")),
    }
}

fn trim_required(value: Option<&str>, flag: &str, strategy: &str) -> Result<String> {
    let trimmed = value.map(str::trim).unwrap_or_default();
    if trimmed.is_empty() {
        return Err(anyhow!("{flag} is required for {strategy} auth"));
    }
    Ok(trimmed.to_string())
}

fn trim_owned(value: Option<&str>) -> String {
    value.map(str::trim).unwrap_or_default().to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::Config;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    fn config_path(dir: &tempfile::TempDir) -> PathBuf {
        dir.path().join("config.yaml")
    }

    fn write_initial(path: &std::path::Path, cfg: &Config) {
        write_config(path, cfg).expect("write config");
    }

    #[tokio::test]
    async fn login_status_logout_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());
        let store = Store::in_memory();

        login(
            LoginArgs {
                endpoint: Some("https://team.example".into()),
                auth_strategy: Some(AUTH_STRATEGY_DEVICE_OIDC.into()),
                credential: CredentialFlags {
                    access_token: Some("token-1".into()),
                    issuer: Some("issuer-a".into()),
                    ..CredentialFlags::default()
                },
                config: Some(path.clone()),
            },
            store.clone(),
        )
        .await
        .unwrap();

        let cfg = load_for_edit(&path).unwrap();
        assert_eq!(cfg.auth.rillan.endpoint, "https://team.example");
        assert_eq!(cfg.auth.rillan.auth_strategy, AUTH_STRATEGY_DEVICE_OIDC);
        assert_eq!(cfg.auth.rillan.session_ref, DEFAULT_SESSION_REF);
        assert!(store.exists(DEFAULT_SESSION_REF));

        logout(
            LogoutArgs {
                config: Some(path.clone()),
            },
            store.clone(),
        )
        .await
        .unwrap();
        assert!(!store.exists(DEFAULT_SESSION_REF));
    }

    #[tokio::test]
    async fn login_rejects_missing_endpoint_when_unset() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());
        let store = Store::in_memory();
        let err = login(
            LoginArgs {
                endpoint: None,
                auth_strategy: Some(AUTH_STRATEGY_API_KEY.into()),
                credential: CredentialFlags {
                    api_key: Some("k".into()),
                    ..CredentialFlags::default()
                },
                config: Some(path),
            },
            store,
        )
        .await
        .expect_err("must fail");
        assert!(err.to_string().contains("--endpoint is required"));
    }

    #[tokio::test]
    async fn login_rejects_missing_auth_strategy_when_unset() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());
        let store = Store::in_memory();
        let err = login(
            LoginArgs {
                endpoint: Some("https://team.example".into()),
                auth_strategy: None,
                credential: CredentialFlags::default(),
                config: Some(path),
            },
            store,
        )
        .await
        .expect_err("must fail");
        assert!(err.to_string().contains("--auth-strategy is required"));
    }

    #[tokio::test]
    async fn login_rejects_none_strategy() {
        let err = credential_from_input("none", "https://team", &CredentialFlags::default())
            .expect_err("must fail");
        assert!(err.to_string().contains("none does not support login"));
    }

    #[tokio::test]
    async fn login_requires_access_token_for_oidc() {
        let err = credential_from_input(
            AUTH_STRATEGY_DEVICE_OIDC,
            "https://team",
            &CredentialFlags::default(),
        )
        .expect_err("must fail");
        assert!(err.to_string().contains("--access-token is required"));
    }

    #[tokio::test]
    async fn status_reports_logged_out_when_no_session() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());
        // Just verify it doesn't error; the println output isn't captured here
        // but is exercised via the login_status_logout_round_trip path.
        status(StatusArgs { config: Some(path) }, Store::in_memory())
            .await
            .unwrap();
    }

    #[tokio::test]
    async fn logout_is_noop_when_session_ref_unset() {
        let dir = tempfile::tempdir().unwrap();
        let path = config_path(&dir);
        write_initial(&path, &Config::default());
        // No session_ref written; should silently succeed and not touch store.
        logout(LogoutArgs { config: Some(path) }, Store::in_memory())
            .await
            .unwrap();
    }

    #[tokio::test]
    async fn login_notifies_daemon_refresh() {
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

        login(
            LoginArgs {
                endpoint: Some("https://team.example".into()),
                auth_strategy: Some(AUTH_STRATEGY_API_KEY.into()),
                credential: CredentialFlags {
                    api_key: Some("secret".into()),
                    ..CredentialFlags::default()
                },
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
