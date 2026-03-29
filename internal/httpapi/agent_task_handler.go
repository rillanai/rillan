package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/agent"
	"github.com/sidekickos/rillan/internal/audit"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

type AgentTaskRequest struct {
	Goal             string                  `json:"goal"`
	ExecutionMode    string                  `json:"execution_mode,omitempty"`
	CurrentStep      string                  `json:"current_step,omitempty"`
	RepoRoot         string                  `json:"repo_root,omitempty"`
	SkillInvocations []agent.SkillInvocation `json:"skill_invocations,omitempty"`
	MCPSnapshot      *agent.MCPSnapshot      `json:"mcp_snapshot,omitempty"`
	ProposedAction   *agent.ActionRequest    `json:"proposed_action,omitempty"`
}

type AgentTaskResponse struct {
	Result   agent.RunResult       `json:"result"`
	Proposal *agent.ActionProposal `json:"proposal,omitempty"`
}

type AgentTaskHandler struct {
	logger *slog.Logger
	runner agent.Runner
	gate   *agent.ApprovalGate
}

func NewAgentTaskHandler(logger *slog.Logger, recorder audit.Recorder) *AgentTaskHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentTaskHandler{
		logger: logger,
		runner: agent.NewRunner(),
		gate:   agent.NewApprovalGate(recorder),
	}
}

func (h *AgentTaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
		return
	}

	var req AgentTaskRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body must be valid JSON")
		return
	}
	if req.Goal == "" {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "goal must not be empty")
		return
	}

	pkg := agent.BuildContextPackage(agent.BuildInput{
		Goal:             req.Goal,
		ExecutionMode:    req.ExecutionMode,
		CurrentStep:      req.CurrentStep,
		RepoRoot:         req.RepoRoot,
		ApprovalRequired: true,
		AllowedEffects:   []string{"read", "propose_write", "propose_execute"},
		ForbiddenEffects: []string{"write", "execute"},
		SkillInvocations: req.SkillInvocations,
		MCPSnapshot:      req.MCPSnapshot,
		OutputKind:       "agent_task_response",
		OutputNote:       "Return orchestration result and optional proposal.",
		Budget:           agent.BudgetSection{MaxEvidenceItems: 8, MaxFacts: 8, MaxOpenQuestions: 4, MaxWorkingMemoryItems: 4, MaxItemChars: 240},
	})
	result, err := h.runner.Run(r.Context(), agent.DefaultRoleProfiles()[agent.RoleOrchestrator], pkg)
	if err != nil {
		internalopenai.WriteError(w, http.StatusInternalServerError, "runtime_error", err.Error())
		return
	}

	response := AgentTaskResponse{Result: result}
	if req.ProposedAction != nil {
		proposal, err := h.gate.Propose(r.Context(), RequestIDFromContext(r.Context()), *req.ProposedAction)
		if err != nil {
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		response.Proposal = &proposal
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
	h.logger.Info("agent task processed", "request_id", RequestIDFromContext(r.Context()), "goal", req.Goal, "proposal", response.Proposal != nil)
}
