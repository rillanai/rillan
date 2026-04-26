// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Agent orchestration. Mirrors the public surface of `internal/agent` from
//! the Go repo. ADR-007.
//!
//! Modules:
//! * [`action_proposal`] — `ActionRequest` / `ActionProposal` shapes used by
//!   the approval gate.
//! * [`context_package`] — full `ContextPackage` schema handed to roles.
//! * [`context_budget`] — `apply_budget` truncates a package to its limits.
//! * [`context_builder`] — builds a `ContextPackage` from policy results,
//!   retrieval metadata, and MCP snapshot inputs.
//! * [`mcp_snapshot`] — IDE/editor snapshot shape + normalizer.
//! * [`orchestrator`] — chooses the next role from the package.
//! * [`gating`] — approval gate + audit-event emission.
//! * [`proposal_store`] — in-memory action-proposal store.
//! * [`roles`] — role catalog.
//! * [`skill_metrics`] — persisted skill latency + invocation counters.

pub mod action_proposal;
pub mod context_budget;
pub mod context_builder;
pub mod context_package;
pub mod gating;
pub mod mcp_snapshot;
pub mod mcp_snapshot_builder;
pub mod orchestrator;
pub mod proposal_store;
pub mod roles;
pub mod runner;
pub mod skill_catalog;
pub mod skill_metrics;
pub mod skills;
pub mod tool_runtime;

pub use action_proposal::{validate_action_request, ActionKind, ActionProposal, ActionRequest};
pub use context_budget::apply_budget;
pub use context_builder::{build_context_package, BuildInput, DiagnosticEvidence};
pub use context_package::{
    policy_trace_from_result, BudgetSection, ConstraintsSection, ContextPackage, EvidenceItem,
    FactItem, OutputSchemaSection, PolicyTraceSection, SkillInvocation, SkillKind, SkillResult,
    TaskSection,
};
pub use gating::{ApprovalGate, GatingError};
pub use mcp_snapshot::{
    McpDiagnostic, McpFileRef, McpSelection, McpSnapshot, McpSnapshotOptions, McpVcsContext,
};
pub use mcp_snapshot_builder::normalize_mcp_snapshot;
pub use orchestrator::decide_execution_mode;
pub use proposal_store::{ProposalError, ProposalStore};
pub use roles::{
    default_role_profiles, ExecutionModeWire, OrchestrationDecision, Role, RoleProfile,
};
pub use runner::{run_result_to_value, RunResult, Runner, RunnerError, SharedRunner};
pub use skill_catalog::{
    default_managed_skill_path, default_skill_catalog_path, get_installed_skill, install_skill,
    list_installed_skills, load_skill_catalog, normalize_skill_id, remove_skill,
    save_skill_catalog, InstalledSkill, SkillCatalog, SkillCatalogError,
};
pub use skill_metrics::{
    default_skill_metrics_path, load_skill_metrics, record_skill_latency, save_skill_metrics,
    SkillMetric, SkillMetricsStore,
};
pub use skills::resolve_approved_repo_root;
pub use tool_runtime::{
    ReadOnlyToolRuntime, ToolCall, ToolDefinition, ToolError, ToolExecutionResult, ToolExecutor,
    ToolKind, ToolSource,
};
