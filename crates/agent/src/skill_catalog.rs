// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Managed markdown skill catalog. Mirrors `internal/agent/skill_catalog.go`.
//!
//! Skills are user-installed markdown files copied into a managed location
//! under `<data_dir>/skills/<id>/SKILL.md`. The catalog file at
//! `<data_dir>/skills/catalog.json` records id, display name, source path,
//! checksum, install timestamp, and the first-paragraph capability summary.

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use thiserror::Error;
use time::format_description::well_known::Rfc3339;
use time::OffsetDateTime;

const SKILL_CATALOG_PARSER_VERSION: &str = "markdown_v1";

/// One managed markdown skill in the local catalog.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct InstalledSkill {
    pub id: String,
    pub display_name: String,
    pub source_path: String,
    pub managed_path: String,
    pub checksum: String,
    pub installed_at: String,
    pub parser_version: String,
    pub capability_summary: String,
}

/// Persisted manifest for installed markdown skills.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SkillCatalog {
    #[serde(default)]
    pub skills: Vec<InstalledSkill>,
}

#[derive(Debug, Error)]
pub enum SkillCatalogError {
    #[error("read skill catalog: {0}")]
    Read(#[source] std::io::Error),
    #[error("parse skill catalog: {0}")]
    Parse(#[source] serde_json::Error),
    #[error("create skill catalog dir: {0}")]
    CreateDir(#[source] std::io::Error),
    #[error("write skill catalog: {0}")]
    Write(#[source] std::io::Error),
    #[error("marshal skill catalog: {0}")]
    Marshal(#[source] serde_json::Error),
    #[error("read skill source: {0}")]
    ReadSource(#[source] std::io::Error),
    #[error("write managed skill: {0}")]
    WriteManaged(#[source] std::io::Error),
    #[error("create managed skill dir: {0}")]
    CreateManagedDir(#[source] std::io::Error),
    #[error("remove managed skill: {0}")]
    RemoveManaged(#[source] std::io::Error),
    #[error("resolve skill source path: {0}")]
    Resolve(#[source] std::io::Error),
    #[error("clock failed to format installed_at: {0}")]
    Time(#[source] time::error::Format),
    #[error("skill {0:?} already exists with different content")]
    ChecksumConflict(String),
    #[error("skill {0:?} not found")]
    NotFound(String),
    #[error("skill {id:?} is still enabled in {path}; disable it first or use --force")]
    StillEnabled { id: String, path: PathBuf },
    #[error("project config: {0}")]
    Project(#[from] rillan_config::Error),
}

impl SkillCatalogError {
    /// True for the "still enabled" sentinel — the CLI uses this to suggest
    /// `--force` instead of dumping a stack trace.
    #[must_use]
    pub fn is_still_enabled(&self) -> bool {
        matches!(self, Self::StillEnabled { .. })
    }
}

/// Returns the managed manifest path: `<data_dir>/skills/catalog.json`.
#[must_use]
pub fn default_skill_catalog_path() -> PathBuf {
    rillan_config::default_data_dir()
        .join("skills")
        .join("catalog.json")
}

/// Returns the managed markdown path for `id`:
/// `<data_dir>/skills/<id>/SKILL.md`.
#[must_use]
pub fn default_managed_skill_path(id: &str) -> PathBuf {
    rillan_config::default_data_dir()
        .join("skills")
        .join(id)
        .join("SKILL.md")
}

/// Loads the persisted catalog from `<data_dir>/skills/catalog.json`. Missing
/// file yields an empty catalog.
pub fn load_skill_catalog() -> Result<SkillCatalog, SkillCatalogError> {
    load_skill_catalog_at(&default_skill_catalog_path())
}

fn load_skill_catalog_at(path: &Path) -> Result<SkillCatalog, SkillCatalogError> {
    match std::fs::read(path) {
        Ok(data) => {
            let mut catalog: SkillCatalog =
                serde_json::from_slice(&data).map_err(SkillCatalogError::Parse)?;
            catalog.skills.sort_by(|a, b| a.id.cmp(&b.id));
            Ok(catalog)
        }
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(SkillCatalog::default()),
        Err(err) => Err(SkillCatalogError::Read(err)),
    }
}

/// Saves `catalog` to `<data_dir>/skills/catalog.json`. Skills are sorted by id.
pub fn save_skill_catalog(catalog: SkillCatalog) -> Result<(), SkillCatalogError> {
    save_skill_catalog_at(&default_skill_catalog_path(), catalog)
}

fn save_skill_catalog_at(path: &Path, mut catalog: SkillCatalog) -> Result<(), SkillCatalogError> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::create_dir_all(parent).map_err(SkillCatalogError::CreateDir)?;
        }
    }
    catalog.skills.sort_by(|a, b| a.id.cmp(&b.id));
    let data = serde_json::to_vec_pretty(&catalog).map_err(SkillCatalogError::Marshal)?;
    std::fs::write(path, data).map_err(SkillCatalogError::Write)
}

/// Returns all installed skills sorted by id.
pub fn list_installed_skills() -> Result<Vec<InstalledSkill>, SkillCatalogError> {
    let mut catalog = load_skill_catalog()?;
    catalog.skills.sort_by(|a, b| a.id.cmp(&b.id));
    Ok(catalog.skills)
}

/// Returns one installed skill by id (after id-normalization).
pub fn get_installed_skill(id: &str) -> Result<InstalledSkill, SkillCatalogError> {
    let normalized = normalize_skill_id(id);
    let catalog = load_skill_catalog()?;
    catalog
        .skills
        .into_iter()
        .find(|skill| skill.id == normalized)
        .ok_or(SkillCatalogError::NotFound(normalized))
}

/// Copies a markdown skill into managed storage and records its catalog entry.
/// Returns the existing entry unchanged when the same source content has
/// already been installed.
pub fn install_skill(
    source_path: &str,
    now: OffsetDateTime,
) -> Result<InstalledSkill, SkillCatalogError> {
    install_skill_at(source_path, now, &default_skill_catalog_path(), &|id| {
        default_managed_skill_path(id)
    })
}

fn install_skill_at(
    source_path: &str,
    now: OffsetDateTime,
    catalog_path: &Path,
    managed_path_for: &dyn Fn(&str) -> PathBuf,
) -> Result<InstalledSkill, SkillCatalogError> {
    let trimmed = source_path.trim();
    let abs_source = std::path::Path::new(trimmed)
        .canonicalize()
        .map_err(SkillCatalogError::Resolve)?;
    let data = std::fs::read(&abs_source).map_err(SkillCatalogError::ReadSource)?;
    let content_str = String::from_utf8_lossy(&data).into_owned();
    let display_name = skill_display_name(&abs_source, &content_str);
    let id = normalize_skill_id(&display_name);
    let checksum = checksum_for_bytes(&data);
    let managed_path = managed_path_for(&id);

    let mut catalog = load_skill_catalog_at(catalog_path)?;
    if let Some(existing) = catalog.skills.iter().find(|skill| skill.id == id) {
        if existing.checksum == checksum {
            return Ok(existing.clone());
        }
        return Err(SkillCatalogError::ChecksumConflict(id));
    }

    if let Some(parent) = managed_path.parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::create_dir_all(parent).map_err(SkillCatalogError::CreateManagedDir)?;
        }
    }
    std::fs::write(&managed_path, &data).map_err(SkillCatalogError::WriteManaged)?;

    let installed_at = now.format(&Rfc3339).map_err(SkillCatalogError::Time)?;
    let skill = InstalledSkill {
        id,
        display_name,
        source_path: abs_source.to_string_lossy().to_string(),
        managed_path: managed_path.to_string_lossy().to_string(),
        checksum,
        installed_at,
        parser_version: SKILL_CATALOG_PARSER_VERSION.into(),
        capability_summary: skill_capability_summary(&content_str),
    };
    catalog.skills.push(skill.clone());
    save_skill_catalog_at(catalog_path, catalog)?;
    Ok(skill)
}

/// Removes a managed markdown skill. Without `force`, refuses to remove a
/// skill the current project still enables under `agent.skills.enabled`.
pub fn remove_skill(id: &str, force: bool) -> Result<InstalledSkill, SkillCatalogError> {
    let normalized = normalize_skill_id(id);
    if !force {
        ensure_skill_not_enabled_in_current_project(&normalized)?;
    }

    let catalog_path = default_skill_catalog_path();
    let mut catalog = load_skill_catalog_at(&catalog_path)?;
    let position = catalog
        .skills
        .iter()
        .position(|skill| skill.id == normalized);
    let Some(idx) = position else {
        return Err(SkillCatalogError::NotFound(normalized));
    };
    let removed = catalog.skills.remove(idx);
    save_skill_catalog_at(&catalog_path, catalog)?;
    if let Some(parent) = std::path::Path::new(&removed.managed_path).parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::remove_dir_all(parent).map_err(SkillCatalogError::RemoveManaged)?;
        }
    }
    Ok(removed)
}

fn ensure_skill_not_enabled_in_current_project(id: &str) -> Result<(), SkillCatalogError> {
    let project_path = rillan_config::resolve_project_config_path("");
    let project = match rillan_config::load_project(&project_path) {
        Ok(value) => value,
        Err(rillan_config::Error::Read(err)) if err.kind() == std::io::ErrorKind::NotFound => {
            return Ok(());
        }
        Err(err) => return Err(err.into()),
    };
    for enabled in &project.agent.skills.enabled {
        if normalize_skill_id(enabled) == id {
            return Err(SkillCatalogError::StillEnabled {
                id: id.to_string(),
                path: project_path,
            });
        }
    }
    Ok(())
}

fn skill_display_name(source_path: &Path, content: &str) -> String {
    for line in content.split('\n') {
        let trimmed = line.trim();
        if let Some(rest) = trimmed.strip_prefix("# ") {
            return rest.trim().to_string();
        }
    }
    let base = source_path
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("");
    if base.is_empty() {
        return "skill".to_string();
    }
    base.to_string()
}

fn skill_capability_summary(content: &str) -> String {
    for line in content.split('\n') {
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with('#') {
            continue;
        }
        if trimmed.len() > 240 {
            return trimmed.chars().take(240).collect();
        }
        return trimmed.to_string();
    }
    String::new()
}

/// Normalizes a free-form display name into a kebab-case id. Mirrors
/// `normalizeSkillID` byte-for-byte so existing catalogs round-trip.
#[must_use]
pub fn normalize_skill_id(value: &str) -> String {
    let lower = value.trim().to_lowercase();
    if lower.is_empty() {
        return "skill".to_string();
    }
    let mut out = String::with_capacity(lower.len());
    let mut last_dash = false;
    for ch in lower.chars() {
        if ch.is_ascii_alphanumeric() {
            out.push(ch);
            last_dash = false;
        } else if !last_dash {
            out.push('-');
            last_dash = true;
        }
    }
    let trimmed = out.trim_matches('-');
    if trimmed.is_empty() {
        "skill".to_string()
    } else {
        trimmed.to_string()
    }
}

fn checksum_for_bytes(data: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data);
    let digest = hasher.finalize();
    let mut out = String::with_capacity(digest.len() * 2);
    for byte in &digest {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn install_into(
        source: &Path,
        catalog_root: &Path,
        now: OffsetDateTime,
    ) -> Result<InstalledSkill, SkillCatalogError> {
        let catalog_path = catalog_root.join("catalog.json");
        let managed_for = catalog_root.to_path_buf();
        install_skill_at(
            source.to_str().unwrap(),
            now,
            &catalog_path,
            &move |id: &str| managed_for.join(id).join("SKILL.md"),
        )
    }

    #[test]
    fn install_copies_managed_markdown_and_catalog_entry() {
        let dir = tempfile::tempdir().unwrap();
        let catalog_root = dir.path().join("skills");
        std::fs::create_dir_all(&catalog_root).unwrap();
        let source = dir.path().join("go-dev.md");
        std::fs::write(&source, b"# Go Dev\n\nUse this skill for Go changes.\n").unwrap();
        let now = OffsetDateTime::from_unix_timestamp(1_711_733_400).unwrap();
        let skill = install_into(&source, &catalog_root, now).expect("install");
        assert_eq!(skill.id, "go-dev");
        assert!(std::path::Path::new(&skill.managed_path).exists());
        assert_eq!(skill.parser_version, "markdown_v1");
        assert!(skill.capability_summary.starts_with("Use this skill"));
    }

    #[test]
    fn install_is_idempotent_for_same_content() {
        let dir = tempfile::tempdir().unwrap();
        let catalog_root = dir.path().join("skills");
        std::fs::create_dir_all(&catalog_root).unwrap();
        let source = dir.path().join("repo-audit.md");
        std::fs::write(
            &source,
            b"# Repo Audit\n\nInspect repositories carefully.\n",
        )
        .unwrap();
        let now = OffsetDateTime::now_utc();
        let first = install_into(&source, &catalog_root, now).unwrap();
        let second = install_into(&source, &catalog_root, now).unwrap();
        assert_eq!(first.checksum, second.checksum);
        let catalog = load_skill_catalog_at(&catalog_root.join("catalog.json")).expect("load");
        assert_eq!(catalog.skills.len(), 1);
    }

    #[test]
    fn install_rejects_checksum_conflict() {
        let dir = tempfile::tempdir().unwrap();
        let catalog_root = dir.path().join("skills");
        std::fs::create_dir_all(&catalog_root).unwrap();
        let source_v1 = dir.path().join("go-dev.md");
        std::fs::write(&source_v1, b"# Go Dev\n\nv1.\n").unwrap();
        let now = OffsetDateTime::now_utc();
        install_into(&source_v1, &catalog_root, now).unwrap();
        let source_v2 = dir.path().join("go-dev-v2.md");
        std::fs::write(&source_v2, b"# Go Dev\n\nv2 content.\n").unwrap();
        let err = install_into(&source_v2, &catalog_root, now).expect_err("conflict");
        assert!(matches!(err, SkillCatalogError::ChecksumConflict(_)));
    }

    #[test]
    fn normalize_skill_id_kebab_cases_and_strips_edges() {
        assert_eq!(normalize_skill_id("Go Dev"), "go-dev");
        assert_eq!(normalize_skill_id("  Go--dev!! "), "go-dev");
        assert_eq!(normalize_skill_id(""), "skill");
        assert_eq!(normalize_skill_id("!!!"), "skill");
        assert_eq!(normalize_skill_id("Repo Audit 2"), "repo-audit-2");
    }

    #[test]
    fn skill_display_name_prefers_h1() {
        let name = skill_display_name(Path::new("/tmp/whatever.md"), "\n# My Skill\n\nbody");
        assert_eq!(name, "My Skill");
    }

    #[test]
    fn skill_display_name_falls_back_to_filename() {
        let name = skill_display_name(Path::new("/tmp/widget.md"), "body");
        assert_eq!(name, "widget");
    }

    #[test]
    fn skill_capability_summary_uses_first_paragraph() {
        let summary = skill_capability_summary("# Title\n\nfirst paragraph\nsecond");
        assert_eq!(summary, "first paragraph");
    }

    #[test]
    fn skill_capability_summary_truncates_long_lines() {
        let long = "x".repeat(500);
        let summary = skill_capability_summary(&long);
        assert_eq!(summary.len(), 240);
    }
}
