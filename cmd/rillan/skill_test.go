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

func TestSkillInstallListShowAndRemove(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	projectHome := t.TempDir()
	t.Setenv("HOME", projectHome)
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nUse this skill for Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	install := newSkillCommand()
	install.SetArgs([]string{"install", source})
	if err := install.Execute(); err != nil {
		t.Fatalf("install Execute returned error: %v", err)
	}

	list := newSkillCommand()
	list.SetArgs([]string{"list"})
	var listOut bytes.Buffer
	list.SetOut(&listOut)
	list.SetErr(&listOut)
	if err := list.Execute(); err != nil {
		t.Fatalf("list Execute returned error: %v", err)
	}
	if !strings.Contains(listOut.String(), "- id: go-dev") {
		t.Fatalf("list output missing skill:\n%s", listOut.String())
	}

	show := newSkillCommand()
	show.SetArgs([]string{"show", "go-dev"})
	var showOut bytes.Buffer
	show.SetOut(&showOut)
	show.SetErr(&showOut)
	if err := show.Execute(); err != nil {
		t.Fatalf("show Execute returned error: %v", err)
	}
	if !strings.Contains(showOut.String(), "display_name: Go Dev") {
		t.Fatalf("show output missing display_name:\n%s", showOut.String())
	}

	remove := newSkillCommand()
	remove.SetArgs([]string{"remove", "go-dev"})
	if err := remove.Execute(); err != nil {
		t.Fatalf("remove Execute returned error: %v", err)
	}
}
