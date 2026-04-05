package index

import (
	"context"
	"log/slog"

	"github.com/rillanai/rillan/internal/config"
)

// RebuildOption configures the Rebuild function.
type RebuildOption func(*rebuildOptions)

type rebuildOptions struct {
	embedder Embedder
}

// WithEmbedder sets a real embedder for producing vectors during indexing.
// When the embedder is reachable, OllamaVectorStore is used; otherwise
// the function falls back to placeholder embeddings.
func WithEmbedder(e Embedder, reachable bool) RebuildOption {
	return func(o *rebuildOptions) {
		if reachable {
			o.embedder = e
		}
	}
}

func Rebuild(ctx context.Context, cfg config.Config, logger *slog.Logger, opts ...RebuildOption) (Status, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var options rebuildOptions
	for _, opt := range opts {
		opt(&options)
	}

	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		return Status{}, err
	}
	defer store.Close()

	var vectorStore VectorStore
	if options.embedder != nil {
		vectorStore = NewOllamaVectorStore(options.embedder, cfg.LocalModel.EmbedModel)
	} else {
		vectorStore, err = NewVectorStore(cfg.Runtime.VectorStoreMode)
		if err != nil {
			return Status{}, err
		}
	}

	runID, err := store.RecordRunStart(ctx, cfg.Index.Root)
	if err != nil {
		return Status{}, err
	}

	files, err := DiscoverFiles(cfg.Index)
	if err != nil {
		_ = store.RecordRunCompletion(ctx, runID, RunStatusFailed, 0, 0, 0, err.Error())
		return Status{}, err
	}

	documents := make([]DocumentRecord, 0, len(files))
	chunks := make([]ChunkRecord, 0)
	for _, file := range files {
		documents = append(documents, BuildDocument(file))
		fileChunks := ChunkFile(file, cfg.Index.ChunkSizeLines)
		chunks = append(chunks, fileChunks...)
	}

	vectors, err := vectorStore.BuildRecords(ctx, chunks)
	if err != nil {
		_ = store.RecordRunCompletion(ctx, runID, RunStatusFailed, 0, 0, 0, err.Error())
		return Status{}, err
	}

	if err := store.ReplaceAll(ctx, documents, chunks, vectors); err != nil {
		_ = store.RecordRunCompletion(ctx, runID, RunStatusFailed, 0, 0, 0, err.Error())
		return Status{}, err
	}

	if err := store.RecordRunCompletion(ctx, runID, RunStatusSucceeded, len(documents), len(chunks), len(vectors), ""); err != nil {
		return Status{}, err
	}

	logger.Info("index rebuild completed",
		"root", cfg.Index.Root,
		"vector_store", vectorStore.Mode(),
		"documents", len(documents),
		"chunks", len(chunks),
		"vectors", len(vectors),
	)

	return store.ReadStatus(ctx)
}

func ReadStatus(ctx context.Context, cfg config.Config) (Status, error) {
	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		return Status{}, err
	}
	defer store.Close()

	status, err := store.ReadStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	status.ConfiguredRootPath = cfg.Index.Root
	return status, nil
}
