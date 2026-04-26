// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Approval gate. Mirrors `internal/agent/gating.go`.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use rillan_audit::{
    Event, Recorder, EVENT_TYPE_AGENT_APPROVED, EVENT_TYPE_AGENT_DENIED, EVENT_TYPE_AGENT_PROPOSAL,
};
use thiserror::Error;

use crate::action_proposal::{
    validate_action_request, ActionProposal, ActionRequest, ValidationError,
};
use crate::proposal_store::{ProposalError, ProposalStore};

#[derive(Debug, Error)]
pub enum GatingError {
    #[error("validation: {0}")]
    Validate(#[from] ValidationError),
    #[error("proposal: {0}")]
    Proposal(#[from] ProposalError),
    #[error("proposal {id} is already {status}")]
    AlreadyResolved { id: String, status: String },
    #[error("action approval required")]
    ApprovalRequired(ActionProposal),
}

impl GatingError {
    /// Extracts the resolved proposal from an `ApprovalRequired` error. Used
    /// by the HTTP layer, which mirrors the Go semantics of returning a 200
    /// with the denied proposal payload.
    pub fn into_denied_proposal(self) -> Result<ActionProposal, Box<Self>> {
        match self {
            Self::ApprovalRequired(proposal) => Ok(proposal),
            other => Err(Box::new(other)),
        }
    }
}

/// Approval gate. Runs proposals through the audit ledger and an in-memory
/// store; later resolution emits matching `agent_action_approved` /
/// `agent_action_denied` events.
pub struct ApprovalGate {
    recorder: Option<Arc<dyn Recorder>>,
    store: ProposalStore,
    counter: AtomicU64,
}

impl ApprovalGate {
    #[must_use]
    pub fn new(recorder: Option<Arc<dyn Recorder>>) -> Self {
        Self {
            recorder,
            store: ProposalStore::new(),
            counter: AtomicU64::new(0),
        }
    }

    /// Records a new proposal and emits an `agent_action_proposed` audit event.
    pub async fn propose(
        &self,
        request_id: &str,
        request: ActionRequest,
    ) -> Result<ActionProposal, GatingError> {
        validate_action_request(&request)?;
        let id = self.counter.fetch_add(1, Ordering::SeqCst).wrapping_add(1);
        let proposal = ActionProposal {
            id: format!("proposal-{id}"),
            kind: request.kind,
            summary: request.summary.clone(),
            payload: request.payload.clone(),
            request_id: request_id.to_string(),
            status: "pending".to_string(),
        };
        self.store.put(proposal.clone());
        self.record(Event {
            kind: EVENT_TYPE_AGENT_PROPOSAL.into(),
            request_id: request_id.to_string(),
            verdict: proposal.status.clone(),
            reason: kind_label(&proposal),
            ..Event::default()
        })
        .await;
        Ok(proposal)
    }

    /// Resolves an existing proposal. When `approved` is false, returns
    /// `Err(GatingError::ApprovalRequired)` after emitting the `denied` event.
    pub async fn resolve(
        &self,
        proposal_id: &str,
        approved: bool,
    ) -> Result<ActionProposal, GatingError> {
        let proposal = self.store.get(proposal_id)?;
        if proposal.status != "pending" {
            return Err(GatingError::AlreadyResolved {
                id: proposal.id,
                status: proposal.status,
            });
        }
        let new_status = if approved { "approved" } else { "denied" };
        let event_type = if approved {
            EVENT_TYPE_AGENT_APPROVED
        } else {
            EVENT_TYPE_AGENT_DENIED
        };
        let updated = self.store.update_status(proposal_id, new_status)?;
        self.record(Event {
            kind: event_type.into(),
            request_id: updated.request_id.clone(),
            verdict: updated.status.clone(),
            reason: kind_label(&updated),
            ..Event::default()
        })
        .await;
        if !approved {
            return Err(GatingError::ApprovalRequired(updated));
        }
        Ok(updated)
    }

    /// Number of proposals currently in the store.
    #[must_use]
    pub fn proposal_count(&self) -> usize {
        self.store.count()
    }

    async fn record(&self, event: Event) {
        if let Some(recorder) = &self.recorder {
            let _ = recorder.record(event).await;
        }
    }
}

impl std::fmt::Debug for ApprovalGate {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ApprovalGate")
            .field("has_recorder", &self.recorder.is_some())
            .field("proposals", &self.store.count())
            .finish()
    }
}

fn kind_label(proposal: &ActionProposal) -> String {
    proposal
        .kind
        .map_or_else(String::new, |k| k.as_str().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::action_proposal::ActionKind;
    use std::sync::Mutex;

    #[derive(Default)]
    struct CapturingRecorder {
        events: Mutex<Vec<Event>>,
    }

    #[async_trait::async_trait]
    impl Recorder for CapturingRecorder {
        async fn record(&self, event: Event) -> Result<(), rillan_audit::Error> {
            self.events.lock().unwrap().push(event);
            Ok(())
        }
    }

    #[tokio::test]
    async fn propose_emits_audit_event_and_stores_proposal() {
        let recorder = Arc::new(CapturingRecorder::default());
        let gate = ApprovalGate::new(Some(recorder.clone() as Arc<dyn Recorder>));
        let proposal = gate
            .propose(
                "req-1",
                ActionRequest {
                    kind: Some(ActionKind::ApplyPatch),
                    summary: "fix bug".into(),
                    ..ActionRequest::default()
                },
            )
            .await
            .unwrap();
        assert_eq!(proposal.status, "pending");
        assert_eq!(gate.proposal_count(), 1);
        let events = recorder.events.lock().unwrap();
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].kind, EVENT_TYPE_AGENT_PROPOSAL);
    }

    #[tokio::test]
    async fn resolve_denied_emits_denied_event() {
        let recorder = Arc::new(CapturingRecorder::default());
        let gate = ApprovalGate::new(Some(recorder.clone() as Arc<dyn Recorder>));
        let proposal = gate
            .propose(
                "req-1",
                ActionRequest {
                    kind: Some(ActionKind::RunTests),
                    summary: "smoke".into(),
                    ..ActionRequest::default()
                },
            )
            .await
            .unwrap();
        let err = gate.resolve(&proposal.id, false).await.expect_err("denied");
        let denied = err
            .into_denied_proposal()
            .map_err(|e| *e)
            .expect("denied proposal");
        assert_eq!(denied.status, "denied");
        let events = recorder.events.lock().unwrap();
        assert_eq!(events.last().unwrap().kind, EVENT_TYPE_AGENT_DENIED);
    }

    #[tokio::test]
    async fn resolve_already_resolved_proposal_errors() {
        let gate = ApprovalGate::new(None);
        let proposal = gate
            .propose(
                "req-1",
                ActionRequest {
                    kind: Some(ActionKind::ApplyPatch),
                    summary: "fix".into(),
                    ..ActionRequest::default()
                },
            )
            .await
            .unwrap();
        gate.resolve(&proposal.id, true).await.unwrap();
        let err = gate.resolve(&proposal.id, true).await.expect_err("twice");
        assert!(matches!(err, GatingError::AlreadyResolved { .. }));
    }
}
