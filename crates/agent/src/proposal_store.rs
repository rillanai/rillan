// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! In-memory proposal store. Mirrors `internal/agent/proposal_store.go`.

use std::collections::HashMap;
use std::sync::Mutex;

use thiserror::Error;

use crate::action_proposal::ActionProposal;

#[derive(Debug, Error)]
pub enum ProposalError {
    #[error("proposal not found")]
    NotFound,
}

#[derive(Default)]
pub struct ProposalStore {
    inner: Mutex<HashMap<String, ActionProposal>>,
}

impl std::fmt::Debug for ProposalStore {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ProposalStore").finish_non_exhaustive()
    }
}

impl ProposalStore {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Stores `proposal`, replacing any existing entry with the same id.
    pub fn put(&self, proposal: ActionProposal) {
        let mut map = self.inner.lock().expect("proposal store mutex poisoned");
        map.insert(proposal.id.clone(), proposal);
    }

    /// Returns a clone of the stored proposal.
    pub fn get(&self, id: &str) -> Result<ActionProposal, ProposalError> {
        let map = self.inner.lock().expect("proposal store mutex poisoned");
        map.get(id).cloned().ok_or(ProposalError::NotFound)
    }

    /// Mutates the status of an existing proposal.
    pub fn update_status(&self, id: &str, status: &str) -> Result<ActionProposal, ProposalError> {
        let mut map = self.inner.lock().expect("proposal store mutex poisoned");
        let proposal = map.get_mut(id).ok_or(ProposalError::NotFound)?;
        proposal.status = status.to_string();
        Ok(proposal.clone())
    }

    /// Returns the current proposal count.
    #[must_use]
    pub fn count(&self) -> usize {
        let map = self.inner.lock().expect("proposal store mutex poisoned");
        map.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::action_proposal::ActionKind;

    #[test]
    fn put_then_get_returns_inserted_proposal() {
        let store = ProposalStore::new();
        let proposal = ActionProposal {
            id: "p1".into(),
            kind: Some(ActionKind::ApplyPatch),
            summary: "demo".into(),
            status: "pending".into(),
            ..ActionProposal::default()
        };
        store.put(proposal.clone());
        assert_eq!(store.get("p1").unwrap(), proposal);
        assert_eq!(store.count(), 1);
    }

    #[test]
    fn update_status_transitions_state() {
        let store = ProposalStore::new();
        store.put(ActionProposal {
            id: "p1".into(),
            status: "pending".into(),
            ..ActionProposal::default()
        });
        let updated = store.update_status("p1", "approved").unwrap();
        assert_eq!(updated.status, "approved");
    }
}
