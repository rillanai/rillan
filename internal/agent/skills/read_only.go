package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var gitCommand = func(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	return cmd.CombinedOutput()
}

func readFileBounded(ctx context.Context, repoRoot string, relativePath string, maxChars int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, relativePath))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return "", fmt.Errorf("path %q escapes repo root", relativePath)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return trimText(string(data), maxChars), nil
}

func searchRepoBounded(ctx context.Context, repoRoot string, query string, maxMatches int, maxSnippetChars int) ([]RepoMatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	results := make([]RepoMatch, 0, maxMatches)
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", ".direnv", ".idea":
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)
		idx := strings.Index(strings.ToLower(content), needle)
		if idx == -1 {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		results = append(results, RepoMatch{Path: rel, Snippet: snippetAround(content, idx, maxSnippetChars)})
		if len(results) >= maxMatches {
			return errStopWalk
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, err
	}
	return results, nil
}

func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := gitCommand(ctx, repoRoot, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func snippetAround(content string, idx int, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return strings.TrimSpace(content)
	}
	half := maxChars / 2
	start := idx - half
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(content) {
		end = len(content)
		if end-maxChars > 0 {
			start = end - maxChars
		} else {
			start = 0
		}
	}
	return trimText(content[start:end], maxChars)
}

var errStopWalk = fmt.Errorf("stop walk")

func trimText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if maxChars < 1 || len(value) <= maxChars {
		return value
	}
	if maxChars <= len("...[truncated]") {
		return value[:maxChars]
	}
	return strings.TrimSpace(value[:maxChars-len("...[truncated]")]) + "...[truncated]"
}
