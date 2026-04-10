// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

type MCPSnapshot struct {
	OpenFiles   []MCPFileRef    `json:"open_files,omitempty"`
	Selection   *MCPSelection   `json:"selection,omitempty"`
	Diagnostics []MCPDiagnostic `json:"diagnostics,omitempty"`
	VCS         *MCPVCSContext  `json:"vcs,omitempty"`
}

type MCPFileRef struct {
	Path string `json:"path"`
}

type MCPSelection struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

type MCPDiagnostic struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type MCPVCSContext struct {
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Dirty  bool   `json:"dirty"`
}

type MCPSnapshotOptions struct {
	MaxOpenFiles   int
	MaxDiagnostics int
	MaxChars       int
}
