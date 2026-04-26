// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! MCP snapshot normalizer. Mirrors `internal/agent/mcp_snapshot_builder.go`.

use crate::context_budget::trim_text;
use crate::mcp_snapshot::{McpSnapshot, McpSnapshotOptions};

/// Truncates `snapshot` to the limits in `opts`. Empty/zero limits fall back
/// to the same defaults the Go implementation uses.
#[must_use]
pub fn normalize_mcp_snapshot(mut snapshot: McpSnapshot, opts: McpSnapshotOptions) -> McpSnapshot {
    let max_files = if opts.max_open_files == 0 {
        8
    } else {
        opts.max_open_files
    };
    let max_diagnostics = if opts.max_diagnostics == 0 {
        20
    } else {
        opts.max_diagnostics
    };
    let max_chars = if opts.max_chars == 0 {
        240
    } else {
        opts.max_chars
    };

    if snapshot.open_files.len() > max_files {
        snapshot.open_files.truncate(max_files);
    }
    for file in &mut snapshot.open_files {
        file.path = trim_text(std::mem::take(&mut file.path), max_chars);
    }
    if let Some(selection) = &mut snapshot.selection {
        selection.path = trim_text(std::mem::take(&mut selection.path), max_chars);
        selection.snippet = trim_text(std::mem::take(&mut selection.snippet), max_chars);
    }
    if snapshot.diagnostics.len() > max_diagnostics {
        snapshot.diagnostics.truncate(max_diagnostics);
    }
    for diag in &mut snapshot.diagnostics {
        diag.path = trim_text(std::mem::take(&mut diag.path), max_chars);
        diag.severity = trim_text(std::mem::take(&mut diag.severity), max_chars);
        diag.message = trim_text(std::mem::take(&mut diag.message), max_chars);
    }
    if let Some(vcs) = &mut snapshot.vcs {
        vcs.branch = trim_text(std::mem::take(&mut vcs.branch), max_chars);
        vcs.head = trim_text(std::mem::take(&mut vcs.head), max_chars);
    }
    snapshot
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::mcp_snapshot::{McpDiagnostic, McpFileRef};

    #[test]
    fn truncates_open_files_and_diagnostics() {
        let snapshot = McpSnapshot {
            open_files: (0..15)
                .map(|i| McpFileRef {
                    path: format!("file{i}"),
                })
                .collect(),
            diagnostics: (0..30)
                .map(|i| McpDiagnostic {
                    path: format!("p{i}"),
                    severity: "warn".into(),
                    message: "boom".into(),
                })
                .collect(),
            ..McpSnapshot::default()
        };
        let normalized = normalize_mcp_snapshot(snapshot, McpSnapshotOptions::default());
        assert_eq!(normalized.open_files.len(), 8);
        assert_eq!(normalized.diagnostics.len(), 20);
    }
}
