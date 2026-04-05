# Rillan Agent Guidance

This file is for coding agents working in `github.com/rillanai/rillan`.

Start with these source-of-truth docs before making non-trivial changes:

- `README.md` — current scope, quickstart, config paths, and CLI surface
- `adrs/ADR-001.md` — OpenAI-compatible upstream is the first real provider path
- `adrs/ADR-002.md` — localhost bind and user-scoped config/data/log defaults
- `adrs/ADR-003.md` — one explicit index root, manual indexing, embedded SQLite storage

If code and docs disagree, follow the ADRs and then update the repo docs to match the implemented behavior.

## Overview

Rillan is a local-first Go daemon with a small Cobra CLI.

Current delivered surface:

- `rillan serve`
- `rillan init`
- `rillan index`
- `rillan status`
- `GET /healthz`
- `GET /readyz`
- `POST /v1/chat/completions`

Current release targets are macOS and Linux.

## Hard Repository Guardrails

- Bind locally by default. Do not change the default daemon bind away from `127.0.0.1:8420` without an ADR.
- Anthropic is not a normal default path. It is discouraged, non-default, and not implemented as a runtime provider today.
- Do not add unofficial provider access paths such as scraped sessions, browser cookies, reverse proxies, or shared credentials.
- Milestone-two indexing is one explicit local root at a time, manual rebuild only, text/code artifacts first, full-replace indexing, and embedded SQLite storage.
- Do not introduce background watchers, retrieval-time context injection, MCP behavior, Ollama runtime coupling, or external vector services unless the change explicitly advances those later milestones.
- Keep config, data, and logs separate. Do not persist runtime-heavy state alongside config.

## Project Structure

```text
cmd/rillan/         Cobra CLI entrypoints and command wiring
internal/app/       Daemon wiring and lifecycle
internal/config/    YAML config, env overrides, validation modes, default paths
internal/httpapi/   HTTP router, handlers, middleware
internal/openai/    OpenAI-compatible request/response shapes and validation
internal/providers/ Upstream provider seam and concrete OpenAI-compatible client
internal/index/     Discovery, chunking, SQLite store, vectors, rebuild/status
internal/version/   Build metadata
configs/            Checked-in reference configuration
testdata/           Test configs and smoke fixtures
adrs/               Binding repository-level architecture decisions
```

Keep `cmd/*` thin. Business logic belongs in `internal/` packages.

## Build, Test, and Development Commands

Run these from the repo root.

```bash
go test ./...
go test -cover ./...
go test -race ./...
go build ./...
go run ./cmd/rillan init
go run ./cmd/rillan serve
go run ./cmd/rillan index --config ./testdata/configs/index-smoke.yaml
go run ./cmd/rillan status --config ./testdata/configs/index-smoke.yaml
go mod tidy
```

If you add or change dependencies, run `go mod tidy`.

## Go Development Principles

### DRY

- Extract repeated logic into named helpers when the same behavior appears twice in a real way.
- Use shared constants for repeated magic strings or numbers.
- Prefer a small amount of obvious duplication over a premature abstraction.

### KISS

- Choose the simplest solution that solves the current milestone.
- Keep functions narrow and readable.
- Prefer direct control flow over clever indirection.

### YAGNI

- Do not add extension points, config knobs, or provider/plugin abstractions until the repo has a real second consumer.
- A seam for testing is valid. A speculative abstraction is not.
- In this repo, new interfaces should usually be justified by an actual consumer or a concrete test need.

## Go-Specific Rules for This Repo

### Configuration

- Load configuration only through `internal/config/`.
- Do not read `os.Getenv` outside the config layer.
- Preserve the current precedence model: env overrides the YAML file, which overlays defaults.
- Use validation modes when commands have different runtime requirements. Do not force provider credentials on offline indexing commands.

### Errors

- Wrap errors with operation context using `fmt.Errorf("context: %w", err)`.
- Log errors at the boundary, not deep in the call chain.
- Use sentinel errors only when callers need to branch on them.
- Do not compare `err.Error()` strings.

### Context

- `ctx context.Context` is always the first parameter when a function needs context.
- Propagate request and command context through storage, provider, and indexing paths.
- Do not store context in structs.

### Logging

- Use structured logging with `log/slog`.
- Include stable keys such as `request_id`, `provider`, `path`, or `root` where relevant.
- Never log API keys, auth headers, or raw sensitive content.
- Keep logs at operation boundaries, not inside tight loops.

### HTTP Handlers

- Follow the existing pattern: decode -> validate -> call service/provider -> encode response.
- Keep domain and provider details out of the transport layer when possible.
- Limit body sizes and return machine-readable errors.

### Concurrency and Lifecycle

- Every goroutine must have a clear owner and shutdown path.
- Use context cancellation for long-lived work.
- Do not add fire-and-forget goroutines in production code.

## Testing Guidance

This repo currently uses standard `testing` with co-located `*_test.go` files. Match that style unless a future package becomes behaviorally complex enough to justify BDD-style tests.

- Follow TDD when adding new behavior: write a failing test first, make it pass, then refactor.
- When fixing a bug, reproduce it with a test first.
- Prefer table-driven tests for pure functions, config loading, parsers, and transformations.
- Prefer focused fakes over heavy mocks.
- Use `t.Helper()` in test helpers.
- Use `t.Parallel()` where it is clearly safe.

Coverage guidance:

- Aim for at least 70% line coverage on applicable packages with meaningful assertions.
- Thin wiring packages may not hit that threshold; business logic packages should.
- Prioritize error paths, boundary conditions, and deterministic behavior over superficial happy-path coverage.

## Coding and Review Checklist

Before finishing a Go change, verify:

- tests exist for the new behavior or bug fix
- `go test ./...` passes
- `go test -race ./...` passes for substantive code changes
- `go build ./...` passes
- `gofmt` has been run on touched Go files
- `go mod tidy` has been run after dependency changes
- exported symbols have doc comments where appropriate
- no dead code, bare TODOs, or speculative extension points were added

## Commit and Branch Guidelines

- Keep `main` protected.
- Use short-lived branches for changes.
- Prefer small, scoped commits.
- Follow the repo’s current Conventional Commit style.
- Use GitHub-verified signed commits on protected branches.
- DCO is the intended contribution model; preserve sign-off-friendly workflows.

## Repository Safety Notes

- Do not commit secrets, local credentials, or personal config.
- `.sisyphus/` is local planning state and stays out of the repo.
- The checked-in reference config belongs in `configs/rillan.example.yaml`; test-only configs belong under `testdata/configs/`.

## Scope Awareness

When modifying the repo, make the smallest change that solves the current problem.

- Do not refactor unrelated code in the same change.
- Do not broaden milestone scope without an explicit request.
- If a change affects architecture, add or update an ADR in `adrs/`.

## When in Doubt

- Prefer the existing repo shape over generic Go architecture advice.
- Prefer explicit local-first behavior over convenience shortcuts.
- Prefer a documented constraint over an assumed future capability.
