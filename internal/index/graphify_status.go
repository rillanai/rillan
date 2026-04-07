package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rillanai/rillan/internal/config"
)

type GraphifyStatus struct {
	Enabled bool
	Path    string
	Present bool
	Nodes   int
	Edges   int
	SHA256  string
}

func ReadGraphifyStatus(cfg config.KnowledgeGraphConfig) (GraphifyStatus, error) {
	status := GraphifyStatus{
		Enabled: cfg.Enabled,
		Path:    strings.TrimSpace(cfg.Path),
	}
	if !cfg.Enabled || status.Path == "" {
		return status, nil
	}

	absPath, err := filepath.Abs(status.Path)
	if err != nil {
		return status, fmt.Errorf("resolve graphify path: %w", err)
	}
	status.Path = absPath

	graphPath := filepath.Join(absPath, "graph.json")
	data, err := os.ReadFile(graphPath)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, fmt.Errorf("read graph.json: %w", err)
	}
	status.Present = true
	sum := sha256.Sum256(data)
	status.SHA256 = hex.EncodeToString(sum[:])

	var graph struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		return status, fmt.Errorf("parse graph.json: %w", err)
	}
	status.Nodes = len(graph.Nodes)
	status.Edges = len(graph.Edges)
	return status, nil
}
