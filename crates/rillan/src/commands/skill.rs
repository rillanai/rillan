// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `rillan skill` — manage installed markdown skills.
//!
//! Mirrors `cmd/rillan/skill.go`. Skills live under
//! `<data_dir>/skills/<id>/SKILL.md` with a `catalog.json` manifest.

use anyhow::Result;
use clap::{Args, Subcommand};
use rillan_agent::{
    get_installed_skill, install_skill, list_installed_skills, remove_skill, InstalledSkill,
};
use time::OffsetDateTime;

#[derive(Debug, Args)]
pub(crate) struct SkillArgs {
    #[command(subcommand)]
    command: SkillCommand,
}

#[derive(Debug, Subcommand)]
enum SkillCommand {
    /// Install a markdown skill into managed Rillan storage.
    Install(InstallArgs),
    /// Remove an installed markdown skill.
    Remove(RemoveArgs),
    /// List installed markdown skills.
    List,
    /// Show metadata for an installed markdown skill.
    Show(ShowArgs),
}

#[derive(Debug, Args)]
struct InstallArgs {
    /// Path to a markdown file describing the skill.
    path: String,
}

#[derive(Debug, Args)]
struct RemoveArgs {
    /// Skill id (kebab-case).
    id: String,
    /// Remove the skill even if the current project still enables it.
    #[arg(long)]
    force: bool,
}

#[derive(Debug, Args)]
struct ShowArgs {
    /// Skill id (kebab-case).
    id: String,
}

pub(crate) async fn run(args: SkillArgs) -> Result<()> {
    match args.command {
        SkillCommand::Install(args) => install(args).await,
        SkillCommand::Remove(args) => remove(args).await,
        SkillCommand::List => list().await,
        SkillCommand::Show(args) => show(args).await,
    }
}

async fn install(args: InstallArgs) -> Result<()> {
    let skill = install_skill(&args.path, OffsetDateTime::now_utc())?;
    println!("installed skill {} at {}", skill.id, skill.managed_path);
    Ok(())
}

async fn remove(args: RemoveArgs) -> Result<()> {
    let removed = remove_skill(&args.id, args.force)?;
    println!("removed skill {}", removed.id);
    Ok(())
}

async fn list() -> Result<()> {
    for skill in list_installed_skills()? {
        print_listing(&skill);
    }
    Ok(())
}

async fn show(args: ShowArgs) -> Result<()> {
    let skill = get_installed_skill(&args.id)?;
    println!("id: {}", skill.id);
    println!("display_name: {}", skill.display_name);
    println!("source_path: {}", skill.source_path);
    println!("managed_path: {}", skill.managed_path);
    println!("checksum: {}", skill.checksum);
    println!("parser_version: {}", skill.parser_version);
    println!("capability_summary: {}", skill.capability_summary.trim());
    Ok(())
}

fn print_listing(skill: &InstalledSkill) {
    println!("- id: {}", skill.id);
    println!("  display_name: {}", skill.display_name);
    println!("  installed_at: {}", skill.installed_at);
    println!("  checksum: {}", skill.checksum);
}
