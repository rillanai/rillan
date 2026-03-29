package retrieval

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

func TestResolveSettingsRequestOverrideWins(t *testing.T) {
	falseValue := false
	overrideTopK := 2
	defaults := config.RetrievalConfig{Enabled: true, TopK: 7, MaxContextChars: 500}

	settings, err := ResolveSettings(defaults, &internalopenai.RetrievalOptions{Enabled: &falseValue, TopK: &overrideTopK})
	if err != nil {
		t.Fatalf("ResolveSettings returned error: %v", err)
	}
	if settings.Enabled {
		t.Fatal("expected request override to disable retrieval")
	}
	if got, want := settings.TopK, 2; got != want {
		t.Fatalf("TopK = %d, want %d", got, want)
	}
	if got, want := settings.MaxContextChars, 500; got != want {
		t.Fatalf("MaxContextChars = %d, want %d", got, want)
	}
}

func TestCompileContextBoundsAndAnnotatesSources(t *testing.T) {
	compiled := CompileContext([]index.SearchResult{{
		ChunkID:      "chunk-1",
		DocumentPath: "docs/guide.md",
		StartLine:    10,
		EndLine:      20,
		Content:      strings.Repeat("useful context ", 20),
		Score:        0.99,
	}}, 120)

	if compiled.Text == "" {
		t.Fatal("expected compiled context text")
	}
	if len(compiled.Text) > 120 {
		t.Fatalf("compiled text length = %d, want <= 120", len(compiled.Text))
	}
	if !strings.Contains(compiled.Text, "[source 1] docs/guide.md:10-20") {
		t.Fatalf("compiled text missing source reference: %s", compiled.Text)
	}
	if len(compiled.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(compiled.Sources))
	}
	if !compiled.Truncated {
		t.Fatal("expected compiled context to be truncated")
	}
}

func TestPrepareAddsCompiledContextAndSanitizesRequest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedStore(t, dbPath, []index.ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: "notes/local.md",
		Ordinal:      0,
		StartLine:    1,
		EndLine:      2,
		Content:      "retrieval can use local indexed context",
		ContentHash:  "chunk-hash",
	}})

	pipeline := NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 400}, dbPath)
	req := internalopenai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []internalopenai.Message{{
			Role:    "user",
			Content: mustMarshalString(t, "how does local retrieval work?"),
		}},
	}

	sanitized, body, err := pipeline.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if len(sanitized.Metadata) != 1 {
		t.Fatalf("metadata entries = %d, want 1", len(sanitized.Metadata))
	}
	if len(sanitized.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sanitized.Messages))
	}
	contextMessage, err := internalopenai.MessageText(sanitized.Messages[0])
	if err != nil {
		t.Fatalf("MessageText returned error: %v", err)
	}
	if !strings.Contains(contextMessage, "<rillan_context>") {
		t.Fatalf("context message missing wrapper: %s", contextMessage)
	}
	if !strings.Contains(contextMessage, "notes/local.md:1-2") {
		t.Fatalf("context message missing source attribution: %s", contextMessage)
	}
	if strings.Contains(string(body), "\"retrieval\"") {
		t.Fatalf("sanitized body leaked retrieval field: %s", string(body))
	}

	var outbound map[string]any
	if err := json.Unmarshal(body, &outbound); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if _, ok := outbound["retrieval"]; ok {
		t.Fatalf("outbound body still contains retrieval field: %v", outbound)
	}
}

