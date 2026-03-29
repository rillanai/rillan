package agent

import (
	"fmt"
	"strings"
)

type ActionKind string

const (
	ActionKindApplyPatch ActionKind = "apply_patch"
	ActionKindRunTests   ActionKind = "run_tests"
)

type ActionRequest struct {
	Kind    ActionKind        `json:"kind"`
	Summary string            `json:"summary"`
	Payload map[string]string `json:"payload,omitempty"`
}

type ActionProposal struct {
	ID        string            `json:"id"`
	Kind      ActionKind        `json:"kind"`
	Summary   string            `json:"summary"`
	Payload   map[string]string `json:"payload,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Status    string            `json:"status"`
}

func validateActionRequest(req ActionRequest) error {
	switch req.Kind {
	case ActionKindApplyPatch, ActionKindRunTests:
	default:
		return fmt.Errorf("unsupported action kind %q", req.Kind)
	}
	if strings.TrimSpace(req.Summary) == "" {
		return fmt.Errorf("action summary must not be empty")
	}
	return nil
}
