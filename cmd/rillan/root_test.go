// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestRootCommandRegistersCommands(t *testing.T) {
	root := newRootCommand()

	if _, _, err := root.Find([]string{"serve"}); err != nil {
		t.Fatalf("serve command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"init"}); err != nil {
		t.Fatalf("init command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"index"}); err != nil {
		t.Fatalf("index command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"status"}); err != nil {
		t.Fatalf("status command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"auth"}); err != nil {
		t.Fatalf("auth command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"llm"}); err != nil {
		t.Fatalf("llm command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"mcp"}); err != nil {
		t.Fatalf("mcp command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"skill"}); err != nil {
		t.Fatalf("skill command not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"config"}); err != nil {
		t.Fatalf("config command not registered: %v", err)
	}

	for _, command := range [][]string{
		{"auth", "login"},
		{"auth", "logout"},
		{"auth", "status"},
		{"llm", "add"},
		{"llm", "remove"},
		{"llm", "list"},
		{"llm", "use"},
		{"llm", "login"},
		{"llm", "logout"},
		{"mcp", "add"},
		{"mcp", "remove"},
		{"mcp", "list"},
		{"mcp", "use"},
		{"mcp", "login"},
		{"mcp", "logout"},
		{"skill", "install"},
		{"skill", "remove"},
		{"skill", "list"},
		{"skill", "show"},
		{"config", "get"},
		{"config", "set"},
		{"config", "list"},
	} {
		if _, _, err := root.Find(command); err != nil {
			t.Fatalf("command %v not registered: %v", command, err)
		}
	}
}
