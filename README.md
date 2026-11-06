<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Rillan (Rust)

Every AI-powered dev tool you use — Claude Code, Cursor, Copilot, opencode — sends your code, your prompts, and your context to a remote API. You trust each tool to handle credentials, route to the right model, and not leak trade secrets. That trust is implicit, spread across a dozen configs, and invisible when it breaks.

Rillan is the local-first Context Control Plane kernel: a Rust daemon that sits between your tools and the LLM providers they call so context, policy, routing, and audit stay under your control. This crate is the in-progress Rust port of the upstream Go daemon at `github.com/rillanai/rillan`. It tracks the upstream runtime behavior, ADRs, and config schema; the documentation distinguishes that implementation from intended product requirements.

## Foundational use case: continue your work on another machine

Portable agent workstreams were part of Rillan's original motivation. A developer working interactively with an agent should be able to pause on a Windows desktop running WSL, continue on a MacBook, and later move to native Linux with their intent, decisions, corrections, and unfinished work intact. That includes work across multiple repositories and dirty working trees, without manually writing a handoff summary, explaining the project again, or making a WIP commit.

The intended contract uses RepoKeeper's existing MCP repository resolution to find each destination checkout, preserves human steering and approval boundaries, and uses the destination's own credentials. It must work after a completed checkpoint even when the source is locked or offline, without requiring Tailscale, an always-on host, Dots, or a particular interface subscription.

**Status: foundational requirement; end-to-end continuity is not implemented.** Current context packages, approval gates, and provider routing are building blocks. Switching an API provider does not resume a Codex or Claude session. Native same-provider resume and reconstructed continuation across providers have different fidelity guarantees.

Read the [workstream continuity use case and contract](docs/workstream-continuity.md), [requirements index](docs/requirements.md), [architecture boundaries](docs/architecture.md#workstream-continuity-boundaries), and [first continuity milestone](ROADMAP.md#c1-explicit-workstream-handoff).

## What it does

**One endpoint, many providers.** Register OpenAI, Anthropic, xAI, DeepSeek, Kimi, z.ai, or a local Ollama instance. Rillan exposes a single OpenAI-compatible API on `127.0.0.1:8420`. Point your tools at it and switch providers without reconfiguring each one.

**Credentials stay in your keyring.** API keys and tokens live in your OS keyring (macOS Keychain, GNOME Keyring, KWallet, Windows Credential Manager), never in plaintext YAML. Each credential is bound to its endpoint and auth strategy — if the config drifts, the credential is rejected rather than sent to the wrong place.

**Policy enforcement before anything leaves your machine.** A regex-based scanner checks every outbound request for API keys, tokens, private keys, and other secrets. Findings are redacted or blocked before the request reaches a provider. Trade-secret classified repos are automatically routed to local models only.

**Deterministic routing you can debug.** Each request produces a full decision trace showing which providers were considered, which were rejected, and why. Route preferences can be set per-project and per-task-type. The same inputs always produce the same provider selection.

**Local context injection.** Index a codebase into SQLite, then Rillan injects relevant chunks into your requests using hybrid vector + keyword search. No external services required — embeddings run locally via Ollama.

**Per-project control.** Drop a `.rillan/project.yaml` in a repo to restrict which providers can see that code, override routing preferences, and set classification levels that drive policy.

## Who this is for

- Developers who use multiple AI coding tools and want one place to manage provider credentials and routing.
- Developers who move interactive agent work between WSL, macOS, and Linux and need continuity across machines and providers (intended capability; see the contract above).
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
proxy on `127.0.0.1:8420`. See the [roadmap](ROADMAP.md),
[architecture](docs/architecture.md), and [ADRs](adrs/)
for implementation status and intended requirements. The Go upstream remains the reference implementation
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
