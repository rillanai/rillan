package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/rillanai/rillan/internal/audit"
)

var ErrApprovalRequired = errors.New("action approval required")

type ApprovalGate struct {
	recorder audit.Recorder
	store    *ProposalStore
	counter  atomic.Uint64
}

func NewApprovalGate(recorder audit.Recorder) *ApprovalGate {
	return &ApprovalGate{recorder: recorder, store: NewProposalStore()}
}

func (g *ApprovalGate) Propose(ctx context.Context, requestID string, req ActionRequest) (ActionProposal, error) {
	if err := validateActionRequest(req); err != nil {
		return ActionProposal{}, err
	}
	proposal := ActionProposal{
		ID:        fmt.Sprintf("proposal-%d", g.counter.Add(1)),
		Kind:      req.Kind,
		Summary:   req.Summary,
		Payload:   clonePayload(req.Payload),
		RequestID: requestID,
		Status:    "pending",
	}
	g.store.Put(proposal)
	g.record(ctx, audit.Event{Type: audit.EventTypeAgentProposal, RequestID: requestID, Verdict: proposal.Status, Reason: string(req.Kind)})
	return proposal, nil
}

func (g *ApprovalGate) Resolve(ctx context.Context, proposalID string, approved bool, execute func(context.Context) error) (ActionProposal, error) {
	proposal, err := g.store.Get(proposalID)
	if err != nil {
		return ActionProposal{}, err
	}
	if proposal.Status != "pending" {
		return proposal, fmt.Errorf("proposal %s is already %s", proposal.ID, proposal.Status)
	}
	status := "approved"
	eventType := audit.EventTypeAgentApproved
	if !approved {
		status = "denied"
		eventType = audit.EventTypeAgentDenied
	}
	updated, err := g.store.UpdateStatus(proposalID, status)
	if err != nil {
		return ActionProposal{}, err
	}
	g.record(ctx, audit.Event{Type: eventType, RequestID: updated.RequestID, Verdict: updated.Status, Reason: string(updated.Kind)})
	if !approved {
		return updated, ErrApprovalRequired
	}
	if execute != nil {
		if err := execute(ctx); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

func (g *ApprovalGate) Execute(ctx context.Context, proposal ActionProposal, approved bool, execute func(context.Context) error) error {
	if !approved {
		g.record(ctx, audit.Event{Type: audit.EventTypeAgentDenied, RequestID: proposal.RequestID, Verdict: "denied", Reason: string(proposal.Kind)})
		return ErrApprovalRequired
	}
	g.record(ctx, audit.Event{Type: audit.EventTypeAgentApproved, RequestID: proposal.RequestID, Verdict: "approved", Reason: string(proposal.Kind)})
	if execute == nil {
		return nil
	}
	return execute(ctx)
}

func (g *ApprovalGate) record(ctx context.Context, event audit.Event) {
	if g == nil || g.recorder == nil {
		return
	}
	_ = g.recorder.Record(ctx, event)
}

func clonePayload(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
