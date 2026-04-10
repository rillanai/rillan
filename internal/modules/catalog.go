// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rillanai/rillan/internal/config"
	"gopkg.in/yaml.v3"
)

const manifestFileName = "module.yaml"

type Manifest struct {
	ID          string                     `yaml:"id,omitempty"`
	DisplayName string                     `yaml:"display_name,omitempty"`
	Version     string                     `yaml:"version,omitempty"`
	Entrypoint  []string                   `yaml:"entrypoint,omitempty"`
	LLMAdapters []config.LLMProviderConfig `yaml:"llm_adapters,omitempty"`
	MCPServers  []config.MCPServerConfig   `yaml:"mcp_servers,omitempty"`
	LSPServers  []LSPServerConfig          `yaml:"lsp_servers,omitempty"`
}

type LSPServerConfig struct {
	ID        string   `yaml:"id,omitempty"`
	Command   []string `yaml:"command,omitempty"`
	Languages []string `yaml:"languages,omitempty"`
}

type LoadedModule struct {
	ID             string
	DisplayName    string
	Version        string
	RootPath       string
	ManifestSHA256 string
	ManifestPath   string
	Entrypoint     []string
	LLMAdapters    []config.LLMProviderConfig
	MCPServers     []config.MCPServerConfig
	LSPServers     []LSPServerConfig
}

type Catalog struct {
	ModulesDir string
	Modules    []LoadedModule
}

func FilterEnabled(catalog Catalog, enabled []string) (Catalog, error) {
	if len(enabled) == 0 {
		return Catalog{ModulesDir: catalog.ModulesDir, Modules: []LoadedModule{}}, nil
	}

	byID := make(map[string]LoadedModule, len(catalog.Modules))
	for _, module := range catalog.Modules {
		byID[module.ID] = module
	}

	filtered := make([]LoadedModule, 0, len(enabled))
	seen := make(map[string]struct{}, len(enabled))
	for _, rawID := range enabled {
		moduleID := strings.TrimSpace(rawID)
		if moduleID == "" {
			continue
		}
		if _, exists := seen[moduleID]; exists {
			continue
		}
		module, ok := byID[moduleID]
		if !ok {
			return Catalog{}, fmt.Errorf("enabled module %q not found in %s", moduleID, catalog.ModulesDir)
		}
		seen[moduleID] = struct{}{}
		filtered = append(filtered, module)
	}

	return Catalog{ModulesDir: catalog.ModulesDir, Modules: filtered}, nil
}

func DefaultProjectModulesDir(projectConfigPath string) string {
	if strings.TrimSpace(projectConfigPath) == "" {
		return filepath.Join(".rillan", "modules")
	}
	return filepath.Join(filepath.Dir(projectConfigPath), "modules")
}

func ProjectRootFromConfigPath(projectConfigPath string) string {
	if strings.TrimSpace(projectConfigPath) == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(projectConfigPath))
}

func LoadProjectCatalog(projectConfigPath string) (Catalog, error) {
	modulesDir := DefaultProjectModulesDir(projectConfigPath)
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Catalog{ModulesDir: modulesDir, Modules: []LoadedModule{}}, nil
		}
		return Catalog{}, fmt.Errorf("read modules dir: %w", err)
	}

	modules := make([]LoadedModule, 0, len(entries))
	seenIDs := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(modulesDir, entry.Name(), manifestFileName)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Catalog{}, fmt.Errorf("read module manifest %s: %w", manifestPath, err)
		}

		var manifest Manifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return Catalog{}, fmt.Errorf("parse module manifest %s: %w", manifestPath, err)
		}

		loaded, err := loadModuleManifest(filepath.Dir(manifestPath), manifestPath, manifest, manifestSHA256(data))
		if err != nil {
			return Catalog{}, err
		}
		if previous, exists := seenIDs[loaded.ID]; exists {
			return Catalog{}, fmt.Errorf("module %q declared more than once in %s and %s", loaded.ID, previous, manifestPath)
		}
		seenIDs[loaded.ID] = manifestPath
		modules = append(modules, loaded)
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].ID < modules[j].ID })
	return Catalog{ModulesDir: modulesDir, Modules: modules}, nil
}

