package agent

import (
	"context"
	"errors"
	"time"

	toolskills "github.com/rillanai/rillan/internal/agent/skills"
)

type Runner interface {
	Run(ctx context.Context, profile RoleProfile, pkg ContextPackage) (RunResult, error)
}

type SharedRunner struct {
	tools ToolExecutor
}

type RunResult struct {
	Role         Role                   `json:"role"`
	Summary      string                 `json:"summary"`
	Decision     *OrchestrationDecision `json:"decision,omitempty"`
	SkillResults []SkillResult          `json:"skill_results,omitempty"`
	ContextEcho  ContextPackage         `json:"context_echo"`
}

func NewRunner(approvedRepoRoots []string) *SharedRunner {
	return &SharedRunner{tools: NewReadOnlyToolRuntime(approvedRepoRoots)}
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
	startedAt := time.Now()
	result, err := r.tools.ExecuteTool(ctx, ToolCall{Name: string(invocation.Kind), RepoRoot: invocation.RepoRoot, Paths: invocation.Paths, Query: invocation.Query, DBPath: invocation.DBPath, StagedOnly: invocation.StagedOnly})
	if err != nil {
		if errors.Is(err, toolskills.ErrUnknownReadOnlyTool) {
			return SkillResult{}, nil
		}
		return SkillResult{}, err
	}
	_ = RecordSkillLatency(string(invocation.Kind), time.Since(startedAt), startedAt)
	return SkillResult{Kind: invocation.Kind, Payload: result.Payload}, nil
}
