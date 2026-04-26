// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Read-only tool dispatcher. Mirrors
//! `internal/agent/skills/tool_runtime.go`. Maps tool-name strings to the
//! corresponding [`super::Registry`] method and returns a serializable
//! payload.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;

use super::registry::Registry;
use super::types::{
    GitDiffRequest, GitStatusRequest, IndexLookupRequest, ReadFilesRequest, SearchRepoRequest,
};
use super::SkillError;

pub const TOOL_NAME_READ_FILES: &str = "read_files";
pub const TOOL_NAME_SEARCH_REPO: &str = "search_repo";
pub const TOOL_NAME_INDEX_LOOKUP: &str = "index_lookup";
pub const TOOL_NAME_GIT_STATUS: &str = "git_status";
pub const TOOL_NAME_GIT_DIFF: &str = "git_diff";

/// Sentinel const used by the runner to silently skip unknown invocations.
pub const UNKNOWN_READ_ONLY_TOOL: &str = "unknown read-only tool";

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ReadOnlyTool {
    pub name: String,
    pub description: String,
}

#[derive(Debug, Clone, Default)]
pub struct ExecuteRequest {
    pub name: String,
    pub repo_root: String,
    pub paths: Vec<String>,
    pub query: String,
    pub db_path: String,
    pub staged_only: bool,
}

#[derive(Debug, Clone)]
pub struct ExecuteResult {
    pub name: String,
    pub payload: Value,
}

#[must_use]
pub fn list_read_only_tools() -> Vec<ReadOnlyTool> {
    vec![
        ReadOnlyTool {
            name: TOOL_NAME_GIT_DIFF.into(),
            description: "Return a bounded git diff".into(),
        },
        ReadOnlyTool {
            name: TOOL_NAME_GIT_STATUS.into(),
            description: "Return bounded git status entries".into(),
        },
        ReadOnlyTool {
            name: TOOL_NAME_INDEX_LOOKUP.into(),
            description: "Query the local index for bounded matches".into(),
        },
        ReadOnlyTool {
            name: TOOL_NAME_READ_FILES.into(),
            description: "Read bounded file contents from the repo".into(),
        },
        ReadOnlyTool {
            name: TOOL_NAME_SEARCH_REPO.into(),
            description: "Search the repo for bounded text matches".into(),
        },
    ]
}

#[derive(Debug, Error)]
pub enum DispatchError {
    #[error("unknown read-only tool {0:?}")]
    Unknown(String),
    #[error("skill: {0}")]
    Skill(#[from] SkillError),
    #[error("encode payload: {0}")]
    Encode(#[from] serde_json::Error),
}

impl DispatchError {
    /// True for the "unknown tool" sentinel — the runner uses this to skip
    /// invocations silently.
    #[must_use]
    pub fn is_unknown(&self) -> bool {
        matches!(self, Self::Unknown(_))
    }

    /// True when the underlying skill rejected an unapproved repo root.
    #[must_use]
    pub fn is_unapproved(&self) -> bool {
        matches!(self, Self::Skill(err) if err.is_unapproved())
    }
}

impl Registry {
    /// Dispatches a read-only tool by name.
    pub async fn execute(&self, req: ExecuteRequest) -> Result<ExecuteResult, DispatchError> {
        let payload = match req.name.as_str() {
            TOOL_NAME_READ_FILES => {
                let res = self
                    .read_files(ReadFilesRequest {
                        repo_root: req.repo_root,
                        paths: req.paths,
                        ..ReadFilesRequest::default()
                    })
                    .await?;
                serde_json::to_value(res)?
            }
            TOOL_NAME_SEARCH_REPO => {
                let res = self
                    .search_repo(SearchRepoRequest {
                        repo_root: req.repo_root,
                        query: req.query,
                        ..SearchRepoRequest::default()
                    })
                    .await?;
                serde_json::to_value(res)?
            }
            TOOL_NAME_INDEX_LOOKUP => {
                let res = self
                    .index_lookup(IndexLookupRequest {
                        db_path: req.db_path,
                        query: req.query,
                        ..IndexLookupRequest::default()
                    })
                    .await?;
                serde_json::to_value(res)?
            }
            TOOL_NAME_GIT_STATUS => {
                let res = self
                    .git_status(GitStatusRequest {
                        repo_root: req.repo_root,
                        ..GitStatusRequest::default()
                    })
                    .await?;
                serde_json::to_value(res)?
            }
            TOOL_NAME_GIT_DIFF => {
                let res = self
                    .git_diff(GitDiffRequest {
                        repo_root: req.repo_root,
                        staged_only: req.staged_only,
                        ..GitDiffRequest::default()
                    })
                    .await?;
                serde_json::to_value(res)?
            }
            other => return Err(DispatchError::Unknown(other.to_string())),
        };
        Ok(ExecuteResult {
            name: req.name,
            payload,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn list_read_only_tools_is_sorted_alphabetically() {
        let tools = list_read_only_tools();
        let names: Vec<&str> = tools.iter().map(|t| t.name.as_str()).collect();
        let mut sorted = names.clone();
        sorted.sort_unstable();
        assert_eq!(names, sorted);
    }

    #[tokio::test]
    async fn execute_dispatches_read_files_payload() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path();
        std::fs::create_dir_all(repo.join("docs")).unwrap();
        std::fs::write(repo.join("docs/guide.md"), "hello world from file").unwrap();
        let registry = Registry::new(vec![repo.to_string_lossy().to_string()]);
        let result = registry
            .execute(ExecuteRequest {
                name: TOOL_NAME_READ_FILES.into(),
                repo_root: repo.to_string_lossy().to_string(),
                paths: vec!["docs/guide.md".into()],
                ..ExecuteRequest::default()
            })
            .await
            .expect("dispatch");
        assert_eq!(result.name, TOOL_NAME_READ_FILES);
        let files = result
            .payload
            .get("files")
            .and_then(Value::as_array)
            .unwrap();
        assert_eq!(files.len(), 1);
    }

    #[tokio::test]
    async fn execute_unknown_returns_unknown_error() {
        let registry = Registry::new(Vec::new());
        let err = registry
            .execute(ExecuteRequest {
                name: "nope".into(),
                ..ExecuteRequest::default()
            })
            .await
            .expect_err("must fail");
        assert!(err.is_unknown());
    }
}
