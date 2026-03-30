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
    │ (router,    │ │(load,│ │ (openai,      │
    │  handlers)  │ │ write)│ │  anthropic*)  │
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

*Anthropic provider is declared but not implemented.

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

### `internal/providers/` -- Provider abstraction

Defines the `Provider` interface:

```go
type Provider interface {
    Name() string
    Ready() bool
    ChatCompletions(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error)
}
```

`New()` factory function selects the provider implementation based on config. Currently only `internal/providers/openai` is implemented.

### `internal/providers/openai/` -- OpenAI-compatible client

HTTP client that translates Rillan's internal request types to OpenAI API calls. Handles request construction, response parsing, and error mapping.

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
| `ApprovalGate` | Gating mechanism for agent actions requiring approval |
| `ContextBuilder` | Builds execution context for agent runs |
| `Runner` | Agent execution control |
| `Orchestrator` | High-level agent orchestration |
| `MCPSnapshot*` | MCP tool and resource snapshot management |

Skills are stored as:
- **Manifest:** `<data_dir>/skills/catalog.json` -- metadata for all installed skills
- **Markdown:** `<data_dir>/skills/<id>.md` -- the actual skill content
- **Metrics:** `<data_dir>/skills/metrics.json` -- per-skill performance data

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
5. **Provider** (selected from the LLM registry) forwards the request to the upstream API
6. **Response** is returned to the client
7. **Audit** records the request/response metadata

## Configuration Flow

```
rillan init
  └─> WriteExampleConfig()  (internal/config/write_example.go)
        └─> writes config.yaml with DefaultConfig() values

rillan llm add <name> --backend openai_compatible --transport http ...
  └─> LoadConfig()           (internal/config/load.go)
  └─> append to LLMs.Providers
  └─> write config back to disk

rillan llm login <name> --api-key "sk-..."
  └─> secretstore.Save("keyring://rillan/llm/<name>", credential)
        └─> JSON-serialize credential into OS keyring

rillan serve
  └─> LoadConfig()
  └─> ResolveBearer() for default provider credential
  └─> providers.New() with resolved API key
  └─> app.Run() starts HTTP server
```

## Key Design Decisions

- **Localhost-only binding** -- the daemon only listens on `127.0.0.1`, not `0.0.0.0`
- **Keyring for secrets** -- no plaintext credentials in config files or environment; the keyring is the source of truth
- **Binding validation** -- credentials are bound to (endpoint, auth_strategy, issuer, audience); if any drift, the credential is rejected
- **Single index root** -- one directory per config, scanned manually via `rillan index`
- **SQLite for everything** -- document metadata, chunks, and vectors all live in one embedded SQLite database
- **Schema versioning** -- config files carry a `schema_version` field; v2 introduced named provider registries

See `adrs/` for formal decision records.
