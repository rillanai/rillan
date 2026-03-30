# Command-Native Setup Guide

This guide covers the M07.5 operator workflow for configuring Rillan. The preferred path is CLI commands rather than hand-editing YAML or relying on environment variables.

## Overview

Rillan manages four categories of state:

| Category | Storage | Managed by |
|----------|---------|------------|
| Named LLM providers | Global runtime config + keyring | `rillan llm` |
| Named MCP endpoints | Global runtime config + keyring | `rillan mcp` |
| Control-plane auth | Global runtime config + keyring | `rillan auth` |
| Markdown skills | Data directory | `rillan skill` |
| Per-repo settings | Repo-local project config | Hand-edited YAML |
| System policy | Encrypted system config | Tooling (not hand-edited) |

## Initial Setup

```bash
# 1. Generate starter config
rillan init

# 2. Register your first LLM provider
rillan llm add work-gpt \
  --backend openai_compatible \
  --transport http \
  --endpoint https://api.openai.com/v1 \
  --default-model gpt-4o \
  --capability chat \
  --capability reasoning \
  --capability tool_calling

# 3. Authenticate it
rillan llm login work-gpt --api-key "$OPENAI_API_KEY"

# 4. Set it as the default
rillan llm use work-gpt

# 5. Verify
rillan llm list
```

## LLM Provider Management

### Adding providers

Each provider needs a unique name, a backend, and a transport:

```bash
# OpenAI-compatible HTTP provider
rillan llm add openai-prod \
  --backend openai_compatible \
  --transport http \
  --endpoint https://api.openai.com/v1 \
  --default-model gpt-4o

# Future stdio-backed extension entry
rillan llm add repo-plugin \
  --backend custom-backend \
  --transport stdio \
  --command rillan-provider-demo \
  --auth-strategy none
```

The current runtime executes built-in `openai_compatible` HTTP providers. `stdio` entries are stored now so the schema tracks ADR-004, but they are not executed yet.

**Auth strategies:** `none`, `api_key`, `browser_oidc`, `device_oidc`

### Authenticating providers

The credential flags you use depend on the auth strategy:

```bash
# API key auth (most common)
rillan llm login openai-prod --api-key "sk-..."

# OIDC-based auth
rillan llm login corp-gpt \
  --access-token "eyJ..." \
  --refresh-token "dGhpcyBpcyBhIHJlZnJlc2g..." \
  --id-token "eyJ..." \
  --issuer "https://auth.corp.example" \
  --audience "rillan-proxy"

# No auth needed
# (skip login for --auth-strategy none providers)
```

Credentials are stored in the OS keyring at `keyring://rillan/llm/<name>`. The config file only holds a reference URI, never the secret itself.

### Switching and removing

```bash
# Set default provider
rillan llm use openai-prod

# Remove a provider (clears config entry and keyring credential)
rillan llm remove vllm-local

# List all providers with their current state
rillan llm list
```

## MCP Endpoint Management

MCP endpoints follow the same add/login/use/remove pattern:

### Adding endpoints

```bash
# HTTP endpoint, no auth
rillan mcp add ide-local \
  --endpoint http://127.0.0.1:8765 \
  --transport http \
  --auth-strategy none

# Authenticated remote endpoint
rillan mcp add repo-gateway \
  --endpoint https://mcp.corp.example/v1 \
  --transport http \
  --auth-strategy api_key \
  --read-only false
```

**Transport types:** `http`, `stdio`

The `--read-only` flag (default: `true`) restricts the endpoint to read-only operations.

### Authenticating endpoints

```bash
rillan mcp login repo-gateway --api-key "mcp-key-..."
```

### Switching and removing

```bash
rillan mcp use ide-local
rillan mcp remove repo-gateway
rillan mcp list
```

## Control-Plane Authentication

The `auth` command group is reserved for Rillan team or control-plane endpoints. It is separate from provider-specific auth.

```bash
# Log in to a team endpoint
rillan auth login \
  --endpoint https://team.example \
  --auth-strategy device_oidc \
  --access-token "token"

# Check auth state
rillan auth status

# Log out
rillan auth logout
```

## Markdown Skill Management

Skills are markdown files that provide reusable instructions for agent behavior. Rillan copies them into managed storage so the runtime does not depend on the source file continuing to exist.

### Installing skills

```bash
rillan skill install ./go-dev.md
rillan skill install ./python-review.md
```

The skill ID is derived from the filename (e.g., `go-dev.md` becomes `go-dev`).

### Inspecting skills

```bash
# List all installed skills
rillan skill list

# Show metadata for a specific skill
rillan skill show go-dev
```

### Removing skills

```bash
# Remove a skill
rillan skill remove go-dev

# Force removal even if the current project config references it
rillan skill remove go-dev --force
```

If the current repo's project config lists the skill in `agent.skills.enabled`, removal will fail unless `--force` is passed.

## Project-Level Configuration

Create a `.rillan/project.yaml` in your repository root to set per-repo constraints. The loader still falls back to `.sidekick/project.yaml` while the repo converges on the new name.

```yaml
name: "my-service"
classification: "internal"

# Restrict which providers this repo can use
providers:
  llm_default: "work-gpt"
  llm_allowed: ["work-gpt", "local-qwen"]
  mcp_enabled: ["ide-local"]

# Enable specific skills for this repo
agent:
  skills:
    enabled: ["go-dev", "python-review"]

# Routing preferences
routing:
  default: "auto"
  task_types:
    code_generation: "prefer_local"
    review: "prefer_local"

# Custom instructions injected into prompts
instructions:
  - "Keep outbound context tightly bounded to the current task."
  - "Never include credentials, tokens, or unrelated proprietary material in outbound requests."
```

**Classification levels:** `open_source`, `internal`, `proprietary`, `trade_secret`

**Routing preferences:** `auto`, `prefer_local`, `prefer_cloud`, `local_only`

Classification affects policy enforcement. For example, `trade_secret` projects can be forced to local-only routing by system policy.

## Configuration Inspection

The `config` command group provides low-level access to the runtime config. It is not the primary onboarding flow -- use `llm`, `mcp`, and `skill` commands for day-to-day setup.

```bash
rillan config list
rillan config get server.port
rillan config set server.log_level debug
```

## Credential Lifecycle

When you change an endpoint URL, auth strategy, issuer, or audience on a provider, you should log in again so the stored credential matches the new binding:

```bash
# Provider endpoint changed
rillan llm remove old-gpt
rillan llm add new-gpt --backend openai_compatible --transport http --endpoint https://new-api.example/v1
rillan llm login new-gpt --api-key "sk-new-..."
```

The keyring stores the full credential bundle (key/token + binding metadata). A credential is validated against its binding on use -- if the binding has drifted, `ResolveBearer` will return an error.

## Notes on Secrets

- YAML config files should only contain `keyring://` credential references, never raw secrets.
- Real API keys and tokens live in the OS keyring (macOS Keychain, GNOME Keyring, KWallet, or Windows Credential Manager).
- Environment variables like `OPENAI_API_KEY` are still supported as overrides for CI/automation, but the keyring path is preferred for interactive use.
