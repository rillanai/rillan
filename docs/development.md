# Development Guide

This guide covers building, testing, and contributing to Rillan.

## Prerequisites

- **Go 1.25+** (the module requires 1.25; the project builds cleanly on 1.26)
- **A system keyring** -- required at runtime for credential storage. Tests use mock hooks to avoid keyring dependency.
- **SQLite** -- provided by `modernc.org/sqlite` (pure Go, no CGo required)
- Optional: [Ollama](https://ollama.ai) for local model integration tests

## Building

```bash
# Build the binary
go build -o rillan ./cmd/rillan

# Or build all packages (checks compilation without producing a named binary)
go build ./...
```

## Running Tests

```bash
# Run all tests
go test ./...

# With coverage
go test -cover ./...

# Run a specific package's tests
go test ./internal/config/...
go test ./cmd/rillan/...

# Verbose output for debugging
go test -v ./internal/secretstore/...
```

### Test conventions

- Tests live alongside the code they test (`foo_test.go` next to `foo.go`).
- Test configs and fixtures live in `testdata/`.
- Keyring-dependent tests use `secretstore.SetKeyringGetForTest()`, `SetKeyringSetForTest()`, and `SetKeyringDeleteForTest()` to inject mock keyring implementations. See `cmd/rillan/secretstore_test_hooks_test.go` for the shared test setup.
- Config loading tests use temporary directories and files rather than touching the real config path.

## Project Layout

```
rillan/
├── cmd/rillan/          # CLI commands (Cobra)
│   ├── main.go          # Entry point, signal handling
│   ├── root.go          # Root command, subcommand registration
│   ├── serve.go         # rillan serve
│   ├── init.go          # rillan init
│   ├── index.go         # rillan index
│   ├── auth.go          # rillan auth login/logout/status
│   ├── llm.go           # rillan llm add/remove/list/use/login/logout
│   ├── mcp.go           # rillan mcp add/remove/list/use/login/logout
│   ├── skill.go         # rillan skill install/remove/list/show
│   ├── config_commands.go  # rillan config get/set/list
│   └── credential_flags.go # Shared credential flag helpers
│
├── internal/
│   ├── app/             # Application wiring (HTTP server, config, providers)
│   ├── config/          # Config types, loading, defaults, example generation
│   ├── httpapi/         # HTTP router, handlers, middleware
│   ├── providers/       # Provider interface and factory
│   │   └── openai/      # OpenAI-compatible provider implementation
│   ├── index/           # Indexer, chunker, SQLite store, vector store
│   ├── retrieval/       # Semantic retrieval pipeline
│   ├── secretstore/     # Keyring-backed credential storage
│   ├── agent/           # Agent orchestration, skill catalog, skill metrics
│   ├── ollama/          # Ollama client for embeddings and rewriting
│   ├── classify/        # Content classification
│   ├── policy/          # Policy evaluation and scanning
│   ├── audit/           # Audit ledger
│   ├── openai/          # Shared OpenAI request/response types
│   └── version/         # Build version info
│
├── configs/             # Example YAML configs (schema v2 references)
├── testdata/            # Test fixtures and sample configs
├── packaging/           # Systemd and launchd service files
├── adrs/                # Architecture decision records
└── docs/                # Documentation
```

## Dependencies

Rillan keeps its dependency tree minimal:

| Dependency | Purpose |
|------------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `gopkg.in/yaml.v3` | YAML config parsing |
| `modernc.org/sqlite` | Embedded SQLite (pure Go, no CGo) |
| `github.com/zalando/go-keyring` | OS keyring access |

All other imports are from the Go standard library.

## Adding a New CLI Command

1. Create a new file in `cmd/rillan/` (e.g., `mycommand.go`).
2. Define a `cobra.Command` and register it in `root.go`'s `init()` function.
3. If the command needs config access, use `config.LoadConfig(configPath)`.
4. If the command needs keyring access, use `secretstore.Save/Load/Delete`.
5. Add a `--config` persistent flag if the command reads the runtime config (see `auth.go` or `llm.go` for examples).
6. Write tests in `mycommand_test.go`. Use the keyring test hooks if credentials are involved.

## Adding a New Provider

1. Create a new package under `internal/providers/` (e.g., `internal/providers/myprovider/`).
2. Implement the `Provider` interface:
   ```go
   type Provider interface {
       Name() string
       Ready(context.Context) error
       ChatCompletions(context.Context, openai.ChatCompletionRequest, []byte) (*http.Response, error)
   }
   ```
3. Add or update preset/backend metadata in `internal/config/config.go`.
4. Wire the new family into the provider host adapter construction path in `internal/providers/provider.go` / `internal/providers/host.go`.

## Adding Config Fields

1. Add the struct field and YAML tag in `internal/config/config.go`.
2. If the field needs an environment variable override, add the override logic in `internal/config/load.go` inside `applyEnvOverrides()`.
3. Update `DefaultConfig()` with the default value.
4. Update `internal/config/write_example.go` so `rillan init` generates the new field.
5. Update `configs/rillan.example.yaml` or `configs/project.example.yaml` as applicable.

## Config Loading Internals

`LoadConfig(path)` does the following in order:

1. Reads and parses the YAML file at `path` (or the platform default path).
2. Applies environment variable overrides on top of parsed values.
3. Returns the populated `Config` struct.

`LoadProjectConfig(path)` and `LoadSystemConfig(path)` follow the same pattern for their respective config tiers.

Environment variables only override scalar fields. Complex structures (provider lists, MCP server lists) must be managed via CLI commands or direct YAML editing.

## Keyring and Secrets

The `internal/secretstore` package wraps `go-keyring` with:

- JSON serialization of `Credential` structs (which include binding metadata)
- Binding validation via `ResolveBearer()` -- ensures the stored credential matches the current endpoint/strategy
- Test hooks to avoid real keyring calls in CI

Credential references in config use the `keyring://` URI scheme (e.g., `keyring://rillan/llm/openai`). The `Save` and `Load` functions parse this URI to determine the keyring service and account.

## Skill Catalog Internals

Installed skills are managed in the data directory:

- `<data_dir>/skills/catalog.json` -- manifest of all installed skills (ID, display name, source path, checksum, install time)
- `<data_dir>/skills/<id>.md` -- the copied markdown content
- `<data_dir>/skills/metrics.json` -- per-skill latency metrics

`InstallSkill()` computes a SHA-256 checksum of the source file, copies it into managed storage, and updates the manifest. `RemoveSkill()` checks for project config references before deleting.

## Running the Daemon Locally

```bash
# Quick start with default config
rillan init
rillan llm add dev --preset openai --default-model gpt-4o
rillan llm login dev --api-key "$OPENAI_API_KEY"
rillan llm use dev
rillan serve

# Or with a custom config
rillan serve --config ./testdata/configs/my-config.yaml
```

The daemon logs structured JSON to stderr. Set `server.log_level` to `debug` for verbose output.

## Packaging

Service files for daemon deployment:

- **Linux:** `packaging/systemd/` contains systemd unit files
- **macOS:** `packaging/launchd/` contains launchd plist files

These run `rillan serve` as a background service with the platform's default config path.

## Architecture Decisions

See [`adrs/`](../adrs/) for recorded decisions:

- **ADR-001** -- OpenAI-compatible upstream as the first provider path
- **ADR-002** -- Localhost bind and local config/data/log defaults
- **ADR-003** -- One explicit local root, manual indexing, embedded SQLite storage
