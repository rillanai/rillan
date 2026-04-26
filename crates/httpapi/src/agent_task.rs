// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! `POST /v1/agent/tasks` handler. Mirrors
//! `internal/httpapi/agent_task_handler.go` plus the transport shapes from
//! `internal/httpapi/agent_transport.go`.

use std::collections::BTreeMap;
use std::sync::Arc;

use axum::extract::State;
use axum::http::header::CONTENT_TYPE;
use axum::http::{HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};
use bytes::Bytes;
use rillan_agent::{
    apply_budget, default_role_profiles, resolve_approved_repo_root, ActionKind, ActionRequest,
    ApprovalGate, BudgetSection, ContextPackage, EvidenceItem, McpDiagnostic, McpFileRef,
    McpSelection, McpSnapshot, McpVcsContext, Role, Runner, SharedRunner, SkillInvocation,
    SkillKind, SkillResult,
};
use rillan_openai::{ApiError, ErrorResponse};
use rillan_retrieval::{DebugMetadata, Pipeline};
use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentTaskRequest {
    #[serde(default)]
    pub goal: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub execution_mode: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub current_step: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub repo_root: String,
    #[serde(default)]
    pub skill_invocations: Vec<AgentSkillInvocation>,
    #[serde(default)]
    pub mcp_snapshot: Option<AgentMcpSnapshot>,
    #[serde(default)]
    pub proposed_action: Option<AgentActionRequest>,
}

