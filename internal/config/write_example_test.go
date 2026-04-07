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
	if !strings.Contains(content, "backend: \"openai_compatible\"") {
		t.Fatalf("starter config missing provider backend: %s", content)
	}
	if !strings.Contains(content, "transport: \"http\"") {
		t.Fatalf("starter config missing provider transport: %s", content)
	}
	if !strings.Contains(content, "index:") {
		t.Fatalf("starter config missing index block: %s", content)
	}
	if !strings.Contains(content, "knowledge_graph:") {
		t.Fatalf("starter config missing knowledge_graph block: %s", content)
	}
	if !strings.Contains(content, "session_ref: \"keyring://rillan/auth/daemon\"") {
		t.Fatalf("starter config missing daemon auth session ref: %s", content)
	}
	if !strings.Contains(content, "allow_non_loopback_bind: false") {
		t.Fatalf("starter config missing non-loopback bind guard: %s", content)
	}
	if !strings.Contains(content, "approved_repo_roots: []") {
		t.Fatalf("starter config missing approved repo roots: %s", content)
	}
	for _, forbidden := range []string{"encrypted_payload", "keyring_service", "system:"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("starter config leaked system-only field %q: %s", forbidden, content)
		}
	}
}

func TestWriteExampleProjectConfigWritesStarterProjectConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".rillan", "project.yaml")

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
	if !strings.Contains(content, "modules:") {
		t.Fatalf("starter project config missing modules block: %s", content)
	}
	for _, forbidden := range []string{"encrypted_payload", "keyring_service", "system:"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("starter project config leaked system-only field %q: %s", forbidden, content)
		}
	}
}
