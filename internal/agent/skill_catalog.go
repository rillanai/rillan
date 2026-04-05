package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/config"
)

const skillCatalogParserVersion = "markdown_v1"

// InstalledSkill describes one managed markdown skill in the local catalog.
type InstalledSkill struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	SourcePath        string `json:"source_path"`
	ManagedPath       string `json:"managed_path"`
	Checksum          string `json:"checksum"`
	InstalledAt       string `json:"installed_at"`
	ParserVersion     string `json:"parser_version"`
	CapabilitySummary string `json:"capability_summary"`
}

// SkillCatalog is the persisted manifest for installed markdown skills.
type SkillCatalog struct {
	Skills []InstalledSkill `json:"skills"`
}

// DefaultSkillCatalogPath returns the managed manifest path for installed
// markdown skills.
func DefaultSkillCatalogPath() string {
	return filepath.Join(config.DefaultDataDir(), "skills", "catalog.json")
}

// DefaultManagedSkillPath returns the managed markdown location for a skill ID.
func DefaultManagedSkillPath(id string) string {
	return filepath.Join(config.DefaultDataDir(), "skills", id, "SKILL.md")
}

// LoadSkillCatalog loads the persisted markdown skill catalog from the data
// directory.
func LoadSkillCatalog() (SkillCatalog, error) {
	path := DefaultSkillCatalogPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SkillCatalog{Skills: []InstalledSkill{}}, nil
		}
		return SkillCatalog{}, fmt.Errorf("read skill catalog: %w", err)
	}

	var catalog SkillCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return SkillCatalog{}, fmt.Errorf("parse skill catalog: %w", err)
	}
	if catalog.Skills == nil {
		catalog.Skills = []InstalledSkill{}
	}
	return catalog, nil
}

// SaveSkillCatalog writes the markdown skill catalog back to the data
// directory.
func SaveSkillCatalog(catalog SkillCatalog) error {
	path := DefaultSkillCatalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill catalog dir: %w", err)
	}
	sort.Slice(catalog.Skills, func(i, j int) bool { return catalog.Skills[i].ID < catalog.Skills[j].ID })
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill catalog: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write skill catalog: %w", err)
	}
	return nil
}

// InstallSkill copies a markdown skill into managed storage and records its
// manifest entry.
func InstallSkill(sourcePath string, now time.Time) (InstalledSkill, error) {
	cleanSource, err := filepath.Abs(strings.TrimSpace(sourcePath))
	if err != nil {
		return InstalledSkill{}, fmt.Errorf("resolve skill source path: %w", err)
	}
	data, err := os.ReadFile(cleanSource)
	if err != nil {
		return InstalledSkill{}, fmt.Errorf("read skill source: %w", err)
	}

	displayName := skillDisplayName(cleanSource, string(data))
	id := normalizeSkillID(displayName)
	checksum := checksumForBytes(data)
	managedPath := DefaultManagedSkillPath(id)

	catalog, err := LoadSkillCatalog()
	if err != nil {
		return InstalledSkill{}, err
	}
	for _, skill := range catalog.Skills {
		if skill.ID != id {
			continue
		}
		if skill.Checksum == checksum {
			return skill, nil
		}
		return InstalledSkill{}, fmt.Errorf("skill %q already exists with different content", id)
	}

	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		return InstalledSkill{}, fmt.Errorf("create managed skill dir: %w", err)
	}
	if err := os.WriteFile(managedPath, data, 0o644); err != nil {
		return InstalledSkill{}, fmt.Errorf("write managed skill: %w", err)
	}

	skill := InstalledSkill{
		ID:                id,
		DisplayName:       displayName,
		SourcePath:        cleanSource,
		ManagedPath:       managedPath,
		Checksum:          checksum,
		InstalledAt:       now.UTC().Format(time.RFC3339),
		ParserVersion:     skillCatalogParserVersion,
		CapabilitySummary: skillCapabilitySummary(string(data)),
	}
	catalog.Skills = append(catalog.Skills, skill)
	if err := SaveSkillCatalog(catalog); err != nil {
		return InstalledSkill{}, err
	}

	return skill, nil
}

// RemoveSkill deletes a managed markdown skill unless the current project still
// enables it and force is false.
func RemoveSkill(id string, force bool) (InstalledSkill, error) {
	id = normalizeSkillID(id)
	if !force {
		if err := ensureSkillNotEnabledInCurrentProject(id); err != nil {
			return InstalledSkill{}, err
		}
	}

	catalog, err := LoadSkillCatalog()
	if err != nil {
		return InstalledSkill{}, err
	}
	next := make([]InstalledSkill, 0, len(catalog.Skills))
	var removed InstalledSkill
	for _, skill := range catalog.Skills {
		if skill.ID == id {
			removed = skill
			continue
		}
		next = append(next, skill)
	}
	if removed.ID == "" {
		return InstalledSkill{}, fmt.Errorf("skill %q not found", id)
	}
	catalog.Skills = next
	if err := SaveSkillCatalog(catalog); err != nil {
		return InstalledSkill{}, err
	}
	if err := os.RemoveAll(filepath.Dir(removed.ManagedPath)); err != nil {
		return InstalledSkill{}, fmt.Errorf("remove managed skill: %w", err)
	}
	return removed, nil
}

// GetInstalledSkill returns one installed markdown skill by ID.
func GetInstalledSkill(id string) (InstalledSkill, error) {
	id = normalizeSkillID(id)
	catalog, err := LoadSkillCatalog()
	if err != nil {
		return InstalledSkill{}, err
	}
	for _, skill := range catalog.Skills {
		if skill.ID == id {
			return skill, nil
		}
	}
	return InstalledSkill{}, fmt.Errorf("skill %q not found", id)
}

// ListInstalledSkills returns all installed markdown skills sorted by ID.
func ListInstalledSkills() ([]InstalledSkill, error) {
	catalog, err := LoadSkillCatalog()
	if err != nil {
		return nil, err
	}
	sort.Slice(catalog.Skills, func(i, j int) bool { return catalog.Skills[i].ID < catalog.Skills[j].ID })
	return catalog.Skills, nil
}

func ensureSkillNotEnabledInCurrentProject(id string) error {
	projectPath := config.ResolveProjectConfigPath("")
	projectCfg, err := config.LoadProject(projectPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, enabled := range projectCfg.Agent.Skills.Enabled {
		if normalizeSkillID(enabled) == id {
			return fmt.Errorf("skill %q is still enabled in %s; disable it first or use --force", id, projectPath)
		}
	}
	return nil
}

func skillDisplayName(sourcePath string, content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	if base == "" {
		return "skill"
	}
	return base
}

func skillCapabilitySummary(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 240 {
			return trimmed[:240]
		}
		return trimmed
	}
	return ""
}

func normalizeSkillID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "skill"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "skill"
	}
	return result
}

func checksumForBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
