// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/rillanai/rillan/internal/config"
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

	for _, statement := range []string{"DELETE FROM vectors", "DELETE FROM chunks_fts", "DELETE FROM chunks", "DELETE FROM documents"} {
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

	ftsStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks_fts(chunk_id, document_path, ordinal, start_line, end_line, content)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare chunks fts insert: %w", err)
	}
	defer ftsStmt.Close()

	for _, chunk := range chunks {
		if _, err = chunkStmt.ExecContext(ctx, chunk.ID, chunk.DocumentPath, chunk.Ordinal, chunk.StartLine, chunk.EndLine, chunk.Content, chunk.ContentHash); err != nil {
			return fmt.Errorf("insert chunk %s: %w", chunk.ID, err)
		}
		if _, err = ftsStmt.ExecContext(ctx, chunk.ID, chunk.DocumentPath, chunk.Ordinal, chunk.StartLine, chunk.EndLine, chunk.Content); err != nil {
			return fmt.Errorf("insert chunk fts %s: %w", chunk.ID, err)
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
	status := Status{LastAttemptState: RunStatusNeverIndexed, DBPath: s.path}

	var (
		lastRunRoot sql.NullString
		state       sql.NullString
		lastError   sql.NullString
		completedAt sql.NullString
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT root_path, status, error_message, completed_at
		FROM index_runs
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&lastRunRoot, &state, &lastError, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read index status: %w", err)
	}

	if state.Valid {
		status.LastAttemptState = state.String
	}
	if lastRunRoot.Valid {
		status.LastAttemptRootPath = lastRunRoot.String
	}
	if lastError.Valid {
		status.LastAttemptError = lastError.String
	}
	if completedAt.Valid {
		status.LastAttemptAt = parseSQLiteTimestamp(completedAt.String)
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
			status.CommittedRootPath = successRoot.String
		}
		if successCompleted.Valid {
			status.CommittedIndexedAt = parseSQLiteTimestamp(successCompleted.String)
		}
	}

	return status, nil
}

func (s *Store) SearchChunks(ctx context.Context, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	if limit < 1 {
		return nil, fmt.Errorf("search limit must be greater than zero")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.document_path, c.ordinal, c.start_line, c.end_line, c.content, v.embedding
		FROM chunks c
		JOIN vectors v ON v.chunk_id = c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query chunk search rows: %w", err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var (
			result        SearchResult
			embeddingBlob []byte
		)
		if err := rows.Scan(&result.ChunkID, &result.DocumentPath, &result.Ordinal, &result.StartLine, &result.EndLine, &result.Content, &embeddingBlob); err != nil {
			return nil, fmt.Errorf("scan chunk search row: %w", err)
		}

		embedding, err := DecodeEmbedding(embeddingBlob)
		if err != nil {
			return nil, fmt.Errorf("decode embedding for %s: %w", result.ChunkID, err)
		}
		if len(queryEmbedding) != len(embedding) {
			return nil, fmt.Errorf("query embedding dimensions %d do not match stored chunk %s dimensions %d", len(queryEmbedding), result.ChunkID, len(embedding))
		}
		result.Score = cosineSimilarity(queryEmbedding, embedding)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunk search rows: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].DocumentPath != results[j].DocumentPath {
			return results[i].DocumentPath < results[j].DocumentPath
		}
		if results[i].Ordinal != results[j].Ordinal {
			return results[i].Ordinal < results[j].Ordinal
		}
		return results[i].ChunkID < results[j].ChunkID
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (s *Store) SearchChunksKeyword(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit < 1 {
		return nil, fmt.Errorf("search limit must be greater than zero")
	}

	matchQuery := buildFTSQuery(query)
	if matchQuery == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_id, document_path, ordinal, start_line, end_line, content
		FROM chunks_fts
		WHERE chunks_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, matchQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("query keyword search rows: %w", err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	position := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var result SearchResult
		if err := rows.Scan(&result.ChunkID, &result.DocumentPath, &result.Ordinal, &result.StartLine, &result.EndLine, &result.Content); err != nil {
			return nil, fmt.Errorf("scan keyword search row: %w", err)
		}
		result.Score = 1.0 / float64(position+1)
		results = append(results, result)
		position++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate keyword search rows: %w", err)
	}

	return results, nil
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

func parseSQLiteTimestamp(value string) time.Time {
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func buildFTSQuery(query string) string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	quoted := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		quoted = append(quoted, fmt.Sprintf("\"%s\"", part))
	}
	return strings.Join(quoted, " OR ")
}

func cosineSimilarity(left, right []float32) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}

	var dot float64
	var leftNorm float64
	var rightNorm float64
	for i := range left {
		l := float64(left[i])
		r := float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
