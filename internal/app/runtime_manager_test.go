package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/httpapi"
)

type managerTestProvider struct {
	name string
}

func (p managerTestProvider) Name() string { return p.name }

func (p managerTestProvider) Ready(context.Context) error { return nil }

func (p managerTestProvider) ChatCompletions(context.Context, chat.ProviderRequest) (*http.Response, error) {
	return nil, nil
}

func TestRuntimeManagerPreservesSnapshotOnFailedRefresh(t *testing.T) {
	initial := &runtimeState{snapshot: httpapi.RuntimeSnapshot{Provider: managerTestProvider{name: "initial"}}}
	buildErr := errors.New("boom")
	builder := func(context.Context) (*runtimeState, error) {
		if buildErr != nil {
			return nil, buildErr
		}
		return &runtimeState{snapshot: httpapi.RuntimeSnapshot{Provider: managerTestProvider{name: "updated"}}}, nil
	}

	manager := newRuntimeManager(initial, builder, slog.Default())
	if err := manager.Refresh(context.Background()); !errors.Is(err, buildErr) {
		t.Fatalf("Refresh error = %v, want %v", err, buildErr)
	}
	if got, want := manager.CurrentSnapshot().Provider.Name(), "initial"; got != want {
		t.Fatalf("provider after failed refresh = %q, want %q", got, want)
	}

	buildErr = nil
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if got, want := manager.CurrentSnapshot().Provider.Name(), "updated"; got != want {
		t.Fatalf("provider after successful refresh = %q, want %q", got, want)
	}
}

func TestRuntimeManagerSerializesConcurrentRefreshes(t *testing.T) {
	var active int
	var maxActive int
	var mu sync.Mutex
	builder := func(context.Context) (*runtimeState, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		active--
		mu.Unlock()
		return &runtimeState{snapshot: httpapi.RuntimeSnapshot{Provider: managerTestProvider{name: "updated"}}}, nil
	}

	manager := newRuntimeManager(&runtimeState{snapshot: httpapi.RuntimeSnapshot{Provider: managerTestProvider{name: "initial"}}}, builder, slog.Default())

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.Refresh(context.Background()); err != nil {
				t.Errorf("Refresh returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got, want := maxActive, 1; got != want {
		t.Fatalf("max concurrent refreshes = %d, want %d", got, want)
	}
}
