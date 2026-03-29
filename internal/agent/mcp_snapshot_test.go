package agent

import "testing"

func TestNormalizeMCPSnapshotBoundsOptionalFields(t *testing.T) {
	snapshot := NormalizeMCPSnapshot(MCPSnapshot{
		OpenFiles:   []MCPFileRef{{Path: "one"}, {Path: "two"}, {Path: "three"}},
		Selection:   &MCPSelection{Path: "file.go", Snippet: "abcdefghijklmnopqrstuvwxyz", Start: 1, End: 5},
		Diagnostics: []MCPDiagnostic{{Path: "a.go", Severity: "warning", Message: "one"}, {Path: "b.go", Severity: "error", Message: "two"}, {Path: "c.go", Severity: "info", Message: "three"}},
		VCS:         &MCPVCSContext{Branch: "feature/branch", Head: "abcdef", Dirty: true},
	}, MCPSnapshotOptions{MaxOpenFiles: 2, MaxDiagnostics: 2, MaxChars: 10})

	if got, want := len(snapshot.OpenFiles), 2; got != want {
		t.Fatalf("open files len = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Diagnostics), 2; got != want {
		t.Fatalf("diagnostics len = %d, want %d", got, want)
	}
	if got := snapshot.Selection.Snippet; len(got) > 10+len("...[truncated]") {
		t.Fatalf("selection snippet too long: %q", got)
	}
}

func TestBuildContextPackageIncludesMCPSnapshotWithoutChangingApprovalBoundary(t *testing.T) {
	pkg := BuildContextPackage(BuildInput{
		Goal:             "inspect current editor state",
		ApprovalRequired: true,
		AllowedEffects:   []string{"read"},
		ForbiddenEffects: []string{"write"},
		Budget:           BudgetSection{MaxEvidenceItems: 6, MaxFacts: 6, MaxOpenQuestions: 2, MaxWorkingMemoryItems: 2, MaxItemChars: 80},
		MCPSnapshot: &MCPSnapshot{
			OpenFiles:   []MCPFileRef{{Path: "internal/httpapi/chat_completions_handler.go"}},
			Selection:   &MCPSelection{Path: "internal/httpapi/chat_completions_handler.go", Snippet: "func (h *ChatCompletionsHandler) ServeHTTP", Start: 109, End: 150},
			Diagnostics: []MCPDiagnostic{{Path: "internal/httpapi/chat_completions_handler.go", Severity: "warning", Message: "example diagnostic"}},
			VCS:         &MCPVCSContext{Branch: "main", Head: "abcdef", Dirty: true},
		},
	})

	if !pkg.Constraints.ApprovalRequired {
		t.Fatal("mcp snapshot should not bypass approval requirement")
	}
	if got := len(pkg.Evidence); got == 0 {
		t.Fatal("expected mcp snapshot evidence to be included")
	}
	if got := len(pkg.Facts); got == 0 {
		t.Fatal("expected mcp vcs facts to be included")
	}
}
