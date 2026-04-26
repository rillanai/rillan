// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Skill request/result shapes. Mirrors `internal/agent/skills/types.go`.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct FileContent {
    pub path: String,
    pub content: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ReadFilesRequest {
    #[serde(default)]
    pub repo_root: String,
    #[serde(default)]
    pub paths: Vec<String>,
    #[serde(default)]
    pub max_files: usize,
    #[serde(default)]
    pub max_chars_per_file: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ReadFilesResult {
    #[serde(default)]
    pub files: Vec<FileContent>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SearchRepoRequest {
    #[serde(default)]
    pub repo_root: String,
    #[serde(default)]
    pub query: String,
    #[serde(default)]
    pub max_matches: usize,
    #[serde(default)]
    pub max_snippet_chars: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct RepoMatch {
    pub path: String,
    pub snippet: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SearchRepoResult {
    #[serde(default)]
    pub matches: Vec<RepoMatch>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct IndexLookupRequest {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub db_path: String,
    #[serde(default)]
    pub query: String,
    #[serde(default)]
    pub max_matches: usize,
    #[serde(default)]
    pub max_snippet_chars: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct IndexMatch {
    pub path: String,
    #[serde(rename = "ref")]
    pub ref_: String,
    pub snippet: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct IndexLookupResult {
    #[serde(default)]
    pub matches: Vec<IndexMatch>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct GitStatusRequest {
    #[serde(default)]
    pub repo_root: String,
    #[serde(default)]
    pub max_entries: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct GitStatusResult {
    #[serde(default)]
    pub entries: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct GitDiffRequest {
    #[serde(default)]
    pub repo_root: String,
    #[serde(default)]
    pub max_chars: usize,
    #[serde(default)]
    pub staged_only: bool,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct GitDiffResult {
    #[serde(default)]
    pub diff: String,
}
