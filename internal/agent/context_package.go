package agent

import (
	"encoding/json"

	"github.com/rillanai/rillan/internal/policy"
)

type ContextPackage struct {
	Task             TaskSection         `json:"task"`
	Constraints      ConstraintsSection  `json:"constraints"`
	SkillInvocations []SkillInvocation   `json:"skill_invocations,omitempty"`
	Evidence         []EvidenceItem      `json:"evidence,omitempty"`
	Facts            []FactItem          `json:"facts,omitempty"`
	OpenQuestions    []string            `json:"open_questions,omitempty"`
	WorkingMemory    []string            `json:"working_memory,omitempty"`
	OutputSchema     OutputSchemaSection `json:"output_schema"`
	Budget           BudgetSection       `json:"budget"`
	PolicyTrace      PolicyTraceSection  `json:"policy_trace"`
}

type SkillKind string

const (
	SkillKindReadFiles   SkillKind = "read_files"
	SkillKindSearchRepo  SkillKind = "search_repo"
	SkillKindIndexLookup SkillKind = "index_lookup"
	SkillKindGitStatus   SkillKind = "git_status"
	SkillKindGitDiff     SkillKind = "git_diff"
)

type SkillInvocation struct {
	Kind       SkillKind `json:"kind"`
	RepoRoot   string    `json:"repo_root,omitempty"`
	Paths      []string  `json:"paths,omitempty"`
	Query      string    `json:"query,omitempty"`
	DBPath     string    `json:"db_path,omitempty"`
	StagedOnly bool      `json:"staged_only,omitempty"`
}

type SkillResult struct {
	Kind    SkillKind       `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type TaskSection struct {
	Goal          string `json:"goal"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	CurrentStep   string `json:"current_step,omitempty"`
}

type ConstraintsSection struct {
	RepoRoot         string   `json:"repo_root,omitempty"`
	ApprovalRequired bool     `json:"approval_required"`
	AllowedEffects   []string `json:"allowed_effects,omitempty"`
	ForbiddenEffects []string `json:"forbidden_effects,omitempty"`
}

type EvidenceItem struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}

type FactItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type OutputSchemaSection struct {
	Kind string `json:"kind"`
	Note string `json:"note,omitempty"`
}

type BudgetSection struct {
	MaxEvidenceItems      int `json:"max_evidence_items"`
	MaxFacts              int `json:"max_facts"`
	MaxOpenQuestions      int `json:"max_open_questions"`
	MaxWorkingMemoryItems int `json:"max_working_memory_items"`
	MaxItemChars          int `json:"max_item_chars"`
}

type PolicyTraceSection struct {
	Phase       string `json:"phase,omitempty"`
	RouteSource string `json:"route_source,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func PolicyTraceFromResult(result policy.EvaluationResult) PolicyTraceSection {
	return PolicyTraceSection{
		Phase:       string(result.Trace.Phase),
		RouteSource: string(result.Trace.RouteSource),
		Verdict:     string(result.Verdict),
		Reason:      result.Reason,
	}
}
