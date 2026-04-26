// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! MCP snapshot shapes. Mirrors `internal/agent/mcp_snapshot.go`.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct McpSnapshot {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub open_files: Vec<McpFileRef>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub selection: Option<McpSelection>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub diagnostics: Vec<McpDiagnostic>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub vcs: Option<McpVcsContext>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct McpFileRef {
    pub path: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct McpSelection {
    pub path: String,
    pub snippet: String,
    pub start: i64,
    pub end: i64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct McpDiagnostic {
    pub path: String,
    pub severity: String,
    pub message: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct McpVcsContext {
    pub branch: String,
    pub head: String,
    pub dirty: bool,
}

#[derive(Debug, Clone, Copy, Default)]
pub struct McpSnapshotOptions {
    pub max_open_files: usize,
    pub max_diagnostics: usize,
    pub max_chars: usize,
}
