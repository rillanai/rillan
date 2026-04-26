# Rillan (Rust)

Every AI-powered dev tool you use — Claude Code, Cursor, Copilot, opencode — sends your code, your prompts, and your context to a remote API. You trust each tool to handle credentials, route to the right model, and not leak trade secrets. That trust is implicit, spread across a dozen configs, and invisible when it breaks.

Rillan is the local-first Context Control Plane kernel: a Rust daemon that sits between your tools and the LLM providers they call so context, policy, routing, and audit stay under your control. This crate is the in-progress Rust port of the upstream Go daemon at `github.com/rillanai/rillan` — behavior, ADRs, and config schema track the upstream verbatim.

## What it does

**One endpoint, many providers.** Register OpenAI, Anthropic, xAI, DeepSeek, Kimi, z.ai, or a local Ollama instance. Rillan exposes a single OpenAI-compatible API on `127.0.0.1:8420`. Point your tools at it and switch providers without reconfiguring each one.

**Credentials stay in your keyring.** API keys and tokens live in your OS keyring (macOS Keychain, GNOME Keyring, KWallet, Windows Credential Manager), never in plaintext YAML. Each credential is bound to its endpoint and auth strategy — if the config drifts, the credential is rejected rather than sent to the wrong place.

**Policy enforcement before anything leaves your machine.** A regex-based scanner checks every outbound request for API keys, tokens, private keys, and other secrets. Findings are redacted or blocked before the request reaches a provider. Trade-secret classified repos are automatically routed to local models only.

**Deterministic routing you can debug.** Each request produces a full decision trace showing which providers were considered, which were rejected, and why. Route preferences can be set per-project and per-task-type. The same inputs always produce the same provider selection.

**Local context injection.** Index a codebase into SQLite, then Rillan injects relevant chunks into your requests using hybrid vector + keyword search. No external services required — embeddings run locally via Ollama.

**Per-project control.** Drop a `.rillan/project.yaml` in a repo to restrict which providers can see that code, override routing preferences, and set classification levels that drive policy.

## Who this is for

- Developers who use multiple AI coding tools and want one place to manage provider credentials and routing.
- Teams that need to enforce data classification policies (internal, proprietary, trade secret) on LLM interactions.
- Anyone who wants to see exactly what's being sent to which provider, rather than trusting each tool's opaque proxy layer.

## Quickstart

### 1. Build

```bash
cargo build -p rillan --release
```

Or run directly with `cargo run -p rillan --` in place of `rillan` below.

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

### 4. Start the daemon

```bash
rillan serve
```

### 5. Point your tools at it

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8420/v1
curl -s http://127.0.0.1:8420/healthz

curl -X POST http://127.0.0.1:8420/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}'
```

### 6. Index a codebase (optional)

Indexing the repository under `~/code/myrepo` writes the SQLite index to the user data dir
(`~/Library/Application Support/rillan/data/` on macOS, `$XDG_DATA_HOME/rillan/`
or `~/.local/share/rillan/` on Linux). Set `index.root` in your config first; the
indexer refuses to run without an explicit root.

```bash
rillan index
```

## Status of the Rust port

The Rust port is in active development. The first delivered milestone is
config loading + policy scanning + an OpenAI-compatible chat-completions
proxy on `127.0.0.1:8420`. See `docs/development.md` and the ADRs in `adrs/`
for the full roadmap. The Go upstream remains the reference implementation
until the Rust port reaches behavioral parity.

## Project layout

```text
crates/rillan/         Binary crate; clap CLI entrypoints.
crates/app/            Daemon wiring and lifecycle.
crates/config/         YAML config, env overrides, validation modes.
crates/httpapi/        HTTP router, handlers, middleware.
crates/openai/         OpenAI-compatible request/response shapes.
crates/providers/      Upstream provider seam and HTTP client.
crates/policy/         Regex-based secret scanner and policy evaluator.
crates/secretstore/    OS keyring abstraction.
crates/observability/  Request-id propagation and metrics primitives.
crates/version/        Build metadata.
crates/chat/           Chat-request value types shared between layers.
configs/               Reference configuration.
testdata/              Test configs and smoke fixtures.
adrs/                  Binding architecture decisions.
```

## License

Apache-2.0 — see `LICENSE` and `NOTICES`.
