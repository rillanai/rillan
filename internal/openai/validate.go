// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func ValidateChatCompletionRequest(req ChatCompletionRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model must not be empty")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages must contain at least one item")
	}

	for idx, message := range req.Messages {
		if !validRole(message.Role) {
			return fmt.Errorf("messages[%d].role must be one of system, developer, user, assistant, or tool", idx)
		}

		if err := validateMessageContent(message); err != nil {
			return fmt.Errorf("messages[%d].content %w", idx, err)
		}
	}

	if req.Retrieval != nil {
		if req.Retrieval.TopK != nil && *req.Retrieval.TopK < 1 {
			return fmt.Errorf("retrieval.top_k must be greater than zero")
		}
		if req.Retrieval.MaxContextChars != nil && *req.Retrieval.MaxContextChars < 1 {
			return fmt.Errorf("retrieval.max_context_chars must be greater than zero")
		}
	}

	return nil
}

func validateMessageContent(message Message) error {
	content := bytes.TrimSpace(message.Content)
	if len(content) == 0 {
		if message.hasField("tool_calls") {
			return nil
		}
		return fmt.Errorf("must be present")
	}
	if !json.Valid(content) {
		return fmt.Errorf("must be valid JSON")
	}
	if bytes.Equal(content, []byte("null")) {
		if message.hasField("tool_calls") {
			return nil
		}
		return fmt.Errorf("must not be null")
	}

	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("must not be empty")
		}
	}

	return nil
}

func MessageText(message Message) (string, error) {
	var content string
	if err := json.Unmarshal(message.Content, &content); err != nil {
		return "", err
	}
	return content, nil
}

func WriteError(w http.ResponseWriter, status int, messageType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: APIError{
			Message: message,
			Type:    messageType,
		},
	})
}

func validRole(role string) bool {
	switch role {
	case "system", "developer", "user", "assistant", "tool":
		return true
	default:
		return false
	}
}
