package skills

type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ReadFilesRequest struct {
	RepoRoot        string   `json:"repo_root"`
	Paths           []string `json:"paths"`
	MaxFiles        int      `json:"max_files"`
	MaxCharsPerFile int      `json:"max_chars_per_file"`
}

type ReadFilesResult struct {
	Files []FileContent `json:"files"`
}

type SearchRepoRequest struct {
	RepoRoot        string `json:"repo_root"`
	Query           string `json:"query"`
	MaxMatches      int    `json:"max_matches"`
	MaxSnippetChars int    `json:"max_snippet_chars"`
}

type RepoMatch struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
}

type SearchRepoResult struct {
	Matches []RepoMatch `json:"matches"`
}

type IndexLookupRequest struct {
	DBPath          string `json:"db_path,omitempty"`
	Query           string `json:"query"`
	MaxMatches      int    `json:"max_matches"`
	MaxSnippetChars int    `json:"max_snippet_chars"`
}

type IndexMatch struct {
	Path    string `json:"path"`
	Ref     string `json:"ref"`
	Snippet string `json:"snippet"`
}

type IndexLookupResult struct {
	Matches []IndexMatch `json:"matches"`
}

type GitStatusRequest struct {
	RepoRoot   string `json:"repo_root"`
	MaxEntries int    `json:"max_entries"`
}

type GitStatusResult struct {
	Entries []string `json:"entries"`
}

type GitDiffRequest struct {
	RepoRoot   string `json:"repo_root"`
	MaxChars   int    `json:"max_chars"`
	StagedOnly bool   `json:"staged_only"`
}

type GitDiffResult struct {
	Diff string `json:"diff"`
}
