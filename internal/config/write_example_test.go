package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteExampleConfigWritesStarterConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rillan.yaml")

	if err := WriteExampleConfig(path, false); err != nil {
		t.Fatalf("WriteExampleConfig returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "type: \"openai\"") {
		t.Fatalf("starter config missing provider type: %s", content)
	}
	if !strings.Contains(content, "enabled: false") {
		t.Fatalf("starter config missing anthropic opt-in flag: %s", content)
	}
	if !strings.Contains(content, "index:") {
		t.Fatalf("starter config missing index block: %s", content)
	}
}

func TestWriteExampleProjectConfigWritesStarterProjectConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sidekick", "project.yaml")

	if err := WriteExampleProjectConfig(path, false); err != nil {
		t.Fatalf("WriteExampleProjectConfig returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "classification: \"open_source\"") {
		t.Fatalf("starter project config missing classification: %s", content)
	}
	if !strings.Contains(content, "sources:") {
		t.Fatalf("starter project config missing sources block: %s", content)
	}
	if !strings.Contains(content, "routing:") {
		t.Fatalf("starter project config missing routing block: %s", content)
	}
}
