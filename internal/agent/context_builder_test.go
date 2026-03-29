package agent

import (
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/retrieval"
)

func TestBuildContextPackagePreservesEvidenceAndPolicyTrace(t *testing.T) {
	pkg := BuildContextPackage(BuildInput{
		Goal:             "summarize repo risk",
		ExecutionMode:    "plan_first",
		CurrentStep:      "collect repo evidence",
		RepoRoot:         "/repo",
		ApprovalRequired: true,
		AllowedEffects:   []string{"read"},
		ForbiddenEffects: []string{"write"},
		Facts:            []FactItem{{Key: "branch", Value: "main"}},
		OpenQuestions:    []string{"What changed recently?"},
		WorkingMemory:    []string{"Need bounded summary"},
		OutputKind:       "summary",
		OutputNote:       "Return short summary",
		Budget:           BudgetSection{MaxEvidenceItems: 5, MaxFacts: 5, MaxOpenQuestions: 5, MaxWorkingMemoryItems: 5, MaxItemChars: 120},
		PolicyResult: policy.EvaluationResult{
			Verdict: policy.VerdictAllow,
			Reason:  "policy_allow",
			Trace:   policy.PolicyTrace{Phase: policy.EvaluationPhasePreflight, RouteSource: policy.DecisionSourceProject},
		},
		Retrieval: &retrieval.DebugMetadata{
			Enabled:  true,
			Query:    "repo risk",
			Compiled: retrieval.CompiledContext{Sources: []retrieval.SourceReference{{DocumentPath: "docs/guide.md", StartLine: 1, EndLine: 2}}},
		},
		Diagnostics: []DiagnosticEvidence{{Path: "internal/httpapi/chat_completions_handler.go", Message: "example warning", Level: "warning"}},
		VCSContext:  []FactItem{{Key: "branch", Value: "main"}},
	})

	if got, want := pkg.PolicyTrace.RouteSource, "project"; got != want {
		t.Fatalf("route source = %q, want %q", got, want)
	}
	if got := len(pkg.Evidence); got < 2 {
		t.Fatalf("evidence len = %d, want at least 2", got)
	}
	if pkg.Evidence[0].Kind == "" {
		t.Fatal("expected evidence kinds to be populated")
	}
}

func TestBuildContextPackageDoesNotPassRawTranscript(t *testing.T) {
	rawTranscript := "user: here is the whole chat transcript\nassistant: and more transcript"
	pkg := BuildContextPackage(BuildInput{
		Goal:          "review patch",
		ExecutionMode: "plan_first",
		Budget:        BudgetSection{MaxEvidenceItems: 2, MaxFacts: 2, MaxOpenQuestions: 2, MaxWorkingMemoryItems: 2, MaxItemChars: 120},
		OpenQuestions: []string{rawTranscript},
	})

	for _, evidence := range pkg.Evidence {
		if strings.Contains(evidence.Summary, "whole chat transcript") {
			t.Fatalf("raw transcript leaked into evidence: %#v", pkg.Evidence)
		}
	}
	if got, want := len(pkg.OpenQuestions), 1; got != want {
		t.Fatalf("open questions len = %d, want %d", got, want)
	}
}
