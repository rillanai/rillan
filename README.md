# Rillan

Every AI-powered dev tool you use -- Claude Code, Cursor, Copilot, opencode -- sends your code, your prompts, and your context to a remote API. You trust each tool to handle credentials, route to the right model, and not leak trade secrets. That trust is implicit, spread across a dozen configs, and invisible when it breaks.

Rillan is a local daemon that sits between your tools and the LLM providers they call. It gives you one place to control what goes out, where it goes, and what comes back.

## What it does

**One endpoint, many providers.** Register OpenAI, Anthropic, xAI, DeepSeek, Kimi, z.ai, or a local Ollama instance. Rillan exposes a single OpenAI-compatible API on `127.0.0.1:8420`. Point your tools at it and switch providers without reconfiguring each one.

**Credentials stay in your keyring.** API keys and tokens live in your OS keyring (macOS Keychain, GNOME Keyring, KWallet, Windows Credential Manager), never in plaintext YAML. Each credential is bound to its endpoint and auth strategy -- if the config drifts, the credential is rejected rather than sent to the wrong place.

**Policy enforcement before anything leaves your machine.** A regex-based scanner checks every outbound request for API keys, tokens, private keys, and other secrets. Findings are redacted or blocked before the request reaches a provider. Trade-secret classified repos are automatically routed to local models only.

**Deterministic routing you can debug.** Each request produces a full decision trace showing which providers were considered, which were rejected, and why. Route preferences can be set per-project and per-task-type. The same inputs always produce the same provider selection.

**Local context injection.** Index a codebase into SQLite, then Rillan injects relevant chunks into your requests using hybrid vector + keyword search. No external services required -- embeddings run locally via Ollama.

**Per-project control.** Drop a `.rillan/project.yaml` in a repo to restrict which providers can see that code, override routing preferences, and set classification levels that drive policy.

## Who this is for

- Developers who use multiple AI coding tools and want one place to manage provider credentials and routing.
- Teams that need to enforce data classification policies (internal, proprietary, trade secret) on LLM interactions.
- Anyone who wants to see exactly what's being sent to which provider, rather than trusting each tool's opaque proxy layer.

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

### 3. Add an LLM provider

```bash
rillan llm add work-gpt \
  --preset openai \
  --default-model gpt-4o

rillan llm login work-gpt \
  --api-key "$OPENAI_API_KEY"

rillan llm use work-gpt
```

This example overrides the bundled OpenAI preset default model (`gpt-5`) with `gpt-4o`.

### 4. Start the daemon

```bash
rillan serve
```

### 5. Point your tools at it

Any tool that supports a custom OpenAI base URL can use Rillan:

```bash
# Claude Code, Cursor, or any OpenAI-compatible client
export OPENAI_BASE_URL=http://127.0.0.1:8420/v1

# Or test directly
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

Retrieval can be enabled per-request or in the daemon config. See [docs/reference.md](docs/reference.md#per-request-retrieval-override) for details.

## Adding more providers

```bash
# Local Ollama for sensitive work
rillan llm add local-qwen \
  --preset ollama \
  --default-model qwen3:8b

# Anthropic
rillan llm add claude \
  --preset anthropic \
  --default-model claude-sonnet-4-5
rillan llm login claude --api-key "$ANTHROPIC_API_KEY"
```

Then set per-project routing in `.rillan/project.yaml`:

```yaml
name: "my-project"
classification: "proprietary"

providers:
  llm_default: "work-gpt"
  llm_allowed: ["work-gpt", "local-qwen"]

routing:
  default: "prefer_local"
  task_types:
    code_generation: "prefer_cloud"
    review: "prefer_local"
```

## Requirements

- Go 1.25+
- A system keyring (macOS Keychain, GNOME Keyring, KWallet, or Windows Credential Manager)
- An upstream LLM provider account (e.g., OpenAI API key)
- Optional: [Ollama](https://ollama.ai) for local embeddings, query rewriting, and local inference

## Documentation

| Doc | What it covers |
|-----|---------------|
| [docs/reference.md](docs/reference.md) | CLI commands, HTTP API, config schema, env vars, file locations |
| [docs/command-native-setup.md](docs/command-native-setup.md) | Step-by-step setup workflows for providers, MCP, skills, and project config |
| [docs/architecture.md](docs/architecture.md) | Internal packages, request flow, and design decisions |
| [docs/development.md](docs/development.md) | Build instructions, test conventions, and contribution guidance |

## Architecture Decisions

Repo-local ADRs are in [`adrs/`](adrs/):

- **ADR-001** -- OpenAI-compatible upstream as the first real provider path
- **ADR-002** -- Localhost bind and local config/data/log defaults
- **ADR-003** -- One explicit local root, manual indexing, and embedded SQLite storage
- **ADR-005** -- Deterministic routing with decision tracing
- **ADR-006** -- Multi-provider host with preset-based capability declaration
- **ADR-007** -- Three-tier configuration with validation modes
- **ADR-008** -- Policy evaluation with four-verdict model
- **ADR-009** -- Hybrid retrieval with reciprocal rank fusion
- **ADR-010** -- Audit ledger as append-only JSONL
- **ADR-011** -- Local intent classification with graceful degradation

## Deployment

Systemd and launchd unit files are in `packaging/`:

- `packaging/systemd/` -- Linux service files
- `packaging/launchd/` -- macOS plist daemon files
