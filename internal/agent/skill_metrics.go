package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sidekickos/rillan/internal/config"
)

// SkillMetric records the recent runtime cost of one skill kind.
type SkillMetric struct {
	SkillID           string `json:"skill_id"`
	InvocationCount   int    `json:"invocation_count"`
	LastObservedAt    string `json:"last_observed_at"`
	LastLatencyMillis int64  `json:"last_latency_millis"`
	AverageLatencyMs  int64  `json:"average_latency_ms"`
}

// SkillMetricsStore is the persisted runtime-state container for skill latency
// metrics.
type SkillMetricsStore struct {
	Skills []SkillMetric `json:"skills"`
}

// DefaultSkillMetricsPath returns the runtime-state path for skill metrics.
func DefaultSkillMetricsPath() string {
	return filepath.Join(config.DefaultDataDir(), "agent", "skill_metrics.json")
}

// LoadSkillMetrics loads persisted skill metrics from the data directory.
func LoadSkillMetrics() (SkillMetricsStore, error) {
	path := DefaultSkillMetricsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SkillMetricsStore{Skills: []SkillMetric{}}, nil
		}
		return SkillMetricsStore{}, fmt.Errorf("read skill metrics: %w", err)
	}
	var store SkillMetricsStore
	if err := json.Unmarshal(data, &store); err != nil {
		return SkillMetricsStore{}, fmt.Errorf("parse skill metrics: %w", err)
	}
	if store.Skills == nil {
		store.Skills = []SkillMetric{}
	}
	return store, nil
}

// SaveSkillMetrics writes persisted skill metrics to the data directory.
func SaveSkillMetrics(store SkillMetricsStore) error {
	path := DefaultSkillMetricsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill metrics dir: %w", err)
	}
	sort.Slice(store.Skills, func(i, j int) bool { return store.Skills[i].SkillID < store.Skills[j].SkillID })
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill metrics: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write skill metrics: %w", err)
	}
	return nil
}

// RecordSkillLatency updates the persisted runtime-state record for one skill
// invocation.
func RecordSkillLatency(skillID string, duration time.Duration, observedAt time.Time) error {
	store, err := LoadSkillMetrics()
	if err != nil {
		return err
	}
	for i := range store.Skills {
		if store.Skills[i].SkillID != skillID {
			continue
		}
		metric := &store.Skills[i]
		metric.InvocationCount++
		metric.LastObservedAt = observedAt.UTC().Format(time.RFC3339)
		metric.LastLatencyMillis = duration.Milliseconds()
		metric.AverageLatencyMs = rollingAverage(metric.AverageLatencyMs, metric.InvocationCount, duration.Milliseconds())
		return SaveSkillMetrics(store)
	}
	store.Skills = append(store.Skills, SkillMetric{
		SkillID:           skillID,
		InvocationCount:   1,
		LastObservedAt:    observedAt.UTC().Format(time.RFC3339),
		LastLatencyMillis: duration.Milliseconds(),
		AverageLatencyMs:  duration.Milliseconds(),
	})
	return SaveSkillMetrics(store)
}

func rollingAverage(previous int64, count int, latest int64) int64 {
	if count <= 1 {
		return latest
	}
	return ((previous * int64(count-1)) + latest) / int64(count)
}
