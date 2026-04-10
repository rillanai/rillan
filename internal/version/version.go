// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package version

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func String() string {
	if Commit == "" {
		return Version
	}

	return Version + " (" + Commit + ")"
}
