# Rillan Linear Seed Plan

- **Type**: backlog seed
- **Status**: active
- **Use this file for**: milestone ordering, backlog decomposition, and public-roadmap translation

This document is the issue-ready internal backlog for seeding a future `SidekickOS` Linear team and `Rillan` project. It is intentionally more concrete than the public repo roadmap, which should remain short and directional. Where a dedicated milestone plan exists, that plan is the execution artifact and this file should stay at the backlog level.

## Project

- Name: `Rillan`
- Team: `SidekickOS` (to be created)
- Public roadmap style: `Now / Next / Later`
- Internal roadmap style: explicit milestones and workstreams

## Progress Summary

| Milestone | Status | Notes |
|-----------|--------|-------|
| M01 - Daemon Skeleton and Proxy | **Done** | Go daemon, CLI (serve/init), OpenAI proxy, provider abstraction |
| M02 - Local Knowledge Substrate | **Done** | SQLite store, file discovery, chunking, placeholder vectors, index/status CLI |
| M03 - Retrieval and Context Compilation | **Done** | Pipeline, context compiler, debug headers, per-request overrides |
| M04 - Local Model Helpers | **Done** | Ollama client, real embeddings, query rewriting, health/status verified |
| M05 - Intent Classification and Policy | Not started | |
| M06 - Security and Packaging Hardening | In progress | Parts 1-2 implemented; packaging artifacts/docs landed; real platform service validation remains |
| M07 - Agent Runtime | Not started | Post-v1 evolution |
| OSS Readiness | Not started | Can run in parallel with M04+ |

## Milestones

### ~~Milestone 01 - Daemon Skeleton and Proxy~~ ✓ DONE

Completed in commits `0472bff` through `2efc2fd`. Delivered:
- Go daemon with Cobra CLI (`rillan serve`, `rillan init`)
- HTTP API on `localhost:8420` with `/healthz`, `/readyz`, `/v1/chat/completions`
- OpenAI-compatible provider adapter with upstream forwarding
- YAML config with environment variable overrides and platform-specific defaults
- Structured logging with request IDs, request size limits, OpenAI-format errors
- ADR-001 (language/runtime), ADR-002 (defaults/filesystem)

### ~~Milestone 02 - Local Knowledge Substrate~~ ✓ DONE

Completed in commits `1a59f07` and `cb82ad5`. Delivered:
- SQLite metadata store with schema versioning (WAL mode, foreign keys)
- File discovery with include/exclude glob patterns, symlink/binary filtering, 1 MB limit
- Deterministic line-based chunking (default 120 lines) with stable chunk IDs
- Placeholder 8-dimensional vector embeddings (SHA256-derived, cosine similarity search)
- `rillan index` and `rillan status` CLI commands
- Full-rebuild indexing strategy (no incremental, no background watchers)
- ADR-003 (single root, manual indexing, embedded SQLite)

### ~~Milestone 03 - Retrieval and Context Compilation~~ ✓ DONE

Goal: inject local context into `/v1/chat/completions` through a bounded, inspectable compilation layer.

Historical notes from execution:
- Retrieval pipeline with settings resolution (global defaults + per-request overrides)
- Query builder extracting user message content
- Context compiler with bounded char limits and source references
- Debug metadata and summary extraction for response headers
- Integration into chat completions handler with debug headers (`X-Rillan-Retrieval-*`)
- Request sanitization (strips retrieval fields before upstream forwarding)

Follow-on work identified during execution:

1. `Replace placeholder vector search with real embedding pipeline`
   - Current search uses 8-dim SHA256-derived vectors — functionally random similarity
   - Options: local embedding model via Ollama, or external embedding API endpoint
   - Define embedding config surface (model name, dimensions, endpoint)
   - Add embedding generation during indexing (replace placeholder in `internal/index/vectors.go`)
   - Update search to use real semantic similarity
   - Dependencies: M04 Ollama client may inform embedding endpoint choice

2. `Add BM25/FTS5 keyword search path`
   - The docs specify hybrid retrieval (keyword + semantic); current impl is vector-only
   - Add SQLite FTS5 index over chunk content during indexing
   - Implement keyword search as a parallel retrieval path
   - Combine keyword and vector results with score fusion (e.g., reciprocal rank fusion)
   - Dependencies: none (SQLite FTS5 is already available via modernc.org/sqlite)

