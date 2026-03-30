# Rillan

Rillan is a local OpenAI-compatible proxy daemon written in Go. It sits between your local tools and upstream AI providers, adding local indexing, semantic retrieval, credential management, policy enforcement, and agent orchestration on top of a standard `/v1/chat/completions` interface.

## Features

- **OpenAI-compatible API** on `127.0.0.1:8420` -- drop-in replacement for local tools that speak the OpenAI protocol
- **Named LLM providers** -- register multiple providers (OpenAI, Kimi, local models) and switch between them
- **Named MCP endpoints** -- register Model Context Protocol servers for tool and resource access
- **Secure credential storage** -- API keys and tokens live in your OS keyring, never in plaintext YAML
- **Local indexing** -- chunk and embed your codebase into SQLite for semantic retrieval
- **Markdown skills** -- install reusable instruction sets that shape agent behavior
- **Project-level config** -- per-repo classification, provider restrictions, and routing rules
- **Policy enforcement** -- system-level rules for PII masking, credential stripping, and routing constraints
- **Optional local models** -- Ollama integration for embeddings and query rewriting without leaving your machine

## Requirements

- Go 1.25+ (module requirement)
- A system keyring (macOS Keychain, GNOME Keyring, KWallet, or Windows Credential Manager)
- An upstream LLM provider account (e.g., OpenAI API key)
- Optional: [Ollama](https://ollama.ai) for local embeddings and query rewriting

## Quickstart

### 1. Build

```bash
go build -o rillan ./cmd/rillan
```

Or run directly with `go run ./cmd/rillan` in place of `rillan` below.

### 2. Initialize configuration

```bash
rillan init
```

This writes a starter `config.yaml` to your platform's config directory (see [File Locations](#file-locations)).

### 3. Add an LLM provider

```bash
rillan llm add work-gpt \
  --backend openai_compatible \
  --transport http \
  --endpoint https://api.openai.com/v1 \
  --default-model gpt-4o

rillan llm login work-gpt \
  --api-key "$OPENAI_API_KEY"

rillan llm use work-gpt
```

### 4. Start the daemon

```bash
rillan serve
```

### 5. Send a request

```bash
curl -s http://127.0.0.1:8420/healthz

curl -X POST http://127.0.0.1:8420/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}'
```

### 6. Index a codebase (optional)

Set `index.root` in your config to point at a local repo, then:

```bash
rillan index
rillan status
```

## CLI Reference

### Core commands

| Command | Description |
|---------|-------------|
| `rillan serve` | Start the proxy daemon |
| `rillan init` | Write starter config files |
| `rillan index` | Build the local corpus index |
| `rillan status` | Show index state, corpus counts, and local model connectivity |

### Provider management

| Command | Description |
|---------|-------------|
| `rillan llm add <name>` | Register a named LLM provider |
| `rillan llm remove <name>` | Remove a named LLM provider |
| `rillan llm list` | List all configured LLM providers |
| `rillan llm use <name>` | Set the default LLM provider |
| `rillan llm login <name>` | Store credentials for a provider |
| `rillan llm logout <name>` | Clear stored credentials |

**`llm add` flags:**

| Flag | Description |
|------|-------------|
| `--backend` | Provider backend identity (current runtime supports `openai_compatible`) |
| `--transport` | Provider transport: `http`, `stdio` |
| `--endpoint` | Provider API base URL |
| `--command` | Repeatable. Command vector for `stdio` transport |
| `--auth-strategy` | Auth method: `none`, `api_key`, `browser_oidc`, `device_oidc` |
| `--default-model` | Default model name for requests |
| `--capability` | Repeatable. Capability tags (e.g., `chat`, `reasoning`, `tool_calling`) |

### MCP endpoint management

| Command | Description |
|---------|-------------|
| `rillan mcp add <name>` | Register a named MCP endpoint |
| `rillan mcp remove <name>` | Remove a named MCP endpoint |
| `rillan mcp list` | List all configured MCP endpoints |
| `rillan mcp use <name>` | Set the default MCP endpoint |
| `rillan mcp login <name>` | Store credentials for an endpoint |
| `rillan mcp logout <name>` | Clear stored credentials |

**`mcp add` flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--endpoint` | | Endpoint URL |
| `--transport` | `http` | Transport type: `http`, `stdio` |
| `--auth-strategy` | `none` | Auth method: `none`, `api_key`, `browser_oidc`, `device_oidc` |
| `--read-only` | `true` | Restrict to read-only operations |

### Credential flags (shared by `login` subcommands)

| Flag | Description |
|------|-------------|
| `--api-key` | API key (for `api_key` auth strategy) |
| `--access-token` | Access token (for OIDC strategies) |
| `--refresh-token` | Refresh token |
| `--id-token` | OIDC ID token |
| `--issuer` | OIDC issuer URL bound to the session |
| `--audience` | OIDC audience bound to the session |

### Skill management

| Command | Description |
|---------|-------------|
| `rillan skill install <path>` | Copy a markdown skill into managed storage |
| `rillan skill remove <id>` | Remove an installed skill (`--force` to override project refs) |
| `rillan skill list` | List installed skills |
| `rillan skill show <id>` | Show metadata for an installed skill |

### Authentication

| Command | Description |
|---------|-------------|
| `rillan auth login` | Log into a Rillan team/control-plane endpoint |
| `rillan auth logout` | Clear control-plane session |
| `rillan auth status` | Show control-plane auth state |

`auth` is reserved for Rillan team or control-plane endpoints. Provider-specific auth uses `llm login` and `mcp login`.

### Configuration inspection

| Command | Description |
|---------|-------------|
| `rillan config get` | Read a configuration value |
| `rillan config set` | Write a configuration value |
| `rillan config list` | List configuration values |

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Liveness probe -- always returns `200` |
| `GET` | `/readyz` | Readiness probe -- returns `200` when the daemon is ready to serve |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat completion passthrough |
| `GET` | `/v1/agent/tasks` | Agent task listing |
| `GET/POST` | `/v1/agent/proposals/` | Agent proposal submission and retrieval |

### Per-request retrieval override

Include a `retrieval` field in the chat completion body to enable local context injection:

```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "summarize this repo"}],
  "retrieval": {"enabled": true, "top_k": 4}
}
```

## Configuration

Rillan uses a three-tier configuration hierarchy. See `configs/rillan.example.yaml` and `configs/project.example.yaml` for annotated references.

### Global runtime config

The primary config file stores daemon settings, named LLM providers, named MCP endpoints, and non-secret metadata. Written by `rillan init` and mutated by CLI commands.

Key sections:

| Section | Purpose |
|---------|---------|
| `server` | Host, port, log level |
| `auth` | Control-plane endpoint and session reference |
| `llms` | Named LLM provider registry with default selection |
| `mcps` | Named MCP endpoint registry with default selection |
| `index` | Index root directory, include/exclude globs, chunk size |
| `retrieval` | Enable/disable retrieval, top-k, max context size |
| `local_model` | Ollama integration for embeddings and query rewriting |
| `agent` | Agent and MCP runtime toggles |
| `runtime` | Vector store mode and local model base URL |

### Project config (repo-local)

Place a `project.yaml` in your repo's `.rillan/` directory to control per-project behavior:

```yaml
name: "my-project"
classification: "internal"   # open_source | internal | proprietary | trade_secret

