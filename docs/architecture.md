<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Architecture

This document describes Rillan's internal structure, package responsibilities, and data flow. It is intended for developers working on or integrating with the codebase.

The current implementation is a Rust Cargo workspace. The package map below points to that source; Go `cmd/` and `internal/` names in older design records describe the upstream reference implementation.

Portable agent workstream continuity is a foundational use case for this Context Control Plane, alongside local context, policy, routing, and audit. The [continuity contract](workstream-continuity.md) and [requirements index](requirements.md) describe intended outcomes; the current request/agent primitives do not implement checkpoint, transfer, or resume. See [workstream continuity boundaries](#workstream-continuity-boundaries) before assigning those responsibilities.

## High-Level Overview

```
┌──────────────────────────────────────────────────┐
│                    CLI (clap)                     │
│   serve | init | index | status | auth | llm ... │
└──────────────────────┬───────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │       crates/app        │
          │  (application wiring)   │
          └────┬──────┬──────┬──────┘
               │      │      │
    ┌──────────▼──┐ ┌─▼────┐ ├──────────────┐
    │ httpapi     │ │config│ │ providers     │
    │ (router,    │ │(load,│ │ (host +       │
    │  handlers)  │ │ write)│ │  families)    │
    └──────┬──────┘ └──────┘ └──────┬───────┘
           │                        │
    ┌──────▼──────┐          ┌──────▼───────┐
    │ retrieval   │          │ upstream API  │
    │ (pipeline)  │          └──────────────┘
    └──────┬──────┘
           │
    ┌──────▼──────┐
    │ index       │
    │ (indexer,   │
    │  chunker,   │
    │  vectorstore│
    │  SQLite)    │
    └─────────────┘
```

Provider execution is host-backed: bundled presets resolve to provider families instead of every upstream becoming a separate kernel path.

## Package Map

### `crates/rillan/` -- CLI entry point

| File | Responsibility |
|------|---------------|
| `src/main.rs` | clap command tree, Tokio runtime, command dispatch, error exit |
| `src/commands/serve.rs` | `rillan serve` -- starts the HTTP daemon via `crates/app` |
| `src/commands/init.rs` | `rillan init` -- writes starter config files |
| `src/commands/index.rs` | `rillan index` -- triggers a full index rebuild |
| `src/commands/auth.rs` | `rillan auth login/logout/status` -- control-plane auth |
| `src/commands/llm.rs` | `rillan llm add/remove/list/use/login/logout` -- provider registry |
| `src/commands/mcp.rs` | `rillan mcp add/remove/list/use/login/logout` -- MCP registry |
| `src/commands/skill.rs` | `rillan skill install/remove/list/show` -- markdown skills |
| `src/commands/daemon.rs` | Daemon refresh helpers |
| `src/commands/config.rs` | Placeholder config inspection/mutation commands |

### `crates/app/` -- Application wiring

Wires together the HTTP server, config loading, providers, retrieval pipeline, and audit system. The `serve` command calls into this package to start the daemon.

`runtime_snapshot_builder.rs` builds the provider/retrieval runtime from configuration, local module trust, and keyring bindings. `runtime_manager.rs` atomically swaps those runtime snapshots on refresh. These snapshots describe daemon runtime state, not saved workstreams or operating-system processes.

### `crates/config/` -- Configuration management

| File | Responsibility |
|------|---------------|
| `src/types.rs`, `src/constants.rs`, `src/lib.rs` | Config types, constants, and defaults |
| `src/load.rs`, `src/paths.rs` | YAML parsing, environment overrides, local path resolution |
| `src/resolve.rs`, `src/validate.rs` | Provider/credential resolution and command-specific validation |
| `src/write_example.rs` | Generates example config files for `rillan init` |

**Three-tier config hierarchy:**

1. **Global runtime config** (`config.yaml`) -- daemon settings, named provider registries, non-secret metadata
2. **Project config** (`.rillan/project.yaml` in a repo) -- classification, provider restrictions, enabled skills
3. **System config** (`system.yaml`) -- encrypted policy and identity rules

Config loading order: YAML file -> environment variable overrides -> per-request overrides.

### `crates/httpapi/` -- HTTP layer

| Component | Responsibility |
|-----------|---------------|
| `src/router.rs` | Builds the axum router |
| `src/chat.rs` | Validates requests, calls provider, optionally injects retrieval context |
| `src/agent_task.rs` | Handles `/v1/agent/tasks` |
| `src/agent_proposal.rs` | Handles `/v1/agent/proposals/{id}/decision` |
| Health handlers | `/healthz` and `/readyz` |
| Middleware | Request logging, request ID injection, context management |

### `crates/providers/` -- Provider abstraction and host

Defines the [`Provider` trait](../crates/providers/src/lib.rs), with `name`, `ready`, and `chat_completions` methods, and the host that instantiates each configured family. Requests use the domain-facing `rillan_chat::ProviderRequest`; responses can carry buffered or streamed bodies.

Current runtime families:

- `openai_compatible/http` — shared by OpenAI, xAI, DeepSeek, Kimi, and z.ai
- `anthropic/http` — native Anthropic translation
- `ollama/internal` — internal Ollama-backed family
- `stdio` — out-of-process module adapters, subject to local module trust

[`src/host.rs`](../crates/providers/src/host.rs) constructs the configured provider set and its default from runtime host config. These adapters translate and dispatch requests; they do not export or resume Codex/Claude sessions.

### `crates/providers/src/openai_client.rs` -- Shared OpenAI-compatible family

HTTP client used by the shared OpenAI-compatible family. OpenAI, xAI, DeepSeek, Kimi, and z.ai currently ride this family through preset-specific defaults.

### `crates/providers/src/anthropic_client.rs` -- Anthropic-native family

Dedicated Anthropic adapter that translates the OpenAI-style ingress request into Anthropic's `/v1/messages` shape and applies documented `x-api-key` auth.

### `crates/providers/src/ollama_client.rs` -- Internal Ollama family

Internal provider adapter that wraps the native Ollama client and returns an OpenAI-shaped chat completion response so the rest of the runtime can stay consistent.

### `crates/index/` -- Local corpus indexing

| Component | Responsibility |
|-----------|---------------|
| `Indexer` | Orchestrates file discovery, chunking, and vectorization |
| `Chunker` | Splits source files into fixed-line chunks |
| `Store` | SQLite persistence for documents, chunks, and vectors |
| `VectorStore` | Interface for embedding storage (Ollama or placeholder implementations) |

Data model: `SourceFile` -> `DocumentRecord` -> `ChunkRecord` -> `VectorRecord`

Storage: SQLite database at `<data_dir>/index/index.db`.

The manual single-root indexing contract in [ADR-003](../adrs/ADR-003.md) still applies. A future multi-repository workstream records a repository set; it does not silently turn this index into a multi-root or synchronized state store.

### `crates/retrieval/` -- Semantic retrieval pipeline

| Component | Responsibility |
|-----------|---------------|
| `Pipeline` | Orchestrates query embedding, optional rewriting, and vector search |
| Embedder interface | Pluggable embedding backend |
| Query rewriter | Optional query expansion via local model |

The pipeline compiles search results into context with source references, respecting `max_context_chars` truncation.

### `crates/secretstore/` -- Keyring-backed credential storage

| Function | Responsibility |
|----------|---------------|
| `Store::save` | Serialize and write a credential to the OS keyring |
| `Store::load` | Read and deserialize a credential from the keyring |
| `Store::delete` | Remove a credential |
| `Store::exists` | Check if a credential exists |
| `Store::resolve_bearer` | Get a bearer token, validating that the stored binding matches |

Credentials are JSON-serialized into the keyring with binding metadata (endpoint, auth strategy, issuer, audience). `resolve_bearer` checks the binding before returning a token -- if the endpoint or strategy has changed since the credential was stored, it returns an error.

`Store::in_memory` and the `Backend` trait allow tests to run without a real keyring. Provider/MCP credentials remain local to each machine. The continuity use case's 1Password-backed Git authentication/signing is a separate destination-local integration, not a key-transfer responsibility of this store.

### `crates/agent/` -- Agent context, orchestration decisions, and tools

| Component | Responsibility |
|-----------|---------------|
| `SkillCatalog` | Manages installed markdown skills (install, remove, list, metadata) |
| `SkillMetrics` | Per-skill latency tracking (invocation count, average/last latency) |
| `ToolRuntime` | Read-only `ToolSource` / `ToolExecutor` seam over passive markdown skills and bounded built-in tools |
| `ApprovalGate` | Gating mechanism for agent actions requiring approval |
| `ContextPackage`, `build_context_package` | Represent and build budgeted execution context for agent runs |
| `Runner` | Agent execution control |
| `Orchestrator` | High-level agent orchestration |
| `McpSnapshot`, normalizer | Bound caller-supplied open files, selection, diagnostics, and VCS context |

Skills and tool runtime state are stored as:
- **Manifest:** `<data_dir>/skills/catalog.json` -- metadata for all installed skills
- **Markdown:** `<data_dir>/skills/<id>.md` -- the actual skill content
- **Metrics:** `<data_dir>/agent/skill_metrics.json` -- per-skill performance data

The [task handler](../crates/httpapi/src/agent_task.rs) invokes `SharedRunner` for bounded read-only skills and orchestration results. The [proposal store](../crates/agent/src/proposal_store.rs) is in memory; audit events do not make pending proposals durable. Neither a context echo nor an MCP snapshot is a completed workstream checkpoint.

### `crates/ollama/` -- Local model integration

Client for Ollama's HTTP API. Used for:
- Real embeddings (replaces placeholder vectors)
- Query rewriting (optional, via configurable model)
- Connectivity checks

### `crates/classify/` -- Content classification

| Component | Responsibility |
|-----------|---------------|
| `Classifier` interface | Categorize queries and content |
| `OllamaClassifier` | Classification via local Ollama model |
| `SafeClassifier` | Wraps a classifier with caching and safety checks |

### `crates/policy/` -- Policy evaluation

| Component | Responsibility |
|-----------|---------------|
| `Evaluator` | Evaluates requests against system policy rules |
| `Scanner` | Default policy scanner |

Policy rules control PII masking, employer reference stripping, trade-secret routing restrictions, and PCI artifact blocking.

### `crates/audit/` -- Audit logging

Ledger-based request and action tracking at `<data_dir>/audit/ledger.jsonl`, as specified by [ADR-010](../adrs/ADR-010.md). Bodies are represented by hashes, not saved transcripts; this is not a workstream reconstruction store.

### `crates/openai/` and `crates/chat/` -- Shared types

OpenAI-compatible transport shapes and the domain-facing request envelope shared by runtime layers.

### `crates/version/` -- Version info

Build-time version string.

## Workstream continuity boundaries

The [continuity contract](workstream-continuity.md#continuity-contract) is an intended requirement, not an expansion of the current daemon's runtime responsibilities. Use the existing seams when evaluating implementation:

| Boundary | Current responsibility | Intended continuity outcome or open decision |
| --- | --- | --- |
| Rillan kernel and `crates/agent` | Local policy, context construction, routing, read-only tools, and proposal decisions. | Preserve intent and human approval boundaries. Whether Rillan owns the authoritative durable workstream, with context packages as execution projections, is unresolved. |
| `crates/providers` | API-family dispatch/translation and provider readiness. | Session-aware adapters would need documented provider resume or reconstruction behavior. How they consume durable state is unresolved; the current trait does not provide it. |
| RepoKeeper MCP | Existing external repository identity-to-local-checkout resolution supplied by this workflow; not integrated into checkpoint/resume here. | Resolve portable repository identities through RepoKeeper, then validate the worktree and enforce local approved-root/path rules. Do not replace RepoKeeper or treat resolution as authorization. |
| Local configuration, credentials, and runtime | Machine-specific config, trust, keyring bindings, indexes, and daemon state. | Check destination compatibility, use its own credentials, and recreate required local services. Exclude credentials and live processes from portable state. |
| Lyra and coordinating interfaces | No Lyra component or interface is defined in the current repository. | Any wider Lyra responsibility for sessions/environment snapshots needs an authoritative design reference and an explicit boundary. Dots or another interface may coordinate handoff; the core capability must not depend on one interface or subscription. |

Do not equate request-level context injection or switching providers with agent-session continuity. Same-provider native resume, same-provider reconstruction, and Claude/GPT cross-provider continuation have [different fidelity guarantees](workstream-continuity.md#wsc-09-explicit-continuation-fidelity).

Multi-repository checkpoints must preserve each repository's Git/dirty state and establish explicit ownership before continuation. This does not relax the single-root/manual-indexing rule, approved-root checks, local module trust, policy enforcement, or config/data/log separation. The core flow must work after a completed checkpoint with the source locked/offline, over allowed transfer paths without Tailscale or an always-on host. Keep the default loopback bind; continuity does not require exposing the daemon remotely.

The [first milestone](workstream-continuity.md#first-milestone-scope) is explicit handoff at a safe stopping point within a bounded provider/platform scope. Durable-state authority, Rillan/Lyra ownership, checkpoint representation, transfer protocol, and ownership coordination remain [design questions](workstream-continuity.md#unresolved-design-questions), not accepted mechanisms or a new component structure.

## Request Flow

A typical chat completion request flows through:

1. **Client** sends `POST /v1/chat/completions` to `127.0.0.1:8420`
2. **Middleware** assigns a request ID, logs the request
3. **Chat handler** validates the request body
4. If retrieval is enabled (globally or per-request):
   a. **Retrieval Pipeline** embeds the query, optionally rewrites it
   b. **Index Store** performs vector similarity search
   c. Retrieved context is injected into the messages
5. **Routing** builds the candidate set, filters it by policy + availability, and selects the winning provider
6. **Provider host** resolves the selected family/preset entry and forwards the request to the upstream API or internal Ollama path
7. **Response** is returned to the client
8. **Audit** records the request/response metadata

Current route choice is deterministic and explainable:

1. policy eligibility
2. exact requested-model match (when any candidate advertises it)
3. required capability gating (`tool_calling`, `multimodal`)
4. route preference fit
5. task-strength fit
6. stable tie-break by provider ID

The router now prefers exact matches from `candidate.model_pins`, falling back to `default_model` only when explicit pins are absent.

## Configuration Flow

```
rillan init
  └─> crates/config/src/write_example.rs
        └─> writes starter config.yaml

rillan llm add <name> --preset openai ...
  └─> crates/config/src/load.rs
  └─> append to llms.providers
  └─> write config back to disk

rillan llm login <name> --api-key "sk-..."
  └─> Store::save("keyring://rillan/llm/<name>", credential)
        └─> JSON-serialize credential into OS keyring

rillan serve
  └─> load config and project/system policy
  └─> App::new() builds the runtime snapshot and provider Host
  └─> App::run() starts the HTTP server

request hits /v1/chat/completions
  └─> classify request (if classifier configured)
  └─> preflight policy evaluation
  └─> build/evaluate routing candidates and readiness
  └─> select a provider and record a route trace
  └─> selected provider handles dispatch
```

## Key Design Decisions

- **Local bind by default** -- `127.0.0.1:8420` remains the default ([ADR-002](../adrs/ADR-002.md)); the [reference](reference.md#secrets) documents the existing explicit non-loopback/auth guard
- **Keyring for stored secrets** -- checked-in/runtime YAML holds credential references; local secrets do not belong in portable checkpoints
- **Binding validation** -- credentials are bound to (endpoint, auth_strategy, issuer, audience); if any drift, the credential is rejected
- **Single index root** -- one directory per config, scanned manually via `rillan index`
- **SQLite for the local corpus** -- document metadata, chunks, and vectors live in one embedded database; this does not establish a durable workstream store
- **Schema versioning** -- config files carry a `schema_version` field; v2 introduced named provider registries

See [ADRs](../adrs/) for formal decision records and the [requirements index](requirements.md) for the intended continuity contract.
