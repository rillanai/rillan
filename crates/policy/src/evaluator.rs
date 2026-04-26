// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Policy evaluator. Mirrors `internal/policy/evaluator.go`.

use rillan_config::{
    ProjectConfig, SystemConfig, PROJECT_CLASSIFICATION_OPEN_SOURCE,
    PROJECT_CLASSIFICATION_TRADE_SECRET,
};
use thiserror::Error;

use crate::types::{
    DecisionSource, EvaluationInput, EvaluationPhase, EvaluationResult, PolicyTrace, RetrievalPlan,
    RuntimePolicy, RuntimePolicyTrace, Sensitivity, Verdict,
};

#[derive(Debug, Error)]
pub enum EvaluatorError {
    #[error("decode redacted body: {0}")]
    DecodeRedactedBody(#[source] serde_json::Error),
}

/// Default evaluator. Stateless; equivalent to Go's `DefaultEvaluator`.
#[derive(Debug, Default, Clone)]
pub struct Evaluator;

impl Evaluator {
    #[must_use]
    pub fn new() -> Self {
        Self
    }

    /// Evaluates a request and returns the verdict + (possibly redacted) body.
    pub fn evaluate(&self, input: EvaluationInput) -> Result<EvaluationResult, EvaluatorError> {
        let mut runtime_policy = input.runtime.clone();
        if runtime_policy.project.name.is_empty()
            && runtime_policy.project.classification.is_empty()
        {
            runtime_policy = merge_runtime_policy(None, &input.project);
        }
        let phase = input.phase.unwrap_or(EvaluationPhase::Egress);

        let mut result = EvaluationResult {
            verdict: Verdict::Allow,
            reason: "policy_allow".to_string(),
            request: input.request.clone(),
            body: input.body.clone(),
            findings: input.scan.findings.clone(),
            trace: PolicyTrace {
                phase: Some(phase),
                route_source: Some(DecisionSource::Default),
            },
            retrieval: RetrievalPlan {
                source: Some(DecisionSource::Default),
                ..RetrievalPlan::default()
            },
        };

        let classification = normalize_policy_string(&runtime_policy.project.classification);
        let classification = if classification.is_empty() {
            PROJECT_CLASSIFICATION_OPEN_SOURCE.to_string()
        } else {
            classification
        };

        if input.scan.has_blocking_findings {
            result.verdict = Verdict::Block;
            result.reason = "secret_scan_block".to_string();
            result.trace.route_source = Some(DecisionSource::Scan);
            if !input.scan.redacted_body.is_empty() {
                sync_request_from_body(&mut result, &input.scan.redacted_body)?;
            }
            return Ok(result);
        }

        if runtime_policy.force_local_for_trade_secret
            && input
                .classification
                .as_ref()
                .is_some_and(|c| c.sensitivity == Sensitivity::TradeSecret)
        {
            result.verdict = Verdict::LocalOnly;
            result.reason = "system_trade_secret".to_string();
            result.trace.route_source = runtime_policy.trace.force_local_for_trade_secret_source;
            if !input.scan.redacted_body.is_empty() && !input.scan.findings.is_empty() {
                sync_request_from_body(&mut result, &input.scan.redacted_body)?;
            }
            return Ok(result);
        }

        if input
            .classification
            .as_ref()
            .is_some_and(|c| c.sensitivity == Sensitivity::TradeSecret)
        {
            result.verdict = Verdict::LocalOnly;
            result.reason = "classifier_trade_secret".to_string();
            result.trace.route_source = Some(DecisionSource::Classifier);
            if !input.scan.redacted_body.is_empty() && !input.scan.findings.is_empty() {
                sync_request_from_body(&mut result, &input.scan.redacted_body)?;
            }
            return Ok(result);
        }

        if classification == PROJECT_CLASSIFICATION_TRADE_SECRET {
            result.verdict = Verdict::LocalOnly;
            result.reason = "project_trade_secret".to_string();
            result.trace.route_source = runtime_policy.trace.project_classification_source;
            if !input.scan.redacted_body.is_empty() && !input.scan.findings.is_empty() {
                sync_request_from_body(&mut result, &input.scan.redacted_body)?;
            }
            return Ok(result);
        }

        if !input.scan.findings.is_empty() {
            result.verdict = Verdict::Redact;
            result.reason = "secret_scan_redact".to_string();
            result.trace.route_source = Some(DecisionSource::Scan);
            sync_request_from_body(&mut result, &input.scan.redacted_body)?;
            return Ok(result);
        }

        if phase == EvaluationPhase::Preflight && runtime_policy.minimize_remote_context {
            result.retrieval = RetrievalPlan {
                apply: true,
                top_k_cap: runtime_policy.remote_retrieval_top_k,
                max_context_chars: runtime_policy.remote_max_context_chars,
                source: Some(DecisionSource::Default),
            };
        }

        Ok(result)
    }
}

/// Merges a system policy override into the runtime policy. Mirrors
/// `MergeRuntimePolicy` in Go.
#[must_use]
pub fn merge_runtime_policy(
    system: Option<&SystemConfig>,
    project: &ProjectConfig,
) -> RuntimePolicy {
    let mut runtime = RuntimePolicy {
        project: project.clone(),
        force_local_for_trade_secret: false,
        minimize_remote_context: true,
        remote_retrieval_top_k: 2,
        remote_max_context_chars: 1200,
        trace: RuntimePolicyTrace {
            project_classification_source: Some(DecisionSource::Default),
            force_local_for_trade_secret_source: Some(DecisionSource::Default),
        },
    };

    if !runtime.project.classification.is_empty() {
        runtime.trace.project_classification_source = Some(DecisionSource::Project);
    }
    if let Some(system_cfg) = system {
        if system_cfg.policy.rules.force_local_for_trade_secret {
            runtime.force_local_for_trade_secret = true;
            runtime.trace.force_local_for_trade_secret_source = Some(DecisionSource::System);
        }
    }

    runtime
}

fn normalize_policy_string(value: &str) -> String {
    value.trim().to_lowercase()
}

fn sync_request_from_body(
    result: &mut EvaluationResult,
    body: &[u8],
) -> Result<(), EvaluatorError> {
    result.body = body.to_vec();
    if result.request.is_some() {
        let parsed: rillan_chat::Request =
            serde_json::from_slice(body).map_err(EvaluatorError::DecodeRedactedBody)?;
        result.request = Some(parsed);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::{IntentClassification, ScanResult};
    use rillan_config::{
        ProjectConfig, SystemConfig, SystemPolicy, SystemPolicyRules,
        PROJECT_CLASSIFICATION_INTERNAL, PROJECT_CLASSIFICATION_OPEN_SOURCE,
        PROJECT_CLASSIFICATION_PROPRIETARY, PROJECT_CLASSIFICATION_TRADE_SECRET,
    };

    use crate::scanner::Scanner;

    fn project(classification: &str) -> ProjectConfig {
        ProjectConfig {
            name: "demo".into(),
            classification: classification.into(),
            ..ProjectConfig::default()
        }
    }

    fn input(project: ProjectConfig, body: &str, scan: ScanResult) -> EvaluationInput {
        EvaluationInput {
            project,
            runtime: RuntimePolicy::default(),
            request: None,
            body: body.as_bytes().to_vec(),
            scan,
            classification: None,
            phase: None,
        }
    }

    #[test]
    fn open_source_allows_clean_body() {
        let evaluator = Evaluator::new();
        let body = r#"{"messages":[{"role":"user","content":"hello"}]}"#;
        let scan = Scanner::default_scanner().scan(body.as_bytes());
        let result = evaluator
            .evaluate(input(
                project(PROJECT_CLASSIFICATION_OPEN_SOURCE),
                body,
                scan,
            ))
            .expect("evaluate");
        assert_eq!(result.verdict, Verdict::Allow);
        assert_eq!(result.reason, "policy_allow");
    }

    #[test]
    fn proprietary_redacts_secret_findings() {
        let evaluator = Evaluator::new();
        let body = r#"{"token":"sk-1234567890abcdefghijklmnop"}"#;
        let scan = Scanner::default_scanner().scan(body.as_bytes());
        let result = evaluator
            .evaluate(input(
                project(PROJECT_CLASSIFICATION_PROPRIETARY),
                body,
                scan,
            ))
            .expect("evaluate");
        assert_eq!(result.verdict, Verdict::Redact);
        assert_eq!(result.reason, "secret_scan_redact");
        assert!(String::from_utf8(result.body)
            .unwrap()
            .contains("[REDACTED OPENAI API KEY]"));
    }

    #[test]
    fn trade_secret_forces_local_only() {
        let evaluator = Evaluator::new();
        let body = r#"{"messages":[{"role":"user","content":"ship it"}]}"#;
        let scan = Scanner::default_scanner().scan(body.as_bytes());
        let result = evaluator
            .evaluate(input(
                project(PROJECT_CLASSIFICATION_TRADE_SECRET),
                body,
                scan,
            ))
            .expect("evaluate");
        assert_eq!(result.verdict, Verdict::LocalOnly);
        assert_eq!(result.reason, "project_trade_secret");
    }

    #[test]
    fn blocking_findings_override_classification() {
        let evaluator = Evaluator::new();
        let body = r#"{"messages":[{"role":"user","content":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}]}"#;
        let scan = Scanner::default_scanner().scan(body.as_bytes());
        let result = evaluator
            .evaluate(input(
                project(PROJECT_CLASSIFICATION_OPEN_SOURCE),
                body,
                scan,
            ))
            .expect("evaluate");
        assert_eq!(result.verdict, Verdict::Block);
        assert_eq!(result.reason, "secret_scan_block");
    }

    #[test]
    fn classifier_trade_secret_escalates_to_local_only() {
        let evaluator = Evaluator::new();
        let body = r#"{"messages":[{"role":"user","content":"ship it"}]}"#;
        let scan = Scanner::default_scanner().scan(body.as_bytes());
        let mut input = input(project(PROJECT_CLASSIFICATION_INTERNAL), body, scan);
        input.classification = Some(IntentClassification {
            sensitivity: Sensitivity::TradeSecret,
            ..IntentClassification::default()
        });
        let result = evaluator.evaluate(input).expect("evaluate");
        assert_eq!(result.verdict, Verdict::LocalOnly);
        assert_eq!(result.reason, "classifier_trade_secret");
    }

    #[test]
    fn system_rule_can_override_route_source() {
        let evaluator = Evaluator::new();
        let project = project(PROJECT_CLASSIFICATION_OPEN_SOURCE);
        let system = SystemConfig {
            policy: SystemPolicy {
                rules: SystemPolicyRules {
                    force_local_for_trade_secret: true,
                    ..SystemPolicyRules::default()
                },
                ..SystemPolicy::default()
            },
            ..SystemConfig::default()
        };
        let body = r#"{"messages":[{"role":"user","content":"ship it"}]}"#;
        let result = evaluator
            .evaluate(EvaluationInput {
                project: project.clone(),
                runtime: merge_runtime_policy(Some(&system), &project),
                request: None,
                body: body.as_bytes().to_vec(),
                scan: ScanResult {
                    redacted_body: body.as_bytes().to_vec(),
                    ..ScanResult::default()
                },
                classification: Some(IntentClassification {
                    sensitivity: Sensitivity::TradeSecret,
                    ..IntentClassification::default()
                }),
                phase: Some(EvaluationPhase::Preflight),
            })
            .expect("evaluate");
        assert_eq!(result.verdict, Verdict::LocalOnly);
        assert_eq!(result.reason, "system_trade_secret");
        assert_eq!(result.trace.route_source, Some(DecisionSource::System));
    }

    #[test]
    fn preflight_applies_retrieval_minimization() {
        let evaluator = Evaluator::new();
        let project = project(PROJECT_CLASSIFICATION_OPEN_SOURCE);
        let body = r#"{"messages":[{"role":"user","content":"explain this repo"}]}"#;
        let result = evaluator
            .evaluate(EvaluationInput {
                project: project.clone(),
                runtime: merge_runtime_policy(None, &project),
                request: None,
                body: body.as_bytes().to_vec(),
                scan: ScanResult {
                    redacted_body: body.as_bytes().to_vec(),
                    ..ScanResult::default()
                },
                classification: None,
                phase: Some(EvaluationPhase::Preflight),
            })
            .expect("evaluate");
        assert!(result.retrieval.apply);
        assert_eq!(result.retrieval.top_k_cap, 2);
        assert_eq!(result.retrieval.max_context_chars, 1200);
    }
}
