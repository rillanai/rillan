// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

func NormalizeMCPSnapshot(snapshot MCPSnapshot, opts MCPSnapshotOptions) MCPSnapshot {
	maxFiles := opts.MaxOpenFiles
	if maxFiles < 1 {
		maxFiles = 8
	}
	maxDiagnostics := opts.MaxDiagnostics
	if maxDiagnostics < 1 {
		maxDiagnostics = 20
	}
	maxChars := opts.MaxChars
	if maxChars < 1 {
		maxChars = 240
	}

	if len(snapshot.OpenFiles) > maxFiles {
		snapshot.OpenFiles = snapshot.OpenFiles[:maxFiles]
	}
	for i := range snapshot.OpenFiles {
		snapshot.OpenFiles[i].Path = trimText(snapshot.OpenFiles[i].Path, maxChars)
	}
	if snapshot.Selection != nil {
		snapshot.Selection.Path = trimText(snapshot.Selection.Path, maxChars)
		snapshot.Selection.Snippet = trimText(snapshot.Selection.Snippet, maxChars)
	}
	if len(snapshot.Diagnostics) > maxDiagnostics {
		snapshot.Diagnostics = snapshot.Diagnostics[:maxDiagnostics]
	}
	for i := range snapshot.Diagnostics {
		snapshot.Diagnostics[i].Path = trimText(snapshot.Diagnostics[i].Path, maxChars)
		snapshot.Diagnostics[i].Severity = trimText(snapshot.Diagnostics[i].Severity, maxChars)
		snapshot.Diagnostics[i].Message = trimText(snapshot.Diagnostics[i].Message, maxChars)
	}
	if snapshot.VCS != nil {
		snapshot.VCS.Branch = trimText(snapshot.VCS.Branch, maxChars)
		snapshot.VCS.Head = trimText(snapshot.VCS.Head, maxChars)
	}

	return snapshot
}
