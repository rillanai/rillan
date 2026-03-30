package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallSkillCopiesManagedMarkdownAndCatalogEntry(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nUse this skill for Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	skill, err := InstallSkill(source, time.Date(2026, 3, 29, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}
	if skill.ID != "go-dev" {
		t.Fatalf("skill id = %q, want go-dev", skill.ID)
	}
	if _, err := os.Stat(skill.ManagedPath); err != nil {
		t.Fatalf("managed skill missing: %v", err)
	}
	catalog, err := LoadSkillCatalog()
	if err != nil {
		t.Fatalf("LoadSkillCatalog returned error: %v", err)
	}
	if got := len(catalog.Skills); got != 1 {
		t.Fatalf("len(catalog.skills) = %d, want 1", got)
	}
}

func TestInstallSkillIsIdempotentForSameContent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	source := filepath.Join(t.TempDir(), "repo-audit.md")
	content := []byte("# Repo Audit\n\nInspect repositories carefully.\n")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	first, err := InstallSkill(source, time.Now())
	if err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}
	second, err := InstallSkill(source, time.Now())
	if err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}
	if first.Checksum != second.Checksum {
		t.Fatalf("checksum mismatch: %q vs %q", first.Checksum, second.Checksum)
	}
	catalog, err := LoadSkillCatalog()
	if err != nil {
		t.Fatalf("LoadSkillCatalog returned error: %v", err)
	}
	if got := len(catalog.Skills); got != 1 {
		t.Fatalf("len(catalog.skills) = %d, want 1", got)
	}
}

func TestRemoveSkillDeletesManagedCopy(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	projectHome := t.TempDir()
	t.Setenv("HOME", projectHome)
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nUse this skill for Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	skill, err := InstallSkill(source, time.Now())
	if err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}

	removed, err := RemoveSkill(skill.ID, false)
	if err != nil {
		t.Fatalf("RemoveSkill returned error: %v", err)
	}
	if removed.ID != skill.ID {
		t.Fatalf("removed id = %q, want %q", removed.ID, skill.ID)
	}
	if _, err := os.Stat(skill.ManagedPath); !os.IsNotExist(err) {
		t.Fatalf("managed path still exists or wrong error: %v", err)
	}
}

func TestRemoveSkillRejectsEnabledProjectSkillWithoutForce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	projectHome := t.TempDir()
	t.Setenv("HOME", projectHome)
	projectRoot := t.TempDir()
	t.Chdir(projectRoot)
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nUse this skill for Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".rillan"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".rillan", "project.yaml"), []byte("name: \"demo\"\nagent:\n  skills:\n    enabled: [go-dev]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	skill, err := InstallSkill(source, time.Now())
	if err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}

	if _, err := RemoveSkill(skill.ID, false); err == nil {
		t.Fatal("expected RemoveSkill to reject enabled project skill")
	}
	if _, err := RemoveSkill(skill.ID, true); err != nil {
		t.Fatalf("RemoveSkill(force) returned error: %v", err)
	}
}
