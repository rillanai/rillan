// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

func RequiredCapabilities(req ChatCompletionRequest) []string {
	set := map[string]struct{}{}
	if requestUsesTools(req) {
		set["tool_calling"] = struct{}{}
	}
	if requestUsesMultimodal(req) {
		set["multimodal"] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	capabilities := make([]string, 0, len(set))
	for capability := range set {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return capabilities
}

func requestUsesTools(req ChatCompletionRequest) bool {
	if raw, ok := req.extra["tools"]; ok {
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err == nil && len(tools) > 0 {
			return true
		}
	}
	if raw, ok := req.extra["tool_choice"]; ok {
		var choice string
		if err := json.Unmarshal(raw, &choice); err == nil {
			return strings.TrimSpace(choice) != "" && strings.TrimSpace(choice) != "none"
		}
		return len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	}
	return false
}

func requestUsesMultimodal(req ChatCompletionRequest) bool {
	for _, message := range req.Messages {
		parts, ok := decodeContentParts(message.Content)
		if !ok {
			continue
		}
		for _, part := range parts {
			if normalizePartType(part.Type) != "text" {
				return true
			}
		}
	}
	return false
}

type contentPart struct {
	Type string `json:"type"`
}

func decodeContentParts(raw json.RawMessage) ([]contentPart, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var parts []contentPart
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return nil, false
	}
	return parts, true
}

func normalizePartType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