func loadModuleManifest(rootPath string, manifestPath string, manifest Manifest, manifestSHA256 string) (LoadedModule, error) {
	moduleID := strings.TrimSpace(manifest.ID)
	if moduleID == "" {
		return LoadedModule{}, fmt.Errorf("module manifest %s id must not be empty", manifestPath)
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		return LoadedModule{}, fmt.Errorf("module %q version must not be empty", moduleID)
	}
	if len(manifest.Entrypoint) == 0 {
		return LoadedModule{}, fmt.Errorf("module %q entrypoint must not be empty", moduleID)
	}

	llmAdapters, err := normalizeLLMAdapters(moduleID, rootPath, manifest.LLMAdapters)
	if err != nil {
		return LoadedModule{}, err
	}
	mcpServers, err := normalizeMCPServers(moduleID, rootPath, manifest.MCPServers)
	if err != nil {
		return LoadedModule{}, err
	}
	lspServers, err := normalizeLSPServers(moduleID, rootPath, manifest.LSPServers)
	if err != nil {
		return LoadedModule{}, err
	}

	return LoadedModule{
		ID:             moduleID,
		DisplayName:    strings.TrimSpace(manifest.DisplayName),
		Version:        version,
		RootPath:       rootPath,
		ManifestSHA256: manifestSHA256,
		ManifestPath:   manifestPath,
		Entrypoint:     normalizeCommand(rootPath, manifest.Entrypoint),
		LLMAdapters:    llmAdapters,
		MCPServers:     mcpServers,
		LSPServers:     lspServers,
	}, nil
}

func FilterTrusted(catalog Catalog, projectConfigPath string, system *config.SystemConfig) (Catalog, error) {
	if len(catalog.Modules) == 0 {
		return catalog, nil
	}
	projectRoot, err := canonicalProjectRoot(ProjectRootFromConfigPath(projectConfigPath))
	if err != nil {
		return Catalog{}, err
	}
	trusted := make([]LoadedModule, 0, len(catalog.Modules))
	for _, module := range catalog.Modules {
		trust, ok := findModuleTrust(system, projectRoot, module)
		if !ok {
			return Catalog{}, fmt.Errorf("enabled module %q is not trusted for repo %s", module.ID, projectRoot)
		}
		if moduleUsesStdio(module) && !trust.AllowStdio {
			return Catalog{}, fmt.Errorf("enabled module %q requires explicit stdio trust", module.ID)
		}
		trusted = append(trusted, module)
	}
	return Catalog{ModulesDir: catalog.ModulesDir, Modules: trusted}, nil
}

func manifestSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalProjectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("project root must not be empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absRoot)
}

func findModuleTrust(system *config.SystemConfig, projectRoot string, module LoadedModule) (config.TrustedModulePolicy, bool) {
	if system == nil {
		return config.TrustedModulePolicy{}, false
	}
	for _, trust := range system.Policy.TrustedModules {
		trustedRoot, err := canonicalProjectRoot(trust.RepoRoot)
		if err != nil {
			continue
		}
		if trustedRoot == projectRoot && strings.TrimSpace(trust.ModuleID) == module.ID && strings.TrimSpace(trust.ManifestSHA256) == module.ManifestSHA256 {
			return trust, true
		}
	}
	return config.TrustedModulePolicy{}, false
}

func moduleUsesStdio(module LoadedModule) bool {
	for _, adapter := range module.LLMAdapters {
		if strings.TrimSpace(adapter.Transport) == config.LLMTransportSTDIO {
			return true
		}
	}
	return false
}

