<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Portable agent workstream continuity

**Status: intended product contract; end-to-end checkpoint, transfer, and resume are not implemented in this repository.** This use case was part of Rillan's original motivation. It gives the existing Context Control Plane direction without changing the current runtime or declaring a new component architecture.

Related: [README](../README.md), [requirements index](requirements.md), [architecture boundaries](architecture.md#workstream-continuity-boundaries), [roadmap](../ROADMAP.md#c1-explicit-workstream-handoff).

## The developer's workstream

A developer works interactively with a coding agent, steering its changes, correcting its assumptions, and approving actions. They move between a Windows desktop running agents inside WSL, a MacBook, and a native Linux laptop. They also switch between Claude and GPT.

They need to stop at a safe point on one machine and continue the same work on another without explaining the project again, composing a handoff summary, or forcing unfinished changes into a WIP commit. A workstream can span multiple repositories, each checked out at a different path on each machine. Preserving a conversation alone is insufficient when the code, approvals, or environment no longer match it.

A **workstream** means the durable intent, decisions, corrections, progress, outstanding work, repository state, and human approval boundaries needed to continue. A **checkpoint** captures a consistent stopping point and its continuation status. A provider's **session** is one execution context for that workstream; its native resumability is provider-dependent.

## What the current implementation provides

These are implementation facts, not a claim of continuity support:

| Existing source | Present behavior | Continuity gap |
| --- | --- | --- |
| [`ContextPackage` and builder](../crates/agent/src/context_package.rs), [context builder](../crates/agent/src/context_builder.rs) | Represent task, constraints, evidence, facts, questions, and working memory; apply context budgets. | Request-scoped, bounded context with local `repo_root`/path strings; no durable multi-repository checkpoint or correction history. A budgeted prompt cannot be assumed to preserve all workstream state. |
| [Task endpoint](../crates/httpapi/src/agent_task.rs) and [runner](../crates/agent/src/runner.rs) | Build a context package, execute bounded read-only skills, and return a result/context echo and optional proposal. | No external Codex/Claude session export, import, or continuation loop. |
| [Approval gate](../crates/agent/src/gating.rs) and [proposal store](../crates/agent/src/proposal_store.rs) | Record proposal decisions; pending proposals live in an in-memory store. | No durable approval or ownership transfer across machines or daemon restarts. |
| [Repository tools](../crates/agent/src/skills/read_only.rs) and [app wiring](../crates/app/src/lib.rs) | Canonicalize and restrict access to approved local roots; provide Git status/diff reads. | No RepoKeeper identity mapping or capture/restore of complete dirty repository state. |
| [Provider trait](../crates/providers/src/lib.rs) and [host](../crates/providers/src/host.rs) | Dispatch chat requests through OpenAI-compatible, Anthropic, Ollama, and stdio adapters. | API translation/routing is not a native agent-session resume adapter. |
| [Runtime manager](../crates/app/src/runtime_manager.rs) | Rebuild and swap the daemon's runtime configuration and provider/retrieval state. | A `RuntimeSnapshot` is not a portable workstream checkpoint or process snapshot. |
| [Audit ledger](../crates/audit/src/lib.rs), [ADR-010](../adrs/ADR-010.md) | Persist request/decision metadata and body hashes. | The ledger does not store the conversation bodies or complete state needed to reconstruct a workstream. |

RepoKeeper's MCP repository resolution is an existing external capability supplied by this workflow. Rillan must consume that abstraction rather than build a competing checkout-discovery system. Its integration into checkpoint/resume is still intended work.

## Continuity contract

The following requirements describe intended behavior. The [requirements index](requirements.md) maps them to acceptance scenarios. They specify outcomes, not a serialization schema or transfer protocol.

### WSC-01: Durable intent and interactive control

Preserve the goal, applicable instructions, decisions and their reasons, user corrections, current plan, completed work, outstanding tasks, open questions, and verification results. Capture this through the workflow; the user must not have to write a handoff summary. Retain provenance so a later summary does not silently revive a corrected assumption or replace a decision with an inference.

Preserve allowed and forbidden effects, denials, pending decisions, and the scope of approvals. Show the continuation point and let the user steer before further effects. A pending action remains pending. An approval is not permission to repeat an action already performed or to act against changed destination state; revalidate its scope and obtain a new decision when that scope no longer holds.

### WSC-02: Repository identity and paths

Identify each repository through its RepoKeeper identity and refer to files by repository-relative path. Resolve that identity through RepoKeeper's MCP interface on the destination, then validate the selected checkout/worktree against the checkpoint. A repository identity alone does not identify which of several worktrees should receive changes. Missing or ambiguous resolution blocks continuation for that repository; never guess a path from the source machine's directory layout.

For example, the same identity may resolve to `/home/dev/src/rillan` in WSL, `/Users/dev/work/rillan` on macOS, and `/srv/dev/rillan` on Linux. A structured reference to `README.md` travels with that identity, not with any of those absolute paths. These are illustrative mappings, not a new RepoKeeper identifier syntax.

### WSC-03: Complete repository state

For every repository in the workstream, account for:

- The exact base/HEAD commit and the branch and worktree identity, including detached HEAD where applicable. Required commits must be available at the destination or included through an allowed transfer mechanism; do not assume every commit is pushed.
- Staged and unstaged changes separately, including tracked deletions and the contents needed to restore them. Preserve the distinction when the same file has both kinds of change.
- Relevant untracked paths and contents, with an inventory of anything excluded or unsupported. A Git diff alone does not capture them. Exclude credentials and unrelated private material; if necessary work cannot be captured safely, report the blocked checkpoint rather than call it complete.
- Any file modes, symlinks, binary changes, or other Git/filesystem features the selected support scope needs. Unsupported state must be identified before claiming a usable checkpoint; do not silently normalize it away.

Capture all repositories at one coordinated safe stopping point. No WIP commit is required. Destination-local edits must remain recoverable, and restoring one repository must not allow the agent to continue while another required repository is unresolved.

### WSC-04: Destination preflight

Before restoration or agent continuation, inspect all destination repository mappings and base commits, branch/worktree state, and existing changes. Check provider/CLI compatibility, tools, dependency versions, shell/platform assumptions, required MCP capabilities, local policy/trust, and access to destination credentials. Report concrete missing prerequisites and supported recovery steps; do not silently install tools, enable modules, or broaden repository access as a side effect of resuming.

Resolve identity through RepoKeeper first, then enforce the destination's existing approved-root and path-containment rules. Resolution does not grant access. Detect case sensitivity, line-ending, permission, symlink, or architecture differences that prevent faithful restoration. Refuse unsupported combinations or request an explicit resolution without overwriting destination work. Rebuild local indexes when needed within the existing manual indexing contract.

### WSC-05: Historical environment state

Treat old absolute paths, process IDs, shell sessions, tool handles, and tool results as evidence from the source environment. Keep historical transcripts intact and add a destination environment map to structured context. Blind transcript search-and-replace can corrupt commands, quotations, and explanations.

Do not replay pending commands or skill invocations automatically from serialized context. Reconstruct the next step against the destination's current state and approval boundary. Mark verification results with their repository revision and environment; rerun relevant checks when those conditions change. A source test pass does not establish destination readiness.

### WSC-06: Safe checkpoint and ownership

Stop the agent and relevant repository mutations at an explicit safe boundary. Finish or cancel in-flight actions and record their actual outcomes; an action whose completion is uncertain blocks a complete checkpoint until reconciled. Confirm that the captured repository state still matches the stopping point.

A **completed handoff checkpoint** means the complete state has been durably saved, its integrity/completeness verified, it is available to the destination independently of the source, and the source's paused/released continuation status has been recorded. A checkpoint saved only on the source is not sufficient for a source-independent handoff.

At most one machine may be authorized to continue editing. The source stops continuing before the destination acquires ownership. The destination may inspect and preflight while no writer is active, but it must not continue until all required repositories are ready and ownership is explicit. The user sees which checkpoint and machine own continuation; the old source must check that status before resuming.

These are semantic states, not a prescribed distributed lock implementation. With no reachable coordination service, an explicit user-mediated ownership decision can be sufficient, but ambiguity must block writing. Leases, lock files, acknowledgements, and ownership-record storage remain mechanism choices. The tool cannot promise prevention of arbitrary out-of-band edits; it must detect known divergence and never silently authorize two continuations.

### WSC-07: Failure and recovery

Recovery must distinguish durable capture, destination preparation, and ownership activation. Retain the checkpoint until successful continuation and a deliberate retention decision; an incomplete transfer must never replace the last complete checkpoint.

| Failure point | Required behavior and recovery |
| --- | --- |
| Capture or delivery fails before a completed handoff checkpoint | Mark it incomplete. The source retains ownership and stays paused until the user retries or explicitly cancels the handoff and resumes there. Do not activate the destination. |
| Transfer is interrupted after durable capture | Preserve the complete checkpoint and source pause. Verify completeness on retry; partial copies cannot activate continuation. Repeating delivery/preparation must not apply edits twice. |
| Destination has conflicting edits, a wrong base/worktree, or missing prerequisites | Leave destination work intact; report the conflict and keep continuation blocked. Retry after explicit resolution or prepare another checkout. Record any partial restoration and provide recovery without destructive reset/clean operations. |
| Preparation completes but ownership activation/acknowledgement is uncertain | Neither side infers ownership from a timeout. Reconcile the ownership record and any observed edits before authorizing one writer. The source must not automatically restart. |
| Source locks or disappears after a completed handoff checkpoint | Continue from that independently available checkpoint using destination credentials and explicit ownership. No source login, process, credential agent, or unlock is required. |
| Source disappears before a complete checkpoint exists | Recover only from the last complete checkpoint, if available. Report its age and uncaptured work; do not imply lossless recovery of state that was never captured. Ownership uncertainty still blocks continuation. |

### WSC-08: Local credentials, configuration, and processes

Portable state carries work and requirements, not private keys, tokens, keyring contents, credential-agent sockets, or a machine's trust grants. Git authentication and signing in this workflow use 1Password on each machine. Locking the source may remove its credentials; the destination uses its own 1Password integration and separately configured provider/MCP access. Rillan's existing OS-keyring provider storage is distinct from that Git setup; this repository does not implement a 1Password migration feature.

Treat machine-specific config, executable paths, provider bindings, caches, and indexes as local concerns. Preserve applicable workstream instructions and constraints, then reconcile them with destination policy without relaxing either. Existing config/data/log separation and the default `127.0.0.1:8420` bind remain in force ([ADR-002](../adrs/ADR-002.md)). Do not transfer live processes, shell sessions, daemons, or tool connections; recreate required services through normal destination setup and approval.

The core flow must work without Tailscale, a continuously reachable source, or an always-on Linux machine. Tailscale is blocked on the primary work computer under corporate policy; use only allowed transfer paths and fail visibly when none is available. Never route around network controls. Always-on hosts may help with storage or coordination but are optional. Dots or other interfaces may coordinate the same contract; no particular interface or subscription may be required.

### WSC-09: Explicit continuation fidelity

| Mode | What it can preserve | Required limit/reporting |
| --- | --- | --- |
| Same-provider native resume | Provider-supported session/conversation artifacts plus the portable workstream and repository checkpoint. | Use only documented, compatible resume/export behavior. Report provider, CLI/session versions, and what was actually restored. Same provider does not guarantee cross-machine or cross-version resumability. |
| Same-provider reconstructed continuation | Rehydrate durable intent, evidence, corrections, progress, and repository state into a new session when native resume is unavailable. | Label it reconstructed, report omissions and stale environment evidence, and surface pending decisions before effects. Do not call a new session a native resume. |
| Cross-provider continuation, such as Claude to GPT | Reconstruct the workstream in the destination provider's supported context and tool model. | Report any translation or context-budget loss and unsupported tool/approval semantics; block when required constraints cannot be represented. This is not a native session migration. |

Never claim migration of hidden model state, internal reasoning, or running processes. Receiving providers may behave differently even with the same evidence. Native artifacts supplement the durable workstream contract rather than substitute for missing repository state or approvals. Existing API provider adapters do not establish any of these session-level guarantees.

## First milestone scope

**C1 is an intended, bounded milestone, not a support claim for the current release.** Its proposed provider/platform scope is Codex CLI (GPT) to Codex CLI on WSL 2's Linux environment, macOS, and native Linux. Each handoff has one source and one destination and occurs at an explicit safe stopping point; the acceptance path is WSL → macOS followed by macOS → Linux. Native Windows agent execution is outside this scope.

Reconstructed same-provider continuation is the minimum required fidelity. Native resume is optional where documented provider support and tested compatibility permit it; inspect and report the achieved mode rather than assume it. Claude/GPT cross-provider continuation remains a required later scenario, outside C1's supported provider scope.

Before C1 can be declared supported, record the exact Codex CLI version/session format, WSL distribution/version, macOS and native Linux versions, architectures, supported repository-state features, and required tool/MCP versions used for A1–A4 and A5's same-provider cases. Unlisted or incompatible combinations must fail preflight or be explicitly reported as unsupported. Choosing that tested matrix is a delivery prerequisite; this document does not claim those combinations have been tested.

All nine contract outcomes apply within this scope. C1 must include multiple repositories, dirty working trees, source-independent completed checkpoints, destination-local credentials, safe recovery, and human steering. Automatic background synchronization, concurrent writers, live process migration, arbitrary provider-session conversion, and a new orchestration platform are outside C1. Storage formats, leases, and transports are proposals to evaluate separately from these outcomes.

## Acceptance scenarios

These are future acceptance criteria, not executed tests of the current runtime.

### A1: WSL desktop to MacBook — C1

Given a Codex workstream covering two repositories resolved by RepoKeeper, with different WSL and macOS checkout paths, a file with both staged and unstaged edits, a tracked deletion, and a relevant untracked file, stop and create a completed handoff checkpoint. On the MacBook, resolve both identities and validate all bases/worktrees before restoring.

Pass when both repositories retain their captured commits and branch/worktree intent, staged/unstaged separation, deletions, and untracked contents; no WIP commit or manual handoff summary is needed. The agent presents the same goal, latest correction, decisions, remaining work, and pending approval before continuing. The WSL source stays paused and cannot silently resume after destination activation. Missing base commits are transferred through an allowed mechanism or block preflight explicitly.

### A2: MacBook to native Linux with missing prerequisites — C1

Given the resulting workstream and different Linux checkout paths, omit a required tool/dependency version and an MCP capability on Linux. Attempt the handoff.

Pass when preflight names each missing prerequisite, checks every required repository before restoration, and leaves existing destination work intact. It does not substitute a source path or replay a command. After the prerequisites are explicitly satisfied, retry from the same checkpoint without duplicate edits, validate local access/trust and filesystem compatibility, and resume with the destination environment map. Revalidate environment-dependent checks before treating them as current.

### A3: Source locked after checkpoint — C1

Complete and independently deliver the checkpoint with the source released/paused, then lock the source, making its 1Password Git/signing credentials unavailable. Keep that host unreachable and use an allowed transfer path without Tailscale or an always-on host.

Pass when destination preflight and explicit ownership activation succeed without contacting or unlocking the source. Any later authorized Git authentication/signing uses the destination's own 1Password setup; provider and MCP access also use destination-local credentials. No private key, token, agent socket, or keyring contents were included in the checkpoint. No interface subscription is required to perform the core flow.

### A4: Interrupted handoff and conflicting destination — C1

Interrupt capture/delivery before a complete checkpoint, then interrupt a later attempt after durable capture and during destination preparation. Include one destination repository with unrelated dirty edits, and repeat a delivery/preparation request. Also make activation acknowledgement uncertain.

Pass when incomplete state cannot activate, the last complete checkpoint remains usable, destination edits survive, and any partial restoration is visible and recoverable. Before checkpoint completion the source retains ownership but remains paused until explicit cancellation/retry; after release neither side restarts on a timeout. Resolve conflicts and ownership explicitly, then activate exactly one continuation without applying changes twice. If the source is lost before completion, recovery reports the last checkpoint and the uncaptured gap instead of promising lossless continuation.

### A5: Same-provider and cross-provider fidelity — C1 plus later scope

For C1, attempt Codex to Codex on a documented compatible native-resume combination and on a combination without native resume. Pass when the first is labelled native only if a supported session artifact actually resumes, while the second creates an explicitly reconstructed session satisfying the same workstream, repository, and approval contract. If no tested native path exists, document native resume as unsupported rather than simulate a passing native case.

For later cross-provider work, continue a Claude workstream in GPT (and test the reverse separately when supported). Pass when the preserved intent, corrections, evidence, remaining work, and approval limits are visible, translation/omissions are disclosed, and incompatible required semantics block continuation. C1 must identify this provider transition as outside its supported scope. Neither mode claims hidden model state or process migration.

## Unresolved design questions

These require decisions before implementation; this document does not settle them:

- **Durable-state authority:** should Rillan own the authoritative workstream independently of provider sessions, and produce budgeted `ContextPackage` projections for execution? Or should a companion own that state while Rillan consumes it? Define correction/provenance retention without treating a truncated prompt or audit hashes as the complete record.
- **Rillan/Lyra boundary:** the current repository implements the Rillan kernel and agent primitives but has no Lyra component or documented Lyra interface. If the broader design assigns session management or environment snapshots to Lyra, bring in that authoritative design and specify the boundary before assigning implementation ownership. Do not infer it from the name `RuntimeSnapshot`.
- **Provider integration:** how will the authoritative state reach provider-specific session adapters or reconstructed sessions, and which documented CLI capabilities and versions support native resume? The current request-level `Provider` trait does not answer this.
- **Repository restoration:** how are RepoKeeper identities/worktrees verified, unpublished commit objects and supported dirty-state features transported, and partial multi-repository restoration recovered without losing destination work?
- **Durability and ownership:** where is a source-independent checkpoint and its ownership status available under allowed network policy? How are stale source restarts, retries, explicit cancellation, and uncertain activation reconciled without requiring an always-on coordinator?
- **Representation and delivery:** which versioned format, integrity/protection rules, retention policy, allowed transports, and tested provider/platform matrix meet the contract? Leases or a particular protocol are candidate mechanisms, not requirements established here.

Use the existing [architecture boundaries](architecture.md#workstream-continuity-boundaries) and [ADRs](../adrs/) when resolving these questions. This task establishes the use case and acceptance contract without changing runtime behavior or accepting a new architecture decision.
