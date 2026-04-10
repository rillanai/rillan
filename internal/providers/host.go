// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package providers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rillanai/rillan/internal/config"
)

type Host struct {
	defaultProviderID string
	providers         map[string]Provider
}

func NewHost(cfg config.RuntimeProviderHostConfig, client *http.Client) (*Host, error) {
	defaultProviderID := strings.TrimSpace(cfg.Default)
	if defaultProviderID == "" {
		return nil, fmt.Errorf("runtime provider host default must not be empty")
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("runtime provider host must include at least one provider")
	}

	host := &Host{
		defaultProviderID: defaultProviderID,
		providers:         make(map[string]Provider, len(cfg.Providers)),
	}
	for _, providerCfg := range cfg.Providers {
		providerID := strings.TrimSpace(providerCfg.ID)
		if providerID == "" {
			return nil, fmt.Errorf("runtime provider id must not be empty")
		}
		if _, exists := host.providers[providerID]; exists {
			return nil, fmt.Errorf("runtime provider %q declared more than once", providerID)
		}
		provider, err := newAdapter(providerCfg, client)
		if err != nil {
			return nil, fmt.Errorf("build runtime provider %q: %w", providerID, err)
		}
		host.providers[providerID] = provider
	}
	if _, ok := host.providers[defaultProviderID]; !ok {
		return nil, fmt.Errorf("runtime provider host default %q not found", defaultProviderID)
	}

	return host, nil
}

func (h *Host) Provider(id string) (Provider, error) {
	providerID := strings.TrimSpace(id)
	provider, ok := h.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("runtime provider %q not found", providerID)
	}
	return provider, nil
}

func (h *Host) DefaultProvider() (Provider, error) {
	return h.Provider(h.defaultProviderID)
}
