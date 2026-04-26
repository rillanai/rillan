// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Build metadata exposed to the CLI and HTTP layer.
//!
//! Mirrors `internal/version` from the upstream Go repo. Values default to
//! `"dev"` and may be overridden at build time through the `RILLAN_VERSION`,
//! `RILLAN_COMMIT`, and `RILLAN_DATE` environment variables (typically set by
//! the release CI).

/// Semantic version string. Set at build time via `RILLAN_VERSION` or defaults
/// to `"dev"`.
pub const VERSION: &str = match option_env!("RILLAN_VERSION") {
    Some(value) => value,
    None => "dev",
};

/// Source commit the binary was built from. Empty when unknown.
pub const COMMIT: &str = match option_env!("RILLAN_COMMIT") {
    Some(value) => value,
    None => "",
};

/// Build date. Empty when unknown.
pub const DATE: &str = match option_env!("RILLAN_DATE") {
    Some(value) => value,
    None => "",
};

/// Returns the human-readable version string. Includes the commit when known.
#[must_use]
pub fn string() -> String {
    if COMMIT.is_empty() {
        VERSION.to_string()
    } else {
        format!("{VERSION} ({COMMIT})")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn version_string_falls_back_to_version_when_commit_unknown() {
        // Default build — COMMIT is empty, so the rendered string is the
        // version alone.
        if COMMIT.is_empty() {
            assert_eq!(string(), VERSION);
        }
    }

    #[test]
    fn version_default_is_non_empty() {
        // The defaults guarantee `VERSION` is always renderable, even before
        // the release build sets `RILLAN_VERSION`.
        assert!(!VERSION.is_empty());
    }
}