#[derive(Debug, Serialize)]
pub(crate) struct AgentTaskResponse {
    pub result: AgentRunResult,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub proposal: Option<AgentActionProposal>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub(crate) struct AgentSkillInvocation {
    #[serde(default)]
    pub kind: String,
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

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentMcpSnapshot {
    #[serde(default)]
    pub open_files: Vec<AgentMcpFileRef>,
    #[serde(default)]
    pub selection: Option<AgentMcpSelection>,
    #[serde(default)]
    pub diagnostics: Vec<AgentMcpDiagnostic>,
    #[serde(default)]
    pub vcs: Option<AgentMcpVcsContext>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentMcpFileRef {
    #[serde(default)]
    pub path: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentMcpSelection {
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub snippet: String,
    #[serde(default)]
    pub start: i64,
    #[serde(default)]
    pub end: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentMcpDiagnostic {
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub severity: String,
    #[serde(default)]
    pub message: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentMcpVcsContext {
    #[serde(default)]
    pub branch: String,
    #[serde(default)]
    pub head: String,
    #[serde(default)]
    pub dirty: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct AgentActionRequest {
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub summary: String,
    #[serde(default)]
    pub payload: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize)]
pub(crate) struct AgentActionProposal {
    pub id: String,
    pub kind: String,
    pub summary: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub payload: BTreeMap<String, String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub request_id: String,
    pub status: String,
}

#[derive(Debug, Clone, Serialize)]
pub(crate) struct AgentRunResult {
    pub role: String,
    pub summary: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub decision: Option<AgentOrchestrationDecision>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub skill_results: Vec<AgentSkillResult>,
    pub context_echo: AgentContextPackage,
}

#[derive(Debug, Clone, Serialize)]
pub(crate) struct AgentOrchestrationDecision {
    pub execution_mode: String,
    pub next_role: String,
    pub reason: String,
}

#[derive(Debug, Clone, Serialize)]
pub(crate) struct AgentSkillResult {
    pub kind: String,
    pub payload: Value,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentContextPackage {
    pub task: AgentTaskSection,
    pub constraints: AgentConstraintsSection,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub skill_invocations: Vec<AgentSkillInvocation>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub evidence: Vec<AgentEvidenceItem>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub facts: Vec<AgentFactItem>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub open_questions: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub working_memory: Vec<String>,
    pub output_schema: AgentOutputSchemaSection,
    pub budget: AgentBudgetSection,
    pub policy_trace: AgentPolicyTraceSection,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentTaskSection {
    pub goal: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub execution_mode: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub current_step: String,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentConstraintsSection {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub repo_root: String,
    pub approval_required: bool,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub allowed_effects: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub forbidden_effects: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentEvidenceItem {
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
    pub summary: String,
    #[serde(default, skip_serializing_if = "String::is_empty", rename = "ref")]
    pub ref_: String,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentFactItem {
    pub key: String,
    pub value: String,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentOutputSchemaSection {
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub note: String,
}

#[derive(Debug, Clone, Copy, Default, Serialize)]
pub(crate) struct AgentBudgetSection {
    pub max_evidence_items: usize,
    pub max_facts: usize,
    pub max_open_questions: usize,
    pub max_working_memory_items: usize,
    pub max_item_chars: usize,
}

#[derive(Debug, Clone, Default, Serialize)]
pub(crate) struct AgentPolicyTraceSection {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub phase: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub route_source: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub verdict: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
}

/// Static dependencies wired into the agent task handler.
#[derive(Clone)]
pub struct AgentTaskState {
    pub gate: Arc<ApprovalGate>,
    pub approved_repo_roots: Arc<Vec<String>>,
    /// Optional retrieval pipeline. When set the handler runs `prepare_query`
    /// against the goal and surfaces the resulting metadata as `retrieval_*`
    /// evidence items in the orchestrator's context package.
    pub pipeline: Option<Arc<Pipeline>>,
}

impl std::fmt::Debug for AgentTaskState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AgentTaskState")
            .field("approved_repo_roots", &self.approved_repo_roots)
            .field("pipeline", &self.pipeline.is_some())
            .finish_non_exhaustive()
    }
}

/// Handler entry point.
pub(crate) async fn handle_agent_task(
    State(state): State<AgentTaskState>,
    body: Bytes,
) -> Response {
    if body.len() > 1 << 20 {
        return error_response(
            StatusCode::PAYLOAD_TOO_LARGE,
            "invalid_request_error",
            "request body exceeds 1 MiB limit",
        );
    }
    let mut request: AgentTaskRequest = match serde_json::from_slice(&body) {
        Ok(value) => value,
        Err(_) => {
            return error_response(
                StatusCode::BAD_REQUEST,
                "invalid_request_error",
                "request body must be valid JSON",
            );
        }
    };
    if request.goal.trim().is_empty() {
        return error_response(
            StatusCode::BAD_REQUEST,
            "invalid_request_error",
            "goal must not be empty",
        );
    }

    let approved_roots = state.approved_repo_roots.as_ref();
    if !request.repo_root.is_empty() {
        match resolve_approved_repo_root(&request.repo_root, approved_roots) {
            Ok(resolved) => request.repo_root = resolved.to_string_lossy().to_string(),
            Err(err) => {
                return error_response(
                    StatusCode::BAD_REQUEST,
                    "invalid_request_error",
                    &err.to_string(),
                );
            }
        }
    }
    for invocation in &mut request.skill_invocations {
        if invocation.repo_root.is_empty() {
            continue;
        }
        match resolve_approved_repo_root(&invocation.repo_root, approved_roots) {
            Ok(resolved) => invocation.repo_root = resolved.to_string_lossy().to_string(),
            Err(err) => {
                return error_response(
                    StatusCode::BAD_REQUEST,
                    "invalid_request_error",
                    &err.to_string(),
                );
            }
        }
    }

    let retrieval_metadata = match state.pipeline.as_ref() {
        Some(pipeline) => match pipeline.prepare_query(&request.goal).await {
            Ok(meta) => meta,
            Err(err) => {
                tracing::warn!(error = %err, "agent task retrieval failed");
                None
            }
        },
        None => None,
    };

    let pkg = build_initial_package(&request, retrieval_metadata.as_ref());
    let runner = SharedRunner::new(approved_roots.clone());
    let profiles = default_role_profiles();
    let orchestrator = profiles
        .get(&Role::Orchestrator)
        .expect("orchestrator profile present");
    let result = match runner.run(orchestrator, pkg).await {
        Ok(value) => value,
        Err(err) => {
            tracing::error!(error = %err, "agent task runner failed");
            return error_response(
                StatusCode::INTERNAL_SERVER_ERROR,
                "runtime_error",
                &err.to_string(),
            );
        }
    };

    let mut response = AgentTaskResponse {
        result: from_agent_run_result(&result),
        proposal: None,
    };
    if let Some(proposed) = request.proposed_action {
        let action_request = match to_action_request(proposed) {
            Ok(value) => value,
            Err(err) => {
                return error_response(StatusCode::BAD_REQUEST, "invalid_request_error", &err);
            }
        };
        match state.gate.propose("", action_request).await {
            Ok(proposal) => {
                response.proposal = Some(from_action_proposal(&proposal));
            }
            Err(err) => {
                return error_response(
                    StatusCode::BAD_REQUEST,
                    "invalid_request_error",
                    &err.to_string(),
                );
            }
        }
    }

    json_response(StatusCode::OK, &response)
}

fn build_initial_package(
    request: &AgentTaskRequest,
    retrieval: Option<&DebugMetadata>,
) -> ContextPackage {
    let mut pkg = ContextPackage {
        task: rillan_agent::TaskSection {
            goal: request.goal.clone(),
            execution_mode: request.execution_mode.clone(),
            current_step: request.current_step.clone(),
        },
        constraints: rillan_agent::ConstraintsSection {
            repo_root: request.repo_root.clone(),
            approval_required: true,
            allowed_effects: vec![
                "read".into(),
                "propose_write".into(),
                "propose_execute".into(),
            ],
            forbidden_effects: vec!["write".into(), "execute".into()],
        },
        skill_invocations: request
            .skill_invocations
            .iter()
            .map(to_agent_skill_invocation)
            .collect(),
        output_schema: rillan_agent::OutputSchemaSection {
            kind: "agent_task_response".into(),
            note: "Return orchestration result and optional proposal.".into(),
        },
        budget: BudgetSection {
            max_evidence_items: 8,
            max_facts: 8,
            max_open_questions: 4,
            max_working_memory_items: 4,
            max_item_chars: 240,
        },
        ..ContextPackage::default()
    };
    if let Some(meta) = retrieval {
        pkg.evidence.extend(evidence_from_retrieval(meta));
    }
    if let Some(snapshot) = &request.mcp_snapshot {
        let mcp = to_agent_mcp_snapshot(snapshot);
        pkg.evidence.extend(evidence_from_mcp(&mcp));
        pkg.facts.extend(facts_from_mcp(&mcp));
    }
    apply_budget(pkg)
}

fn evidence_from_retrieval(metadata: &DebugMetadata) -> Vec<EvidenceItem> {
    let mut items: Vec<EvidenceItem> = Vec::with_capacity(metadata.compiled.sources.len() + 1);
    let query = metadata.query.trim();
    if !query.is_empty() {
        items.push(EvidenceItem {
            kind: "retrieval_query".into(),
            summary: query.to_string(),
            ..EvidenceItem::default()
        });
    }
    for source in &metadata.compiled.sources {
        let reference = format!(
            "{}:{}-{}",
            source.document_path, source.start_line, source.end_line
        );
        items.push(EvidenceItem {
            kind: "retrieval_source".into(),
            path: source.document_path.clone(),
            summary: reference.clone(),
            ref_: reference,
        });
    }
    items
}

fn to_agent_skill_invocation(dto: &AgentSkillInvocation) -> SkillInvocation {
    SkillInvocation {
        kind: parse_skill_kind(&dto.kind),
        repo_root: dto.repo_root.clone(),
        paths: dto.paths.clone(),
        query: dto.query.clone(),
        db_path: dto.db_path.clone(),
        staged_only: dto.staged_only,
    }
}

fn parse_skill_kind(value: &str) -> Option<SkillKind> {
    match value {
        "read_files" => Some(SkillKind::ReadFiles),
        "search_repo" => Some(SkillKind::SearchRepo),
        "index_lookup" => Some(SkillKind::IndexLookup),
        "git_status" => Some(SkillKind::GitStatus),
        "git_diff" => Some(SkillKind::GitDiff),
        _ => None,
    }
}

fn to_agent_mcp_snapshot(dto: &AgentMcpSnapshot) -> McpSnapshot {
    McpSnapshot {
        open_files: dto
            .open_files
            .iter()
            .map(|f| McpFileRef {
                path: f.path.clone(),
            })
            .collect(),
        selection: dto.selection.as_ref().map(|s| McpSelection {
            path: s.path.clone(),
            snippet: s.snippet.clone(),
            start: s.start,
            end: s.end,
        }),
        diagnostics: dto
            .diagnostics
            .iter()
            .map(|d| McpDiagnostic {
                path: d.path.clone(),
                severity: d.severity.clone(),
                message: d.message.clone(),
            })
            .collect(),
        vcs: dto.vcs.as_ref().map(|v| McpVcsContext {
            branch: v.branch.clone(),
            head: v.head.clone(),
            dirty: v.dirty,
        }),
    }
}

fn evidence_from_mcp(snapshot: &McpSnapshot) -> Vec<rillan_agent::EvidenceItem> {
    let mut items: Vec<rillan_agent::EvidenceItem> = Vec::new();
    for file in &snapshot.open_files {
        items.push(rillan_agent::EvidenceItem {
            kind: "mcp_open_file".into(),
            path: file.path.clone(),
            summary: file.path.clone(),
            ref_: file.path.clone(),
        });
    }
    if let Some(selection) = &snapshot.selection {
        items.push(rillan_agent::EvidenceItem {
            kind: "mcp_selection".into(),
            path: selection.path.clone(),
            summary: selection.path.clone(),
            ref_: selection.path.clone(),
        });
    }
    for diag in &snapshot.diagnostics {
        items.push(rillan_agent::EvidenceItem {
            kind: "mcp_diagnostic".into(),
            path: diag.path.clone(),
            summary: format!("[{}] {}", diag.severity, diag.message),
            ref_: diag.path.clone(),
        });
    }
    items
}

fn facts_from_mcp(snapshot: &McpSnapshot) -> Vec<rillan_agent::FactItem> {
    let Some(vcs) = &snapshot.vcs else {
        return Vec::new();
    };
    vec![
        rillan_agent::FactItem {
            key: "mcp_branch".into(),
            value: vcs.branch.clone(),
        },
        rillan_agent::FactItem {
            key: "mcp_head".into(),
            value: vcs.head.clone(),
        },
        rillan_agent::FactItem {
            key: "mcp_dirty".into(),
            value: vcs.dirty.to_string(),
        },
    ]
}

fn to_action_request(dto: AgentActionRequest) -> Result<ActionRequest, String> {
    let kind = match dto.kind.as_str() {
        "apply_patch" => Some(ActionKind::ApplyPatch),
        "run_tests" => Some(ActionKind::RunTests),
        "" => None,
        other => return Err(format!("unsupported action kind {other:?}")),
    };
    Ok(ActionRequest {
        kind,
        summary: dto.summary,
        payload: dto.payload,
    })
}

fn from_action_proposal(proposal: &rillan_agent::ActionProposal) -> AgentActionProposal {
    AgentActionProposal {
        id: proposal.id.clone(),
        kind: proposal
            .kind
            .map(|k| k.as_str().to_string())
            .unwrap_or_default(),
        summary: proposal.summary.clone(),
        payload: proposal.payload.clone(),
        request_id: proposal.request_id.clone(),
        status: proposal.status.clone(),
    }
}

fn from_agent_run_result(result: &rillan_agent::RunResult) -> AgentRunResult {
    AgentRunResult {
        role: result.role.as_str().to_string(),
        summary: result.summary.clone(),
        decision: result
            .decision
            .as_ref()
            .map(|d| AgentOrchestrationDecision {
                execution_mode: match d.execution_mode {
                    rillan_agent::ExecutionModeWire::Direct => "direct".into(),
                    rillan_agent::ExecutionModeWire::PlanFirst => "plan_first".into(),
                },
                next_role: d.next_role.as_str().to_string(),
                reason: d.reason.clone(),
            }),
        skill_results: result
            .skill_results
            .iter()
            .map(skill_result_to_dto)
            .collect(),
        context_echo: from_context_package(&result.context_echo),
    }
}

fn skill_result_to_dto(value: &SkillResult) -> AgentSkillResult {
    AgentSkillResult {
        kind: value
            .kind
            .map(|k| k.as_str().to_string())
            .unwrap_or_default(),
        payload: value.payload.clone(),
    }
}

fn from_context_package(pkg: &ContextPackage) -> AgentContextPackage {
    AgentContextPackage {
        task: AgentTaskSection {
            goal: pkg.task.goal.clone(),
            execution_mode: pkg.task.execution_mode.clone(),
            current_step: pkg.task.current_step.clone(),
        },
        constraints: AgentConstraintsSection {
            repo_root: pkg.constraints.repo_root.clone(),
            approval_required: pkg.constraints.approval_required,
            allowed_effects: pkg.constraints.allowed_effects.clone(),
            forbidden_effects: pkg.constraints.forbidden_effects.clone(),
        },
        skill_invocations: pkg
            .skill_invocations
            .iter()
            .map(|inv| AgentSkillInvocation {
                kind: inv.kind.map(|k| k.as_str().to_string()).unwrap_or_default(),
                repo_root: inv.repo_root.clone(),
                paths: inv.paths.clone(),
                query: inv.query.clone(),
                db_path: inv.db_path.clone(),
                staged_only: inv.staged_only,
            })
            .collect(),
        evidence: pkg
            .evidence
            .iter()
            .map(|e| AgentEvidenceItem {
                kind: e.kind.clone(),
                path: e.path.clone(),
                summary: e.summary.clone(),
                ref_: e.ref_.clone(),
            })
            .collect(),
        facts: pkg
            .facts
            .iter()
            .map(|f| AgentFactItem {
                key: f.key.clone(),
                value: f.value.clone(),
            })
            .collect(),
        open_questions: pkg.open_questions.clone(),
        working_memory: pkg.working_memory.clone(),
        output_schema: AgentOutputSchemaSection {
            kind: pkg.output_schema.kind.clone(),
            note: pkg.output_schema.note.clone(),
        },
        budget: AgentBudgetSection {
            max_evidence_items: pkg.budget.max_evidence_items,
            max_facts: pkg.budget.max_facts,
            max_open_questions: pkg.budget.max_open_questions,
            max_working_memory_items: pkg.budget.max_working_memory_items,
            max_item_chars: pkg.budget.max_item_chars,
        },
        policy_trace: AgentPolicyTraceSection {
            phase: pkg.policy_trace.phase.clone(),
            route_source: pkg.policy_trace.route_source.clone(),
            verdict: pkg.policy_trace.verdict.clone(),
            reason: pkg.policy_trace.reason.clone(),
        },
    }
}

fn json_response<T: Serialize>(status: StatusCode, value: &T) -> Response {
    let body = serde_json::to_vec(value).unwrap_or_else(|_| b"{}".to_vec());
    Response::builder()
        .status(status)
        .header(CONTENT_TYPE, HeaderValue::from_static("application/json"))
        .body(axum::body::Body::from(body))
        .unwrap_or_else(|_| status.into_response())
}

fn error_response(status: StatusCode, kind: &str, message: &str) -> Response {
    let payload = ErrorResponse {
        error: ApiError {
            message: message.to_string(),
            kind: kind.to_string(),
            param: String::new(),
            code: String::new(),
        },
    };
    let body = serde_json::to_vec(&payload).unwrap_or_else(|_| b"{}".to_vec());
    Response::builder()
        .status(status)
        .header(CONTENT_TYPE, HeaderValue::from_static("application/json"))
        .body(axum::body::Body::from(body))
        .unwrap_or_else(|_| status.into_response())
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(value: &bool) -> bool {
    !*value
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;
    use axum::routing::post;
    use axum::Router;
    use rillan_audit::Store as AuditStore;

    fn build_router(state: AgentTaskState) -> Router {
        Router::new()
            .route("/v1/agent/tasks", post(handle_agent_task))
            .with_state(state)
    }

    async fn invoke(state: AgentTaskState, body: &str) -> (StatusCode, Vec<u8>) {
        let router = build_router(state);
        let request = axum::http::Request::builder()
            .method("POST")
            .uri("/v1/agent/tasks")
            .header("content-type", "application/json")
            .body(axum::body::Body::from(body.to_string()))
            .unwrap();
        let response = tower::ServiceExt::oneshot(router, request).await.unwrap();
        let status = response.status();
        let bytes = to_bytes(response.into_body(), 1 << 20).await.unwrap();
        (status, bytes.to_vec())
    }

    fn task_state(approved: Vec<String>) -> AgentTaskState {
        let dir = tempfile::tempdir().unwrap();
        let store = AuditStore::new(dir.path().join("ledger.jsonl")).unwrap();
        let recorder: Arc<dyn rillan_audit::Recorder> = Arc::new(store);
        AgentTaskState {
            gate: Arc::new(ApprovalGate::new(Some(recorder))),
            approved_repo_roots: Arc::new(approved),
            pipeline: None,
        }
    }

    #[tokio::test]
    async fn returns_orchestration_result() {
        let state = task_state(Vec::new());
        let (status, body) = invoke(
            state,
            r#"{"goal":"review repo risk","execution_mode":"plan_first"}"#,
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(response["result"]["role"], "orchestrator");
        assert!(response.get("proposal").is_none() || response["proposal"].is_null());
    }

    #[tokio::test]
    async fn returns_proposal_for_proposed_action() {
        let state = task_state(Vec::new());
        let (status, body) = invoke(
            state,
            r#"{"goal":"patch repo","proposed_action":{"kind":"apply_patch","summary":"apply patch to repo"}}"#,
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert!(response["proposal"].is_object());
        assert_eq!(response["proposal"]["status"], "pending");
        assert_eq!(response["proposal"]["kind"], "apply_patch");
    }

    #[tokio::test]
    async fn rejects_invalid_proposal() {
        let state = task_state(Vec::new());
        let (status, _body) = invoke(
            state,
            r#"{"goal":"patch repo","proposed_action":{"kind":"unknown","summary":"oops"}}"#,
        )
        .await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
    }

    #[tokio::test]
    async fn rejects_unapproved_repo_root() {
        let state = task_state(Vec::new());
        let tmp = tempfile::tempdir().unwrap();
        let body = format!(
            r#"{{"goal":"inspect repo","repo_root":"{}"}}"#,
            tmp.path().display()
        );
        let (status, _body) = invoke(state, &body).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
    }

    #[tokio::test]
    async fn rejects_unapproved_skill_invocation_repo_root() {
        let state = task_state(Vec::new());
        let tmp = tempfile::tempdir().unwrap();
        let body = format!(
            r#"{{"goal":"inspect repo","skill_invocations":[{{"kind":"read_files","repo_root":"{}","paths":["docs/guide.md"]}}]}}"#,
            tmp.path().display()
        );
        let (status, _body) = invoke(state, &body).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
    }

    #[tokio::test]
    async fn returns_read_only_skill_results() {
        let tmp = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(tmp.path().join("docs")).unwrap();
        std::fs::write(
            tmp.path().join("docs/guide.md"),
            b"bounded read-only skill output",
        )
        .unwrap();
        let approved = tmp.path().to_string_lossy().to_string();
        let state = task_state(vec![approved.clone()]);
        let body = format!(
            r#"{{"goal":"inspect repo","repo_root":"{approved}","skill_invocations":[{{"kind":"read_files","repo_root":"{approved}","paths":["docs/guide.md"]}}]}}"#
        );
        let (status, body) = invoke(state, &body).await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        let skill_results = response["result"]["skill_results"].as_array().unwrap();
        assert_eq!(skill_results.len(), 1);
        assert_eq!(skill_results[0]["kind"], "read_files");
    }

    #[tokio::test]
    async fn accepts_optional_mcp_snapshot() {
        let state = task_state(Vec::new());
        let (status, _body) = invoke(
            state,
            r#"{"goal":"inspect editor state","mcp_snapshot":{"open_files":[{"path":"crates/httpapi/src/chat.rs"}],"diagnostics":[{"path":"crates/httpapi/src/chat.rs","severity":"warning","message":"example"}]}}"#,
        )
        .await;
        assert_eq!(status, StatusCode::OK);
    }

    fn pipeline_state_with(pipeline: Arc<Pipeline>) -> AgentTaskState {
        let dir = tempfile::tempdir().unwrap();
        let store = AuditStore::new(dir.path().join("ledger.jsonl")).unwrap();
        let recorder: Arc<dyn rillan_audit::Recorder> = Arc::new(store);
        AgentTaskState {
            gate: Arc::new(ApprovalGate::new(Some(recorder))),
            approved_repo_roots: Arc::new(Vec::new()),
            pipeline: Some(pipeline),
        }
    }

    #[tokio::test]
    async fn surfaces_retrieval_query_evidence_when_pipeline_enabled() {
        let dir = tempfile::tempdir().unwrap();
        let db_path = dir.path().join("idx.db");
        // Initialize an empty index so the pipeline can open the store.
        rillan_index::Store::open(&db_path).unwrap();
        let pipeline = Arc::new(rillan_retrieval::Pipeline::new(
            rillan_config::RetrievalConfig {
                enabled: true,
                top_k: 4,
                max_context_chars: 2000,
            },
            db_path,
        ));
        let state = pipeline_state_with(pipeline);
        let (status, body) = invoke(state, r#"{"goal":"investigate audit ledger"}"#).await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        let evidence = response["result"]["context_echo"]["evidence"]
            .as_array()
            .expect("evidence array");
        let kinds: Vec<&str> = evidence.iter().filter_map(|e| e["kind"].as_str()).collect();
        assert!(
            kinds.contains(&"retrieval_query"),
            "expected retrieval_query evidence, got {kinds:?}"
        );
    }

    #[tokio::test]
    async fn surfaces_retrieval_source_evidence_for_indexed_chunks() {
        use rillan_index::types::{ChunkRecord, DocumentRecord};
        let dir = tempfile::tempdir().unwrap();
        let db_path = dir.path().join("idx.db");
        let store = rillan_index::Store::open(&db_path).unwrap();
        store
            .replace_all(
                &[DocumentRecord {
                    path: "docs/audit.md".into(),
                    content_hash: "h1".into(),
                    size_bytes: 32,
                }],
                &[ChunkRecord {
                    id: "c1".into(),
                    document_path: "docs/audit.md".into(),
                    ordinal: 0,
                    start_line: 1,
                    end_line: 3,
                    content: "audit ledger persists policy decisions".into(),
                    content_hash: "ch1".into(),
                }],
                &[],
            )
            .unwrap();
        drop(store);

        let pipeline = Arc::new(rillan_retrieval::Pipeline::new(
            rillan_config::RetrievalConfig {
                enabled: true,
                top_k: 4,
                max_context_chars: 2000,
            },
            db_path,
        ));
        let state = pipeline_state_with(pipeline);
        let (status, body) = invoke(state, r#"{"goal":"audit ledger lookup"}"#).await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        let evidence = response["result"]["context_echo"]["evidence"]
            .as_array()
            .expect("evidence array");
        let source_paths: Vec<&str> = evidence
            .iter()
            .filter(|e| e["kind"] == "retrieval_source")
            .filter_map(|e| e["path"].as_str())
            .collect();
        assert!(
            source_paths.contains(&"docs/audit.md"),
            "expected retrieval_source for docs/audit.md, got {source_paths:?}"
        );
    }

    #[tokio::test]
    async fn pipeline_disabled_emits_no_retrieval_evidence() {
        let dir = tempfile::tempdir().unwrap();
        let db_path = dir.path().join("idx.db");
        rillan_index::Store::open(&db_path).unwrap();
        let pipeline = Arc::new(rillan_retrieval::Pipeline::new(
            rillan_config::RetrievalConfig::default(),
            db_path,
        ));
        let state = pipeline_state_with(pipeline);
        let (status, body) = invoke(state, r#"{"goal":"no retrieval expected"}"#).await;
        assert_eq!(status, StatusCode::OK);
        let response: serde_json::Value = serde_json::from_slice(&body).unwrap();
        let evidence = response["result"]["context_echo"]["evidence"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        assert!(
            evidence
                .iter()
                .all(|e| e["kind"] != "retrieval_query" && e["kind"] != "retrieval_source"),
            "no retrieval evidence expected when pipeline disabled"
        );
    }
}
