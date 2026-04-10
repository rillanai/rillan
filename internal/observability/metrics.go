// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu                        sync.Mutex
	httpRequestsTotal         map[string]uint64
	httpRequestDurationMillis map[string]uint64
	providerRequestsTotal     map[string]uint64
	retrievalRequestsTotal    uint64
	retrievalSourcesTotal     uint64
	retrievalTruncatedTotal   uint64
}

func NewRegistry() *Registry {
	return &Registry{
		httpRequestsTotal:         map[string]uint64{},
		httpRequestDurationMillis: map[string]uint64{},
		providerRequestsTotal:     map[string]uint64{},
	}
}

func (r *Registry) RecordHTTPRequest(method string, path string, status int, durationMillis int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := formatLabels(map[string]string{"method": method, "path": path, "status": fmt.Sprintf("%d", status)})
	r.httpRequestsTotal[key]++
	r.httpRequestDurationMillis[key] += uint64(durationMillis)
}

func (r *Registry) RecordProviderRequest(provider string, outcome string, status int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := formatLabels(map[string]string{"provider": provider, "outcome": outcome, "status": fmt.Sprintf("%d", status)})
	r.providerRequestsTotal[key]++
}

func (r *Registry) RecordRetrieval(sourceCount int, truncated bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retrievalRequestsTotal++
	r.retrievalSourcesTotal += uint64(sourceCount)
	if truncated {
		r.retrievalTruncatedTotal++
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(r.RenderPrometheus()))
	})
}

func (r *Registry) RenderPrometheus() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.httpRequestsTotal)+len(r.httpRequestDurationMillis)+len(r.providerRequestsTotal)+6)
	appendMetricMap(&lines, "rillan_http_requests_total", r.httpRequestsTotal)
	appendMetricMap(&lines, "rillan_http_request_duration_millis_total", r.httpRequestDurationMillis)
	appendMetricMap(&lines, "rillan_provider_requests_total", r.providerRequestsTotal)
	lines = append(lines,
		fmt.Sprintf("rillan_retrieval_requests_total %d", r.retrievalRequestsTotal),
		fmt.Sprintf("rillan_retrieval_sources_total %d", r.retrievalSourcesTotal),
		fmt.Sprintf("rillan_retrieval_truncated_total %d", r.retrievalTruncatedTotal),
	)
	return strings.Join(lines, "\n") + "\n"
}

func appendMetricMap(lines *[]string, metricName string, values map[string]uint64) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		*lines = append(*lines, fmt.Sprintf("%s%s %d", metricName, key, values[key]))
	}
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, labels[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