providers:
  llm_default: "work-gpt"
  llm_allowed: ["work-gpt", "local-qwen"]
  mcp_enabled: ["ide-local"]

agent:
  skills:
    enabled: ["go-dev"]

instructions:
  - "Keep outbound context tightly bounded to the current task."
```

### System config (machine-local)

Encrypted policy and identity rules stored in `system.yaml`. Managed by tooling, not hand-edited.

### Secrets

All secrets (API keys, tokens, OIDC bundles) are stored in the OS keyring. Config files reference them via `keyring://` URIs (e.g., `keyring://rillan/llm/openai`). Never put secrets in YAML.

## File Locations

| Purpose | macOS | Linux |
|---------|-------|-------|
| Config | `~/Library/Application Support/rillan/config.yaml` | `${XDG_CONFIG_HOME:-~/.config}/rillan/config.yaml` |
| Data | `~/Library/Application Support/rillan/data/` | `${XDG_DATA_HOME:-~/.local/share}/rillan/` |
| Logs | `~/Library/Logs/rillan/` | `${XDG_STATE_HOME:-~/.local/state}/rillan/logs/` |

The data directory holds the SQLite index database (`index/index.db`), installed markdown skills, and skill metrics state.

## Environment Variable Overrides

Environment variables are supported for backward compatibility and CI/automation, but CLI commands are the preferred setup path.

| Variable | Maps to |
|----------|---------|
| `RILLAN_SERVER_HOST` | `server.host` |
| `RILLAN_SERVER_PORT` | `server.port` |
| `RILLAN_SERVER_LOG_LEVEL` | `server.log_level` |
| `RILLAN_PROVIDER_TYPE` | `provider.type` |
| `RILLAN_OPENAI_BASE_URL` | `provider.openai.base_url` |
| `RILLAN_OPENAI_API_KEY` / `OPENAI_API_KEY` | `provider.openai.api_key` |
| `RILLAN_INDEX_ROOT` | `index.root` |
| `RILLAN_INDEX_INCLUDES` | `index.includes` |
| `RILLAN_INDEX_EXCLUDES` | `index.excludes` |
| `RILLAN_INDEX_CHUNK_SIZE_LINES` | `index.chunk_size_lines` |
| `RILLAN_RETRIEVAL_ENABLED` | `retrieval.enabled` |
| `RILLAN_RETRIEVAL_TOP_K` | `retrieval.top_k` |
| `RILLAN_RETRIEVAL_MAX_CONTEXT_CHARS` | `retrieval.max_context_chars` |
| `RILLAN_LOCAL_MODEL_ENABLED` | `local_model.enabled` |
| `RILLAN_LOCAL_MODEL_BASE_URL` | `local_model.base_url` |
| `RILLAN_LOCAL_MODEL_EMBED_MODEL` | `local_model.embed_model` |
| `RILLAN_LOCAL_MODEL_QUERY_REWRITE_ENABLED` | `local_model.query_rewrite.enabled` |
| `RILLAN_LOCAL_MODEL_QUERY_REWRITE_MODEL` | `local_model.query_rewrite.model` |
| `RILLAN_VECTOR_STORE_MODE` | `runtime.vector_store_mode` |

## Provider Policy

- OpenAI-compatible upstreams are the default provider path.
- Anthropic is represented in config as a future-facing seam but is not implemented as a runtime provider.
- No unofficial access paths, shared credentials, scraped sessions, or browser-cookie flows are supported.

## Development

See [docs/development.md](docs/development.md) for build instructions, test conventions, and contribution guidance.

See [docs/architecture.md](docs/architecture.md) for a walkthrough of internal packages and data flow.

## Architecture Decisions

Repo-local ADRs are in [`adrs/`](adrs/):

- **ADR-001** -- OpenAI-compatible upstream as the first real provider path
- **ADR-002** -- Localhost bind and local config/data/log defaults
- **ADR-003** -- One explicit local root, manual indexing, and embedded SQLite storage

## Deployment

Systemd and launchd unit files are in `packaging/`:

- `packaging/systemd/` -- Linux service files
- `packaging/launchd/` -- macOS plist daemon files
