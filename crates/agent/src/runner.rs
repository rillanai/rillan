// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Role runner. Mirrors `internal/agent/runner.go`.
//!
//! Runs the requested role profile against a context package: the
//! orchestrator role surfaces a routing decision; every role executes the
//! attached skill invocations against a shared read-only tool runtime; the
//! returned result includes a budget-applied echo of the input package.

use std::sync::Arc;

use async_trait::async_trait;
use serde::Serialize;
use serde_json::Value;
use thiserror::Error;
use time::OffsetDateTime;

use crate::context_budget::apply_budget;
use crate::context_package::{ContextPackage, SkillInvocation, SkillResult};
use crate::orchestrator::decide_execution_mode;
use crate::roles::{OrchestrationDecision, Role, RoleProfile};
use crate::skill_metrics::record_skill_latency;
use crate::tool_runtime::{ReadOnlyToolRuntime, ToolCall, ToolError, ToolExecutor};

/// Output of one role run.
#[derive(Debug, Clone, Serialize)]
pub struct RunResult {
    pub role: Role,
    pub summary: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub decision: Option<OrchestrationDecision>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub skill_results: Vec<SkillResult>,
    pub context_echo: ContextPackage,
}

#[derive(Debug, Error)]
pub enum RunnerError {
    #[error("tool: {0}")]
    Tool(#[from] ToolError),
}

/// Trait implemented by the in-process role runner.
#[async_trait]
pub trait Runner: Send + Sync {
    async fn run(
        &self,
        profile: &RoleProfile,
        package: ContextPackage,
    ) -> Result<RunResult, RunnerError>;
}

/// Default role runner. Holds a single read-only tool runtime that is reused
/// across roles for a single agent task.
pub struct SharedRunner {
    tools: Arc<dyn ToolExecutor>,
}

impl SharedRunner {
    /// Builds a runner with a freshly-initialized read-only tool runtime.
    #[must_use]
    pub fn new(approved_repo_roots: Vec<String>) -> Self {
        let runtime = Arc::new(ReadOnlyToolRuntime::new(approved_repo_roots));
        Self {
            tools: runtime.into_executor(),
        }
    }

    /// Builds a runner with an existing executor (used by tests).
    #[must_use]
    pub fn with_executor(tools: Arc<dyn ToolExecutor>) -> Self {
        Self { tools }
    }

    async fn run_skill_invocations(
        &self,
        invocations: &[SkillInvocation],
    ) -> Result<Vec<SkillResult>, RunnerError> {
        let mut results: Vec<SkillResult> = Vec::with_capacity(invocations.len());
        for invocation in invocations {
            if let Some(result) = self.run_skill_invocation(invocation).await? {
                results.push(result);
            }
        }
        Ok(results)
    }

    async fn run_skill_invocation(
        &self,
        invocation: &SkillInvocation,
    ) -> Result<Option<SkillResult>, RunnerError> {
        let Some(kind) = invocation.kind else {
            return Ok(None);
        };
        let started = std::time::Instant::now();
        let call = ToolCall {
            name: kind.as_str().to_string(),
            repo_root: invocation.repo_root.clone(),
            paths: invocation.paths.clone(),
            query: invocation.query.clone(),
            db_path: invocation.db_path.clone(),
            staged_only: invocation.staged_only,
        };
        let result = match self.tools.execute_tool(call).await {
            Ok(value) => value,
            Err(err) if err.is_unknown() => return Ok(None),
            Err(err) => return Err(err.into()),
        };
        // Record latency best-effort. Mirrors Go's `_ = RecordSkillLatency`.
        let _ = record_skill_latency(kind.as_str(), started.elapsed(), OffsetDateTime::now_utc());
        Ok(Some(SkillResult {
            kind: Some(kind),
            payload: result.payload,
        }))
    }
}

impl std::fmt::Debug for SharedRunner {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SharedRunner").finish_non_exhaustive()
    }
}

#[async_trait]
impl Runner for SharedRunner {
    async fn run(
        &self,
        profile: &RoleProfile,
        package: ContextPackage,
    ) -> Result<RunResult, RunnerError> {
        let context_echo = apply_budget(package);
        let decision = if profile.role == Role::Orchestrator {
            Some(decide_execution_mode(&context_echo))
        } else {
            None
        };
        let invocations = context_echo.skill_invocations.clone();
        let skill_results = self.run_skill_invocations(&invocations).await?;
        Ok(RunResult {
            role: profile.role,
            summary: profile.description.clone(),
            decision,
            skill_results,
            context_echo,
        })
    }
}

