package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const exampleConfig = `schema_version: 2

server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"
  allow_non_loopback_bind: false
  auth:
    enabled: false
    auth_strategy: "api_key"
    session_ref: "keyring://rillan/auth/daemon"

auth:
  rillan:
    endpoint: ""
    auth_strategy: ""
    session_ref: "keyring://rillan/auth/control-plane"

llms:
  default: "openai"
  providers:
    - id: "openai"
      backend: "openai_compatible"
      transport: "http"
      endpoint: "https://api.openai.com/v1"
      auth_strategy: "browser_oidc"
      default_model: "gpt-5"
      capabilities: ["chat", "reasoning", "tool_calling"]
      credential_ref: "keyring://rillan/llm/openai"

mcps:
  default: ""
  servers: []

index:
  root: ""
  includes: []
  excludes:
    - ".git"
    - "node_modules"
    - ".direnv"
    - ".idea"
  chunk_size_lines: 120

knowledge_graph:
  enabled: false
  path: ""
  auto_update: "none"
  traversal_depth: 1
  include_inferred: true
  max_nodes: 2000

retrieval:
  enabled: false
  top_k: 4
  max_context_chars: 6000

local_model:
  enabled: false
  base_url: "http://127.0.0.1:11434"
  embed_model: "nomic-embed-text"
  query_rewrite:
    enabled: false
    model: "qwen3:0.6b"

runtime:
  vector_store_mode: "embedded"
  local_model_base_url: "http://127.0.0.1:11434"

agent:
  enabled: false
  approved_repo_roots: []
  mcp:
    enabled: false
    read_only: true
    max_open_files: 8
    max_diagnostics: 20
`

const exampleProjectConfig = `name: "example-project"
classification: "open_source"

sources:
  - path: "cmd"
    type: "go"
  - path: "internal"
    type: "go"

routing:
  default: "auto"
  task_types:
    code_generation: "prefer_local"
    review: "prefer_local"

providers:
  llm_default: "openai"
  llm_allowed: ["openai"]
  mcp_enabled: []

modules:
  enabled: []

agent:
  skills:
    enabled: []

system_prompt: ""

instructions:
  - "Keep outbound context tightly bounded to the current task."
  - "Never include credentials, tokens, or unrelated proprietary material in outbound requests."
`

func WriteExampleConfig(path string, overwrite bool) error {
	return writeExampleFile(path, exampleConfig, overwrite)
}

func WriteExampleProjectConfig(path string, overwrite bool) error {
	return writeExampleFile(path, exampleProjectConfig, overwrite)
}

func writeExampleFile(path string, content string, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing file at %s; pass --force to replace it", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check config path: %w", err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
