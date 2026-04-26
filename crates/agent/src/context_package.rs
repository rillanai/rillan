// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Context package shapes. Mirrors `internal/agent/context_package.go`.

use rillan_policy::EvaluationResult;
use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ContextPackage {
    pub task: TaskSection,
    pub constraints: ConstraintsSection,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub skill_invocations: Vec<SkillInvocation>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub evidence: Vec<EvidenceItem>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub facts: Vec<FactItem>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub open_questions: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub working_memory: Vec<String>,
    pub output_schema: OutputSchemaSection,
    pub budget: BudgetSection,
    pub policy_trace: PolicyTraceSection,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SkillKind {
    ReadFiles,
    SearchRepo,
    IndexLookup,
    GitStatus,
    GitDiff,
}

impl SkillKind {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::ReadFiles => "read_files",
            Self::SearchRepo => "search_repo",
            Self::IndexLookup => "index_lookup",
            Self::GitStatus => "git_status",
            Self::GitDiff => "git_diff",
        }
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SkillInvocation {
    pub kind: Option<SkillKind>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub repo_root: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub paths: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub query: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub db_path: String,
    #[serde(default, skip_serializing_if = "is_false")]
    pub staged_only: bool,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SkillResult {
    pub kind: Option<SkillKind>,
    pub payload: Value,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct TaskSection {
    pub goal: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub execution_mode: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub current_step: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ConstraintsSection {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub repo_root: String,
    pub approval_required: bool,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub allowed_effects: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub forbidden_effects: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct EvidenceItem {
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
    pub summary: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub ref_: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct FactItem {
    pub key: String,
    pub value: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct OutputSchemaSection {
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub note: String,
}

#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct BudgetSection {
    pub max_evidence_items: usize,
    pub max_facts: usize,
    pub max_open_questions: usize,
    pub max_working_memory_items: usize,
    pub max_item_chars: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct PolicyTraceSection {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub phase: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub route_source: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub verdict: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
}

#[must_use]
pub fn policy_trace_from_result(result: &EvaluationResult) -> PolicyTraceSection {
    PolicyTraceSection {
        phase: result
            .trace
            .phase
            .map(|p| p.as_str().to_string())
            .unwrap_or_default(),
        route_source: result
            .trace
            .route_source
            .map(|p| p.as_str().to_string())
            .unwrap_or_default(),
        verdict: result.verdict.as_str().to_string(),
        reason: result.reason.clone(),
    }
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(value: &bool) -> bool {
    !*value
}
