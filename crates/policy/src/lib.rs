// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Regex-based secret scanner + policy evaluator. Mirrors `internal/policy`.

mod evaluator;
mod scanner;
mod types;

pub use evaluator::{merge_runtime_policy, Evaluator, EvaluatorError};
pub use scanner::{Rule, Scanner};
pub use types::{
    ActionType, DecisionSource, EvaluationInput, EvaluationPhase, EvaluationResult, ExecutionMode,
    Finding, FindingAction, IntentClassification, PolicyTrace, RetrievalPlan, RuntimePolicy,
    RuntimePolicyTrace, ScanResult, Sensitivity, Verdict,
};
