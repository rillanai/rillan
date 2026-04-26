// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Context budgeting. Mirrors `internal/agent/context_budget.go`.

use crate::context_package::{ContextPackage, EvidenceItem, FactItem};

/// Truncates `pkg` to the limits in its `budget`. Returns the truncated
/// package; the input may be partially mutated.
#[must_use]
pub fn apply_budget(mut pkg: ContextPackage) -> ContextPackage {
    let max_chars = pkg.budget.max_item_chars;
    pkg.evidence = trim_evidence(pkg.evidence, pkg.budget.max_evidence_items, max_chars);
    pkg.facts = trim_facts(pkg.facts, pkg.budget.max_facts, max_chars);
    pkg.open_questions = trim_strings(pkg.open_questions, pkg.budget.max_open_questions, max_chars);
    pkg.working_memory = trim_strings(
        pkg.working_memory,
        pkg.budget.max_working_memory_items,
        max_chars,
    );
    pkg.task.goal = trim_text(pkg.task.goal, max_chars);
    pkg.task.current_step = trim_text(pkg.task.current_step, max_chars);
    pkg.output_schema.note = trim_text(pkg.output_schema.note, max_chars);
    pkg
}

fn trim_evidence(items: Vec<EvidenceItem>, limit: usize, max_chars: usize) -> Vec<EvidenceItem> {
    items
        .into_iter()
        .take(limit)
        .map(|item| EvidenceItem {
            kind: trim_text(item.kind, max_chars),
            path: trim_text(item.path, max_chars),
            summary: trim_text(item.summary, max_chars),
            ref_: trim_text(item.ref_, max_chars),
        })
        .collect()
}

fn trim_facts(items: Vec<FactItem>, limit: usize, max_chars: usize) -> Vec<FactItem> {
    items
        .into_iter()
        .take(limit)
        .map(|item| FactItem {
            key: trim_text(item.key, max_chars),
            value: trim_text(item.value, max_chars),
        })
        .collect()
}

fn trim_strings(items: Vec<String>, limit: usize, max_chars: usize) -> Vec<String> {
    items
        .into_iter()
        .take(limit)
        .map(|s| trim_text(s, max_chars))
        .collect()
}

pub(crate) fn trim_text(value: String, max_chars: usize) -> String {
    let trimmed = value.trim();
    if max_chars == 0 || trimmed.len() <= max_chars {
        return trimmed.to_string();
    }
    let suffix = "...[truncated]";
    if max_chars <= suffix.len() {
        return trimmed.chars().take(max_chars).collect();
    }
    let prefix_limit = max_chars - suffix.len();
    let prefix: String = trimmed.chars().take(prefix_limit).collect();
    format!("{}{suffix}", prefix.trim_end())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::context_package::BudgetSection;

    #[test]
    fn trim_text_truncates_long_strings() {
        let trimmed = trim_text("hello world ".repeat(20), 24);
        assert!(trimmed.ends_with("...[truncated]"));
        assert!(trimmed.len() <= 24);
    }

    #[test]
    fn apply_budget_caps_lists() {
        let pkg = ContextPackage {
            budget: BudgetSection {
                max_evidence_items: 1,
                max_facts: 1,
                max_open_questions: 1,
                max_working_memory_items: 1,
                max_item_chars: 100,
            },
            evidence: vec![
                EvidenceItem {
                    kind: "a".into(),
                    summary: "1".into(),
                    ..EvidenceItem::default()
                },
                EvidenceItem {
                    kind: "b".into(),
                    summary: "2".into(),
                    ..EvidenceItem::default()
                },
            ],
            facts: vec![
                FactItem {
                    key: "k".into(),
                    value: "v".into(),
                },
                FactItem {
                    key: "k2".into(),
                    value: "v2".into(),
                },
            ],
            open_questions: vec!["q1".into(), "q2".into()],
            working_memory: vec!["w1".into(), "w2".into()],
            ..ContextPackage::default()
        };
        let out = apply_budget(pkg);
        assert_eq!(out.evidence.len(), 1);
        assert_eq!(out.facts.len(), 1);
        assert_eq!(out.open_questions.len(), 1);
        assert_eq!(out.working_memory.len(), 1);
    }
}
