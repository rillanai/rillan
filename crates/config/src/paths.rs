// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Per-platform path resolution. Mirrors the Go repo's `Default*Path` /
//! `Default*Dir` helpers using the `dirs` crate.

use std::path::{Path, PathBuf};

/// Returns the default runtime config path: `$USER_CONFIG_DIR/rillan/config.yaml`.
#[must_use]
pub fn default_config_path() -> PathBuf {
    match dirs::config_dir() {
        Some(base) => base.join("rillan").join("config.yaml"),
        None => PathBuf::from("rillan.yaml"),
    }
}

/// Returns the default repo-local project config path under `root`. When
/// `root` is empty, anchors against the current working directory.
#[must_use]
pub fn default_project_config_path(root: &str) -> PathBuf {
    let base = resolve_base(root);
    base.join(".rillan").join("project.yaml")
}

/// Legacy `.sidekick/project.yaml` location, retained so existing repos keep
/// loading. Mirrors `LegacyProjectConfigPath` in the Go repo.
#[must_use]
pub fn legacy_project_config_path(root: &str) -> PathBuf {
    let base = resolve_base(root);
    base.join(".sidekick").join("project.yaml")
}

/// Returns the project config path that exists on disk if any, otherwise the
/// preferred path.
#[must_use]
pub fn resolve_project_config_path(root: &str) -> PathBuf {
    let preferred = default_project_config_path(root);
    if preferred.exists() {
        return preferred;
    }
    let legacy = legacy_project_config_path(root);
    if legacy.exists() {
        return legacy;
    }
    preferred
}

/// Returns `~/.rillan/system.yaml`.
#[must_use]
pub fn default_system_config_path() -> PathBuf {
    match dirs::home_dir() {
        Some(home) => home.join(".rillan").join("system.yaml"),
        None => PathBuf::from(".rillan").join("system.yaml"),
    }
}

/// Legacy `~/.sidekick/system.yaml` location.
#[must_use]
pub fn legacy_system_config_path() -> PathBuf {
    match dirs::home_dir() {
        Some(home) => home.join(".sidekick").join("system.yaml"),
        None => PathBuf::from(".sidekick").join("system.yaml"),
    }
}

/// Returns the system config path that exists on disk if any, otherwise the
/// preferred path.
#[must_use]
pub fn resolve_system_config_path() -> PathBuf {
    let preferred = default_system_config_path();
    if preferred.exists() {
        return preferred;
    }
    let legacy = legacy_system_config_path();
    if legacy.exists() {
        return legacy;
    }
    preferred
}

/// Returns the runtime data directory.
///
/// macOS: `~/Library/Application Support/rillan/data`.
/// Linux: `$XDG_DATA_HOME/rillan` or `~/.local/share/rillan`.
#[must_use]
pub fn default_data_dir() -> PathBuf {
    if cfg!(target_os = "macos") {
        if let Some(home) = dirs::home_dir() {
            return home
                .join("Library")
                .join("Application Support")
                .join("rillan")
                .join("data");
        }
    }
    match dirs::data_dir() {
        Some(base) => base.join("rillan"),
        None => PathBuf::from(".").join(".rillan"),
    }
}

/// Returns the log directory.
///
/// macOS: `~/Library/Logs/rillan`.
/// Linux: `$XDG_STATE_HOME/rillan/logs` or `~/.local/state/rillan/logs`.
#[must_use]
pub fn default_log_dir() -> PathBuf {
    if cfg!(target_os = "macos") {
        if let Some(home) = dirs::home_dir() {
            return home.join("Library").join("Logs").join("rillan");
        }
    }
    match dirs::state_dir() {
        Some(base) => base.join("rillan").join("logs"),
        None => match dirs::home_dir() {
            Some(home) => home
                .join(".local")
                .join("state")
                .join("rillan")
                .join("logs"),
            None => PathBuf::from(".").join(".rillan").join("logs"),
        },
    }
}

fn resolve_base(root: &str) -> PathBuf {
    let trimmed = root.trim();
    if trimmed.is_empty() {
        return std::env::current_dir().unwrap_or_else(|_| PathBuf::from("."));
    }
    let path = Path::new(trimmed);
    path.canonicalize().unwrap_or_else(|_| path.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_config_path_ends_with_rillan_config() {
        let path = default_config_path();
        let display = path.to_string_lossy();
        assert!(display.ends_with("config.yaml"), "{display}");
    }

    #[test]
    fn project_config_path_uses_root_when_provided() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = default_project_config_path(tmp.path().to_str().unwrap());
        assert!(
            path.starts_with(tmp.path()),
            "expected {} to be under {}",
            path.display(),
            tmp.path().display(),
        );
        assert!(path.ends_with(".rillan/project.yaml"));
    }
}
