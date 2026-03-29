# Rillan Milestone 06 - Security Completion and Remote-Egress Hardening

- **Type**: milestone plan
- **Status**: active
- **Depends on**: `.sisyphus/plans/rillan-milestone-05.md`, `.sisyphus/plans/rillan-deployment-baseline.md`, and the M01-M04 foundations recorded in `.sisyphus/plans/rillan-linear-seed.md`
- **Use this file for**: the canonical execution plan for completing the three-tier security path and remote-egress hardening work that follows M05

## Goal

Complete the first full security model for Rillan and make remote egress traceable, bounded, and policy-driven.

Milestone 05 established the first outbound policy seam and project-scoped policy config. Milestone 06 should finish the system-local and runtime-ephemeral layers around that seam and make every remote egress traceable and bounded. Local service packaging is now deferred to backlog work rather than blocking this milestone.

## Why this milestone now

M05 makes request evaluation structural. M06 is where that structure becomes trustworthy in day-to-day use: machine-local identity rules, ephemeral policy merge, deterministic outbound minimization, and durable auditability.

This milestone should make Rillan meaningfully safer without turning packaging into a release-engineering project or turning fragmentation into a model-quality research track.

## Current Implementation Snapshot

Implemented in the working tree:

- tier-0 encrypted system-config envelope, machine-local pathing, validation, keyring-backed key fetch seam, and AES-GCM decryption into in-memory policy
- request-scoped tier-2 runtime policy merge plus preflight and egress policy evaluation phases
- deterministic remote retrieval minimization before retrieval preparation, including caps that do not widen already-bounded requests
- append-only local audit ledger for remote egress and remote denial events
- richer runtime visibility through `rillan status` and `/readyz`
- early user-level `launchd` and `systemd` packaging artifacts, validation scripts, and packaging docs landed as groundwork, but packaging no longer blocks the milestone

Still requiring confirmation before calling the milestone fully complete:

- confirmation that the keyring-backed tier-0 path behaves correctly on target OS keychain implementations, not just through mocked tests

## Inputs and Constraints

- The daemon remains a single primary Go binary.
- The local API boundary remains the product boundary.
- Tier-0 system configuration is machine-local only, never committed, and must not leave the machine.
- Tier-2 merged policy is ephemeral and must never be persisted in combined form.
- Auditability belongs directly in the outbound policy path; the plan should not treat it as optional observability garnish.
- Level 1 targeted retrieval belongs in M06; abstraction rewriting and question extraction do not.
- Local service packaging, release-signing, provenance, cross-arch release hardening, and advanced installer work are valid later concerns but should not become blocking scope for this milestone.

## In Scope

- Tier-0 encrypted system identity config for local machine-specific policy inputs.
- Tier-2 runtime policy merge that combines tier-0 and tier-1 in memory at request-evaluation time only.
- Deterministic Level 1 IP fragmentation through targeted retrieval and bounded outbound selection.
- An append-only audit ledger for remote egress decisions and policy traceability.
- Readiness/status semantics that expose when retrieval or routing depends on a local-model-backed corpus or unavailable policy dependency.

## Out of Scope

- Level 2 abstraction rewriting.
- Level 3 question extraction.
- User-level service packaging as a completion blocker.
- Release-signing workflows, cosign provenance, checksums, or artifact-verification docs.
- System-wide installers or root-owned service setup.
- Bundled Ollama management, model downloads, or custom background process orchestration.
- Major provider-matrix expansion.

## Deliverables

1. A machine-local encrypted tier-0 system config surface with clear boundaries and safe defaults.
2. An in-memory tier-2 merge path that evaluates tier-0 and tier-1 together for each outbound request.
3. A deterministic targeted-retrieval minimization step that reduces outbound context for remote-provider requests.
4. An append-only audit ledger that records egress decisions, hashes, referenced artifacts, and policy reasons.
5. Readiness and status outputs that explain when the active corpus, local-model state, or policy requirements make runtime behavior degraded or incompatible.

## Proposed File Touch Points

