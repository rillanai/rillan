<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# CLAUDE.md

Primary guidance for this repo lives in `AGENTS.md`. Read it first; the guardrails, project structure, testing discipline, graphify rules, and commit conventions apply to every change.

This file only adds details specific to Claude Code sessions.

## Origin

This crate is a Rust port of `github.com/rillanai/rillan`, the original Go daemon. ADRs, README behavior, and configuration semantics are preserved verbatim from the upstream Go repo. When the Go repo and Rust port disagree, the ADRs win and the Rust code is updated to match.

## Claude Workflow

Before editing unfamiliar areas, Claude Code users can orient with:

- `/graphify query "<question>"` — grounded Q&A with source citations
- `/graphify path "A" "B"` — shortest connection between two concepts
- `/graphify explain "<node>"` — node + its neighborhood in plain language

Treat graphify as advisory context only; the shared rules in `AGENTS.md` about verifying `INFERRED` and `AMBIGUOUS` edges still apply.
