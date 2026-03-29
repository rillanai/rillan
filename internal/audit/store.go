package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sidekickos/rillan/internal/config"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func DefaultLedgerPath() string {
	return filepath.Join(config.DefaultDataDir(), "audit", "ledger.jsonl")
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create audit directory: %w", err)
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Record(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open audit ledger: %w", err)
	}
	defer file.Close()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}

	return nil
}

func (s *Store) ReadAll(ctx context.Context) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open audit ledger: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	events := make([]Event, 0)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit ledger: %w", err)
	}

	return events, nil
}
