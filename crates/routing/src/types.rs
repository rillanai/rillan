// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

use std::collections::BTreeMap;

use rillan_config::ProjectConfig;
use rillan_policy::{ActionType, Verdict};

/// Local vs remote provider classification.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Location {
    Local,
    Remote,
}

impl Location {
    /// Wire string used in decision traces.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Local => "local",
            Self::Remote => "remote",
        }
    }
}

/// Tracks which layer of config produced the final route preference.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreferenceSource {
    Default,
    ProjectDefault,
    TaskType,
}

impl PreferenceSource {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Default => "default",
            Self::ProjectDefault => "project_default",
            Self::TaskType => "task_type",
        }
    }
}

/// Routing preference resolved against the project config.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPreference {
    pub preference: String,
    pub source: PreferenceSource,
}

/// Per-provider snapshot used by the decision engine.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Candidate {
    pub id: String,
    pub backend: String,
    pub preset: String,
    pub transport: String,
    pub endpoint: String,
    pub default_model: String,
    pub model_pins: Vec<String>,
    pub capabilities: Vec<String>,
    pub location: Option<Location>,
}

/// Set of candidates available to the decision engine.
#[derive(Debug, Clone, Default)]
pub struct Catalog {
    pub candidates: Vec<Candidate>,
    pub by_id: BTreeMap<String, Candidate>,
    pub allowed: bool,
}

/// One full routing decision input.
#[derive(Debug, Clone)]
pub struct DecisionInput {
    pub requested_model: String,
    pub required_capabilities: Vec<String>,
    pub action: Option<ActionType>,
    pub project: ProjectConfig,
    pub policy_verdict: Verdict,
    pub candidates: Vec<Candidate>,
}

/// Output of the decision engine.
#[derive(Debug, Clone)]
pub struct Decision {
    pub preference: ResolvedPreference,
    pub selected: Option<Candidate>,
    pub ranked: Vec<Candidate>,
    pub trace: DecisionTrace,
}

/// Decision-level trace captured for the audit log + decision-trace headers.
#[derive(Debug, Clone)]
pub struct DecisionTrace {
    pub policy_verdict: Verdict,
    pub model_target: String,
    pub model_match: String,
    pub required_capabilities: Vec<String>,
    pub preference: String,
    pub preference_source: PreferenceSource,
    pub candidates: Vec<CandidateTrace>,
}

/// Per-candidate trace.
#[derive(Debug, Clone, Default)]
pub struct CandidateTrace {
    pub id: String,
    pub location: Option<Location>,
    pub eligible: bool,
    pub rejected: bool,
    pub selected: bool,
    pub reason: String,
    pub model_match: bool,
    pub missing_capabilities: Vec<String>,
    pub preference_score: i32,
    pub task_strength: i32,
}