- `internal/config/` — tier-0 system config schema, loading, validation, path handling, and encryption/keychain integration seams.
- `internal/policy/` — tier-2 merge logic, runtime evaluation inputs, transform selection, and policy trace structures.
- `internal/retrieval/` — targeted retrieval selection and outbound minimization logic.
- `internal/httpapi/` — request handling path where tier-2 policy results and runtime readiness become user-visible.
- `internal/index/` — artifact metadata and source references needed for targeted retrieval and audit traceability.
- `internal/audit/` or a similarly narrow new package — append-only ledger types and storage.
- `internal/app/` and `cmd/rillan/` — runtime wiring, status/readiness semantics, and service-oriented operational behavior.
- `packaging/` or `configs/services/` — existing service-artifact groundwork that remains available for later backlog packaging work
- `README.md` and ADRs — only if the runtime contract, packaging doctrine, or security posture needs durable repo-facing documentation.

## Execution Parts

### Part 1 - Complete the security foundation

**Purpose**: finish the policy inputs and merge behavior that M05 explicitly deferred, so Rillan can evaluate outbound requests using machine-local and project-local rules together.

**Includes**:

- Phase 1 - tier-0 system identity config
- Phase 2 - ephemeral tier-2 policy merge

**Part outcome**: Rillan has a real three-tier policy path in which tier-0 remains machine-local, tier-1 remains repo-committable, and tier-2 exists only at request-evaluation time.

**Do not move on until**:

- tier-0 is machine-local only and never emitted into repo-local artifacts
- tier-2 merge behavior is request-scoped and provably non-persistent
- policy evaluation can explain which rule source drove a decision

#### Phase 1 - Add tier-0 system identity config

- Define the tier-0 config surface for machine-local identity and protection rules.
- Capture only data needed for deterministic masking and routing rules, such as personal identifiers, employer references, and credential patterns.
- Keep the file machine-local and encrypted, with a seam for OS keychain-backed material handling where required.
- Ensure this config is structurally separate from the repo-committable tier-1 `.sidekick/project.yaml` surface.

Verification:

- Run `go test ./internal/config/...` and require tier-0 config load/validate tests for valid, malformed, missing, and disallowed-unencrypted cases.
- Add fixtures or helper tests that prove tier-0 values never appear in generated example repo configs or repo-local fixtures.
- Require a smoke path that loads valid tier-1 config with no tier-0 file and confirms the daemon still starts in a degraded-but-valid posture.

#### Phase 2 - Implement ephemeral tier-2 policy merge

- Merge tier-0 and tier-1 policy inputs only in memory at request-evaluation time.
- Extend the existing policy seam so routing and transformation decisions can consider both system-local and project-local rules.
- Make the merged view discardable and non-persisted by construction.
- Expose enough typed trace data that later audit recording does not need to infer policy state from logs.

Verification:

- Run `go test ./internal/policy/... ./internal/httpapi/...` and require tests proving merged policy is constructed per request and never written back to persistent config surfaces.
- Add request-path tests showing tier-0 rules can override tier-1 routing or transformation behavior without changing the stored tier-1 config.
- Add a regression test that fails if merged policy artifacts are serialized into repo-local config or persisted state.

### Part 2 - Minimize and trace remote egress

**Purpose**: make remote-provider usage both smaller and inspectable by reducing outbound context and recording why it left the machine.

**Includes**:

- Phase 3 - Level 1 targeted retrieval minimization
- Phase 4 - append-only audit ledger
- Phase 5 - readiness and status semantics

**Part outcome**: remote egress is deterministically minimized, every remote egress or denial leaves a local trace, and runtime surfaces clearly explain whether the system is truly ready or operating in a degraded mode.

**Do not move on until**:

- remote-provider requests use bounded targeted retrieval where policy requires it
- audit traces exist for remote egress decisions and are usable for forensics
- `status` and `readyz` describe actual runtime dependency and compatibility state

#### Phase 3 - Add Level 1 targeted retrieval minimization

