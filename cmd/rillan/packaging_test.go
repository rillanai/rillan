// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdPackagingArtifactContainsExpectedServiceContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "launchd", "com.rillanai.rillan.plist"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	content := string(data)
	for _, want := range []string{"com.rillanai.rillan", "rillan\" serve --config", "__RILLAN_WORKDIR__", "RunAtLoad", "KeepAlive"} {
		if !strings.Contains(content, want) {
			t.Fatalf("launchd artifact missing %q:\n%s", want, content)
		}
	}
}

func TestSystemdPackagingArtifactContainsExpectedServiceContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", "rillan.service"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	content := string(data)
	for _, want := range []string{"[Service]", "ExecStart=%h/.local/bin/rillan serve --config %h/.config/rillan/config.yaml", "WorkingDirectory=%h", "WantedBy=default.target"} {
		if !strings.Contains(content, want) {
			t.Fatalf("systemd artifact missing %q:\n%s", want, content)
		}
	}
}

func TestPackagingReadmeContainsValidationAndLifecycleCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "README.md"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	content := string(data)
	for _, want := range []string{"plutil -lint", "systemd-analyze --user verify", "launchctl bootstrap", "systemctl --user enable --now", "go run ./cmd/rillan serve"} {
		if !strings.Contains(content, want) {
			t.Fatalf("packaging README missing %q:\n%s", want, content)
		}
	}
}
