// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Outbound secret scanner. Mirrors `internal/policy/scanner.go`.

use regex::Regex;

use crate::types::{Finding, FindingAction, ScanResult};

/// One regex-based redaction or blocking rule.
#[derive(Debug, Clone)]
pub struct Rule {
    pub id: String,
    pub category: String,
    pub action: FindingAction,
    pub pattern: Regex,
    pub replacement: String,
}

/// Sequential scanner that runs every rule against the request body.
#[derive(Debug, Clone)]
pub struct Scanner {
    rules: Vec<Rule>,
}

#[derive(Debug, Clone)]
struct Match {
    rule_index: usize,
    finding: Finding,
}

impl Scanner {
    /// Builds a scanner from `rules`.
    #[must_use]
    pub fn new(rules: Vec<Rule>) -> Self {
        Self { rules }
    }

    /// Returns the bundled scanner with the same rule set as the Go daemon.
    #[must_use]
    pub fn default_scanner() -> Self {
        Self::new(default_rules())
    }

    /// Scans `body`, returning the redacted bytes and the list of findings.
    #[must_use]
    pub fn scan(&self, body: &[u8]) -> ScanResult {
        let text = match std::str::from_utf8(body) {
            Ok(value) => value,
            Err(_) => {
                // Non-UTF-8 bodies are passed through untouched. Mirrors the
                // Go scanner, which operates on bytes as if they were UTF-8.
                return ScanResult {
                    redacted_body: body.to_vec(),
                    ..ScanResult::default()
                };
            }
        };

        let mut matches: Vec<Match> = Vec::new();
        for (index, rule) in self.rules.iter().enumerate() {
            for caps in rule.pattern.find_iter(text) {
                let start = caps.start();
                let end = caps.end();
                matches.push(Match {
                    rule_index: index,
                    finding: Finding {
                        rule_id: rule.id.clone(),
                        category: rule.category.clone(),
                        action: rule.action,
                        start,
                        end,
                        length: end - start,
                        replacement: rule.replacement.clone(),
                    },
                });
            }
        }

        if matches.is_empty() {
            return ScanResult {
                redacted_body: body.to_vec(),
                ..ScanResult::default()
            };
        }

        // Stable sort: earliest start first, then longer match first, then
        // lower rule index first. Mirrors `sort.SliceStable` from the Go.
        matches.sort_by(|a, b| {
            a.finding
                .start
                .cmp(&b.finding.start)
                .then_with(|| b.finding.end.cmp(&a.finding.end))
                .then_with(|| a.rule_index.cmp(&b.rule_index))
        });

        let mut selected: Vec<Match> = Vec::with_capacity(matches.len());
        let mut last_end: usize = 0;
        let mut have_last = false;
        for candidate in matches {
            if have_last && candidate.finding.start < last_end {
                continue;
            }
            last_end = candidate.finding.end;
            have_last = true;
            selected.push(candidate);
        }

        let mut redacted = text.to_string();
        for candidate in selected.iter().rev() {
            let finding = &candidate.finding;
            redacted.replace_range(finding.start..finding.end, &finding.replacement);
        }

        let mut findings = Vec::with_capacity(selected.len());
        let mut has_blocking = false;
        for candidate in selected {
            if candidate.finding.action == FindingAction::Block {
                has_blocking = true;
            }
            findings.push(candidate.finding);
        }

        ScanResult {
            findings,
            redacted_body: redacted.into_bytes(),
            has_blocking_findings: has_blocking,
        }
    }
}

fn default_rules() -> Vec<Rule> {
    vec![
        Rule {
            id: "openai_api_key".into(),
            category: "api_key".into(),
            action: FindingAction::Redact,
            pattern: Regex::new(r"\bsk-[A-Za-z0-9]{20,}\b").expect("openai_api_key regex"),
            replacement: "[REDACTED OPENAI API KEY]".into(),
        },
        Rule {
            id: "github_token".into(),
            category: "token".into(),
            action: FindingAction::Redact,
            pattern: Regex::new(r"\bgh[pousr]_[A-Za-z0-9_]{20,}\b").expect("github_token regex"),
            replacement: "[REDACTED GITHUB TOKEN]".into(),
        },
        Rule {
            id: "bearer_token".into(),
            category: "authorization".into(),
            action: FindingAction::Redact,
            pattern: Regex::new(r"(?i)Bearer\s+[A-Za-z0-9._\-+/=]{16,}")
                .expect("bearer_token regex"),
            replacement: "Bearer [REDACTED TOKEN]".into(),
        },
        Rule {
            id: "aws_access_key".into(),
            category: "api_key".into(),
            action: FindingAction::Redact,
            pattern: Regex::new(r"\bAKIA[0-9A-Z]{16}\b").expect("aws_access_key regex"),
            replacement: "[REDACTED AWS ACCESS KEY]".into(),
        },
        Rule {
            id: "private_key".into(),
            category: "private_key".into(),
            action: FindingAction::Block,
            pattern: Regex::new(r"-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----")
                .expect("private_key regex"),
            replacement: "[BLOCKED PRIVATE KEY]".into(),
        },
    ]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn redacts_known_tokens() {
        let scanner = Scanner::default_scanner();
        let body = r#"{"token":"sk-1234567890abcdefghijklmnop","auth":"Bearer abcdefghijklmnopqrstuvwxyz123456"}"#;
        let result = scanner.scan(body.as_bytes());
        assert_eq!(result.findings.len(), 2);
        assert!(!result.has_blocking_findings);
        let redacted = String::from_utf8(result.redacted_body).expect("utf8");
        assert!(redacted.contains("[REDACTED OPENAI API KEY]"));
        assert!(redacted.contains("Bearer [REDACTED TOKEN]"));
        assert!(!redacted.contains("sk-1234567890abcdefghijklmnop"));
    }

    #[test]
    fn blocks_private_key_material() {
        let scanner = Scanner::default_scanner();
        let body = "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----";
        let result = scanner.scan(body.as_bytes());
        assert_eq!(result.findings.len(), 1);
        assert!(result.has_blocking_findings);
        let redacted = String::from_utf8(result.redacted_body).expect("utf8");
        assert!(redacted.contains("[BLOCKED PRIVATE KEY]"));
    }

    #[test]
    fn ignores_short_non_matching_strings() {
        let scanner = Scanner::default_scanner();
        let body = r#"{"token":"sk-short","auth":"Bearer short"}"#;
        let result = scanner.scan(body.as_bytes());
        assert!(result.findings.is_empty());
        assert!(!result.has_blocking_findings);
    }

    #[test]
    fn scan_is_deterministic() {
        let scanner = Scanner::default_scanner();
        let body = r#"{"token":"ghp_abcdefghijklmnopqrstuvwxyz123456","auth":"Bearer abcdefghijklmnopqrstuvwxyz123456"}"#;
        let first = scanner.scan(body.as_bytes());
        let second = scanner.scan(body.as_bytes());
        assert_eq!(first.findings.len(), second.findings.len());
        assert_eq!(first.redacted_body, second.redacted_body);
    }
}
