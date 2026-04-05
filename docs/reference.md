# Reference

Quick-reference tables for the Rillan CLI, HTTP API, configuration, and file layout.

## CLI Commands

### Core

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
| `--preset` | Bundled preset: `openai`, `anthropic`, `xai`, `deepseek`, `kimi`, `zai` |
| `--backend` | Provider backend identity for manual entries |
| `--transport` | Provider transport: `http`, `stdio` |
| `--endpoint` | Provider API base URL |
| `--command` | Repeatable. Command vector for `stdio` transport |
| `--auth-strategy` | Auth method: `none`, `api_key`, `browser_oidc`, `device_oidc` |
| `--default-model` | Default model name for requests |
| `--capability` | Repeatable. Capability tags (e.g., `chat`, `reasoning`, `tool_calling`) |

Bundled runtime families:

- shared `openai_compatible/http` for OpenAI, xAI, DeepSeek, Kimi, and z.ai
- native `anthropic/http`
- internal `ollama`

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
| `rillan config get` | Placeholder command; not implemented yet |
| `rillan config set` | Placeholder command; not implemented yet |
| `rillan config list` | Placeholder command; not implemented yet |

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Liveness probe -- always returns `200` and stays unauthenticated |
| `GET` | `/readyz` | Readiness probe -- returns `200` when the daemon is ready and `503` with degraded provider details when upstream readiness fails |
| `GET` | `/metrics` | Prometheus-style runtime metrics; requires `Authorization: Bearer <token>` when `server.auth.enabled` is true |
| `POST` | `/v1/chat/completions` | OpenAI-compatible ingress with routed provider dispatch; requires `Authorization: Bearer <token>` when `server.auth.enabled` is true |
| `POST` | `/v1/agent/tasks` | Agent task execution/proposal endpoint; requires `Authorization: Bearer <token>` when `server.auth.enabled` is true |
| `POST` | `/v1/agent/proposals/{id}/decision` | Agent proposal approval/denial endpoint; requires `Authorization: Bearer <token>` when `server.auth.enabled` is true |

When `server.auth.enabled` is true, all non-health HTTP endpoints (including `/metrics`) require a bearer token. Missing or invalid bearer tokens return `401 Unauthorized` with `WWW-Authenticate: Bearer`.

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

| Section | Purpose |
|---------|---------|
| `server` | Host, port, log level, daemon bind guard, and optional daemon endpoint auth |
| `auth` | Control-plane endpoint and session reference |
| `llms` | Named LLM provider registry with default selection |
| `mcps` | Named MCP endpoint registry with default selection |
| `index` | Index root directory, include/exclude globs, chunk size |
| `retrieval` | Enable/disable retrieval, top-k, max context size |
| `local_model` | Ollama integration for embeddings and query rewriting |
| `agent` | Agent toggles, approved repo roots, and MCP runtime limits |
| `runtime` | Vector store mode and local model base URL |

Provider entries can also declare `model_pins` to tell the router which exact model names should prefer that entry.

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

Encrypted policy and identity rules stored in `~/.rillan/system.yaml` by default, with legacy `.sidekick/system.yaml` still recognized as a fallback. Managed by tooling, not hand-edited.

Repo-local modules become runtime-active only when both of these are true:
- the repo enables them via `project.modules.enabled`
- the local machine trusts them in encrypted system policy via `trusted_modules`

Each trusted module record is matched by repo root, module ID, and manifest SHA-256. Module-provided `stdio` adapters additionally require `allow_stdio: true` in the trust record.

### Secrets

All secrets (API keys, tokens, OIDC bundles) are stored in the OS keyring. Config files reference them via `keyring://` URIs (e.g., `keyring://rillan/llm/openai`). Never put secrets in YAML.

If `server.auth.enabled` is true, `server.auth.session_ref` must point at a keyring-backed credential containing either an `api_key` or `access_token`. The daemon treats that value as an opaque bearer token for inbound HTTP authentication.

If `server.host` is not loopback, `server.allow_non_loopback_bind` must be true and `server.auth.enabled` must also be true. The current runtime supports wildcard non-loopback binds (`0.0.0.0`, `::`) but not specific non-loopback interface addresses.

If `agent.approved_repo_roots` is set, agent read-only repo tools only operate on exact approved repo roots after canonical path resolution. Caller-supplied repo roots outside that allowlist are rejected.

The daemon exposes low-cardinality Prometheus-style counters for HTTP requests, provider outcomes, and retrieval usage at `/metrics`.

### Routing behavior

- Route preferences come from `project.routing.default` and `project.routing.task_types`.
- An exact `request.model` match outranks normal routing when a known candidate advertises that model.
- Explicit provider `model_pins` are used for exact model affinity before falling back to `default_model`.
- Requests that need `tool_calling` or `multimodal` capabilities exclude candidates that do not advertise them.
- Policy verdicts can force local-only handling before provider selection.
- Candidate availability considers configuration, auth validity, and provider readiness.
- Tie-breaks stay deterministic by provider ID when candidates are otherwise equal.

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
| `RILLAN_SERVER_ALLOW_NON_LOOPBACK_BIND` | `server.allow_non_loopback_bind` |
| `RILLAN_SERVER_AUTH_ENABLED` | `server.auth.enabled` |
| `RILLAN_SERVER_AUTH_STRATEGY` | `server.auth.auth_strategy` |
| `RILLAN_SERVER_AUTH_SESSION_REF` | `server.auth.session_ref` |
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
- Anthropic is implemented as a native runtime provider family behind the provider host.
- No unofficial access paths, shared credentials, scraped sessions, or browser-cookie flows are supported.
