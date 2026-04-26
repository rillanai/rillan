// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

use rillan_chat::Request;
use rillan_config::ProjectConfig;

/// Allow / redact / block / local-only verdict surfaced by the evaluator.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Verdict {
    Allow,
    Redact,
    Block,
    LocalOnly,
}

impl Verdict {
    /// Wire-format string used by the audit ledger and HTTP error envelopes.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Allow => "allow",
            Self::Redact => "redact",
            Self::Block => "block",
            Self::LocalOnly => "local_only",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EvaluationPhase {
    Preflight,
    Egress,
}

impl EvaluationPhase {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Preflight => "preflight",
            Self::Egress => "egress",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DecisionSource {
    Default,
    Project,
    System,
    Classifier,
    Scan,
}

impl DecisionSource {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Default => "default",
            Self::Project => "project",
            Self::System => "system",
            Self::Classifier => "classifier",
            Self::Scan => "scan",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FindingAction {
    Redact,
    Block,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ActionType {
    CodeDiagnosis,
    CodeGeneration,
    Architecture,
    Explanation,
    Refactor,
    Review,
    GeneralQa,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Sensitivity {
    Public,
    Internal,
    Proprietary,
    TradeSecret,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ExecutionMode {
    Direct,
    PlanFirst,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Finding {
    pub rule_id: String,
    pub category: String,
    pub action: FindingAction,
    pub start: usize,
    pub end: usize,
    pub length: usize,
    pub replacement: String,
}

#[derive(Debug, Clone, Default)]
pub struct ScanResult {
    pub findings: Vec<Finding>,
    pub redacted_body: Vec<u8>,
    pub has_blocking_findings: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct IntentClassification {
    pub action: ActionType,
    pub sensitivity: Sensitivity,
    pub requires_context: bool,
    pub execution_mode: ExecutionMode,
    pub confidence: f64,
}

impl Default for IntentClassification {
    fn default() -> Self {
        Self {
            action: ActionType::GeneralQa,
            sensitivity: Sensitivity::Public,
            requires_context: false,
            execution_mode: ExecutionMode::Direct,
            confidence: 0.0,
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct RuntimePolicy {
    pub project: ProjectConfig,
    pub force_local_for_trade_secret: bool,
    pub minimize_remote_context: bool,
    pub remote_retrieval_top_k: usize,
    pub remote_max_context_chars: usize,
    pub trace: RuntimePolicyTrace,
}

#[derive(Debug, Clone, Default)]
pub struct RuntimePolicyTrace {
    pub project_classification_source: Option<DecisionSource>,
    pub force_local_for_trade_secret_source: Option<DecisionSource>,
}

#[derive(Debug, Clone, Default)]
pub struct PolicyTrace {
    pub phase: Option<EvaluationPhase>,
    pub route_source: Option<DecisionSource>,
}

#[derive(Debug, Clone, Default)]
pub struct RetrievalPlan {
    pub apply: bool,
    pub top_k_cap: usize,
    pub max_context_chars: usize,
    pub source: Option<DecisionSource>,
}

#[derive(Debug, Clone)]
pub struct EvaluationInput {
    pub project: ProjectConfig,
    pub runtime: RuntimePolicy,
    pub request: Option<Request>,
    pub body: Vec<u8>,
    pub scan: ScanResult,
    pub classification: Option<IntentClassification>,
    pub phase: Option<EvaluationPhase>,
}

#[derive(Debug, Clone)]
pub struct EvaluationResult {
    pub verdict: Verdict,
    pub reason: String,
    pub request: Option<Request>,
    pub body: Vec<u8>,
    pub findings: Vec<Finding>,
    pub trace: PolicyTrace,
    pub retrieval: RetrievalPlan,
}