- Implement deterministic targeted retrieval as the first IP-fragmentation strategy.
- Restrict remote-provider outbound context to the minimal chunk set needed for the current request instead of forwarding larger local context packages by default.
- Keep this strategy structural and deterministic; it should not depend on model rewriting quality.
- Make the policy layer choose this path when remote-provider dispatch requires minimization.

Verification:

- Run `go test ./internal/retrieval/... ./internal/policy/... ./internal/httpapi/...` and require tests showing outbound payloads contain only bounded targeted context under the relevant policy paths.
- Add an integration test comparing a local-only request path versus a remote-targeted request path and asserting the remote path sends less context while keeping source attribution intact.
- Require regression coverage for empty index, no-match, truncation, and keyword-only retrieval scenarios so minimization remains deterministic.

#### Phase 4 - Add append-only audit ledger

- Implement an append-only local ledger for outbound egress events.
- Record request identifiers, provider identity, model metadata, hashes of outbound payloads, source/chunk references, policy decisions, and response hashes where feasible.
- Keep the ledger readable enough for forensics and reproducibility without leaking raw sensitive payloads by default.
- Treat audit recording as part of the remote egress flow, not a best-effort side effect bolted on later.

Verification:

- Run `go test ./internal/audit/... ./internal/policy/... ./internal/httpapi/...` and require ledger append/read tests plus handler-level tests proving a remote egress creates a trace entry.
- Add a test ensuring blocked requests either record a denial event or an explicit non-egress audit record, depending on the chosen contract.
- Add regression coverage proving sensitive raw values are not logged or persisted verbatim where hashes or redacted snapshots are intended.

#### Phase 5 - Tighten readiness and status semantics

- Extend readiness and status so they expose when the runtime is degraded or incompatible with the committed corpus and active policy requirements.
- Surface committed retrieval mode, active local-model dependency, and the reason a local-model-dependent corpus or policy path is currently unavailable.
- Keep `readyz` aligned with the deployment baseline: local-model unavailability should matter only when the configured runtime actually depends on it.

Verification:

- Run `go test ./cmd/rillan/... ./internal/httpapi/... ./internal/index/...` and require status/readiness tests for embedded-only mode, local-model-required mode, and incompatible or unavailable runtime states.
- Add a smoke path that starts the daemon with and without local-model dependency and verifies readiness semantics match the committed corpus and policy requirements.

## Milestone-Wide Acceptance Checks

- Tier-0 system policy is machine-local, encrypted or keychain-backed where required, and never leaves the machine.
- Tier-2 policy merge exists only in memory during request evaluation and is not persisted in combined form.
- Remote egress paths use deterministic targeted retrieval minimization when policy requires it.
- Every remote egress or remote denial leaves an audit trace with request identity and policy reason.
- `rillan status` and `/readyz` explain when the runtime is degraded, incompatible, or fully ready.

## Risks and Open Questions

- **OS keychain integration scope**: tier-0 encryption may pull in platform-specific work faster than expected.
- **Audit payload design**: the ledger needs enough fidelity for forensics without becoming a sensitive-data sink.
- **Minimization sufficiency**: Level 1 targeted retrieval may not be enough for all proprietary/trade-secret cases, but the milestone should not expand into abstraction rewriting to compensate.
- **Readiness semantics**: the daemon should not become spuriously unready just because an optional local model is down; readiness must track actual runtime dependency.
- **Packaging backlog drift**: deferred service artifacts can go stale if later backlog work is not tied closely to the runtime contract now being finalized.

## Definition of Done

- Tier-0 system identity config exists and is machine-local only.
- Tier-2 policy merge evaluates tier-0 and tier-1 together per request and remains ephemeral.
- Remote egress uses deterministic targeted retrieval minimization when policy requires it.
- An append-only audit ledger records outbound policy traces and remote egress events.
- Status and readiness surfaces reflect real runtime dependency and compatibility state.

## Deferred Backlog After M06

Packaging is no longer a milestone-06 completion requirement, but the groundwork landed during M06 should be preserved as backlog input:

- user-level macOS `launchd` packaging path
- user-level Linux `systemd --user` packaging path
- install/uninstall/start/stop lifecycle validation on target platforms
- parity validation against foreground `rillan serve`
