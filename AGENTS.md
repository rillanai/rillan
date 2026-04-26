# Repository Guidelines

## Source of Truth

This file is the shared contributor and agent guide for the Rust port of `github.com/rillanai/rillan`. Start with `README.md` for current behavior and quickstart, then check `adrs/`, especially `ADR-001`, `ADR-002`, `ADR-003`, and `ADR-013`, before making non-trivial changes. If code and docs disagree, the ADRs win; update repo docs to match the implemented behavior.

## Project Structure & Module Organization

This repo is a Rust Cargo workspace. Keep executable wiring in `crates/rillan/` and business logic in library crates under `crates/`, including `app/`, `config/`, `httpapi/`, `providers/`, `policy/`, `index/`, `retrieval/`, and `agent/`. Reference configs live in `configs/`, reusable fixtures in `testdata/`, decisions in `adrs/`, docs in `docs/`, and service assets in `packaging/`. Keep `crates/rillan` thin.

## Repository Guardrails

- Keep the default bind at `127.0.0.1:8420` unless an ADR changes it.
- Preserve local-first behavior: no unofficial provider access paths, shared credentials, or remote-first shortcuts.
- Do not broaden indexing or retrieval scope with background watchers, external vector services, or runtime graph coupling unless the milestone explicitly requires it.
- Keep config, data, and logs separate; do not persist runtime-heavy state next to checked-in config.

## Build, Test, and Development Commands

Run commands from the repo root. Prefer `task`.

- `task build` builds the workspace and all targets.
- `task build:release` produces optimized binaries.
- `task test` runs `cargo test --workspace --all-features`.
- `task lint` runs strict Clippy checks with warnings denied.
- `task fmt` or `task fmt:check` formats or verifies formatting.
- `task ci` runs the main local gate.
- `cargo run -p rillan -- serve` starts the daemon locally.

## Coding Style & Naming Conventions

Follow Rust 2021 and the workspace lints in `Cargo.toml`. Format with `cargo fmt`; `rustfmt.toml` sets `max_width = 100`. Use 4-space indentation, `snake_case` for functions/modules/files, `CamelCase` for types/traits, and `SCREAMING_SNAKE_CASE` for constants. Load config only through `rillan-config`, prefer `thiserror` in library crates, and use structured `tracing` fields without logging secrets. Choose simple implementations over speculative abstractions.

## Testing Guidelines

Tests are primarily colocated with source using `#[cfg(test)] mod tests`. Use `#[tokio::test]` for async paths and prefer focused fixtures from `testdata/`, temp directories, and lightweight mocks such as `wiremock`. For bug fixes, add a reproducing test first. Before opening a PR, run `task test`, `task lint`, and `task fmt:check`. Use `task cover:summary` when changing core logic and prioritize edge cases, failure paths, and deterministic behavior.

## Graphify Callouts

Graphify is optional, advisory context, not a source of truth. The repo keeps graphify artifacts under `graphify-out/`, including `graph.json`, `graph.html`, and `GRAPH_REPORT.md`, and the index crate has explicit graphify discovery/status code in `crates/index/src/graphify.rs`. Use graphify to orient in unfamiliar areas, but verify any `INFERRED` or `AMBIGUOUS` relationships against source files before coding. Do not make runtime behavior depend on graphify output unless the change is explicitly about that integration path.

## Commit & Pull Request Guidelines

`git log` is empty in this checkout, so follow documented repo conventions rather than local history: keep commits small and scoped, use Conventional Commit prefixes when practical, and avoid unrelated refactors. PRs should explain behavioral changes, link the relevant issue or ADR when applicable, include CLI or HTTP examples for user-visible changes, and call out config, packaging, migration, or graphify impacts explicitly.

## Security & Configuration Notes

Do not commit secrets, local credentials, or personal config. Keep checked-in examples in `configs/*.example.yaml` and test-only inputs under `testdata/configs/`. Preserve the local-first defaults documented in `README.md` and the ADRs when changing runtime behavior.
