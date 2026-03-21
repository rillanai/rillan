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

	"github.com/sidekickos/rillan/internal/config"
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
	for _, pattern := range patterns {
		cleanPattern := filepath.ToSlash(strings.TrimSpace(pattern))
		if cleanPattern == "" {
			continue
		}
		if strings.ContainsAny(cleanPattern, "*?[") {
			matched, err := path.Match(cleanPattern, value)
			if err == nil && matched {
				return true
			}
			continue
		}
		if value == cleanPattern || strings.HasPrefix(value, cleanPattern+"/") {
			return true
		}
	}
	return false
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
