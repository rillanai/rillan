// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `rillan status` — print resolved config + path metadata.

use std::path::PathBuf;

use anyhow::Result;
use clap::Args;
use serde_json::json;

#[derive(Debug, Args)]
pub(crate) struct StatusArgs {
    /// Path to the runtime config file.
    #[arg(long, value_name = "PATH")]
    pub(crate) config: Option<PathBuf>,
}

pub(crate) async fn run(args: StatusArgs) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_with_mode(&config_path, rillan_config::Validation::Status)?;
    let payload = json!({
        "version": rillan_version::string(),
        "config_path": config_path.display().to_string(),
        "data_dir": rillan_config::default_data_dir().display().to_string(),
        "log_dir": rillan_config::default_log_dir().display().to_string(),
        "server": {
            "host": cfg.server.host,
            "port": cfg.server.port,
            "log_level": cfg.server.log_level,
            "auth_enabled": cfg.server.auth.enabled,
        },
        "llms": {
            "default": cfg.llms.default,
            "providers": cfg.llms.providers.iter().map(|p| p.id.clone()).collect::<Vec<_>>(),
        },
    });
    println!("{}", serde_json::to_string_pretty(&payload)?);
    Ok(())
}
