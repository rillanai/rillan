package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sidekickos/rillan/internal/policy"
)

func TestSharedRunnerReusesOneRuntimeAcrossRoles(t *testing.T) {
	runner := NewRunner()
	profiles := DefaultRoleProfiles()
	pkg := ContextPackage{
		Task:   TaskSection{Goal: "review repo", ExecutionMode: string(policy.ExecutionModePlanFirst)},
		Budget: BudgetSection{MaxEvidenceItems: 2, MaxFacts: 2, MaxOpenQuestions: 2, MaxWorkingMemoryItems: 2, MaxItemChars: 80},
	}

	for _, role := range []Role{RoleOrchestrator, RolePlanner, RoleResearcher, RoleCoder, RoleReviewer} {
		result, err := runner.Run(context.Background(), profiles[role], pkg)
		if err != nil {
			t.Fatalf("Run(%s) returned error: %v", role, err)
		}
		if got, want := result.Role, role; got != want {
			t.Fatalf("result role = %q, want %q", got, want)
		}
	}
}

func TestSharedRunnerExecutesRequestedReadOnlySkills(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("agent skills can read repo files"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	runner := NewRunner()
	profiles := DefaultRoleProfiles()
	pkg := ContextPackage{
		Task:             TaskSection{Goal: "inspect repo", ExecutionMode: string(policy.ExecutionModeDirect)},
		SkillInvocations: []SkillInvocation{{Kind: SkillKindReadFiles, RepoRoot: repo, Paths: []string{"docs/guide.md"}}},
		Budget:           BudgetSection{MaxEvidenceItems: 2, MaxFacts: 2, MaxOpenQuestions: 2, MaxWorkingMemoryItems: 2, MaxItemChars: 120},
	}

	result, err := runner.Run(context.Background(), profiles[RoleResearcher], pkg)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := len(result.SkillResults), 1; got != want {
		t.Fatalf("skill results len = %d, want %d", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.SkillResults[0].Payload, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("unexpected files payload: %#v", payload)
	}
}

func TestSharedRunnerAppliesBudgetBeforeReturningContextEcho(t *testing.T) {
	runner := NewRunner()
	profiles := DefaultRoleProfiles()
	pkg := ContextPackage{
		Task:   TaskSection{Goal: "review repo", ExecutionMode: string(policy.ExecutionModeDirect)},
		Facts:  []FactItem{{Key: "branch", Value: "main"}, {Key: "drop", Value: "me"}},
		Budget: BudgetSection{MaxEvidenceItems: 1, MaxFacts: 1, MaxOpenQuestions: 1, MaxWorkingMemoryItems: 1, MaxItemChars: 80},
	}

	result, err := runner.Run(context.Background(), profiles[RoleResearcher], pkg)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := len(result.ContextEcho.Facts), 1; got != want {
		t.Fatalf("facts len = %d, want %d", got, want)
	}
}
