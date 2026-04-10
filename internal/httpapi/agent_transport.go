// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"

	"github.com/rillanai/rillan/internal/agent"
)

type AgentTaskRequest struct {
	Goal             string                 `json:"goal"`
	ExecutionMode    string                 `json:"execution_mode,omitempty"`
	CurrentStep      string                 `json:"current_step,omitempty"`
	RepoRoot         string                 `json:"repo_root,omitempty"`
	SkillInvocations []AgentSkillInvocation `json:"skill_invocations,omitempty"`
	MCPSnapshot      *AgentMCPSnapshot      `json:"mcp_snapshot,omitempty"`
	ProposedAction   *AgentActionRequest    `json:"proposed_action,omitempty"`
}

type AgentTaskResponse struct {
	Result   AgentRunResult       `json:"result"`
	Proposal *AgentActionProposal `json:"proposal,omitempty"`
}

type AgentSkillInvocation struct {
	Kind       string   `json:"kind"`
	RepoRoot   string   `json:"repo_root,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	Query      string   `json:"query,omitempty"`
	DBPath     string   `json:"db_path,omitempty"`
	StagedOnly bool     `json:"staged_only,omitempty"`
}

type AgentMCPSnapshot struct {
	OpenFiles   []AgentMCPFileRef    `json:"open_files,omitempty"`
	Selection   *AgentMCPSelection   `json:"selection,omitempty"`
	Diagnostics []AgentMCPDiagnostic `json:"diagnostics,omitempty"`
	VCS         *AgentMCPVCSContext  `json:"vcs,omitempty"`
}

type AgentMCPFileRef struct {
	Path string `json:"path"`
}
type AgentMCPSelection struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}
type AgentMCPDiagnostic struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
type AgentMCPVCSContext struct {
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Dirty  bool   `json:"dirty"`
}

type AgentActionRequest struct {
	Kind    string            `json:"kind"`
	Summary string            `json:"summary"`
	Payload map[string]string `json:"payload,omitempty"`
}

type AgentActionProposal struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Summary   string            `json:"summary"`
	Payload   map[string]string `json:"payload,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Status    string            `json:"status"`
}

type AgentRunResult struct {
	Role         string                      `json:"role"`
	Summary      string                      `json:"summary"`
	Decision     *AgentOrchestrationDecision `json:"decision,omitempty"`
	SkillResults []AgentSkillResult          `json:"skill_results,omitempty"`
	ContextEcho  AgentContextPackage         `json:"context_echo"`
}

type AgentOrchestrationDecision struct {
	ExecutionMode string `json:"execution_mode"`
	NextRole      string `json:"next_role"`
	Reason        string `json:"reason"`
}

type AgentSkillResult struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type AgentContextPackage struct {
	Task             AgentTaskSection         `json:"task"`
	Constraints      AgentConstraintsSection  `json:"constraints"`
	SkillInvocations []AgentSkillInvocation   `json:"skill_invocations,omitempty"`
	Evidence         []AgentEvidenceItem      `json:"evidence,omitempty"`
	Facts            []AgentFactItem          `json:"facts,omitempty"`
	OpenQuestions    []string                 `json:"open_questions,omitempty"`
	WorkingMemory    []string                 `json:"working_memory,omitempty"`
	OutputSchema     AgentOutputSchemaSection `json:"output_schema"`
	Budget           AgentBudgetSection       `json:"budget"`
	PolicyTrace      AgentPolicyTraceSection  `json:"policy_trace"`
}

type AgentTaskSection struct {
	Goal          string `json:"goal"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	CurrentStep   string `json:"current_step,omitempty"`
}
type AgentConstraintsSection struct {
	RepoRoot         string   `json:"repo_root,omitempty"`
	ApprovalRequired bool     `json:"approval_required"`
	AllowedEffects   []string `json:"allowed_effects,omitempty"`
	ForbiddenEffects []string `json:"forbidden_effects,omitempty"`
}
type AgentEvidenceItem struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}
type AgentFactItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type AgentOutputSchemaSection struct {
	Kind string `json:"kind"`
	Note string `json:"note,omitempty"`
}
type AgentBudgetSection struct {
	MaxEvidenceItems      int `json:"max_evidence_items"`
	MaxFacts              int `json:"max_facts"`
	MaxOpenQuestions      int `json:"max_open_questions"`
	MaxWorkingMemoryItems int `json:"max_working_memory_items"`
	MaxItemChars          int `json:"max_item_chars"`
}
type AgentPolicyTraceSection struct {
	Phase       string `json:"phase,omitempty"`
	RouteSource string `json:"route_source,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func toAgentSkillInvocation(dto AgentSkillInvocation) agent.SkillInvocation {
	return agent.SkillInvocation{Kind: agent.SkillKind(dto.Kind), RepoRoot: dto.RepoRoot, Paths: append([]string(nil), dto.Paths...), Query: dto.Query, DBPath: dto.DBPath, StagedOnly: dto.StagedOnly}
}

func toAgentSkillInvocations(dtos []AgentSkillInvocation) []agent.SkillInvocation {
	invocations := make([]agent.SkillInvocation, 0, len(dtos))
	for _, dto := range dtos {
		invocations = append(invocations, toAgentSkillInvocation(dto))
	}
	return invocations
}

