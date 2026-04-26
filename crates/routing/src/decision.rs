// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Decision engine. Mirrors `internal/routing/decision.go`.

use std::collections::BTreeMap;

use rillan_config::{
    ProjectConfig, ROUTE_PREFERENCE_AUTO, ROUTE_PREFERENCE_LOCAL_ONLY,
    ROUTE_PREFERENCE_PREFER_CLOUD, ROUTE_PREFERENCE_PREFER_LOCAL,
};
use rillan_policy::{ActionType, Verdict};

use crate::types::{
    Candidate, CandidateTrace, Decision, DecisionInput, DecisionTrace, Location, PreferenceSource,
    ResolvedPreference,
};

/// Resolves the routing preference for a project + action combination.
#[must_use]
pub fn resolve_preference(
    project: &ProjectConfig,
    action: Option<ActionType>,
) -> ResolvedPreference {
    if let Some(action) = action {
        if let Some(raw) = project.routing.task_types.get(action_type_key(action)) {
            if let Some(preference) = normalize_preference(raw) {
                return ResolvedPreference {
                    preference,
                    source: PreferenceSource::TaskType,
                };
            }
        }
    }
    if let Some(preference) = normalize_preference(&project.routing.default) {
        return ResolvedPreference {
            preference,
            source: PreferenceSource::ProjectDefault,
        };
    }
    ResolvedPreference {
        preference: ROUTE_PREFERENCE_AUTO.to_string(),
        source: PreferenceSource::Default,
    }
}

/// Runs the decision pipeline. Mirrors `Decide` in Go.
#[must_use]
pub fn decide(input: DecisionInput) -> Decision {
    let preference = resolve_preference(&input.project, input.action);
    let requested_model = normalize(&input.requested_model);
    let required_capabilities = normalize_capabilities(&input.required_capabilities);

    let mut states: Vec<CandidateState> = input
        .candidates
        .into_iter()
        .map(|candidate| {
            let model_match = model_matches(&requested_model, &candidate);
            let mut state = CandidateState {
                trace: CandidateTrace {
                    id: candidate.id.clone(),
                    location: candidate.location,
                    model_match,
                    ..CandidateTrace::default()
                },
                candidate,
            };

            // Policy eligibility.
            if let Some(reason) = policy_eligibility(input.policy_verdict, &state.candidate) {
                state.trace.eligible = false;
                state.trace.rejected = true;
                state.trace.reason = reason;
                return state;
            }
            // Capability eligibility.
            let missing =
                missing_capabilities(&required_capabilities, &state.candidate.capabilities);
            if !missing.is_empty() {
                state.trace.eligible = false;
                state.trace.rejected = true;
                state.trace.reason = "missing_required_capabilities".into();
                state.trace.missing_capabilities = missing;
                return state;
            }
            // Route preference eligibility.
            let (score, eligible, reason) =
                route_preference_score(&preference.preference, &state.candidate);
            state.trace.preference_score = score;
            if !eligible {
                state.trace.eligible = false;
                state.trace.rejected = true;
                state.trace.reason = reason;
                return state;
            }
            state.trace.eligible = true;
            state.trace.task_strength = task_strength(input.action, &state.candidate.capabilities);
            state
        })
        .collect();

    apply_model_affinity(&mut states, &requested_model);

    states.sort_by(compare_candidate_state);

    let mut ranked: Vec<Candidate> = Vec::new();
    let mut traces: Vec<CandidateTrace> = Vec::with_capacity(states.len());
    let mut selected: Option<Candidate> = None;
    for state in &mut states {
        if selected.is_none() && state.trace.eligible {
            selected = Some(state.candidate.clone());
            state.trace.selected = true;
        }
        if state.trace.eligible {
            ranked.push(state.candidate.clone());
        }
        traces.push(state.trace.clone());
    }

    let model_match_label = model_match_label(&states, &requested_model);

    Decision {
        preference: preference.clone(),
        selected,
        ranked,
        trace: DecisionTrace {
            policy_verdict: input.policy_verdict,
            model_target: input.requested_model,
            model_match: model_match_label,
            required_capabilities,
            preference: preference.preference,
            preference_source: preference.source,
            candidates: traces,
        },
    }
}

#[derive(Debug)]
struct CandidateState {
    candidate: Candidate,
    trace: CandidateTrace,
}

