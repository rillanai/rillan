package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/policy"
)

func TestContextPackageJSONRoundTrip(t *testing.T) {
	pkg := ContextPackage{
		Task:          TaskSection{Goal: "review a patch", ExecutionMode: "plan_first"},
		Constraints:   ConstraintsSection{RepoRoot: "/repo", ApprovalRequired: true},
		Evidence:      []EvidenceItem{{Kind: "retrieval_source", Path: "docs/guide.md", Summary: "docs/guide.md:1-2", Ref: "docs/guide.md:1-2"}},
		Facts:         []FactItem{{Key: "branch", Value: "main"}},
		OpenQuestions: []string{"Should this be plan-first?"},
		WorkingMemory: []string{"Need policy-trace-aware response"},
		OutputSchema:  OutputSchemaSection{Kind: "proposal"},
		Budget:        BudgetSection{MaxEvidenceItems: 4, MaxFacts: 4, MaxOpenQuestions: 4, MaxWorkingMemoryItems: 4, MaxItemChars: 120},
		PolicyTrace:   PolicyTraceSection{Phase: "preflight", RouteSource: "project", Verdict: "allow", Reason: "policy_allow"},
	}

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var decoded ContextPackage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := decoded.Task.Goal, "review a patch"; got != want {
		t.Fatalf("goal = %q, want %q", got, want)
	}
	if got, want := decoded.PolicyTrace.RouteSource, "project"; got != want {
		t.Fatalf("route_source = %q, want %q", got, want)
	}
}

func TestApplyBudgetTruncatesAndBoundsSections(t *testing.T) {
	pkg := ContextPackage{
		Task:          TaskSection{Goal: strings.Repeat("goal ", 20)},
		Evidence:      []EvidenceItem{{Kind: "retrieval_source", Summary: strings.Repeat("summary ", 20)}, {Kind: "extra", Summary: "drop me"}},
		Facts:         []FactItem{{Key: "branch", Value: strings.Repeat("main ", 20)}, {Key: "extra", Value: "drop me"}},
		OpenQuestions: []string{strings.Repeat("question ", 20), "drop me"},
		WorkingMemory: []string{strings.Repeat("memory ", 20), "drop me"},
		Budget:        BudgetSection{MaxEvidenceItems: 1, MaxFacts: 1, MaxOpenQuestions: 1, MaxWorkingMemoryItems: 1, MaxItemChars: 24},
	}

	trimmed := ApplyBudget(pkg)
	if got, want := len(trimmed.Evidence), 1; got != want {
		t.Fatalf("evidence len = %d, want %d", got, want)
	}
	if got, want := len(trimmed.Facts), 1; got != want {
		t.Fatalf("facts len = %d, want %d", got, want)
	}
	if got, want := len(trimmed.OpenQuestions), 1; got != want {
		t.Fatalf("open questions len = %d, want %d", got, want)
	}
	if got, want := len(trimmed.WorkingMemory), 1; got != want {
		t.Fatalf("working memory len = %d, want %d", got, want)
	}
	if !strings.Contains(trimmed.Task.Goal, "...[truncated]") {
		t.Fatalf("expected truncated goal, got %q", trimmed.Task.Goal)
	}
}

func TestPolicyTraceFromResult(t *testing.T) {
	trace := PolicyTraceFromResult(policy.EvaluationResult{
		Verdict: policy.VerdictLocalOnly,
		Reason:  "classifier_trade_secret",
		Trace: policy.PolicyTrace{
			Phase:       policy.EvaluationPhasePreflight,
			RouteSource: policy.DecisionSourceClassifier,
		},
	})

	if got, want := trace.RouteSource, "classifier"; got != want {
		t.Fatalf("route source = %q, want %q", got, want)
	}
	if got, want := trace.Verdict, "local_only"; got != want {
		t.Fatalf("verdict = %q, want %q", got, want)
	}
}
