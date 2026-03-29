package agent

import (
	"context"
	"encoding/json"

	"github.com/sidekickos/rillan/internal/agent/skills"
)

type Runner interface {
	Run(ctx context.Context, profile RoleProfile, pkg ContextPackage) (RunResult, error)
}

type SharedRunner struct {
	registry *skills.Registry
}

type RunResult struct {
	Role         Role                   `json:"role"`
	Summary      string                 `json:"summary"`
	Decision     *OrchestrationDecision `json:"decision,omitempty"`
	SkillResults []SkillResult          `json:"skill_results,omitempty"`
	ContextEcho  ContextPackage         `json:"context_echo"`
}

func NewRunner() *SharedRunner {
	return &SharedRunner{registry: skills.NewRegistry()}
}

func (r *SharedRunner) Run(ctx context.Context, profile RoleProfile, pkg ContextPackage) (RunResult, error) {
	if err := ctx.Err(); err != nil {
		return RunResult{}, err
	}

	result := RunResult{
		Role:        profile.Role,
		Summary:     profile.Description,
		ContextEcho: ApplyBudget(pkg),
	}
	if profile.Role == RoleOrchestrator {
		decision := DecideExecutionMode(result.ContextEcho)
		result.Decision = &decision
	}
	skillResults, err := r.runSkillInvocations(ctx, result.ContextEcho.SkillInvocations)
	if err != nil {
		return RunResult{}, err
	}
	result.SkillResults = skillResults

	return result, nil
}

func (r *SharedRunner) runSkillInvocations(ctx context.Context, invocations []SkillInvocation) ([]SkillResult, error) {
	results := make([]SkillResult, 0, len(invocations))
	for _, invocation := range invocations {
		result, err := r.runSkillInvocation(ctx, invocation)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *SharedRunner) runSkillInvocation(ctx context.Context, invocation SkillInvocation) (SkillResult, error) {
	var payload any
	var err error
	switch invocation.Kind {
	case SkillKindReadFiles:
		payload, err = r.registry.ReadFiles(ctx, skills.ReadFilesRequest{RepoRoot: invocation.RepoRoot, Paths: invocation.Paths})
	case SkillKindSearchRepo:
		payload, err = r.registry.SearchRepo(ctx, skills.SearchRepoRequest{RepoRoot: invocation.RepoRoot, Query: invocation.Query})
	case SkillKindIndexLookup:
		payload, err = r.registry.IndexLookup(ctx, skills.IndexLookupRequest{DBPath: invocation.DBPath, Query: invocation.Query})
	case SkillKindGitStatus:
		payload, err = r.registry.GitStatus(ctx, skills.GitStatusRequest{RepoRoot: invocation.RepoRoot})
	case SkillKindGitDiff:
		payload, err = r.registry.GitDiff(ctx, skills.GitDiffRequest{RepoRoot: invocation.RepoRoot, StagedOnly: invocation.StagedOnly})
	default:
		return SkillResult{}, nil
	}
	if err != nil {
		return SkillResult{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return SkillResult{}, err
	}
	return SkillResult{Kind: invocation.Kind, Payload: encoded}, nil
}
