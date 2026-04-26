# Architecture

This document describes Rillan's internal structure, package responsibilities, and data flow. It is intended for developers working on or integrating with the codebase.

## High-Level Overview

```
┌──────────────────────────────────────────────────┐
│                    CLI (Cobra)                    │
│   serve | init | index | status | auth | llm ... │
└──────────────────────┬───────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │      internal/app       │
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

### `cmd/rillan/` -- CLI entry point

| File | Responsibility |
|------|---------------|
| `main.go` | Signal handling, error exit |
| `root.go` | Cobra root command, subcommand registration |
| `serve.go` | `rillan serve` -- starts the HTTP daemon via `internal/app` |
| `init.go` | `rillan init` -- writes starter config files |
| `index.go` | `rillan index` -- triggers a full index rebuild |
| `auth.go` | `rillan auth login/logout/status` -- control-plane auth |
| `llm.go` | `rillan llm add/remove/list/use/login/logout` -- provider registry |
| `mcp.go` | `rillan mcp add/remove/list/use/login/logout` -- MCP registry |
| `skill.go` | `rillan skill install/remove/list/show` -- markdown skills |
| `config_commands.go` | `rillan config get/set/list` -- config inspection |
| `credential_flags.go` | Shared `--api-key`, `--access-token`, etc. flag helpers |
| `command_placeholders.go` | Stub commands for unimplemented subcommands |

### `internal/app/` -- Application orchestrator

Wires together the HTTP server, config loading, providers, retrieval pipeline, and audit system. The `serve` command calls into this package to start the daemon.

### `internal/config/` -- Configuration management

| File | Responsibility |
|------|---------------|
| `config.go` | All config struct types, constants, and defaults (`DefaultConfig()`, `DefaultProjectConfig()`, `DefaultSystemConfig()`) |
| `load.go` | YAML parsing, environment variable overrides, path resolution (`LoadConfig()`, `LoadProjectConfig()`, `LoadSystemConfig()`) |
| `write_example.go` | Generates example config files for `rillan init` |

**Three-tier config hierarchy:**

1. **Global runtime config** (`config.yaml`) -- daemon settings, named provider registries, non-secret metadata
2. **Project config** (`.rillan/project.yaml` in a repo) -- classification, provider restrictions, enabled skills
3. **System config** (`system.yaml`) -- encrypted policy and identity rules

Config loading order: YAML file -> environment variable overrides -> per-request overrides.

### `internal/httpapi/` -- HTTP layer

| Component | Responsibility |
|-----------|---------------|
| `NewRouter()` | Builds the `http.ServeMux` with all routes |
| `ChatCompletionsHandler` | Validates requests, calls provider, optionally injects retrieval context |
| `AgentTaskHandler` | Handles `/v1/agent/tasks` |
| `AgentProposalHandler` | Handles `/v1/agent/proposals/` |
| Health handlers | `/healthz` and `/readyz` |
| Middleware | Request logging, request ID injection, context management |

### `internal/providers/` -- Provider abstraction and host

Defines the `Provider` interface and the host that instantiates the selected family:

```go
type Provider interface {
    Name() string
    Ready(context.Context) error
    ChatCompletions(context.Context, openai.ChatCompletionRequest, []byte) (*http.Response, error)
}
```

Current runtime families:

- `openai_compatible/http` — shared by OpenAI, xAI, DeepSeek, Kimi, and z.ai
- `anthropic/http` — native Anthropic translation
- `ollama/internal` — internal Ollama-backed family

`internal/providers/host.go` is the seam that instantiates the default provider from runtime host config.

### `internal/providers/openai/` -- Shared OpenAI-compatible family

HTTP client used by the shared OpenAI-compatible family. OpenAI, xAI, DeepSeek, Kimi, and z.ai currently ride this family through preset-specific defaults.

### `internal/providers/anthropic/` -- Anthropic-native family

Dedicated Anthropic adapter that translates the OpenAI-style ingress request into Anthropic's `/v1/messages` shape and applies documented `x-api-key` auth.

### `internal/providers/ollama/` -- Internal Ollama family

Internal provider adapter that wraps the native Ollama client and returns an OpenAI-shaped chat completion response so the rest of the runtime can stay consistent.

### `internal/index/` -- Local corpus indexing

| Component | Responsibility |
|-----------|---------------|
| `Indexer` | Orchestrates file discovery, chunking, and vectorization |
| `Chunker` | Splits source files into fixed-line chunks |
| `Store` | SQLite persistence for documents, chunks, and vectors |
| `VectorStore` | Interface for embedding storage (Ollama or placeholder implementations) |

Data model: `SourceFile` -> `DocumentRecord` -> `ChunkRecord` -> `VectorRecord`

Storage: SQLite database at `<data_dir>/index/index.db`.

### `internal/retrieval/` -- Semantic retrieval pipeline

| Component | Responsibility |
|-----------|---------------|
| `Pipeline` | Orchestrates query embedding, optional rewriting, and vector search |
| Embedder interface | Pluggable embedding backend |
| Query rewriter | Optional query expansion via local model |

The pipeline compiles search results into context with source references, respecting `max_context_chars` truncation.

### `internal/secretstore/` -- Keyring-backed credential storage

| Function | Responsibility |
|----------|---------------|
| `Save(ref, credential)` | Serialize and write a credential to the OS keyring |
| `Load(ref)` | Read and deserialize a credential from the keyring |
| `Delete(ref)` | Remove a credential |
| `Exists(ref)` | Check if a credential exists |
| `ResolveBearer(ref, binding)` | Get a bearer token, validating that the stored binding matches |

Credentials are JSON-serialized into the keyring with binding metadata (endpoint, auth strategy, issuer, audience). `ResolveBearer` checks the binding before returning a token -- if the endpoint or strategy has changed since the credential was stored, it returns an error.

Test hooks (`SetKeyringGetForTest`, etc.) allow tests to run without a real keyring.

### `internal/agent/` -- Agent orchestration

| Component | Responsibility |
|-----------|---------------|
| `SkillCatalog` | Manages installed markdown skills (install, remove, list, metadata) |
| `SkillMetrics` | Per-skill latency tracking (invocation count, average/last latency) |
| `ToolRuntime` | Read-only `ToolSource` / `ToolExecutor` seam over passive markdown skills and bounded built-in tools |
| `ApprovalGate` | Gating mechanism for agent actions requiring approval |
| `ContextBuilder` | Builds execution context for agent runs |
| `Runner` | Agent execution control |
| `Orchestrator` | High-level agent orchestration |
| `MCPSnapshot*` | MCP tool and resource snapshot management |

Skills and tool runtime state are stored as:
- **Manifest:** `<data_dir>/skills/catalog.json` -- metadata for all installed skills
- **Markdown:** `<data_dir>/skills/<id>.md` -- the actual skill content
- **Metrics:** `<data_dir>/agent/skill_metrics.json` -- per-skill performance data

### `internal/ollama/` -- Local model integration

Client for Ollama's HTTP API. Used for:
- Real embeddings (replaces placeholder vectors)
- Query rewriting (optional, via configurable model)
- Connectivity checks (`Ping`)

### `internal/classify/` -- Content classification

| Component | Responsibility |
|-----------|---------------|
| `Classifier` interface | Categorize queries and content |
| `OllamaClassifier` | Classification via local Ollama model |
| `SafeClassifier` | Wraps a classifier with caching and safety checks |

### `internal/policy/` -- Policy evaluation

| Component | Responsibility |
|-----------|---------------|
| `Evaluator` | Evaluates requests against system policy rules |
| `Scanner` | Default policy scanner |

Policy rules control PII masking, employer reference stripping, trade-secret routing restrictions, and PCI artifact blocking.

### `internal/audit/` -- Audit logging

Ledger-based request and action tracking. Writes to the logs directory (`<state_dir>/logs/`).

### `internal/openai/` -- Shared types

Common request/response structs for OpenAI-compatible chat completion payloads, shared across the provider and HTTP layers.

### `internal/version/` -- Version info

Build-time version string.

## Request Flow

A typical chat completion request flows through:

1. **Client** sends `POST /v1/chat/completions` to `127.0.0.1:8420`
2. **Middleware** assigns a request ID, logs the request
3. **ChatCompletionsHandler** validates the request body
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
  └─> WriteExampleConfig()  (internal/config/write_example.go)
        └─> writes config.yaml with DefaultConfig() values

rillan llm add <name> --preset openai ...
  └─> LoadConfig()           (internal/config/load.go)
  └─> append to LLMs.Providers
  └─> write config back to disk

rillan llm login <name> --api-key "sk-..."
  └─> secretstore.Save("keyring://rillan/llm/<name>", credential)
        └─> JSON-serialize credential into OS keyring

rillan serve
  └─> LoadConfig()
  └─> ResolveRuntimeProviderHostConfig()
  └─> providers.NewHost()
  └─> app.Run() starts HTTP server

request hits /v1/chat/completions
  └─> classify request (if classifier configured)
  └─> preflight policy evaluation
  └─> routing.BuildCatalog() + routing.BuildStatusCatalog()
  └─> routing.Decide() selects a provider and records a route trace
  └─> selected provider handles dispatch
```

## Key Design Decisions

- **Localhost-only binding** -- the daemon only listens on `127.0.0.1`, not `0.0.0.0`
- **Keyring for secrets** -- no plaintext credentials in config files or environment; the keyring is the source of truth
- **Binding validation** -- credentials are bound to (endpoint, auth_strategy, issuer, audience); if any drift, the credential is rejected
- **Single index root** -- one directory per config, scanned manually via `rillan index`
- **SQLite for everything** -- document metadata, chunks, and vectors all live in one embedded SQLite database
- **Schema versioning** -- config files carry a `schema_version` field; v2 introduced named provider registries

See `adrs/` for formal decision records.
