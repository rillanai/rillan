// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan serve` — start the local API daemon.

use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Args;
use rillan_app::App;
use rillan_secretstore::Store;

#[derive(Debug, Args)]
pub(crate) struct ServeArgs {
    /// Path to the runtime config file.
    #[arg(long, value_name = "PATH")]
    pub(crate) config: Option<PathBuf>,
}

pub(crate) async fn run(args: ServeArgs, store: Store) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load(&config_path)
        .with_context(|| format!("load config: {}", config_path.display()))?;

    let project_path = rillan_config::resolve_project_config_path(&cfg.index.root);
    let project_cfg = match rillan_config::load_project(&project_path) {
        Ok(cfg) => cfg,
        Err(rillan_config::Error::Read(io)) if io.kind() == std::io::ErrorKind::NotFound => {
            rillan_config::default_project_config()
        }
        Err(err) => return Err(err.into()),
    };

    let system_path = rillan_config::resolve_system_config_path();
    let system_cfg = match rillan_config::load_system(&system_path) {
        Ok(cfg) => Some(cfg),
        Err(rillan_config::Error::Read(io)) if io.kind() == std::io::ErrorKind::NotFound => None,
        Err(err) => return Err(err.into()),
    };

    let app = App::new(
        cfg,
        project_cfg,
        system_cfg,
        store,
        config_path,
        project_path,
        system_path,
    )
    .await?;

    let shutdown = async {
        if let Err(err) = tokio::signal::ctrl_c().await {
            tracing::error!(error = %err, "ctrl-c handler failed");
        }
    };
    app.run(shutdown).await?;
    Ok(())
}
