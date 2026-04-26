// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan config` — placeholder subcommand. Mirrors
//! `cmd/rillan/config_commands.go`, which intentionally returns
//! "not implemented yet" for `get` / `set` / `list` until the underlying
//! schema-aware editor lands.

use std::path::PathBuf;

use anyhow::{anyhow, Result};
use clap::{Args, Subcommand};

#[derive(Debug, Args)]
pub(crate) struct ConfigArgs {
    #[command(subcommand)]
    command: ConfigCommand,
    /// Path to the runtime config file. Currently accepted but unused —
    /// every subcommand returns "not implemented yet" to match the upstream
    /// Go stub.
    #[arg(long, value_name = "PATH", global = true)]
    config: Option<PathBuf>,
}

#[derive(Debug, Subcommand)]
enum ConfigCommand {
    /// Read a configuration value.
    Get,
    /// Write a configuration value.
    Set,
    /// List configuration values.
    List,
}

pub(crate) async fn run(args: ConfigArgs) -> Result<()> {
    let path = match args.command {
        ConfigCommand::Get => "rillan config get",
        ConfigCommand::Set => "rillan config set",
        ConfigCommand::List => "rillan config list",
    };
    Err(anyhow!("{path} not implemented yet"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn get_returns_not_implemented() {
        let err = run(ConfigArgs {
            command: ConfigCommand::Get,
            config: None,
        })
        .await
        .expect_err("must fail");
        assert!(err
            .to_string()
            .contains("rillan config get not implemented"));
    }

    #[tokio::test]
    async fn set_returns_not_implemented() {
        let err = run(ConfigArgs {
            command: ConfigCommand::Set,
            config: None,
        })
        .await
        .expect_err("must fail");
        assert!(err
            .to_string()
            .contains("rillan config set not implemented"));
    }

    #[tokio::test]
    async fn list_returns_not_implemented() {
        let err = run(ConfigArgs {
            command: ConfigCommand::List,
            config: None,
        })
        .await
        .expect_err("must fail");
        assert!(err
            .to_string()
            .contains("rillan config list not implemented"));
    }
}
