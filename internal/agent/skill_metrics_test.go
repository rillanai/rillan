// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rillanai/rillan/internal/policy"
)

func TestRecordSkillLatencyPersistsOutsideConfigAndIndex(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	if err := RecordSkillLatency("read_files", 25*time.Millisecond, time.Date(2026, 3, 29, 18, 30, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RecordSkillLatency returned error: %v", err)
	}
	if _, err := os.Stat(DefaultSkillMetricsPath()); err != nil {
		t.Fatalf("skill metrics file missing: %v", err)
	}
	store, err := LoadSkillMetrics()
	if err != nil {
		t.Fatalf("LoadSkillMetrics returned error: %v", err)
	}
	if got, want := len(store.Skills), 1; got != want {
		t.Fatalf("len(skills) = %d, want %d", got, want)
	}
	if got, want := store.Skills[0].SkillID, "read_files"; got != want {
		t.Fatalf("skill_id = %q, want %q", got, want)
	}
}

func TestSharedRunnerRecordsSkillLatency(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("agent skills can read repo files"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	runner := NewRunner([]string{repo})
	profiles := DefaultRoleProfiles()
	pkg := ContextPackage{
		Task:             TaskSection{Goal: "inspect repo", ExecutionMode: string(policy.ExecutionModeDirect)},
		SkillInvocations: []SkillInvocation{{Kind: SkillKindReadFiles, RepoRoot: repo, Paths: []string{"docs/guide.md"}}},
		Budget:           BudgetSection{MaxEvidenceItems: 2, MaxFacts: 2, MaxOpenQuestions: 2, MaxWorkingMemoryItems: 2, MaxItemChars: 120},
	}
	if _, err := runner.Run(context.Background(), profiles[RoleResearcher], pkg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	store, err := LoadSkillMetrics()
	if err != nil {
		t.Fatalf("LoadSkillMetrics returned error: %v", err)
	}
	if got, want := len(store.Skills), 1; got != want {
		t.Fatalf("len(skills) = %d, want %d", got, want)
	}
	if got, want := store.Skills[0].SkillID, string(SkillKindReadFiles); got != want {
		t.Fatalf("skill_id = %q, want %q", got, want)
	}
	if store.Skills[0].InvocationCount < 1 {
		t.Fatalf("invocation_count = %d, want >= 1", store.Skills[0].InvocationCount)
	}
}
