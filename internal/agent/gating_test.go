package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rillanai/rillan/internal/audit"
)

func TestApprovalGateProposeRecordsAuditEvent(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	gate := NewApprovalGate(store)
	proposal, err := gate.Propose(context.Background(), "req-1", ActionRequest{Kind: ActionKindApplyPatch, Summary: "apply a repo patch"})
	if err != nil {
		t.Fatalf("Propose returned error: %v", err)
	}
	if got, want := proposal.Status, "pending"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	if got, want := events[0].Type, audit.EventTypeAgentProposal; got != want {
		t.Fatalf("event type = %q, want %q", got, want)
	}
}

func TestApprovalGateExecuteRequiresApproval(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	gate := NewApprovalGate(store)
	proposal, err := gate.Propose(context.Background(), "req-1", ActionRequest{Kind: ActionKindRunTests, Summary: "run targeted tests"})
	if err != nil {
		t.Fatalf("Propose returned error: %v", err)
	}

	executed := false
	err = gate.Execute(context.Background(), proposal, false, func(context.Context) error {
		executed = true
		return nil
	})
	if err != ErrApprovalRequired {
		t.Fatalf("Execute error = %v, want %v", err, ErrApprovalRequired)
	}
	if executed {
		t.Fatal("expected execute callback not to run")
	}

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := events[len(events)-1].Type, audit.EventTypeAgentDenied; got != want {
		t.Fatalf("last event type = %q, want %q", got, want)
	}
}

func TestApprovalGateExecuteApprovedRunsCallbackAndRecordsAudit(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	gate := NewApprovalGate(store)
	proposal, err := gate.Propose(context.Background(), "req-1", ActionRequest{Kind: ActionKindRunTests, Summary: "run targeted tests"})
	if err != nil {
		t.Fatalf("Propose returned error: %v", err)
	}

	executed := false
	err = gate.Execute(context.Background(), proposal, true, func(context.Context) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !executed {
		t.Fatal("expected execute callback to run")
	}

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := events[len(events)-1].Type, audit.EventTypeAgentApproved; got != want {
		t.Fatalf("last event type = %q, want %q", got, want)
	}
}

func TestApprovalGateResolveFindsAndUpdatesStoredProposal(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	gate := NewApprovalGate(store)
	proposal, err := gate.Propose(context.Background(), "req-1", ActionRequest{Kind: ActionKindApplyPatch, Summary: "apply a repo patch"})
	if err != nil {
		t.Fatalf("Propose returned error: %v", err)
	}
	updated, err := gate.Resolve(context.Background(), proposal.ID, true, nil)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := updated.Status, "approved"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