fn compare_candidate_state(left: &CandidateState, right: &CandidateState) -> std::cmp::Ordering {
    use std::cmp::Ordering;
    if left.trace.eligible != right.trace.eligible {
        return if left.trace.eligible {
            Ordering::Less
        } else {
            Ordering::Greater
        };
    }
    if left.trace.preference_score != right.trace.preference_score {
        return if left.trace.preference_score > right.trace.preference_score {
            Ordering::Less
        } else {
            Ordering::Greater
        };
    }
    if left.trace.model_match != right.trace.model_match {
        return if left.trace.model_match {
            Ordering::Less
        } else {
            Ordering::Greater
        };
    }
    if left.trace.task_strength != right.trace.task_strength {
        return if left.trace.task_strength > right.trace.task_strength {
            Ordering::Less
        } else {
            Ordering::Greater
        };
    }
    left.candidate.id.cmp(&right.candidate.id)
}

fn apply_model_affinity(states: &mut [CandidateState], requested_model: &str) {
    if requested_model.is_empty() {
        return;
    }
    let has_match = states
        .iter()
        .any(|state| state.trace.eligible && state.trace.model_match);
    if !has_match {
        return;
    }
    for state in states.iter_mut() {
        if !state.trace.eligible || state.trace.model_match {
            continue;
        }
        state.trace.eligible = false;
        state.trace.rejected = true;
        state.trace.reason = "requested_model_mismatch".into();
    }
}

fn model_matches(requested_model: &str, candidate: &Candidate) -> bool {
    if requested_model.is_empty() {
        return false;
    }
    candidate
        .model_pins
        .iter()
        .any(|pin| normalize(pin) == requested_model)
        || normalize(&candidate.default_model) == requested_model
}

fn model_match_label(states: &[CandidateState], requested_model: &str) -> String {
    if requested_model.is_empty() {
        return "none".to_string();
    }
    if states.iter().any(|state| state.trace.model_match) {
        "exact".to_string()
    } else {
        "none".to_string()
    }
}

fn policy_eligibility(verdict: Verdict, candidate: &Candidate) -> Option<String> {
    match verdict {
        Verdict::Block => Some("policy_blocked".into()),
        Verdict::LocalOnly => {
            if candidate.location != Some(Location::Local) {
                Some("policy_local_only".into())
            } else {
                None
            }
        }
        Verdict::Allow | Verdict::Redact => None,
    }
}

fn route_preference_score(preference: &str, candidate: &Candidate) -> (i32, bool, String) {
    match normalize_preference(preference).as_deref() {
        Some(ROUTE_PREFERENCE_PREFER_LOCAL) => {
            if candidate.location == Some(Location::Local) {
                (1, true, String::new())
            } else {
                (0, true, String::new())
            }
        }
        Some(ROUTE_PREFERENCE_PREFER_CLOUD) => {
            if candidate.location == Some(Location::Remote) {
                (1, true, String::new())
            } else {
                (0, true, String::new())
            }
        }
        Some(ROUTE_PREFERENCE_LOCAL_ONLY) => {
            if candidate.location == Some(Location::Local) {
                (1, true, String::new())
            } else {
                (0, false, "route_preference_local_only".into())
            }
        }
        _ => (0, true, String::new()),
    }
}

fn task_strength(action: Option<ActionType>, capabilities: &[String]) -> i32 {
    let weights = capability_weights(action);
    if weights.is_empty() {
        return 0;
    }
    let available: BTreeMap<String, ()> = capabilities.iter().map(|c| (normalize(c), ())).collect();
    weights
        .into_iter()
        .filter(|(cap, _)| available.contains_key(*cap))
        .map(|(_, weight)| weight)
        .sum()
}

fn capability_weights(action: Option<ActionType>) -> Vec<(&'static str, i32)> {
    match action {
        Some(ActionType::CodeDiagnosis | ActionType::CodeGeneration | ActionType::Refactor) => {
            vec![("tool_calling", 3), ("reasoning", 2), ("chat", 1)]
        }
        Some(ActionType::Review | ActionType::Architecture | ActionType::Explanation) => {
            vec![("reasoning", 2), ("chat", 1)]
        }
        _ => vec![("chat", 1)],
    }
}

