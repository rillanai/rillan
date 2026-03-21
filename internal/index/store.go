package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sidekickos/rillan/internal/config"
)

type Store struct {
	path string
	db   *sql.DB
}

func DefaultDBPath() string {
	return filepath.Join(config.DefaultDataDir(), "index", "index.db")
}

func OpenStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	dsn := "file:" + dbPath + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{path: dbPath, db: db}
	if err := store.bootstrap(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) bootstrap(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, bootstrapSQL); err != nil {
		return fmt.Errorf("bootstrap schema: %w", err)
	}

	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version.Valid && version.Int64 >= currentSchemaVersion {
		return nil
	}

	_, err := s.db.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES (?)", currentSchemaVersion)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return nil
}

func (s *Store) RecordRunStart(ctx context.Context, root string) (int64, error) {
	result, err := s.db.ExecContext(ctx, "INSERT INTO index_runs(root_path, status) VALUES (?, ?)", root, "running")
	if err != nil {
		return 0, fmt.Errorf("record index run start: %w", err)
	}
	return result.LastInsertId()
}

func (s *Store) RecordRunCompletion(ctx context.Context, runID int64, status string, documents, chunks, vectors int, errMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE index_runs
		SET status = ?, documents_count = ?, chunks_count = ?, vectors_count = ?, error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, documents, chunks, vectors, nullableString(errMessage), runID)
	if err != nil {
		return fmt.Errorf("record index run completion: %w", err)
	}
	return nil
}

func (s *Store) ReplaceAll(ctx context.Context, documents []DocumentRecord, chunks []ChunkRecord, vectors []VectorRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, statement := range []string{"DELETE FROM vectors", "DELETE FROM chunks", "DELETE FROM documents"} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("clear existing index: %w", err)
		}
	}

	docStmt, err := tx.PrepareContext(ctx, "INSERT INTO documents(path, content_hash, size_bytes) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare documents insert: %w", err)
	}
	defer docStmt.Close()

	for _, document := range documents {
		if _, err = docStmt.ExecContext(ctx, document.Path, document.ContentHash, document.SizeBytes); err != nil {
			return fmt.Errorf("insert document %s: %w", document.Path, err)
		}
	}

	chunkStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks(id, document_path, ordinal, start_line, end_line, content, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare chunks insert: %w", err)
	}
	defer chunkStmt.Close()

	for _, chunk := range chunks {
		if _, err = chunkStmt.ExecContext(ctx, chunk.ID, chunk.DocumentPath, chunk.Ordinal, chunk.StartLine, chunk.EndLine, chunk.Content, chunk.ContentHash); err != nil {
			return fmt.Errorf("insert chunk %s: %w", chunk.ID, err)
		}
	}

	vectorStmt, err := tx.PrepareContext(ctx, "INSERT INTO vectors(chunk_id, dimensions, embedding) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare vectors insert: %w", err)
	}
	defer vectorStmt.Close()

	for _, vector := range vectors {
		if _, err = vectorStmt.ExecContext(ctx, vector.ChunkID, vector.Dimensions, vector.Embedding); err != nil {
			return fmt.Errorf("insert vector for chunk %s: %w", vector.ChunkID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit replace transaction: %w", err)
	}

	return nil
}

func (s *Store) ReadStatus(ctx context.Context) (Status, error) {
	status := Status{State: RunStatusNeverIndexed, DBPath: s.path}

	var (
		lastRunRoot sql.NullString
		state       sql.NullString
		lastError   sql.NullString
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT root_path, status, error_message
		FROM index_runs
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&lastRunRoot, &state, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read index status: %w", err)
	}

	if state.Valid {
		status.State = state.String
	}
	if lastError.Valid {
		status.LastError = lastError.String
	}

	status.Documents, err = s.countRows(ctx, "documents")
	if err != nil {
		return Status{}, err
	}
	status.Chunks, err = s.countRows(ctx, "chunks")
	if err != nil {
		return Status{}, err
	}
	status.Vectors, err = s.countRows(ctx, "vectors")
	if err != nil {
		return Status{}, err
	}

	var (
		successRoot      sql.NullString
		successCompleted sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT root_path, completed_at
		FROM index_runs
		WHERE status = ?
		ORDER BY id DESC
		LIMIT 1
	`, RunStatusSucceeded).Scan(&successRoot, &successCompleted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, fmt.Errorf("read latest successful index run: %w", err)
	}
	if err == nil {
		if successRoot.Valid {
			status.RootPath = successRoot.String
		}
		if successCompleted.Valid {
			parsed, parseErr := time.Parse("2006-01-02 15:04:05", successCompleted.String)
			if parseErr == nil {
				status.LastIndexedAt = parsed.UTC()
			}
		}
	} else if lastRunRoot.Valid {
		status.RootPath = lastRunRoot.String
	}

	return status, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) countRows(ctx context.Context, table string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count rows in %s: %w", table, err)
	}
	return count, nil
}
