// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"regexp"
	"sort"
)

type Rule struct {
	ID          string
	Category    string
	Action      FindingAction
	Pattern     *regexp.Regexp
	Replacement string
}

type Scanner struct {
	rules []Rule
}

type match struct {
	ruleIndex int
	finding   Finding
}

func NewScanner(rules []Rule) *Scanner {
	cloned := make([]Rule, len(rules))
	copy(cloned, rules)
	return &Scanner{rules: cloned}
}

func DefaultScanner() *Scanner {
	return NewScanner([]Rule{
		{
			ID:          "openai_api_key",
			Category:    "api_key",
			Action:      FindingActionRedact,
			Pattern:     regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`),
			Replacement: "[REDACTED OPENAI API KEY]",
		},
		{
			ID:          "github_token",
			Category:    "token",
			Action:      FindingActionRedact,
			Pattern:     regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
			Replacement: "[REDACTED GITHUB TOKEN]",
		},
		{
			ID:          "bearer_token",
			Category:    "authorization",
			Action:      FindingActionRedact,
			Pattern:     regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-+/=]{16,}`),
			Replacement: "Bearer [REDACTED TOKEN]",
		},
		{
			ID:          "aws_access_key",
			Category:    "api_key",
			Action:      FindingActionRedact,
			Pattern:     regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
			Replacement: "[REDACTED AWS ACCESS KEY]",
		},
		{
			ID:          "private_key",
			Category:    "private_key",
			Action:      FindingActionBlock,
			Pattern:     regexp.MustCompile(`-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----`),
			Replacement: "[BLOCKED PRIVATE KEY]",
		},
	})
}

func (s *Scanner) Scan(body []byte) ScanResult {
	text := string(body)
	matches := make([]match, 0)

	for i, rule := range s.rules {
		indices := rule.Pattern.FindAllStringIndex(text, -1)
		for _, index := range indices {
			start, end := index[0], index[1]
			matches = append(matches, match{
				ruleIndex: i,
				finding: Finding{
					RuleID:      rule.ID,
					Category:    rule.Category,
					Action:      rule.Action,
					Start:       start,
					End:         end,
					Length:      end - start,
					Replacement: rule.Replacement,
				},
			})
		}
	}

	if len(matches) == 0 {
		return ScanResult{RedactedBody: append([]byte(nil), body...)}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].finding.Start != matches[j].finding.Start {
			return matches[i].finding.Start < matches[j].finding.Start
		}
		if matches[i].finding.End != matches[j].finding.End {
			return matches[i].finding.End > matches[j].finding.End
		}
		return matches[i].ruleIndex < matches[j].ruleIndex
	})

	selected := make([]match, 0, len(matches))
	lastEnd := -1
	for _, candidate := range matches {
		if candidate.finding.Start < lastEnd {
			continue
		}
		selected = append(selected, candidate)
		lastEnd = candidate.finding.End
	}

	redacted := text
	for i := len(selected) - 1; i >= 0; i-- {
		finding := selected[i].finding
		redacted = redacted[:finding.Start] + finding.Replacement + redacted[finding.End:]
	}

	findings := make([]Finding, 0, len(selected))
	hasBlocking := false
	for _, candidate := range selected {
		findings = append(findings, candidate.finding)
		if candidate.finding.Action == FindingActionBlock {
			hasBlocking = true
		}
	}

	return ScanResult{
		Findings:            findings,
		RedactedBody:        []byte(redacted),
		HasBlockingFindings: hasBlocking,
	}
}
