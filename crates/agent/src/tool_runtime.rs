// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Read-only tool runtime. Mirrors `internal/agent/tool_runtime.go` including
//! the `ListInstalledSkills`-backed `passive_context` layer.

use std::path::PathBuf;
use std::sync::Arc;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;

use crate::skill_catalog::{list_installed_skills, InstalledSkill, SkillCatalogError};
use crate::skills::{DispatchError, ExecuteRequest, Registry};

/// Tool kind. The PassiveContext variant comes first deliberately so the
/// derived `Ord` matches the Go `tool_runtime.ListTools` sort order
/// (`passive_context` < `read_only_action`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ToolKind {
    PassiveContext,
    ReadOnlyAction,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ToolDefinition {
    pub name: String,
    pub kind: ToolKind,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub content: String,
}

/// Inputs to [`ToolExecutor::execute_tool`]. Mirrors the Go `ToolCall` struct.
#[derive(Debug, Clone, Default)]
pub struct ToolCall {
    pub name: String,
    pub repo_root: String,
    pub paths: Vec<String>,
    pub query: String,
    pub db_path: String,
    pub staged_only: bool,
}

/// Encoded result of a tool dispatch.
#[derive(Debug, Clone, PartialEq)]
pub struct ToolExecutionResult {
    pub name: String,
    pub payload: Value,
}

/// Async trait implemented by every tool source.
#[async_trait]
pub trait ToolExecutor: Send + Sync {
    async fn execute_tool(&self, call: ToolCall) -> Result<ToolExecutionResult, ToolError>;
}

/// Async trait for surfaces that enumerate available tools.
#[async_trait]
pub trait ToolSource: Send + Sync {
    async fn list_tools(&self) -> Result<Vec<ToolDefinition>, ToolError>;
}

#[derive(Debug, Error)]
pub enum ToolError {
    #[error("unknown read-only tool {0:?}")]
    Unknown(String),
    #[error("dispatch: {0}")]
    Dispatch(DispatchError),
    #[error("list installed skills: {0}")]
    SkillCatalog(#[from] SkillCatalogError),
    #[error("read managed skill {id:?}: {source}")]
    ReadManagedSkill {
        id: String,
        #[source]
        source: std::io::Error,
    },
}

impl ToolError {
    #[must_use]
    pub fn is_unknown(&self) -> bool {
        matches!(self, Self::Unknown(_))
    }

    #[must_use]
    pub fn is_unapproved(&self) -> bool {
        matches!(self, Self::Dispatch(err) if err.is_unapproved())
    }
}

impl From<DispatchError> for ToolError {
    fn from(value: DispatchError) -> Self {
        if let DispatchError::Unknown(name) = &value {
            return Self::Unknown(name.clone());
        }
        Self::Dispatch(value)
    }
}

/// Loader that surfaces installed markdown skills to `list_tools`. Pluggable
/// so tests don't have to touch the user's data directory.
pub type SkillsLoader =
    Arc<dyn Fn() -> Result<Vec<InstalledSkill>, SkillCatalogError> + Send + Sync>;

/// Loader that reads a managed-skill markdown file. Pluggable for the same
/// reason as [`SkillsLoader`].
pub type SkillReader = Arc<dyn Fn(&PathBuf) -> Result<Vec<u8>, std::io::Error> + Send + Sync>;

/// Read-only tool runtime backed by [`Registry`] + the skill catalog.
pub struct ReadOnlyToolRuntime {
    registry: Registry,
    list_installed: SkillsLoader,
    read_file: SkillReader,
}

impl ReadOnlyToolRuntime {
    /// Builds the runtime restricted to `approved_repo_roots`. The default
    /// skill loader reads from `<data_dir>/skills/catalog.json`; override it
    /// via [`with_skill_loader`].
    #[must_use]
    pub fn new(approved_repo_roots: Vec<String>) -> Self {
        Self {
            registry: Registry::new(approved_repo_roots),
            list_installed: Arc::new(list_installed_skills),
            read_file: Arc::new(|path: &PathBuf| std::fs::read(path)),
        }
    }

    #[must_use]
    pub fn registry(&self) -> &Registry {
        &self.registry
    }

    /// Replaces the skill-catalog loader. Used by tests.
    #[must_use]
    pub fn with_skill_loader(mut self, loader: SkillsLoader) -> Self {
        self.list_installed = loader;
        self
    }

    /// Replaces the markdown-file reader. Used by tests.
    #[must_use]
    pub fn with_skill_reader(mut self, reader: SkillReader) -> Self {
        self.read_file = reader;
        self
    }

    /// Returns an `Arc<dyn ToolExecutor>` for handing the runtime to the
    /// agent runner.
    #[must_use]
    pub fn into_executor(self: Arc<Self>) -> Arc<dyn ToolExecutor> {
        self
    }
}

impl std::fmt::Debug for ReadOnlyToolRuntime {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ReadOnlyToolRuntime")
            .field("approved_repo_roots", &self.registry.approved_repo_roots())
            .finish()
    }
}

