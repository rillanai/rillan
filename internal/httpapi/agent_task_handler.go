package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rillanai/rillan/internal/agent"
	skilltools "github.com/rillanai/rillan/internal/agent/skills"
	internalopenai "github.com/rillanai/rillan/internal/openai"
)

type AgentTaskHandler struct {
	logger            *slog.Logger
	runner            agent.Runner
	gate              *agent.ApprovalGate
	runtimeSnapshot   RuntimeSnapshotFunc
	approvedRepoRoots []string
}

func NewAgentTaskHandler(logger *slog.Logger, gate *agent.ApprovalGate, runtimeSnapshot RuntimeSnapshotFunc, approvedRepoRoots []string) *AgentTaskHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if gate == nil {
		gate = agent.NewApprovalGate(nil)
	}
	return &AgentTaskHandler{
		logger:            logger,
		runner:            agent.NewRunner(approvedRepoRoots),
		gate:              gate,
		runtimeSnapshot:   runtimeSnapshot,
		approvedRepoRoots: append([]string(nil), approvedRepoRoots...),
	}
}

func (h *AgentTaskHandler) currentApprovedRepoRoots() []string {
	if h.runtimeSnapshot == nil {
		return append([]string(nil), h.approvedRepoRoots...)
	}
	snapshot := h.runtimeSnapshot()
	if snapshot.Config.Server.Host == "" {
		return append([]string(nil), h.approvedRepoRoots...)
	}
	return buildApprovedRepoRoots(snapshot.Config)
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
	approvedRepoRoots := h.currentApprovedRepoRoots()
	if req.RepoRoot != "" {
		resolvedRoot, err := skilltools.ResolveApprovedRepoRoot(req.RepoRoot, approvedRepoRoots)
		if err != nil {
			writeAgentTaskRequestError(w, err)
			return
		}
		req.RepoRoot = resolvedRoot
	}
	for i := range req.SkillInvocations {
		if req.SkillInvocations[i].RepoRoot == "" {
			continue
		}
		resolvedRoot, err := skilltools.ResolveApprovedRepoRoot(req.SkillInvocations[i].RepoRoot, approvedRepoRoots)
		if err != nil {
			writeAgentTaskRequestError(w, err)
			return
		}
		req.SkillInvocations[i].RepoRoot = resolvedRoot
	}

	pkg := agent.BuildContextPackage(agent.BuildInput{
		Goal:             req.Goal,
		ExecutionMode:    req.ExecutionMode,
		CurrentStep:      req.CurrentStep,
		RepoRoot:         req.RepoRoot,
		ApprovalRequired: true,
		AllowedEffects:   []string{"read", "propose_write", "propose_execute"},
		ForbiddenEffects: []string{"write", "execute"},
		SkillInvocations: toAgentSkillInvocations(req.SkillInvocations),
		MCPSnapshot:      toAgentMCPSnapshot(req.MCPSnapshot),
		OutputKind:       "agent_task_response",
		OutputNote:       "Return orchestration result and optional proposal.",
		Budget:           agent.BudgetSection{MaxEvidenceItems: 8, MaxFacts: 8, MaxOpenQuestions: 4, MaxWorkingMemoryItems: 4, MaxItemChars: 240},
	})
	result, err := h.runner.Run(r.Context(), agent.DefaultRoleProfiles()[agent.RoleOrchestrator], pkg)
	if err != nil {
		if errors.Is(err, skilltools.ErrUnapprovedRepoRoot) {
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		internalopenai.WriteError(w, http.StatusInternalServerError, "runtime_error", err.Error())
		return
	}

	response := AgentTaskResponse{Result: fromAgentRunResult(result)}
	if req.ProposedAction != nil {
		proposalRequest := toAgentActionRequest(req.ProposedAction)
		proposal, err := h.gate.Propose(r.Context(), RequestIDFromContext(r.Context()), *proposalRequest)
		if err != nil {
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		transportProposal := fromAgentActionProposal(proposal)
		response.Proposal = &transportProposal
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
	h.logger.Info("agent task processed", "request_id", RequestIDFromContext(r.Context()), "goal", req.Goal, "proposal", response.Proposal != nil)
}

func writeAgentTaskRequestError(w http.ResponseWriter, err error) {
	internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
}
