package index

import "testing"

func TestChunkDocumentProducesStableChunkIDs(t *testing.T) {
	file := SourceFile{RelativePath: "main.go", Content: "one\ntwo\nthree\nfour", SizeBytes: 18}

	first := ChunkFile(file, 2)
	second := ChunkFile(file, 2)

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("unexpected chunk counts: %d %d", len(first), len(second))
	}
	if first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("chunk IDs are not deterministic: %#v %#v", first, second)
	}
}

func TestChunkDocumentUsesConfiguredLineBoundaries(t *testing.T) {
	file := SourceFile{RelativePath: "main.go", Content: "one\ntwo\nthree", SizeBytes: 13}
	chunks := ChunkFile(file, 2)

	if got, want := chunks[0].StartLine, 1; got != want {
		t.Fatalf("start line = %d, want %d", got, want)
	}
	if got, want := chunks[0].EndLine, 2; got != want {
		t.Fatalf("end line = %d, want %d", got, want)
	}
	if got, want := chunks[1].StartLine, 3; got != want {
		t.Fatalf("second start line = %d, want %d", got, want)
	}
}
