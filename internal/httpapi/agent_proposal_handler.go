// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rillanai/rillan/internal/agent"
	internalopenai "github.com/rillanai/rillan/internal/openai"
)

type AgentProposalDecisionRequest struct {
	Approved bool `json:"approved"`
}

type AgentProposalHandler struct {
	logger *slog.Logger
	gate   *agent.ApprovalGate
}

func NewAgentProposalHandler(logger *slog.Logger, gate *agent.ApprovalGate) *AgentProposalHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentProposalHandler{logger: logger, gate: gate}
}

func (h *AgentProposalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
		return
	}
	proposalID := strings.TrimPrefix(r.URL.Path, "/v1/agent/proposals/")
	proposalID = strings.TrimSuffix(proposalID, "/decision")
	proposalID = strings.Trim(proposalID, "/")
	if proposalID == "" {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "proposal id must not be empty")
		return
	}
	var req AgentProposalDecisionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body must be valid JSON")
		return
	}
	proposal, err := h.gate.Resolve(r.Context(), proposalID, req.Approved, nil)
	if err != nil && !errors.Is(err, agent.ErrApprovalRequired) {
		if errors.Is(err, agent.ErrProposalNotFound) {
			internalopenai.WriteError(w, http.StatusNotFound, "not_found_error", err.Error())
			return
		}
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(fromAgentActionProposal(proposal))
	h.logger.Info("agent proposal resolved", "request_id", RequestIDFromContext(r.Context()), "proposal_id", proposal.ID, "status", proposal.Status)
}
