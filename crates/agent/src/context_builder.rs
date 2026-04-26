// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Context-package builder. Mirrors `internal/agent/context_builder.go`.

use rillan_policy::EvaluationResult;
use rillan_retrieval::DebugMetadata;

use crate::context_budget::apply_budget;
use crate::context_package::{
    policy_trace_from_result, BudgetSection, ConstraintsSection, ContextPackage, EvidenceItem,
    FactItem, OutputSchemaSection, SkillInvocation, TaskSection,
};
use crate::mcp_snapshot::{McpSnapshot, McpSnapshotOptions};
use crate::mcp_snapshot_builder::normalize_mcp_snapshot;

#[derive(Debug, Clone)]
pub struct DiagnosticEvidence {
    pub path: String,
    pub message: String,
    pub level: String,
}

#[derive(Debug)]
pub struct BuildInput<'a> {
    pub goal: String,
    pub execution_mode: String,
    pub current_step: String,
    pub repo_root: String,
    pub approval_required: bool,
    pub allowed_effects: Vec<String>,
    pub forbidden_effects: Vec<String>,
    pub skill_invocations: Vec<SkillInvocation>,
    pub facts: Vec<FactItem>,
    pub open_questions: Vec<String>,
    pub working_memory: Vec<String>,
    pub output_kind: String,
    pub output_note: String,
    pub budget: BudgetSection,
    pub policy_result: &'a EvaluationResult,
    pub retrieval: Option<&'a DebugMetadata>,
    pub mcp_snapshot: Option<McpSnapshot>,
    pub diagnostics: Vec<DiagnosticEvidence>,
    pub vcs_context: Vec<FactItem>,
}

#[must_use]
pub fn build_context_package(input: BuildInput<'_>) -> ContextPackage {
    let mut evidence: Vec<EvidenceItem> = Vec::new();
    let mut facts = input.facts;
    facts.extend(input.vcs_context);

    if let Some(retrieval) = input.retrieval {
        evidence.extend(evidence_from_retrieval(retrieval));
    }
    if let Some(snapshot) = input.mcp_snapshot {
        let normalized = normalize_mcp_snapshot(
            snapshot,
            McpSnapshotOptions {
                max_open_files: input.budget.max_evidence_items,
                max_diagnostics: input.budget.max_evidence_items,
                max_chars: input.budget.max_item_chars,
            },
        );
        evidence.extend(evidence_from_mcp_snapshot(&normalized));
        facts.extend(facts_from_mcp_snapshot(&normalized));
    }
    for diagnostic in input.diagnostics {
        let summary = format!("[{}] {}", diagnostic.level, diagnostic.message)
            .trim()
            .to_string();
        evidence.push(EvidenceItem {
            kind: "diagnostic".into(),
            path: diagnostic.path.clone(),
            summary,
            ref_: diagnostic.path,
        });
    }

    let pkg = ContextPackage {
        task: TaskSection {
            goal: input.goal,
            execution_mode: input.execution_mode,
            current_step: input.current_step,
        },
        constraints: ConstraintsSection {
            repo_root: input.repo_root,
            approval_required: input.approval_required,
            allowed_effects: input.allowed_effects,
            forbidden_effects: input.forbidden_effects,
        },
        skill_invocations: input.skill_invocations,
        evidence,
        facts,
        open_questions: input.open_questions,
        working_memory: input.working_memory,
        output_schema: OutputSchemaSection {
            kind: input.output_kind,
            note: input.output_note,
        },
        budget: input.budget,
        policy_trace: policy_trace_from_result(input.policy_result),
    };
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

fn evidence_from_mcp_snapshot(snapshot: &McpSnapshot) -> Vec<EvidenceItem> {
    let mut items: Vec<EvidenceItem> =
        Vec::with_capacity(snapshot.open_files.len() + snapshot.diagnostics.len() + 1);
    for file in &snapshot.open_files {
        items.push(EvidenceItem {
            kind: "mcp_open_file".into(),
            path: file.path.clone(),
            summary: file.path.clone(),
            ref_: file.path.clone(),
        });
    }
    if let Some(selection) = &snapshot.selection {
        items.push(EvidenceItem {
            kind: "mcp_selection".into(),
            path: selection.path.clone(),
            summary: selection.path.clone(),
            ref_: selection.path.clone(),
        });
    }
    for diag in &snapshot.diagnostics {
        items.push(EvidenceItem {
            kind: "mcp_diagnostic".into(),
            path: diag.path.clone(),
            summary: format!("[{}] {}", diag.severity, diag.message),
            ref_: diag.path.clone(),
        });
    }
    items
}

fn facts_from_mcp_snapshot(snapshot: &McpSnapshot) -> Vec<FactItem> {
    let Some(vcs) = &snapshot.vcs else {
        return Vec::new();
    };
    vec![
        FactItem {
            key: "mcp_branch".into(),
            value: vcs.branch.clone(),
        },
        FactItem {
            key: "mcp_head".into(),
            value: vcs.head.clone(),
        },
        FactItem {
            key: "mcp_dirty".into(),
            value: vcs.dirty.to_string(),
        },
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_policy::{EvaluationResult, PolicyTrace, RetrievalPlan, Verdict};

    fn allow_result() -> EvaluationResult {
        EvaluationResult {
            verdict: Verdict::Allow,
            reason: "policy_allow".into(),
            request: None,
            body: Vec::new(),
            findings: Vec::new(),
            trace: PolicyTrace::default(),
            retrieval: RetrievalPlan::default(),
        }
    }

    #[test]
    fn build_context_package_includes_diagnostics() {
        let result = allow_result();
        let pkg = build_context_package(BuildInput {
            goal: "fix the build".into(),
            execution_mode: "direct".into(),
            current_step: String::new(),
            repo_root: "/repo".into(),
            approval_required: true,
            allowed_effects: vec!["read".into()],
            forbidden_effects: vec!["write".into()],
            skill_invocations: Vec::new(),
            facts: Vec::new(),
            open_questions: Vec::new(),
            working_memory: Vec::new(),
            output_kind: "summary".into(),
            output_note: String::new(),
            budget: BudgetSection {
                max_evidence_items: 5,
                max_facts: 5,
                max_open_questions: 5,
                max_working_memory_items: 5,
                max_item_chars: 200,
            },
            policy_result: &result,
            retrieval: None,
            mcp_snapshot: None,
            diagnostics: vec![DiagnosticEvidence {
                path: "main.rs".into(),
                message: "unused".into(),
                level: "warning".into(),
            }],
            vcs_context: Vec::new(),
        });
        assert_eq!(pkg.evidence.len(), 1);
        assert_eq!(pkg.evidence[0].kind, "diagnostic");
        assert_eq!(pkg.policy_trace.verdict, "allow");
    }
}
