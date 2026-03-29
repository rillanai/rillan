package agent

import "strings"

func ApplyBudget(pkg ContextPackage) ContextPackage {
	pkg.Evidence = trimEvidence(pkg.Evidence, pkg.Budget.MaxEvidenceItems, pkg.Budget.MaxItemChars)
	pkg.Facts = trimFacts(pkg.Facts, pkg.Budget.MaxFacts, pkg.Budget.MaxItemChars)
	pkg.OpenQuestions = trimStrings(pkg.OpenQuestions, pkg.Budget.MaxOpenQuestions, pkg.Budget.MaxItemChars)
	pkg.WorkingMemory = trimStrings(pkg.WorkingMemory, pkg.Budget.MaxWorkingMemoryItems, pkg.Budget.MaxItemChars)
	pkg.Task.Goal = trimText(pkg.Task.Goal, pkg.Budget.MaxItemChars)
	pkg.Task.CurrentStep = trimText(pkg.Task.CurrentStep, pkg.Budget.MaxItemChars)
	pkg.OutputSchema.Note = trimText(pkg.OutputSchema.Note, pkg.Budget.MaxItemChars)
	return pkg
}

func trimEvidence(items []EvidenceItem, limit int, maxChars int) []EvidenceItem {
	items = items[:min(limit, len(items))]
	trimmed := make([]EvidenceItem, 0, len(items))
	for _, item := range items {
		trimmed = append(trimmed, EvidenceItem{
			Kind:    trimText(item.Kind, maxChars),
			Path:    trimText(item.Path, maxChars),
			Summary: trimText(item.Summary, maxChars),
			Ref:     trimText(item.Ref, maxChars),
		})
	}
	return trimmed
}

func trimFacts(items []FactItem, limit int, maxChars int) []FactItem {
	items = items[:min(limit, len(items))]
	trimmed := make([]FactItem, 0, len(items))
	for _, item := range items {
		trimmed = append(trimmed, FactItem{
			Key:   trimText(item.Key, maxChars),
			Value: trimText(item.Value, maxChars),
		})
	}
	return trimmed
}

func trimStrings(items []string, limit int, maxChars int) []string {
	items = items[:min(limit, len(items))]
	trimmed := make([]string, 0, len(items))
	for _, item := range items {
		trimmed = append(trimmed, trimText(item, maxChars))
	}
	return trimmed
}

func trimText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if maxChars < 1 || len(value) <= maxChars {
		return value
	}
	if maxChars <= len("...[truncated]") {
		return value[:maxChars]
	}
	return strings.TrimSpace(value[:maxChars-len("...[truncated]")]) + "...[truncated]"
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