3. `Add retrieval integration tests with realistic corpus`
   - Current test fixtures are minimal smoke tests
   - Add end-to-end test: index a real-ish project → query → verify context injection
   - Cover edge cases: empty index, no matches, truncation, per-request overrides
   - Dependencies: retrieval pipeline stable

Definition of done:

- Rillan can answer a chat request using meaningfully retrieved local context
- Retrieval supports at least one real similarity signal (keyword or embedding)
- Retrieval remains bounded and source-aware
- Provider adapters stay separate from retrieval logic

### ~~Milestone 04 - Local Model Helpers~~ ✓ DONE

Goal: add optional small-model helper behavior through Ollama without making local models a core daemon dependency.

Delivered:
- `internal/ollama/` — Thin HTTP client for Ollama native API (`Embed` via `/api/embed`, `Generate` via `/api/generate`, `Ping`)
- `LocalModelConfig` + `QueryRewriteConfig` in config with 5 env overrides and validation
- `Embedder` interface + `OllamaVectorStore` for real embedding generation during `rillan index`
- `SearchChunks` signature changed to accept `[]float32` query embedding (caller-owned)
- `QueryEmbedder` interface with `PlaceholderEmbedder` and `OllamaEmbedder` implementations
- `QueryRewriter` interface with `OllamaQueryRewriter` using `/api/generate`
- Pipeline options pattern: `WithQueryEmbedder`, `WithQueryRewriter`
- `Rebuild()` accepts `RebuildOption` with automatic fallback when Ollama unreachable
- `rillan status` prints local model connectivity; `/readyz` includes informational `local_model` field
- App wiring creates Ollama client and injects embedder/rewriter when `local_model.enabled`
- Example config and README updated

Deferred to future work:
- Contextual grep expansion helper (not needed until M05+ retrieval improvements)
- Batch embedding support for large corpora (optimization, not correctness)

Definition of done:

- Rillan can use a local helper model for embeddings and query rewriting when configured
- Core daemon behavior still works when Ollama is absent
- `rillan status` reports local model connectivity

### Milestone 05 - Intent Classification and Policy

Canonical plan: `.sisyphus/plans/rillan-milestone-05.md`

Goal: add local intent classification and the first layer of the tiered security model, making Rillan context-aware about what it's sending and where.

Issues:

1. `Implement intent classification via local model`
   - Classify incoming requests using a small local model (1-3B params on GPU)
   - Output: `action_type` (code_diagnosis, code_generation, architecture, explanation, refactor, review, general_qa)
   - Output: `sensitivity` estimate (public, ip_protected, ip_core, trade_secret)
   - Output: `requires_context` (boolean — does this need vault retrieval?)
   - Output: `execution_mode` (direct, plan_first)
   - Keep classification optional — bypass when local model unavailable
   - Dependencies: M04 Ollama client

2. `Add tier-1 project config (`.sidekick/project.yaml`)`
   - Project name, classification level (open_source, internal, proprietary, trade_secret)
   - Source directories to auto-index with type annotations
   - Routing preferences per task type
   - System prompt and project-specific instructions
   - CRITICAL: no sensitive data in this file — safe to commit
   - Dependencies: none

3. `Add deterministic secret scanning on outbound requests`
   - Regex-based scanning for API keys, tokens, credentials, common secret patterns
   - Scan compiled context and full outbound payload before provider dispatch
   - Block or redact secrets based on match confidence
   - Make patterns configurable and extensible
   - Dependencies: retrieval/context pipeline stable

4. `Add outbound request policy seam`
   - Insert a policy evaluation point between context compilation and provider dispatch
   - Policy inputs: intent classification, project classification, secret scan results
   - Policy outputs: allow, redact, block, route-to-local-only
   - Keep the seam structural — full tier-0/tier-2 merge comes later
   - Dependencies: intent classification, secret scanning

5. `Add routing decisions based on sensitivity`
   - Use intent classification and project config to influence provider selection
   - `trade_secret` → local-only inference (hard deny cloud)
   - `proprietary` → cloud with redaction
   - `internal` → cloud with employer stripping
   - `open_source` → cloud with targeted retrieval only
   - Dependencies: policy seam, intent classification

6. `Add correction memory system`
   - Deferred out of milestone 05 to keep the policy seam and routing work bounded
   - Will require project-scoped persisted artifact types, retrieval injection rules, and schema/index changes beyond the current M05 storage model
   - Follow-on milestone should store user corrections as project-scoped anti-pattern artifacts and define when they are injected into future context packages
   - Dependencies: stabilized M05 policy path plus additional index/storage design work