func TestPrepareFallsBackToPlaceholderEmbeddingWhenPrimaryEmbedderFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedStore(t, dbPath, []index.ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: "notes/local.md",
		Ordinal:      0,
		StartLine:    1,
		EndLine:      2,
		Content:      "retrieval can use local indexed context",
		ContentHash:  "chunk-hash",
	}})

	pipeline := NewPipeline(
		config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 400},
		dbPath,
		WithQueryEmbedder(NewFallbackEmbedder(
			failingQueryEmbedder{err: context.DeadlineExceeded},
			PlaceholderEmbedder{},
		)),
	)
	req := internalopenai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []internalopenai.Message{{
			Role:    "user",
			Content: mustMarshalString(t, "how does local retrieval work?"),
		}},
	}

	sanitized, _, err := pipeline.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if len(sanitized.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sanitized.Messages))
	}
	contextMessage, err := internalopenai.MessageText(sanitized.Messages[0])
	if err != nil {
		t.Fatalf("MessageText returned error: %v", err)
	}
	if !strings.Contains(contextMessage, "notes/local.md:1-2") {
		t.Fatalf("context message missing source attribution after fallback: %s", contextMessage)
	}
}

func TestPrepareUsesKeywordSearchWhenEmbeddingUnavailable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedStore(t, dbPath, []index.ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: "notes/local.md",
		Ordinal:      0,
		StartLine:    1,
		EndLine:      2,
		Content:      "retrieval can use local indexed context",
		ContentHash:  "chunk-hash",
	}})

	pipeline := NewPipeline(
		config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 400},
		dbPath,
		WithQueryEmbedder(failingQueryEmbedder{err: context.DeadlineExceeded}),
	)
	req := internalopenai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []internalopenai.Message{{
			Role:    "user",
			Content: mustMarshalString(t, "how does local retrieval work?"),
		}},
	}

	sanitized, _, err := pipeline.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if len(sanitized.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sanitized.Messages))
	}
	contextMessage, err := internalopenai.MessageText(sanitized.Messages[0])
	if err != nil {
		t.Fatalf("MessageText returned error: %v", err)
	}
	if !strings.Contains(contextMessage, "notes/local.md:1-2") {
		t.Fatalf("context message missing source attribution after keyword fallback: %s", contextMessage)
	}
}

func TestSummarizeDebugBoundsLongSourceRefsBySize(t *testing.T) {
	metadata := DebugMetadata{
		Enabled:  true,
		Settings: Settings{TopK: 3},
		Compiled: CompiledContext{Sources: []SourceReference{{
			DocumentPath: strings.Repeat("segment/", 20) + "file.md",
			StartLine:    10,
			EndLine:      20,
		}}},
	}

	summary := SummarizeDebugBounded(metadata, 1, 40, 40)
	if len(summary.SourceRefs) != 1 {
		t.Fatalf("source refs = %d, want 1", len(summary.SourceRefs))
	}
	if got := summary.SourceRefs[0]; len(got) > 40 {
		t.Fatalf("source ref length = %d, want <= 40", len(got))
	}
	if got := summary.SourceRefs[0]; !strings.Contains(got, "#") {
		t.Fatalf("source ref = %q, want hash marker", got)
	}
	if got := summary.SourceRefs[0]; strings.Contains(got, strings.Repeat("segment/", 20)) {
		t.Fatalf("source ref leaked full path: %q", got)
	}
}

func seedStore(t *testing.T, dbPath string, chunks []index.ChunkRecord) {
	t.Helper()

	store, err := index.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	vectors := make([]index.VectorRecord, 0, len(chunks))
	docsByPath := make(map[string]index.DocumentRecord)
	for _, chunk := range chunks {
		docsByPath[chunk.DocumentPath] = index.DocumentRecord{Path: chunk.DocumentPath, ContentHash: chunk.DocumentPath, SizeBytes: int64(len(chunk.Content))}
		vectors = append(vectors, index.VectorRecord{
			ChunkID:    chunk.ID,
			Dimensions: 8,
			Embedding:  index.EncodeEmbedding(index.PlaceholderEmbedding(chunk.Content)),
		})
	}

	documents := make([]index.DocumentRecord, 0, len(docsByPath))
	for _, document := range docsByPath {
		documents = append(documents, document)
	}

	if err := store.ReplaceAll(context.Background(), documents, chunks, vectors); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}
}

func mustMarshalString(t *testing.T, value string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return data
}