func normalizeLLMAdapters(moduleID string, rootPath string, adapters []config.LLMProviderConfig) ([]config.LLMProviderConfig, error) {
	seen := make(map[string]struct{}, len(adapters))
	normalized := make([]config.LLMProviderConfig, 0, len(adapters))
	for _, adapter := range adapters {
		id := strings.TrimSpace(adapter.ID)
		if id == "" {
			return nil, fmt.Errorf("module %q llm_adapters.id must not be empty", moduleID)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("module %q llm adapter %q declared more than once", moduleID, id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(adapter.Backend) == "" {
			return nil, fmt.Errorf("module %q llm adapter %q backend must not be empty", moduleID, id)
		}
		switch strings.TrimSpace(adapter.Transport) {
		case config.LLMTransportHTTP:
			if strings.TrimSpace(adapter.Endpoint) == "" {
				return nil, fmt.Errorf("module %q llm adapter %q endpoint must not be empty when transport is %q", moduleID, id, config.LLMTransportHTTP)
			}
		case config.LLMTransportSTDIO:
			if len(adapter.Command) == 0 {
				return nil, fmt.Errorf("module %q llm adapter %q command must not be empty when transport is %q", moduleID, id, config.LLMTransportSTDIO)
			}
			adapter.Command = normalizeCommand(rootPath, adapter.Command)
		default:
			return nil, fmt.Errorf("module %q llm adapter %q transport must be %q or %q", moduleID, id, config.LLMTransportHTTP, config.LLMTransportSTDIO)
		}
		normalized = append(normalized, adapter)
	}
	return normalized, nil
}

func normalizeMCPServers(moduleID string, rootPath string, servers []config.MCPServerConfig) ([]config.MCPServerConfig, error) {
	seen := make(map[string]struct{}, len(servers))
	normalized := make([]config.MCPServerConfig, 0, len(servers))
	for _, server := range servers {
		id := strings.TrimSpace(server.ID)
		if id == "" {
			return nil, fmt.Errorf("module %q mcp_servers.id must not be empty", moduleID)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("module %q mcp server %q declared more than once", moduleID, id)
		}
		seen[id] = struct{}{}
		switch strings.TrimSpace(server.Transport) {
		case config.LLMTransportHTTP:
			if strings.TrimSpace(server.Endpoint) == "" {
				return nil, fmt.Errorf("module %q mcp server %q endpoint must not be empty when transport is %q", moduleID, id, config.LLMTransportHTTP)
			}
		case config.LLMTransportSTDIO:
			if len(server.Command) == 0 {
				return nil, fmt.Errorf("module %q mcp server %q command must not be empty when transport is %q", moduleID, id, config.LLMTransportSTDIO)
			}
			server.Command = normalizeCommand(rootPath, server.Command)
		default:
			return nil, fmt.Errorf("module %q mcp server %q transport must be %q or %q", moduleID, id, config.LLMTransportHTTP, config.LLMTransportSTDIO)
		}
		normalized = append(normalized, server)
	}
	return normalized, nil
}

func normalizeLSPServers(moduleID string, rootPath string, servers []LSPServerConfig) ([]LSPServerConfig, error) {
	seen := make(map[string]struct{}, len(servers))
	normalized := make([]LSPServerConfig, 0, len(servers))
	for _, server := range servers {
		id := strings.TrimSpace(server.ID)
		if id == "" {
			return nil, fmt.Errorf("module %q lsp_servers.id must not be empty", moduleID)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("module %q lsp server %q declared more than once", moduleID, id)
		}
		seen[id] = struct{}{}
		if len(server.Command) == 0 {
			return nil, fmt.Errorf("module %q lsp server %q command must not be empty", moduleID, id)
		}
		server.Command = normalizeCommand(rootPath, server.Command)
		normalized = append(normalized, server)
	}
	return normalized, nil
}

func normalizeCommand(rootPath string, command []string) []string {
	if len(command) == 0 {
		return nil
	}
	normalized := append([]string(nil), command...)
	first := strings.TrimSpace(normalized[0])
	if first == "" {
		return normalized
	}
	if filepath.IsAbs(first) || !strings.ContainsAny(first, `/\`) {
		return normalized
	}
	normalized[0] = filepath.Clean(filepath.Join(rootPath, first))
	return normalized
}
