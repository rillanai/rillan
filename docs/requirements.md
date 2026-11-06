<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Product requirements index

This index makes the foundational workstream continuity requirements traceable. It is not an exhaustive specification of Rillan. The [use-case document](workstream-continuity.md) owns the detailed contract and acceptance criteria; [architecture](architecture.md#workstream-continuity-boundaries) describes current implementation boundaries, and the [roadmap](../ROADMAP.md#c1-explicit-workstream-handoff) tracks delivery. None of the continuity requirements below is an implemented end-to-end capability today.

| Requirement | Contract | Acceptance evidence required |
| --- | --- | --- |
| WSC-01 | [Durable intent and interactive control](workstream-continuity.md#wsc-01-durable-intent-and-interactive-control) | A1, A5: preserve corrections, decisions, progress, and pending approvals without a manual summary. |
| WSC-02 | [Repository identity and paths](workstream-continuity.md#wsc-02-repository-identity-and-paths) | A1, A2: resolve RepoKeeper identities on each machine and validate the chosen worktrees. |
| WSC-03 | [Complete repository state](workstream-continuity.md#wsc-03-complete-repository-state) | A1, A4: preserve every required repository's base, branch/worktree, staged/unstaged edits, deletions, and relevant untracked contents. |
| WSC-04 | [Destination preflight](workstream-continuity.md#wsc-04-destination-preflight) | A1, A2, A4: block missing prerequisites, conflicts, and unsupported state before restoration/continuation. |
| WSC-05 | [Historical environment state](workstream-continuity.md#wsc-05-historical-environment-state) | A2, A5: map structured references, retain transcript history, revalidate evidence, and avoid command replay. |
| WSC-06 | [Safe checkpoint and ownership](workstream-continuity.md#wsc-06-safe-checkpoint-and-ownership) | A1, A3, A4: stop safely, make the checkpoint independently available, and authorize one writer. |
| WSC-07 | [Failure and recovery](workstream-continuity.md#wsc-07-failure-and-recovery) | A4: incomplete delivery, conflicting state, retries, uncertain activation, and source loss have explicit recovery outcomes. |
| WSC-08 | [Local credentials, configuration, and processes](workstream-continuity.md#wsc-08-local-credentials-configuration-and-processes) | A2, A3: use destination credentials/trust and allowed transport without source reachability, Tailscale, or an interface subscription. |
| WSC-09 | [Explicit continuation fidelity](workstream-continuity.md#wsc-09-explicit-continuation-fidelity) | A5: distinguish native resume, same-provider reconstruction, and later cross-provider continuation. |

See [acceptance scenarios A1–A5](workstream-continuity.md#acceptance-scenarios) for the concrete pass conditions. All nine outcomes apply within [C1's proposed Codex CLI/platform scope](workstream-continuity.md#first-milestone-scope); A5's Claude/GPT transition is a later requirement. Native resume is conditional on documented, tested provider support. Exact versions and architectures must be recorded before support is claimed.

Mechanisms such as leases, checkpoint formats, transport protocols, or an authoritative state store are unresolved design choices, not additional requirements silently selected by this index. Existing accepted [ADRs](../adrs/) continue to govern runtime behavior, including local defaults and manual single-root indexing.
