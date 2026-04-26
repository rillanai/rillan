// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Role catalog. Mirrors `internal/agent/roles.go`.

use std::collections::BTreeMap;

use rillan_policy::ExecutionMode;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Role {
    Orchestrator,
    Planner,
    Researcher,
    Coder,
    Reviewer,
}

impl Role {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Orchestrator => "orchestrator",
            Self::Planner => "planner",
            Self::Researcher => "researcher",
            Self::Coder => "coder",
            Self::Reviewer => "reviewer",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RoleProfile {
    pub role: Role,
    pub description: String,
    pub allowed_effects: Vec<String>,
    pub forbidden_effects: Vec<String>,
}

#[must_use]
pub fn default_role_profiles() -> BTreeMap<Role, RoleProfile> {
    [
        (
            Role::Orchestrator,
            RoleProfile {
                role: Role::Orchestrator,
                description: "Chooses direct vs planned execution and next-step routing.".into(),
                allowed_effects: vec!["read".into()],
                forbidden_effects: vec!["write".into(), "execute".into()],
            },
        ),
        (
            Role::Planner,
            RoleProfile {
                role: Role::Planner,
                description: "Converts goals into bounded implementation or research plans.".into(),
                allowed_effects: vec!["read".into()],
                forbidden_effects: vec!["write".into(), "execute".into()],
            },
        ),
        (
            Role::Researcher,
            RoleProfile {
                role: Role::Researcher,
                description: "Collects repo evidence and index-backed facts.".into(),
                allowed_effects: vec!["read".into()],
                forbidden_effects: vec!["write".into(), "execute".into()],
            },
        ),
        (
            Role::Coder,
            RoleProfile {
                role: Role::Coder,
                description:
                    "Produces bounded code changes only through later approval-gated actions."
                        .into(),
                allowed_effects: vec![
                    "read".into(),
                    "propose_write".into(),
                    "propose_execute".into(),
                ],
                forbidden_effects: vec!["write".into(), "execute".into()],
            },
        ),
        (
            Role::Reviewer,
            RoleProfile {
                role: Role::Reviewer,
                description: "Validates work against plan and policy constraints.".into(),
                allowed_effects: vec!["read".into()],
                forbidden_effects: vec!["write".into(), "execute".into()],
            },
        ),
    ]
    .into_iter()
    .collect()
}

#[derive(Debug, Clone, Serialize)]
pub struct OrchestrationDecision {
    pub execution_mode: ExecutionModeWire,
    pub next_role: Role,
    pub reason: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ExecutionModeWire {
    Direct,
    PlanFirst,
}

impl From<ExecutionMode> for ExecutionModeWire {
    fn from(value: ExecutionMode) -> Self {
        match value {
            ExecutionMode::PlanFirst => Self::PlanFirst,
            ExecutionMode::Direct => Self::Direct,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_role_profiles_lists_five_roles() {
        let profiles = default_role_profiles();
        assert_eq!(profiles.len(), 5);
        assert!(profiles.contains_key(&Role::Orchestrator));
        assert!(profiles.contains_key(&Role::Coder));
    }
}
