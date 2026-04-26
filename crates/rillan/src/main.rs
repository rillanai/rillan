// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan` CLI entry point. Mirrors `cmd/rillan` in the Go repo.

use std::process::ExitCode;

use clap::{Parser, Subcommand};
use rillan_secretstore::Store;
use tracing_subscriber::EnvFilter;

mod commands;

/// Top-level CLI definition. Mirrors the Cobra command tree from `cmd/rillan`.
#[derive(Debug, Parser)]
#[command(
    name = "rillan",
    version = rillan_version::VERSION,
    about = "Local OpenAI-compatible proxy daemon",
    long_about = None,
    disable_help_subcommand = true,
)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Debug, Subcommand)]
enum Command {
    /// Write a starter config for Rillan.
    Init(commands::init::InitArgs),
    /// Start the local Rillan API daemon.
    Serve(commands::serve::ServeArgs),
    /// Show daemon and config status.
    Status(commands::status::StatusArgs),
    /// Manage stored LLM provider credentials.
    Llm(commands::llm::LlmArgs),
    /// Manage named MCP endpoints.
    Mcp(commands::mcp::McpArgs),
    /// Rebuild the local SQLite index.
    Index(commands::index::IndexArgs),
    /// Daemon-side helpers.
    Daemon(commands::daemon::DaemonArgs),
    /// Manage installed markdown skills.
    Skill(commands::skill::SkillArgs),
    /// Manage Rillan team and control-plane authentication.
    Auth(commands::auth::AuthArgs),
    /// Inspect and mutate Rillan configuration (placeholder).
    Config(commands::config::ConfigArgs),
}

fn main() -> ExitCode {
    init_tracing();
    let cli = Cli::parse();

    let runtime = match tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
    {
        Ok(rt) => rt,
        Err(err) => {
            eprintln!("rillan: failed to start tokio runtime: {err}");
            return ExitCode::FAILURE;
        }
    };

    let store = Store::os_keyring();
    let result = runtime.block_on(async {
        match cli.command {
            Command::Init(args) => commands::init::run(args).await,
            Command::Serve(args) => commands::serve::run(args, store).await,
            Command::Status(args) => commands::status::run(args).await,
            Command::Llm(args) => commands::llm::run(args, store.clone()).await,
            Command::Mcp(args) => commands::mcp::run(args, store.clone()).await,
            Command::Index(args) => commands::index::run(args).await,
            Command::Daemon(args) => commands::daemon::run(args, store.clone()).await,
            Command::Skill(args) => commands::skill::run(args).await,
            Command::Auth(args) => commands::auth::run(args, store).await,
            Command::Config(args) => commands::config::run(args).await,
        }
    });

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("rillan: {err:#}");
            ExitCode::FAILURE
        }
    }
}

fn init_tracing() {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    let _ = tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_target(false)
        .json()
        .try_init();
}
