// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan init` — write the starter configs to disk.

use std::path::PathBuf;

use anyhow::Result;
use clap::Args;

#[derive(Debug, Args)]
pub(crate) struct InitArgs {
    /// Path to write the starter runtime config.
    #[arg(long, value_name = "PATH")]
    pub(crate) output: Option<PathBuf>,
    /// Path to write the starter project config.
    #[arg(long, value_name = "PATH")]
    pub(crate) project_output: Option<PathBuf>,
    /// Overwrite existing files at the destination paths.
    #[arg(long)]
    pub(crate) force: bool,
}

pub(crate) async fn run(args: InitArgs) -> Result<()> {
    let output = args
        .output
        .unwrap_or_else(rillan_config::default_config_path);
    let project_output = args
        .project_output
        .unwrap_or_else(|| rillan_config::default_project_config_path(""));
    rillan_config::write_example_config(&output, args.force)?;
    rillan_config::write_example_project_config(&project_output, args.force)?;
    println!("wrote config to {}", output.display());
    println!("wrote project config to {}", project_output.display());
    Ok(())
}
