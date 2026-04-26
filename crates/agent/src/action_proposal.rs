// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Action proposal shapes. Mirrors `internal/agent/action_proposal.go`.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ActionKind {
    ApplyPatch,
    RunTests,
}

impl ActionKind {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::ApplyPatch => "apply_patch",
            Self::RunTests => "run_tests",
        }
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ActionRequest {
    pub kind: Option<ActionKind>,
    pub summary: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub payload: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ActionProposal {
    pub id: String,
    pub kind: Option<ActionKind>,
    pub summary: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub payload: BTreeMap<String, String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub request_id: String,
    pub status: String,
}

#[derive(Debug, Error)]
pub enum ValidationError {
    #[error("unsupported action kind")]
    BadKind,
    #[error("action summary must not be empty")]
    EmptySummary,
}

pub fn validate_action_request(req: &ActionRequest) -> Result<(), ValidationError> {
    req.kind.ok_or(ValidationError::BadKind)?;
    if req.summary.trim().is_empty() {
        return Err(ValidationError::EmptySummary);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_required_fields() {
        let req = ActionRequest {
            kind: Some(ActionKind::ApplyPatch),
            summary: "fix bug".into(),
            payload: BTreeMap::new(),
        };
        assert!(validate_action_request(&req).is_ok());
    }

    #[test]
    fn rejects_empty_summary() {
        let req = ActionRequest {
            kind: Some(ActionKind::RunTests),
            summary: "  ".into(),
            payload: BTreeMap::new(),
        };
        assert!(matches!(
            validate_action_request(&req),
            Err(ValidationError::EmptySummary),
        ));
    }
}
