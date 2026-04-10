// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadOnlyToolRuntimeListsPassiveMarkdownSkills(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nUse this skill for Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := InstallSkill(source, time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}

	runtime := NewReadOnlyToolRuntime(nil)
	tools, err := runtime.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	for _, tool := range tools {
		if tool.Name != "go-dev" {
			continue
		}
		if got, want := tool.Kind, ToolKindPassiveContext; got != want {
			t.Fatalf("tool kind = %q, want %q", got, want)
		}
		if tool.Content == "" {
			t.Fatal("expected passive markdown skill content")
		}
		return
	}
	t.Fatalf("go-dev not found in tools: %#v", tools)
}

func TestReadOnlyToolRuntimeExecutesBuiltInReadOnlyTool(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("agent skills can read repo files"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	runtime := NewReadOnlyToolRuntime([]string{repo})
	result, err := runtime.ExecuteTool(context.Background(), ToolCall{Name: "read_files", RepoRoot: repo, Paths: []string{"docs/guide.md"}})
	if err != nil {
		t.Fatalf("ExecuteTool returned error: %v", err)
	}
	if got, want := result.Name, "read_files"; got != want {
		t.Fatalf("result name = %q, want %q", got, want)
	}
	var payload struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := len(payload.Files), 1; got != want {
		t.Fatalf("files len = %d, want %d", got, want)
	}
}