Definition of done:

- Rillan classifies intent and sensitivity of requests when local model is available
- Secret scanning blocks or redacts known credential patterns on outbound requests
- Routing decisions respect project classification level
- Policy enforcement is structural and observable in logs

### Milestone 06 - Security Hardening and Packaging

Canonical plan: `.sisyphus/plans/rillan-milestone-06.md`

Execution shape in canonical plan:
- Part 1 — complete the security foundation
- Part 2 — minimize and trace remote egress
- Part 3 — package durable local services

Goal: complete the tiered security model and make local deployment durable on macOS and Linux.

Current status:

- Part 1 implemented in code: tier-0 encrypted system-config envelope, tier-2 runtime merge, and request-scoped policy trace surface
- Part 2 implemented in code: targeted remote retrieval minimization, append-only audit ledger, and expanded runtime readiness/status reporting
- Part 3 implemented as packaging artifacts and validation docs/scripts, but not yet fully proven through live `launchd` and `systemd --user` install/start/stop runs on target OSes

Issues:

1. `Implement tier-0 system identity config (`~/.sidekick/system.yaml`, encrypted)`
   - Personal PII patterns (name, email, phone, address)
   - PCI patterns (credit card, bank account, SSN)
   - Employer references (hashed, opaque)
   - Credential format detection rules
   - System rules (IF open_source THEN strip employer refs; IF remote_provider THEN mask PII)
   - Encrypted via OS keychain integration
   - NEVER committed, never leaves the machine
   - Dependencies: M05 policy seam

2. `Implement tier-2 runtime policy merge`
   - Ephemeral, in-memory only — never persisted in combined form
   - Load tier-0 + tier-1 → evaluate system rules against project classification
   - Scan outbound package against tier-0 patterns
   - Apply transformations (masking, stripping, abstracting)
   - Make routing decision → record in audit ledger → discard merged policy
   - Dependencies: tier-0 config, M05 policy seam

3. `Add IP fragmentation strategies`
   - Level 1 — Targeted retrieval: send only relevant section, not full file (deterministic, always on for remote)
   - Level 2 — Abstraction rewriting: generalize implementation specifics via local model before cloud send
   - Level 3 — Question extraction: distill to pure technical form with zero project context
   - Strategy selection driven by project classification level
   - Dependencies: tier-2 policy, local model helpers

4. `Implement audit ledger (append-only)`
   - Record every request/response: request ID, engine, model version
   - Hashes of outbound payload (optionally encrypted snapshot)
   - Artifacts referenced + chunk hashes
   - Policy decisions applied, response hash
   - Enable forensics ("what left my machine and why?"), reproducibility, compliance
   - Dependencies: policy pipeline

5. `Add macOS launchd packaging path`
   - launchd plist for `rillan serve` as a user-level service
   - Install/uninstall tooling or documentation
   - Log routing to system log or dedicated file
   - Dependencies: daemon startup flow stable

6. `Add Linux systemd packaging path`
   - systemd user unit file for `rillan serve`
   - Install/uninstall tooling or documentation
   - Journal integration for structured logs
   - Dependencies: daemon startup flow stable

7. `Harden release packaging and local service install flow`
   - Signed release artifacts with cosign keyless provenance
   - Cross-platform builds: `darwin` and `linux` on `amd64` and `arm64`
   - Checksum files, verification docs
   - Dependencies: OSS readiness release pipeline

Definition of done:

- Full three-tier security model operational (tier-0 encrypted, tier-1 committable, tier-2 ephemeral)
- IP fragmentation strategies available for proprietary/trade-secret projects
- Audit ledger records all egress with policy trace
- macOS and Linux have documented, repeatable service-install flows
- Release artifacts are signed and verifiable

### Milestone 07 - Agent Runtime (Post-v1)

Goal: evolve Rillan from a proxy into a local-first agent runtime with explicit orchestration, role-specific agents, and structured artifact passing.

Issues:

1. `Define agent orchestration model and role boundaries`
   - Core roles: Orchestrator, Planner, Coder, Researcher, Reviewer
   - Orchestrator: classifies requests, decides direct vs planned execution, assigns steps, synthesizes responses
   - Planner: converts goals into executable plans with dependencies and acceptance criteria
   - Coder: inspects code, produces bounded edits, runs targeted verification
   - Researcher: gathers repo facts, queries local index, prepares evidence bundles
   - Reviewer: validates implementation against plan, identifies regressions
   - Design principle: agents decide; skills execute
   - Dependencies: M05 intent classification

