// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rillanai/rillan/internal/index"
)

const (
	defaultMaxFiles        = 8
	defaultMaxCharsPerFile = 2000
	defaultMaxMatches      = 10
	defaultMaxSnippetChars = 240
	defaultMaxGitEntries   = 20
	defaultMaxGitDiffChars = 4000
)

type Registry struct {
	approvedRepoRoots []string
}

func NewRegistry(approvedRepoRoots []string) *Registry {
	return &Registry{approvedRepoRoots: append([]string(nil), approvedRepoRoots...)}
}

func (r *Registry) ReadFiles(ctx context.Context, req ReadFilesRequest) (ReadFilesResult, error) {
	paths, maxChars, err := validateReadFilesRequest(req)
	if err != nil {
		return ReadFilesResult{}, err
	}
	approvedRoot, err := ResolveApprovedRepoRoot(req.RepoRoot, r.approvedRepoRoots)
	if err != nil {
		return ReadFilesResult{}, err
	}
	files := make([]FileContent, 0, len(paths))
	for _, path := range paths {
		content, err := readFileBounded(ctx, approvedRoot, path, maxChars)
		if err != nil {
			return ReadFilesResult{}, err
		}
		files = append(files, FileContent{Path: path, Content: content})
	}
	return ReadFilesResult{Files: files}, nil
}

func (r *Registry) SearchRepo(ctx context.Context, req SearchRepoRequest) (SearchRepoResult, error) {
	maxMatches := positiveOrDefault(req.MaxMatches, defaultMaxMatches)
	maxSnippetChars := positiveOrDefault(req.MaxSnippetChars, defaultMaxSnippetChars)
	if strings.TrimSpace(req.RepoRoot) == "" {
		return SearchRepoResult{}, fmt.Errorf("search_repo.repo_root must not be empty")
	}
	if strings.TrimSpace(req.Query) == "" {
		return SearchRepoResult{}, fmt.Errorf("search_repo.query must not be empty")
	}
	approvedRoot, err := ResolveApprovedRepoRoot(req.RepoRoot, r.approvedRepoRoots)
	if err != nil {
		return SearchRepoResult{}, err
	}
	results, err := searchRepoBounded(ctx, approvedRoot, req.Query, maxMatches, maxSnippetChars)
	if err != nil {
		return SearchRepoResult{}, err
	}
	return SearchRepoResult{Matches: results}, nil
}

func (r *Registry) IndexLookup(ctx context.Context, req IndexLookupRequest) (IndexLookupResult, error) {
	maxMatches := positiveOrDefault(req.MaxMatches, defaultMaxMatches)
	maxSnippetChars := positiveOrDefault(req.MaxSnippetChars, defaultMaxSnippetChars)
	if strings.TrimSpace(req.Query) == "" {
		return IndexLookupResult{}, fmt.Errorf("index_lookup.query must not be empty")
	}
	dbPath := req.DBPath
	if strings.TrimSpace(dbPath) == "" {
		dbPath = index.DefaultDBPath()
	}
	store, err := index.OpenStore(dbPath)
	if err != nil {
		return IndexLookupResult{}, err
	}
	defer store.Close()

	results, err := store.SearchChunksKeyword(ctx, req.Query, maxMatches)
	if err != nil {
		return IndexLookupResult{}, err
	}
	matches := make([]IndexMatch, 0, len(results))
	for _, result := range results {
		matches = append(matches, IndexMatch{
			Path:    result.DocumentPath,
			Ref:     fmt.Sprintf("%s:%d-%d", result.DocumentPath, result.StartLine, result.EndLine),
			Snippet: trimText(result.Content, maxSnippetChars),
		})
	}
	return IndexLookupResult{Matches: matches}, nil
}

func (r *Registry) GitStatus(ctx context.Context, req GitStatusRequest) (GitStatusResult, error) {
	if strings.TrimSpace(req.RepoRoot) == "" {
		return GitStatusResult{}, fmt.Errorf("git_status.repo_root must not be empty")
	}
	approvedRoot, err := ResolveApprovedRepoRoot(req.RepoRoot, r.approvedRepoRoots)
	if err != nil {
		return GitStatusResult{}, err
	}
	output, err := runGit(ctx, approvedRoot, "status", "--short")
	if err != nil {
		return GitStatusResult{}, err
	}
	entries := splitNonEmptyLines(output)
	if len(entries) > positiveOrDefault(req.MaxEntries, defaultMaxGitEntries) {
		entries = entries[:positiveOrDefault(req.MaxEntries, defaultMaxGitEntries)]
	}
	return GitStatusResult{Entries: entries}, nil
}

func (r *Registry) GitDiff(ctx context.Context, req GitDiffRequest) (GitDiffResult, error) {
	if strings.TrimSpace(req.RepoRoot) == "" {
		return GitDiffResult{}, fmt.Errorf("git_diff.repo_root must not be empty")
	}
	approvedRoot, err := ResolveApprovedRepoRoot(req.RepoRoot, r.approvedRepoRoots)
	if err != nil {
		return GitDiffResult{}, err
	}
	args := []string{"diff", "--no-ext-diff"}
	if req.StagedOnly {
		args = append(args, "--staged")
	}
	output, err := runGit(ctx, approvedRoot, args...)
	if err != nil {
		return GitDiffResult{}, err
	}
	return GitDiffResult{Diff: trimText(output, positiveOrDefault(req.MaxChars, defaultMaxGitDiffChars))}, nil
}

func validateReadFilesRequest(req ReadFilesRequest) ([]string, int, error) {
	if strings.TrimSpace(req.RepoRoot) == "" {
		return nil, 0, fmt.Errorf("read_files.repo_root must not be empty")
	}
	if len(req.Paths) == 0 {
		return nil, 0, fmt.Errorf("read_files.paths must not be empty")
	}
	maxFiles := positiveOrDefault(req.MaxFiles, defaultMaxFiles)
	if len(req.Paths) > maxFiles {
		return nil, 0, fmt.Errorf("read_files.paths exceeds limit of %d", maxFiles)
	}
	cleaned := make([]string, 0, len(req.Paths))
	for _, path := range req.Paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			return nil, 0, fmt.Errorf("read_files.paths contains invalid path")
		}
		cleaned = append(cleaned, path)
	}
	return cleaned, positiveOrDefault(req.MaxCharsPerFile, defaultMaxCharsPerFile), nil
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
