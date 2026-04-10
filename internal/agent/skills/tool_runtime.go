// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"context"
	"errors"
	"fmt"
)

var ErrUnknownReadOnlyTool = errors.New("unknown read-only tool")

const (
	ToolNameReadFiles   = "read_files"
	ToolNameSearchRepo  = "search_repo"
	ToolNameIndexLookup = "index_lookup"
	ToolNameGitStatus   = "git_status"
	ToolNameGitDiff     = "git_diff"
)

type ReadOnlyTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ExecuteRequest struct {
	Name       string
	RepoRoot   string
	Paths      []string
	Query      string
	DBPath     string
	StagedOnly bool
}

type ExecuteResult struct {
	Name    string
	Payload any
}

func ListReadOnlyTools() []ReadOnlyTool {
	return []ReadOnlyTool{
		{Name: ToolNameGitDiff, Description: "Return a bounded git diff"},
		{Name: ToolNameGitStatus, Description: "Return bounded git status entries"},
		{Name: ToolNameIndexLookup, Description: "Query the local index for bounded matches"},
		{Name: ToolNameReadFiles, Description: "Read bounded file contents from the repo"},
		{Name: ToolNameSearchRepo, Description: "Search the repo for bounded text matches"},
	}
}

func (r *Registry) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	var (
		payload any
		err     error
	)
	switch req.Name {
	case ToolNameReadFiles:
		payload, err = r.ReadFiles(ctx, ReadFilesRequest{RepoRoot: req.RepoRoot, Paths: req.Paths})
	case ToolNameSearchRepo:
		payload, err = r.SearchRepo(ctx, SearchRepoRequest{RepoRoot: req.RepoRoot, Query: req.Query})
	case ToolNameIndexLookup:
		payload, err = r.IndexLookup(ctx, IndexLookupRequest{DBPath: req.DBPath, Query: req.Query})
	case ToolNameGitStatus:
		payload, err = r.GitStatus(ctx, GitStatusRequest{RepoRoot: req.RepoRoot})
	case ToolNameGitDiff:
		payload, err = r.GitDiff(ctx, GitDiffRequest{RepoRoot: req.RepoRoot, StagedOnly: req.StagedOnly})
	default:
		return ExecuteResult{}, fmt.Errorf("%w %q", ErrUnknownReadOnlyTool, req.Name)
	}
	if err != nil {
		return ExecuteResult{}, err
	}
	return ExecuteResult{Name: req.Name, Payload: payload}, nil
}
