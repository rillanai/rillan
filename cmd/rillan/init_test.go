// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommandWritesRuntimeAndProjectConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	runtimePath := filepath.Join(t.TempDir(), "rillan.yaml")
	cmd := newInitCommand()
	cmd.SetArgs([]string{"--output", runtimePath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("runtime config missing: %v", err)
	}
	projectPath := filepath.Join(".rillan", "project.yaml")
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("project config missing: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "wrote project config") {
		t.Fatalf("output = %q, want project config message", output)
	}
}

func TestInitCommandHonorsProjectOutputFlag(t *testing.T) {
	t.Chdir(t.TempDir())

	runtimePath := filepath.Join(t.TempDir(), "rillan.yaml")
	projectPath := filepath.Join(t.TempDir(), "nested", ".rillan", "project.yaml")
	cmd := newInitCommand()
	cmd.SetArgs([]string{"--output", runtimePath, "--project-output", projectPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("project config missing at custom path: %v", err)
	}
}