func toAgentMCPSnapshot(dto *AgentMCPSnapshot) *agent.MCPSnapshot {
	if dto == nil {
		return nil
	}
	result := &agent.MCPSnapshot{OpenFiles: make([]agent.MCPFileRef, 0, len(dto.OpenFiles)), Diagnostics: make([]agent.MCPDiagnostic, 0, len(dto.Diagnostics))}
	for _, file := range dto.OpenFiles {
		result.OpenFiles = append(result.OpenFiles, agent.MCPFileRef{Path: file.Path})
	}
	if dto.Selection != nil {
		result.Selection = &agent.MCPSelection{Path: dto.Selection.Path, Snippet: dto.Selection.Snippet, Start: dto.Selection.Start, End: dto.Selection.End}
	}
	for _, diagnostic := range dto.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, agent.MCPDiagnostic{Path: diagnostic.Path, Severity: diagnostic.Severity, Message: diagnostic.Message})
	}
	if dto.VCS != nil {
		result.VCS = &agent.MCPVCSContext{Branch: dto.VCS.Branch, Head: dto.VCS.Head, Dirty: dto.VCS.Dirty}
	}
	return result
}

func toAgentActionRequest(dto *AgentActionRequest) *agent.ActionRequest {
	if dto == nil {
		return nil
	}
	return &agent.ActionRequest{Kind: agent.ActionKind(dto.Kind), Summary: dto.Summary, Payload: cloneStringMap(dto.Payload)}
}

func fromAgentActionProposal(proposal agent.ActionProposal) AgentActionProposal {
	return AgentActionProposal{ID: proposal.ID, Kind: string(proposal.Kind), Summary: proposal.Summary, Payload: cloneStringMap(proposal.Payload), RequestID: proposal.RequestID, Status: proposal.Status}
}

func fromAgentRunResult(result agent.RunResult) AgentRunResult {
	transport := AgentRunResult{Role: string(result.Role), Summary: result.Summary, ContextEcho: fromAgentContextPackage(result.ContextEcho), SkillResults: make([]AgentSkillResult, 0, len(result.SkillResults))}
	if result.Decision != nil {
		transport.Decision = &AgentOrchestrationDecision{ExecutionMode: string(result.Decision.ExecutionMode), NextRole: string(result.Decision.NextRole), Reason: result.Decision.Reason}
	}
	for _, skillResult := range result.SkillResults {
		transport.SkillResults = append(transport.SkillResults, AgentSkillResult{Kind: string(skillResult.Kind), Payload: append(json.RawMessage(nil), skillResult.Payload...)})
	}
	return transport
}

func fromAgentContextPackage(pkg agent.ContextPackage) AgentContextPackage {
	transport := AgentContextPackage{Task: AgentTaskSection{Goal: pkg.Task.Goal, ExecutionMode: pkg.Task.ExecutionMode, CurrentStep: pkg.Task.CurrentStep}, Constraints: AgentConstraintsSection{RepoRoot: pkg.Constraints.RepoRoot, ApprovalRequired: pkg.Constraints.ApprovalRequired, AllowedEffects: append([]string(nil), pkg.Constraints.AllowedEffects...), ForbiddenEffects: append([]string(nil), pkg.Constraints.ForbiddenEffects...)}, OpenQuestions: append([]string(nil), pkg.OpenQuestions...), WorkingMemory: append([]string(nil), pkg.WorkingMemory...), OutputSchema: AgentOutputSchemaSection{Kind: pkg.OutputSchema.Kind, Note: pkg.OutputSchema.Note}, Budget: AgentBudgetSection{MaxEvidenceItems: pkg.Budget.MaxEvidenceItems, MaxFacts: pkg.Budget.MaxFacts, MaxOpenQuestions: pkg.Budget.MaxOpenQuestions, MaxWorkingMemoryItems: pkg.Budget.MaxWorkingMemoryItems, MaxItemChars: pkg.Budget.MaxItemChars}, PolicyTrace: AgentPolicyTraceSection{Phase: pkg.PolicyTrace.Phase, RouteSource: pkg.PolicyTrace.RouteSource, Verdict: pkg.PolicyTrace.Verdict, Reason: pkg.PolicyTrace.Reason}, SkillInvocations: make([]AgentSkillInvocation, 0, len(pkg.SkillInvocations)), Evidence: make([]AgentEvidenceItem, 0, len(pkg.Evidence)), Facts: make([]AgentFactItem, 0, len(pkg.Facts))}
	for _, invocation := range pkg.SkillInvocations {
		transport.SkillInvocations = append(transport.SkillInvocations, AgentSkillInvocation{Kind: string(invocation.Kind), RepoRoot: invocation.RepoRoot, Paths: append([]string(nil), invocation.Paths...), Query: invocation.Query, DBPath: invocation.DBPath, StagedOnly: invocation.StagedOnly})
	}
	for _, evidence := range pkg.Evidence {
		transport.Evidence = append(transport.Evidence, AgentEvidenceItem{Kind: evidence.Kind, Path: evidence.Path, Summary: evidence.Summary, Ref: evidence.Ref})
	}
	for _, fact := range pkg.Facts {
		transport.Facts = append(transport.Facts, AgentFactItem{Key: fact.Key, Value: fact.Value})
	}
	return transport
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
