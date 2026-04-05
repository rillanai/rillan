package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	toolskills "github.com/rillanai/rillan/internal/agent/skills"
)

type ToolKind string

const (
	ToolKindPassiveContext ToolKind = "passive_context"
	ToolKindReadOnlyAction ToolKind = "read_only_action"
)

type ToolDefinition struct {
	Name        string   `json:"name"`
	Kind        ToolKind `json:"kind"`
	Description string   `json:"description,omitempty"`
	Content     string   `json:"content,omitempty"`
}

type ToolCall struct {
	Name       string `json:"name"`
	RepoRoot   string `json:"repo_root,omitempty"`
	Paths      []string
	Query      string `json:"query,omitempty"`
	DBPath     string `json:"db_path,omitempty"`
	StagedOnly bool   `json:"staged_only,omitempty"`
}

type ToolExecutionResult struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

type ToolSource interface {
	ListTools(ctx context.Context) ([]ToolDefinition, error)
}

type ToolExecutor interface {
	ExecuteTool(ctx context.Context, call ToolCall) (ToolExecutionResult, error)
}

type ReadOnlyToolRuntime struct {
	registry      *toolskills.Registry
	listInstalled func() ([]InstalledSkill, error)
	readFile      func(string) ([]byte, error)
}

func NewReadOnlyToolRuntime(approvedRepoRoots []string) *ReadOnlyToolRuntime {
	return &ReadOnlyToolRuntime{
		registry:      toolskills.NewRegistry(approvedRepoRoots),
		listInstalled: ListInstalledSkills,
		readFile:      os.ReadFile,
	}
}

func (r *ReadOnlyToolRuntime) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools := make([]ToolDefinition, 0, len(toolskills.ListReadOnlyTools()))
	for _, tool := range toolskills.ListReadOnlyTools() {
		tools = append(tools, ToolDefinition{Name: tool.Name, Kind: ToolKindReadOnlyAction, Description: tool.Description})
	}
	installed, err := r.listInstalled()
	if err != nil {
		return nil, err
	}
	for _, skill := range installed {
		content, err := r.readFile(skill.ManagedPath)
		if err != nil {
			return nil, fmt.Errorf("read managed skill %q: %w", skill.ID, err)
		}
		tools = append(tools, ToolDefinition{
			Name:        skill.ID,
			Kind:        ToolKindPassiveContext,
			Description: skill.CapabilitySummary,
			Content:     strings.TrimSpace(string(content)),
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Kind != tools[j].Kind {
			return tools[i].Kind < tools[j].Kind
		}
		return tools[i].Name < tools[j].Name
	})
	return tools, nil
}

func (r *ReadOnlyToolRuntime) ExecuteTool(ctx context.Context, call ToolCall) (ToolExecutionResult, error) {
	result, err := r.registry.Execute(ctx, toolskills.ExecuteRequest{
		Name:       call.Name,
		RepoRoot:   call.RepoRoot,
		Paths:      call.Paths,
		Query:      call.Query,
		DBPath:     call.DBPath,
		StagedOnly: call.StagedOnly,
	})
	if err != nil {
		return ToolExecutionResult{}, err
	}
	encoded, err := json.Marshal(result.Payload)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	return ToolExecutionResult{Name: result.Name, Payload: encoded}, nil
}
