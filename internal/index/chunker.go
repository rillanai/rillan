package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func BuildDocument(file SourceFile) DocumentRecord {
	return DocumentRecord{
		Path:        file.RelativePath,
		ContentHash: hashString(file.Content),
		SizeBytes:   file.SizeBytes,
	}
}

func ChunkFile(file SourceFile, linesPerChunk int) []ChunkRecord {
	if linesPerChunk < 1 {
		linesPerChunk = 120
	}
	if file.Content == "" {
		return nil
	}

	lines := strings.Split(file.Content, "\n")
	chunks := make([]ChunkRecord, 0, (len(lines)/linesPerChunk)+1)
	ordinal := 0

	for start := 0; start < len(lines); start += linesPerChunk {
		end := start + linesPerChunk
		if end > len(lines) {
			end = len(lines)
		}
		content := strings.Join(lines[start:end], "\n")
		if content == "" {
			continue
		}
		contentHash := hashString(content)
		chunks = append(chunks, ChunkRecord{
			ID:           hashString(fmt.Sprintf("%s:%d:%s", file.RelativePath, ordinal, contentHash)),
			DocumentPath: file.RelativePath,
			Ordinal:      ordinal,
			StartLine:    start + 1,
			EndLine:      end,
			Content:      content,
			ContentHash:  contentHash,
		})
		ordinal++
	}

	return chunks
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