#[async_trait]
impl ToolSource for ReadOnlyToolRuntime {
    async fn list_tools(&self) -> Result<Vec<ToolDefinition>, ToolError> {
        let mut tools: Vec<ToolDefinition> = crate::skills::list_read_only_tools()
            .into_iter()
            .map(|tool| ToolDefinition {
                name: tool.name,
                kind: ToolKind::ReadOnlyAction,
                description: tool.description,
                content: String::new(),
            })
            .collect();

        let installed = (self.list_installed)()?;
        for skill in installed {
            let path = PathBuf::from(&skill.managed_path);
            let bytes = (self.read_file)(&path).map_err(|source| ToolError::ReadManagedSkill {
                id: skill.id.clone(),
                source,
            })?;
            let content = String::from_utf8_lossy(&bytes).trim().to_string();
            tools.push(ToolDefinition {
                name: skill.id,
                kind: ToolKind::PassiveContext,
                description: skill.capability_summary,
                content,
            });
        }
        tools.sort_by(|a, b| a.kind.cmp(&b.kind).then_with(|| a.name.cmp(&b.name)));
        Ok(tools)
    }
}

#[async_trait]
impl ToolExecutor for ReadOnlyToolRuntime {
    async fn execute_tool(&self, call: ToolCall) -> Result<ToolExecutionResult, ToolError> {
        let result = self
            .registry
            .execute(ExecuteRequest {
                name: call.name.clone(),
                repo_root: call.repo_root,
                paths: call.paths,
                query: call.query,
                db_path: call.db_path,
                staged_only: call.staged_only,
            })
            .await?;
        Ok(ToolExecutionResult {
            name: result.name,
            payload: result.payload,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn empty_loader() -> SkillsLoader {
        Arc::new(|| Ok(Vec::new()))
    }

    fn loader_with(skills: Vec<InstalledSkill>) -> SkillsLoader {
        Arc::new(move || Ok(skills.clone()))
    }

    fn reader_returning(content: &'static [u8]) -> SkillReader {
        Arc::new(move |_path: &PathBuf| Ok(content.to_vec()))
    }

    #[tokio::test]
    async fn list_tools_emits_passive_context_first() {
        let skill = InstalledSkill {
            id: "go-dev".into(),
            display_name: "Go Dev".into(),
            source_path: "/tmp/go-dev.md".into(),
            managed_path: "/tmp/managed/go-dev/SKILL.md".into(),
            checksum: "abc".into(),
            installed_at: "2026-01-01T00:00:00Z".into(),
            parser_version: "markdown_v1".into(),
            capability_summary: "Use this skill for Go changes.".into(),
        };
        let runtime = ReadOnlyToolRuntime::new(Vec::new())
            .with_skill_loader(loader_with(vec![skill]))
            .with_skill_reader(reader_returning(b"# Go Dev\n\nbody.\n"));
        let tools = runtime.list_tools().await.expect("list");
        // PassiveContext sorts before ReadOnlyAction.
        assert_eq!(tools[0].kind, ToolKind::PassiveContext);
        assert_eq!(tools[0].name, "go-dev");
        assert_eq!(tools[0].content, "# Go Dev\n\nbody.");
        assert_eq!(tools[0].description, "Use this skill for Go changes.");
        // Read-only actions follow alphabetically.
        let read_only_names: Vec<_> = tools
            .iter()
            .filter(|t| t.kind == ToolKind::ReadOnlyAction)
            .map(|t| t.name.as_str())
            .collect();
        assert_eq!(
            read_only_names,
            vec![
                "git_diff",
                "git_status",
                "index_lookup",
                "read_files",
                "search_repo",
            ],
        );
    }

    #[tokio::test]
    async fn list_tools_passes_through_when_no_skills() {
        let runtime = ReadOnlyToolRuntime::new(Vec::new()).with_skill_loader(empty_loader());
        let tools = runtime.list_tools().await.expect("list");
        assert!(tools.iter().all(|t| t.kind == ToolKind::ReadOnlyAction));
    }

    #[tokio::test]
    async fn list_tools_propagates_read_errors() {
        let skill = InstalledSkill {
            id: "broken".into(),
            display_name: "Broken".into(),
            source_path: String::new(),
            managed_path: "/tmp/missing/SKILL.md".into(),
            checksum: String::new(),
            installed_at: String::new(),
            parser_version: "markdown_v1".into(),
            capability_summary: String::new(),
        };
        let runtime = ReadOnlyToolRuntime::new(Vec::new())
            .with_skill_loader(loader_with(vec![skill]))
            .with_skill_reader(Arc::new(|_| {
                Err(std::io::Error::new(std::io::ErrorKind::NotFound, "nope"))
            }));
        let err = runtime.list_tools().await.expect_err("must fail");
        assert!(matches!(err, ToolError::ReadManagedSkill { .. }));
    }
}
