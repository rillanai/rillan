package index

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sidekickos/rillan/internal/config"
)

func Rebuild(ctx context.Context, cfg config.Config, logger *slog.Logger) (Status, error) {
	if logger == nil {
		logger = slog.Default()
	}

	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		return Status{}, err
	}
	defer store.Close()

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
	vectors := make([]VectorRecord, 0)
	for _, file := range files {
		documents = append(documents, BuildDocument(file))
		fileChunks := ChunkFile(file, cfg.Index.ChunkSizeLines)
		chunks = append(chunks, fileChunks...)
		for _, chunk := range fileChunks {
			embedding := PlaceholderEmbedding(chunk.Content)
			vectors = append(vectors, VectorRecord{
				ChunkID:    chunk.ID,
				Dimensions: len(embedding),
				Embedding:  EncodeEmbedding(embedding),
			})
		}
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
		"documents", len(documents),
		"chunks", len(chunks),
		"vectors", len(vectors),
	)

	return store.ReadStatus(ctx)
}

func ReadStatus(ctx context.Context) (Status, error) {
	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		return Status{}, err
	}
	defer store.Close()

	status, err := store.ReadStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	if status.RootPath == "" {
		status.RootPath = fmt.Sprintf("not yet configured")
	}
	return status, nil
}