/// Convenience: returns a JSON-serializable shape of the run result.
#[must_use]
pub fn run_result_to_value(result: &RunResult) -> Value {
    serde_json::to_value(result).unwrap_or(Value::Null)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::context_package::{BudgetSection, FactItem, SkillKind, TaskSection};
    use crate::roles::{default_role_profiles, ExecutionModeWire};
    use std::collections::BTreeMap;

    fn write_fixture(repo: &std::path::Path, rel: &str, content: &str) {
        let path = repo.join(rel);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, content).unwrap();
    }

    #[tokio::test]
    async fn runner_reuses_one_runtime_across_roles() {
        let runner = SharedRunner::new(Vec::new());
        let profiles = default_role_profiles();
        let pkg = ContextPackage {
            task: TaskSection {
                goal: "review repo".into(),
                execution_mode: "plan_first".into(),
                ..TaskSection::default()
            },
            budget: BudgetSection {
                max_evidence_items: 2,
                max_facts: 2,
                max_open_questions: 2,
                max_working_memory_items: 2,
                max_item_chars: 80,
            },
            ..ContextPackage::default()
        };
        for role in [
            Role::Orchestrator,
            Role::Planner,
            Role::Researcher,
            Role::Coder,
            Role::Reviewer,
        ] {
            let profile = profiles.get(&role).unwrap();
            let result = runner.run(profile, pkg.clone()).await.expect("run");
            assert_eq!(result.role, role);
        }
    }

    #[tokio::test]
    async fn runner_executes_requested_read_only_skills() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        write_fixture(&repo, "docs/guide.md", "agent skills can read repo files");
        let runner = SharedRunner::new(vec![repo.to_string_lossy().to_string()]);
        let profiles = default_role_profiles();
        let pkg = ContextPackage {
            task: TaskSection {
                goal: "inspect repo".into(),
                execution_mode: "direct".into(),
                ..TaskSection::default()
            },
            skill_invocations: vec![SkillInvocation {
                kind: Some(SkillKind::ReadFiles),
                repo_root: repo.to_string_lossy().to_string(),
                paths: vec!["docs/guide.md".into()],
                ..SkillInvocation::default()
            }],
            budget: BudgetSection {
                max_evidence_items: 2,
                max_facts: 2,
                max_open_questions: 2,
                max_working_memory_items: 2,
                max_item_chars: 120,
            },
            ..ContextPackage::default()
        };
        let researcher = profiles.get(&Role::Researcher).unwrap();
        let result = runner.run(researcher, pkg).await.expect("run");
        assert_eq!(result.skill_results.len(), 1);
        let payload = &result.skill_results[0].payload;
        let files = payload.get("files").and_then(Value::as_array).unwrap();
        assert_eq!(files.len(), 1);
    }

    #[tokio::test]
    async fn runner_applies_budget_before_returning_context_echo() {
        let runner = SharedRunner::new(Vec::new());
        let profiles = default_role_profiles();
        let pkg = ContextPackage {
            task: TaskSection {
                goal: "review repo".into(),
                execution_mode: "direct".into(),
                ..TaskSection::default()
            },
            facts: vec![
                FactItem {
                    key: "branch".into(),
                    value: "main".into(),
                },
                FactItem {
                    key: "drop".into(),
                    value: "me".into(),
                },
            ],
            budget: BudgetSection {
                max_evidence_items: 1,
                max_facts: 1,
                max_open_questions: 1,
                max_working_memory_items: 1,
                max_item_chars: 80,
            },
            ..ContextPackage::default()
        };
        let researcher = profiles.get(&Role::Researcher).unwrap();
        let result = runner.run(researcher, pkg).await.expect("run");
        assert_eq!(result.context_echo.facts.len(), 1);
    }

    #[tokio::test]
    async fn runner_rejects_unapproved_repo_roots() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        write_fixture(&repo, "docs/guide.md", "secret");
        let runner = SharedRunner::new(Vec::new());
        let profiles = default_role_profiles();
        let pkg = ContextPackage {
            task: TaskSection {
                goal: "inspect repo".into(),
                execution_mode: "direct".into(),
                ..TaskSection::default()
            },
            skill_invocations: vec![SkillInvocation {
                kind: Some(SkillKind::ReadFiles),
                repo_root: repo.to_string_lossy().to_string(),
                paths: vec!["docs/guide.md".into()],
                ..SkillInvocation::default()
            }],
            budget: BudgetSection {
                max_evidence_items: 2,
                max_facts: 2,
                max_open_questions: 2,
                max_working_memory_items: 2,
                max_item_chars: 120,
            },
            ..ContextPackage::default()
        };
        let researcher = profiles.get(&Role::Researcher).unwrap();
        let err = runner.run(researcher, pkg).await.expect_err("unapproved");
        match err {
            RunnerError::Tool(tool_err) => assert!(tool_err.is_unapproved()),
        }
    }

    #[tokio::test]
    async fn runner_orchestrator_returns_decision() {
        let runner = SharedRunner::new(Vec::new());
        let profiles = default_role_profiles();
        let pkg = ContextPackage {
            task: TaskSection {
                goal: "build something".into(),
                execution_mode: "plan_first".into(),
                ..TaskSection::default()
            },
            budget: BudgetSection {
                max_evidence_items: 1,
                max_facts: 1,
                max_open_questions: 1,
                max_working_memory_items: 1,
                max_item_chars: 80,
            },
            ..ContextPackage::default()
        };
        let orchestrator = profiles.get(&Role::Orchestrator).unwrap();
        let result = runner.run(orchestrator, pkg).await.expect("run");
        let decision = result.decision.expect("decision");
        assert!(matches!(
            decision.execution_mode,
            ExecutionModeWire::PlanFirst
        ));
        assert_eq!(decision.next_role, Role::Planner);
    }

    // Suppress `unused import` when this file is the only reference.
    fn _types() {
        let _: BTreeMap<String, String> = BTreeMap::new();
    }
}
