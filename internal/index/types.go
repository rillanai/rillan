// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import "time"

const (
	RunStatusNeverIndexed = "never_indexed"
	RunStatusSucceeded    = "succeeded"
	RunStatusFailed       = "failed"
)

type SourceFile struct {
	AbsolutePath string
	RelativePath string
	Content      string
	SizeBytes    int64
}

type DocumentRecord struct {
	Path        string
	ContentHash string
	SizeBytes   int64
}

type ChunkRecord struct {
	ID           string
	DocumentPath string
	Ordinal      int
	StartLine    int
	EndLine      int
	Content      string
	ContentHash  string
}

type VectorRecord struct {
	ChunkID    string
	Dimensions int
	Embedding  []byte
}

type SearchResult struct {
	ChunkID      string
	DocumentPath string
	Ordinal      int
	StartLine    int
	EndLine      int
	Content      string
	Score        float64
}

type Status struct {
	ConfiguredRootPath  string
	LastAttemptState    string
	LastAttemptRootPath string
	LastAttemptAt       time.Time
	LastAttemptError    string
	CommittedRootPath   string
	CommittedIndexedAt  time.Time
	Documents           int
	Chunks              int
	Vectors             int
	DBPath              string
}