fn normalize_preference(value: &str) -> Option<String> {
    let normalized = normalize(value);
    match normalized.as_str() {
        ROUTE_PREFERENCE_AUTO
        | ROUTE_PREFERENCE_PREFER_LOCAL
        | ROUTE_PREFERENCE_PREFER_CLOUD
        | ROUTE_PREFERENCE_LOCAL_ONLY => Some(normalized),
        _ => None,
    }
}

fn normalize(value: &str) -> String {
    value.trim().to_lowercase()
}

fn normalize_capabilities(values: &[String]) -> Vec<String> {
    let mut seen: BTreeMap<String, ()> = BTreeMap::new();
    for value in values {
        let normalized = normalize(value);
        if normalized.is_empty() {
            continue;
        }
        seen.insert(normalized, ());
    }
    seen.into_keys().collect()
}

fn missing_capabilities(required: &[String], available: &[String]) -> Vec<String> {
    if required.is_empty() {
        return Vec::new();
    }
    let available_set: BTreeMap<String, ()> =
        available.iter().map(|c| (normalize(c), ())).collect();
    required
        .iter()
        .filter(|cap| !available_set.contains_key(cap.as_str()))
        .cloned()
        .collect()
}

const fn action_type_key(action: ActionType) -> &'static str {
    match action {
        ActionType::CodeDiagnosis => "code_diagnosis",
        ActionType::CodeGeneration => "code_generation",
        ActionType::Architecture => "architecture",
        ActionType::Explanation => "explanation",
        ActionType::Refactor => "refactor",
        ActionType::Review => "review",
        ActionType::GeneralQa => "general_qa",
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::ProjectRoutingConfig;

    fn project_with(default: &str, task: Option<(&str, &str)>) -> ProjectConfig {
        let mut task_types = BTreeMap::new();
        if let Some((key, value)) = task {
            task_types.insert(key.to_string(), value.to_string());
        }
        ProjectConfig {
            routing: ProjectRoutingConfig {
                default: default.to_string(),
                task_types,
            },
            ..ProjectConfig::default()
        }
    }

    fn find_trace<'a>(traces: &'a [CandidateTrace], id: &str) -> &'a CandidateTrace {
        traces.iter().find(|c| c.id == id).expect("trace not found")
    }

    #[test]
    fn task_override_wins_over_project_default() {
        let project = project_with(
            ROUTE_PREFERENCE_PREFER_CLOUD,
            Some(("review", ROUTE_PREFERENCE_PREFER_LOCAL)),
        );
        let resolved = resolve_preference(&project, Some(ActionType::Review));
        assert_eq!(resolved.preference, ROUTE_PREFERENCE_PREFER_LOCAL);
        assert_eq!(resolved.source, PreferenceSource::TaskType);

        let resolved = resolve_preference(&project, Some(ActionType::Architecture));
        assert_eq!(resolved.preference, ROUTE_PREFERENCE_PREFER_CLOUD);
        assert_eq!(resolved.source, PreferenceSource::ProjectDefault);
    }

    #[test]
    fn policy_local_only_excludes_remote() {
        let decision = decide(DecisionInput {
            requested_model: String::new(),
            required_capabilities: Vec::new(),
            action: Some(ActionType::CodeGeneration),
            project: project_with(ROUTE_PREFERENCE_PREFER_CLOUD, None),
            policy_verdict: Verdict::LocalOnly,
            candidates: vec![
                Candidate {
                    id: "remote-a".into(),
                    location: Some(Location::Remote),
                    capabilities: vec!["chat".into(), "reasoning".into(), "tool_calling".into()],
                    ..Candidate::default()
                },
                Candidate {
                    id: "local-a".into(),
                    location: Some(Location::Local),
                    capabilities: vec!["chat".into(), "reasoning".into(), "tool_calling".into()],
                    ..Candidate::default()
                },
            ],
        });

        let selected = decision.selected.expect("selected");
        assert_eq!(selected.id, "local-a");
        let rejected = find_trace(&decision.trace.candidates, "remote-a");
        assert!(!rejected.eligible);
        assert_eq!(rejected.reason, "policy_local_only");
        assert_eq!(decision.trace.policy_verdict, Verdict::LocalOnly);
    }

    #[test]
    fn stable_tie_break_by_provider_id() {
        let decision = decide(DecisionInput {
            requested_model: String::new(),
            required_capabilities: Vec::new(),
            action: Some(ActionType::GeneralQa),
            project: project_with(ROUTE_PREFERENCE_AUTO, None),
            policy_verdict: Verdict::Allow,
            candidates: vec![
                Candidate {
                    id: "zeta".into(),
                    location: Some(Location::Remote),
                    capabilities: vec!["chat".into()],
                    ..Candidate::default()
                },
                Candidate {
                    id: "alpha".into(),
                    location: Some(Location::Remote),
                    capabilities: vec!["chat".into()],
                    ..Candidate::default()
                },
            ],
        });
        assert_eq!(decision.selected.unwrap().id, "alpha");
        assert_eq!(decision.ranked[0].id, "alpha");
        assert_eq!(decision.ranked[1].id, "zeta");
    }

    #[test]
    fn requested_model_exact_match_restricts_candidates() {
        let decision = decide(DecisionInput {
            requested_model: "claude-sonnet-4-5".into(),
            required_capabilities: Vec::new(),
            action: Some(ActionType::Review),
            project: project_with(ROUTE_PREFERENCE_PREFER_LOCAL, None),
            policy_verdict: Verdict::Allow,
            candidates: vec![
                Candidate {
                    id: "local-qwen".into(),
                    location: Some(Location::Local),
                    default_model: "qwen3:8b".into(),
                    model_pins: vec!["qwen3:8b".into()],
                    capabilities: vec!["chat".into(), "reasoning".into()],
                    ..Candidate::default()
                },
                Candidate {
                    id: "claude-prod".into(),
                    location: Some(Location::Remote),
                    default_model: "claude-sonnet-4-5".into(),
                    model_pins: vec!["claude-sonnet-4-5".into()],
                    capabilities: vec!["chat".into(), "reasoning".into()],
                    ..Candidate::default()
                },
            ],
        });
        assert_eq!(decision.selected.unwrap().id, "claude-prod");
        assert_eq!(decision.trace.model_match, "exact");
        let rejected = find_trace(&decision.trace.candidates, "local-qwen");
        assert_eq!(rejected.reason, "requested_model_mismatch");
    }

    #[test]
    fn required_capabilities_exclude_candidates_missing_tools() {
        let decision = decide(DecisionInput {
            requested_model: "gpt-5".into(),
            required_capabilities: vec!["tool_calling".into()],
            action: Some(ActionType::CodeGeneration),
            project: project_with(ROUTE_PREFERENCE_AUTO, None),
            policy_verdict: Verdict::Allow,
            candidates: vec![
                Candidate {
                    id: "chat-only".into(),
                    location: Some(Location::Remote),
                    model_pins: vec!["gpt-5".into()],
                    capabilities: vec!["chat".into(), "reasoning".into()],
                    ..Candidate::default()
                },
                Candidate {
                    id: "tool-capable".into(),
                    location: Some(Location::Remote),
                    model_pins: vec!["gpt-5".into()],
                    capabilities: vec!["chat".into(), "reasoning".into(), "tool_calling".into()],
                    ..Candidate::default()
                },
            ],
        });
        assert_eq!(decision.selected.unwrap().id, "tool-capable");
        let rejected = find_trace(&decision.trace.candidates, "chat-only");
        assert_eq!(rejected.reason, "missing_required_capabilities");
        assert_eq!(
            rejected.missing_capabilities,
            vec!["tool_calling".to_string()]
        );
    }

    #[test]
    fn local_only_rejects_remote_via_route_preference() {
        let decision = decide(DecisionInput {
            requested_model: String::new(),
            required_capabilities: Vec::new(),
            action: Some(ActionType::Review),
            project: project_with(ROUTE_PREFERENCE_LOCAL_ONLY, None),
            policy_verdict: Verdict::Allow,
            candidates: vec![
                Candidate {
                    id: "remote-a".into(),
                    location: Some(Location::Remote),
                    capabilities: vec!["chat".into(), "reasoning".into()],
                    ..Candidate::default()
                },
                Candidate {
                    id: "local-a".into(),
                    location: Some(Location::Local),
                    capabilities: vec!["chat".into(), "reasoning".into()],
                    ..Candidate::default()
                },
            ],
        });
        assert_eq!(decision.selected.unwrap().id, "local-a");
        let rejected = find_trace(&decision.trace.candidates, "remote-a");
        assert_eq!(rejected.reason, "route_preference_local_only");
        assert!(rejected.rejected);
    }
}
