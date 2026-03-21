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
}
