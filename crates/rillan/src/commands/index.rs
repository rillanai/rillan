// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan index` — rebuild the local SQLite index. Mirrors `cmd/rillan/index.go`.

use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Args;
use rillan_index::{rebuild, RebuildOptions};

#[derive(Debug, Args)]
pub(crate) struct IndexArgs {
    /// Path to the runtime config file.
    #[arg(long, value_name = "PATH")]
    config: Option<PathBuf>,
}

pub(crate) async fn run(args: IndexArgs) -> Result<()> {
    let config_path = args
        .config
        .unwrap_or_else(rillan_config::default_config_path);
    let cfg = rillan_config::load_with_mode(&config_path, rillan_config::Validation::Index)
        .with_context(|| format!("load config: {}", config_path.display()))?;
    let status = rebuild(&cfg, RebuildOptions::default()).await?;
    println!(
        "indexed {} documents, {} chunks, {} vectors at {}",
        status.documents,
        status.chunks,
        status.vectors,
        status.db_path.display()
    );
    Ok(())
}
