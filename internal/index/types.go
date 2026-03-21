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

type Status struct {
	State         string
	RootPath      string
	Documents     int
	Chunks        int
	Vectors       int
	LastError     string
	LastIndexedAt time.Time
	DBPath        string
}
