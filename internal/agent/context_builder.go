package agent

import (
	"fmt"
	"strings"

	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/retrieval"
)

type BuildInput struct {
	Goal             string
	ExecutionMode    string
	CurrentStep      string
	RepoRoot         string
	ApprovalRequired bool
	AllowedEffects   []string
	ForbiddenEffects []string
	SkillInvocations []SkillInvocation
	Facts            []FactItem
	OpenQuestions    []string
	WorkingMemory    []string
	OutputKind       string
	OutputNote       string
	Budget           BudgetSection
	PolicyResult     policy.EvaluationResult
	Retrieval        *retrieval.DebugMetadata
	MCPSnapshot      *MCPSnapshot
	Diagnostics      []DiagnosticEvidence
	VCSContext       []FactItem
}

type DiagnosticEvidence struct {
	Path    string
	Message string
	Level   string
}

func BuildContextPackage(input BuildInput) ContextPackage {
	evidence := make([]EvidenceItem, 0)
	facts := append([]FactItem(nil), input.Facts...)
	facts = append(facts, input.VCSContext...)

	if input.Retrieval != nil {
		evidence = append(evidence, evidenceFromRetrieval(*input.Retrieval)...)
	}
	if input.MCPSnapshot != nil {
		normalized := NormalizeMCPSnapshot(*input.MCPSnapshot, MCPSnapshotOptions{MaxOpenFiles: input.Budget.MaxEvidenceItems, MaxDiagnostics: input.Budget.MaxEvidenceItems, MaxChars: input.Budget.MaxItemChars})
		evidence = append(evidence, evidenceFromMCPSnapshot(normalized)...)
		facts = append(facts, factsFromMCPSnapshot(normalized)...)
	}
	for _, diagnostic := range input.Diagnostics {
		evidence = append(evidence, EvidenceItem{
			Kind:    "diagnostic",
			Path:    diagnostic.Path,
			Summary: strings.TrimSpace(fmt.Sprintf("[%s] %s", diagnostic.Level, diagnostic.Message)),
			Ref:     diagnostic.Path,
		})
	}

	pkg := ContextPackage{
		Task: TaskSection{
			Goal:          input.Goal,
			ExecutionMode: input.ExecutionMode,
			CurrentStep:   input.CurrentStep,
		},
		Constraints: ConstraintsSection{
			RepoRoot:         input.RepoRoot,
			ApprovalRequired: input.ApprovalRequired,
			AllowedEffects:   append([]string(nil), input.AllowedEffects...),
			ForbiddenEffects: append([]string(nil), input.ForbiddenEffects...),
		},
		SkillInvocations: append([]SkillInvocation(nil), input.SkillInvocations...),
		Evidence:         evidence,
		Facts:            facts,
		OpenQuestions:    append([]string(nil), input.OpenQuestions...),
		WorkingMemory:    append([]string(nil), input.WorkingMemory...),
		OutputSchema: OutputSchemaSection{
			Kind: input.OutputKind,
			Note: input.OutputNote,
		},
		Budget:      input.Budget,
		PolicyTrace: PolicyTraceFromResult(input.PolicyResult),
	}

	return ApplyBudget(pkg)
}

func evidenceFromRetrieval(metadata retrieval.DebugMetadata) []EvidenceItem {
	items := make([]EvidenceItem, 0, len(metadata.Compiled.Sources)+1)
	if query := strings.TrimSpace(metadata.Query); query != "" {
		items = append(items, EvidenceItem{Kind: "retrieval_query", Summary: query})
	}
	for _, source := range metadata.Compiled.Sources {
		ref := fmt.Sprintf("%s:%d-%d", source.DocumentPath, source.StartLine, source.EndLine)
		items = append(items, EvidenceItem{
			Kind:    "retrieval_source",
			Path:    source.DocumentPath,
			Summary: ref,
			Ref:     ref,
		})
	}
	return items
}

func evidenceFromMCPSnapshot(snapshot MCPSnapshot) []EvidenceItem {
	items := make([]EvidenceItem, 0, len(snapshot.OpenFiles)+len(snapshot.Diagnostics)+1)
	for _, file := range snapshot.OpenFiles {
		items = append(items, EvidenceItem{Kind: "mcp_open_file", Path: file.Path, Summary: file.Path, Ref: file.Path})
	}
	if snapshot.Selection != nil {
		items = append(items, EvidenceItem{Kind: "mcp_selection", Path: snapshot.Selection.Path, Summary: snapshot.Selection.Path, Ref: snapshot.Selection.Path})
	}
	for _, diagnostic := range snapshot.Diagnostics {
		items = append(items, EvidenceItem{Kind: "mcp_diagnostic", Path: diagnostic.Path, Summary: fmt.Sprintf("[%s] %s", diagnostic.Severity, diagnostic.Message), Ref: diagnostic.Path})
	}
	return items
}

func factsFromMCPSnapshot(snapshot MCPSnapshot) []FactItem {
	if snapshot.VCS == nil {
		return nil
	}
	return []FactItem{{Key: "mcp_branch", Value: snapshot.VCS.Branch}, {Key: "mcp_head", Value: snapshot.VCS.Head}, {Key: "mcp_dirty", Value: fmt.Sprintf("%t", snapshot.VCS.Dirty)}}
}