2. `Implement reusable skill system`
   - Initial skills: read_files, search_repo, apply_patch, run_tests, git_status, git_diff, index_lookup
   - Skills are atomic, bounded operations with typed inputs/outputs
   - No broad context transcript passing — structured artifact handoff instead
   - Dependencies: agent role definitions

3. `Add MCP integration for environment awareness`
   - Daemon connects to user environment via MCP servers (IDE, OS, collaboration tools)
   - IDE servers: open files, cursor position, compilation errors, project structure
   - OS servers: active application, clipboard, filesystem watches, terminal output
   - Collaboration servers: Git, GitHub PRs/issues, Jira/Linear tickets
   - Environment synthesizer merges MCP signals into coherent snapshot
   - Dependencies: agent orchestration

4. `Implement action gating and confirmation`
   - v1 policy: ALL actions require explicit user confirmation
   - Future evolution: per-action-type trust levels, per-MCP-server trust levels
   - Action proposals returned to client; execution only on approval
   - Dependencies: MCP integration, agent roles

5. `Add context package builder for agent handoffs`
   - Provider-agnostic Context Package: task, constraints, working_memory, facts, evidence, open_questions, output_schema, budget, policy_trace
   - Each agent step gets a compiled package, not raw conversation history
   - Budget enforcement: token/size limits per section
   - Dependencies: context compiler (M03), policy pipeline (M05)

Definition of done:

- Rillan can orchestrate multi-step tasks through role-specific agents
- Agent handoffs use structured artifacts, not raw transcripts
- MCP provides real-time environment context to the agent runtime
- All agent actions are gated by user confirmation

### OSS Readiness

Goal: make the repository publishable, governable, and releasable as an open-source project. Can run in parallel with M04+.

Issues:

1. `Add Apache-2.0 license and rename-friendly ownership wording`
   - Use broad-adoption licensing
   - Keep temporary ownership naming easy to update later
   - Dependencies: none

2. `Add contributing guide with enforced DCO workflow`
   - Document DCO sign-off requirements
   - Keep CLA out for now
   - Dependencies: none

3. `Add SECURITY.md using GitHub Security Advisories`
   - Define private disclosure path
   - Dependencies: none

4. `Add Contributor Covenant and community templates`
   - Add code of conduct, issue templates, PR template
   - Dependencies: none

5. `Add Rillan-native governance and doctrine docs`
   - `THESIS.md` — the four foundational principles (context as local asset, providers as interchangeable solvers, acquisition+packaging as product, audit as default)
   - `PRINCIPLES.md` — code-first not code-only, headless daemon, no UI assumptions
   - `GUARDRAILS.md` — local binding default, Anthropic policy, no unofficial access, indexing constraints
   - `GOVERNANCE.md` — contribution model, decision-making, maintainer responsibilities
   - Dependencies: current planning decisions captured

6. `Add public Now/Next/Later roadmap`
   - Keep external roadmap brief and comprehensible
   - Dependencies: milestone sequence stable enough for publication

7. `Add signed release workflow with cosign keyless provenance`
   - GitHub Releases first
   - `darwin` and `linux` on `amd64` and `arm64`
   - Any GitHub-verified commit signature accepted on protected branches
   - Dependencies: repo publishing posture stable

8. `Add release checklist and artifact verification guidance`
   - Document how users verify checksums, signatures, and provenance
   - Dependencies: signing workflow

Definition of done:

- The repo can be published publicly without obvious governance or release-process gaps
- Contributors can understand how to participate safely and correctly

## Public Roadmap Translation

The public repo roadmap should stay much simpler than Linear:

- `Now`: intent classification, secret scanning, policy seam (M05)
- `Next`: full tiered security, IP fragmentation, audit ledger, OS-native packaging (M06)
- `Later`: agent runtime with orchestration and MCP integration (M07)

## Current Known Blockers

1. The `SidekickOS` team does not yet exist in Linear, so project placement is blocked.
2. Actual issue creation should wait until the project lives under the correct team.
3. Tier-0 encrypted config requires OS keychain integration research per platform.

## Immediate Next Moves Once SidekickOS Team Exists

1. Create `Rillan` project under `SidekickOS`
2. Add milestones from this document
3. Seed issues milestone by milestone
4. Keep repo-facing roadmap and docs simpler than Linear internals
