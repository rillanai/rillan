package index

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rillanai/rillan/internal/config"
)

const maxIndexableBytes int64 = 1 << 20

func DiscoverFiles(cfg config.IndexConfig) ([]SourceFile, error) {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		return nil, fmt.Errorf("index root is empty")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve index root: %w", err)
	}

	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat index root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("index root must be a directory")
	}

	files := make([]SourceFile, 0)
	err = filepath.WalkDir(absRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if filePath == absRoot {
			return nil
		}

		relPath, err := filepath.Rel(absRoot, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) || matchesPattern(relPath, cfg.Excludes) {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		if matchesPattern(relPath, cfg.Excludes) {
			return nil
		}
		if len(cfg.Includes) > 0 && !matchesPattern(relPath, cfg.Includes) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxIndexableBytes {
			return nil
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if !isIndexableText(data) {
			return nil
		}

		files = append(files, SourceFile{
			AbsolutePath: filePath,
			RelativePath: relPath,
			Content:      normalizeContent(string(data)),
			SizeBytes:    info.Size(),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk index root: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})

	return files, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".direnv", ".idea":
		return true
	default:
		return false
	}
}

func matchesPattern(value string, patterns []string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	base := path.Base(value)

	for _, pattern := range patterns {
		cleanPattern := filepath.ToSlash(strings.TrimSpace(pattern))
		if cleanPattern == "" {
			continue
		}
		if value == cleanPattern || strings.HasPrefix(value, cleanPattern+"/") {
			return true
		}
		if matchesGlob(cleanPattern, value) {
			return true
		}
		if !strings.Contains(cleanPattern, "/") && matchesGlob(cleanPattern, base) {
			return true
		}
	}
	return false
}

func matchesGlob(pattern string, value string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		return false
	}

	patternSegments := strings.Split(pattern, "/")
	valueSegments := strings.Split(value, "/")
	return matchPathSegments(patternSegments, valueSegments)
}

func matchPathSegments(patternSegments []string, valueSegments []string) bool {
	if len(patternSegments) == 0 {
		return len(valueSegments) == 0
	}

	if patternSegments[0] == "**" {
		if matchPathSegments(patternSegments[1:], valueSegments) {
			return true
		}
		if len(valueSegments) == 0 {
			return false
		}
		return matchPathSegments(patternSegments, valueSegments[1:])
	}

	if len(valueSegments) == 0 {
		return false
	}

	matched, err := path.Match(patternSegments[0], valueSegments[0])
	if err != nil || !matched {
		return false
	}

	return matchPathSegments(patternSegments[1:], valueSegments[1:])
}

func isIndexableText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

func normalizeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}
