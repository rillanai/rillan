<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Rillan Rust Port — Roadmap

This file tracks the gap between this Rust crate and the upstream Go reference at `../rillan`. Items below are ordered roughly by milestone.

## ✅ Delivered

### Foundations + Standards
- Workspace + standards: `rust-toolchain.toml`, `rustfmt.toml`, workspace clippy lints (`-D warnings` on `clippy::all`), `deny.toml`, `Taskfile.yml`, repo-level `AGENTS.md` + `CLAUDE.md`.
- ADRs, docs, configs, packaging units, and testdata copied verbatim from the Go repo.

### Library crates
- `rillan-version` — build metadata (`VERSION`, `COMMIT`, `DATE`, `string()`).
- `rillan-observability` — request-id generator, in-process metrics registry.
- `rillan-openai` — `ChatCompletionRequest` / `Message` / `RetrievalOptions` types preserving unknown fields in alphabetic order, validation rules, capability detection.
- `rillan-chat` — shared chat-request value types + provider envelope.
- `rillan-secretstore` — `Store` with `OS keyring` and `in-memory` backends, JSON-encoded `Credential`, binding validation.
- `rillan-policy` — regex-based scanner (matches the Go default rules byte-for-byte) and four-verdict evaluator (allow / redact / block / local-only).
- `rillan-config` — schema-v2 types, default config, env overrides, validation modes (serve / index / status), bundled provider presets, project + system config loaders, `write_example_*` from the embedded `configs/*.yaml`, AES-256-GCM `decrypt_system_policy`.
- `rillan-audit` — append-only JSONL ledger + `Recorder` trait + SHA-256 `hash_bytes` helper.
- `rillan-modules` — module catalog with discovery, validation, and trust filtering.
- `rillan-tokenize` — tiktoken-rs backed counter for `cl100k_base` / `o200k_base` with fallback heuristic + warn-once dedupe.
- `rillan-classify` — Ollama-backed intent classifier + `SafeClassifier` wrapper that suppresses errors.
- `rillan-routing` — catalog construction, deterministic decision engine (policy / capability / model-affinity / route-preference), readiness status catalog.
- `rillan-providers` — `Provider` trait, `Host` (multi-provider), and OpenAI-compatible / Anthropic / Ollama / stdio adapters.
- `rillan-index` — file discovery, deterministic chunking, SQLite + FTS5 store with vector + keyword search, embedded vector store + Ollama vector store, top-level rebuild orchestrator, graphify discovery + status.
- `rillan-retrieval` — `QueryEmbedder` / `QueryRewriter` traits, hybrid retrieval pipeline with reciprocal rank fusion, context compilation, sanitized request builder.
- `rillan-ollama` — thin async client for `/api/embed`, `/api/generate`, and `/`.
- `rillan-agent` — context-package schema + budgeting, role catalog, MCP snapshot + normalizer, orchestrator, approval gate (audit-event emitting), proposal store, skill metrics persistence, **read-only skills (read_files / search_repo / index_lookup / git_status / git_diff) with `ResolveApprovedRepoRoot` symlink-escape protection**, **`Runner` trait + `SharedRunner` + `ReadOnlyToolRuntime`** that dispatches skill invocations.
- `rillan-httpapi` — axum router with `/healthz`, `/readyz`, `/v1/chat/completions`, **`/v1/agent/tasks`**, `/v1/agent/proposals/{id}/decision`, and the loopback-only `/admin/runtime/refresh`. Bearer-token middleware for protected endpoints. Denied proposals now round-trip the proposal payload back to the caller, matching the Go semantics.
- `rillan-app` — daemon wiring + lifecycle + graceful shutdown + `RuntimeManager` with hot-swappable `RuntimeSnapshot` + `build_approved_repo_roots` from `index.root` / `agent.approved_repo_roots`. **`SnapshotBuilder`** ports `internal/app/runtime_snapshot_builder.go`: discovers + filters the project module catalog, augments the runtime config with module-provided LLM adapters, resolves the runtime provider host through the keyring, builds the multi-provider `Host`, probes per-candidate readiness via `routing::build_status_catalog`, wires Ollama-backed classifier + embedder + rewriter when `local_model.enabled`, and constructs the retrieval pipeline. `build_from_disk` re-reads config + project + system from disk so `POST /admin/runtime/refresh` actually swaps in fresh state.

### Binary
- `crates/rillan/` — clap CLI: `rillan init`, `rillan serve`, `rillan status`, `rillan llm {list,add,remove,use,login,logout}`, `rillan index`, `rillan daemon refresh`.

### Verification
`cargo build`, `cargo fmt --check`, `cargo clippy --workspace --all-targets --all-features -- -D warnings`, and `cargo test --workspace` all pass. **182 tests** mirror the upstream Go `*_test.go` cases for each ported package.

Lines of code: **~17,300** Rust across 17 crates.

End-to-end smoke:
- `POST /v1/agent/tasks {"goal":"...","execution_mode":"plan_first"}` returns the orchestrator role with a routing decision; `proposed_action` produces a pending proposal that subsequently resolves through `POST /v1/agent/proposals/{id}/decision`.
- Editing the on-disk config and `POST /admin/runtime/refresh` returns 204; `/readyz` reflects the new state immediately (e.g. `retrieval_mode` flips from `disabled` → `targeted_remote`).

## 🚧 Not yet ported

The Go repo has ~22.5k LoC across 153 files. The slice above hits parity for almost every functional layer; the remaining gaps are non-blocking polish items rather than missing functionality.

1. **`rillan config get/set/list`** — the Rust port mirrors the Go stub (`not implemented yet`). When the upstream lands a real schema-aware editor, port it.
2. **Daemon-side post-mutation refresh** — `rillan llm`, `rillan mcp`, and `rillan auth` write to the on-disk config but don't yet POST `/admin/runtime/refresh` afterwards. The endpoint exists; the CLIs need a small `notify_daemon_runtime_refresh` helper so edits take effect without a restart.

## Test parity check

Each ported crate carries the Go file's tests (table-driven where the original was). When porting the next layer, mirror the same patterns:

- Co-located `#[cfg(test)] mod tests`.
- `#[tokio::test]` for async paths.
- Avoid mocks in favor of in-memory backends (e.g. `Store::in_memory()`, `wiremock::MockServer` for HTTP).

Coverage tooling: `task cover:summary` (cargo-llvm-cov). Aim for ≥70 % line coverage on new business-logic crates.
