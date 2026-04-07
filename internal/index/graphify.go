package index

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rillanai/rillan/internal/config"
)

const graphifyPrefix = "graphify/"

type graphifyGraph struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

func DiscoverGraphifyFiles(cfg config.KnowledgeGraphConfig) ([]SourceFile, error) {
	if !cfg.Enabled || strings.TrimSpace(cfg.Path) == "" {
		return nil, nil
	}

	root, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve knowledge graph path: %w", err)
	}

	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat knowledge graph path: %w", err)
	}

	files := make([]SourceFile, 0)

	graphJSONPath := filepath.Join(root, "graph.json")
	if graphData, err := os.ReadFile(graphJSONPath); err == nil {
		content, parseErr := summarizeGraphJSON(graphData, cfg)
		if parseErr != nil {
			return nil, parseErr
		}
		files = append(files, SourceFile{
			AbsolutePath: graphJSONPath,
			RelativePath: graphifyPrefix + "graph.json",
			Content:      content,
			SizeBytes:    int64(len(content)),
		})
	}

	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(entry.Name()) != ".md" {
			return nil
		}

		relPath, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		content := normalizeContent(string(data))
		if strings.TrimSpace(content) == "" {
			return nil
		}

		files = append(files, SourceFile{
			AbsolutePath: filePath,
			RelativePath: graphifyPrefix + relPath,
			Content:      content,
			SizeBytes:    int64(len(data)),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk knowledge graph files: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})
	return files, nil
}

func summarizeGraphJSON(data []byte, cfg config.KnowledgeGraphConfig) (string, error) {
	var graph graphifyGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return "", fmt.Errorf("parse graph.json: %w", err)
	}

	limit := cfg.MaxNodes
	if limit <= 0 {
		limit = config.DefaultConfig().KnowledgeGraph.MaxNodes
	}
	if limit > len(graph.Nodes) {
		limit = len(graph.Nodes)
	}

	lines := []string{
		fmt.Sprintf("nodes: %d", len(graph.Nodes)),
		fmt.Sprintf("edges: %d", len(graph.Edges)),
	}
	for i := 0; i < limit; i++ {
		node := graph.Nodes[i]
		lines = append(lines, fmt.Sprintf("node[%d]: id=%v label=%v type=%v", i, node["id"], node["label"], node["type"]))
	}

	return strings.Join(lines, "\n"), nil
}
