// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"bytes"
	"encoding/json"
	"sort"
)

type ChatCompletionRequest struct {
	Model     string            `json:"model"`
	Messages  []Message         `json:"messages"`
	Stream    bool              `json:"stream,omitempty"`
	Retrieval *RetrievalOptions `json:"retrieval,omitempty"`
	Metadata  []interface{}     `json:"-"`
	extra     map[string]json.RawMessage
}

type RetrievalOptions struct {
	Enabled         *bool `json:"enabled,omitempty"`
	TopK            *int  `json:"top_k,omitempty"`
	MaxContextChars *int  `json:"max_context_chars,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	extra   map[string]json.RawMessage
}

func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type requestAlias struct {
		Model     string            `json:"model"`
		Messages  []Message         `json:"messages"`
		Stream    bool              `json:"stream,omitempty"`
		Retrieval *RetrievalOptions `json:"retrieval,omitempty"`
	}

	var alias requestAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delete(raw, "model")
	delete(raw, "messages")
	delete(raw, "stream")
	delete(raw, "retrieval")

	r.Model = alias.Model
	r.Messages = alias.Messages
	r.Stream = alias.Stream
	r.Retrieval = alias.Retrieval
	r.Metadata = nil
	r.extra = raw

	return nil
}

func (r ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	model, err := json.Marshal(r.Model)
	if err != nil {
		return nil, err
	}
	messages, err := json.Marshal(r.Messages)
	if err != nil {
		return nil, err
	}

	fields := []jsonField{
		{name: "model", value: model},
		{name: "messages", value: messages},
	}
	if r.Stream {
		value, err := json.Marshal(r.Stream)
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{name: "stream", value: value})
	}
	if r.Retrieval != nil {
		value, err := json.Marshal(r.Retrieval)
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{name: "retrieval", value: value})
	}

	return marshalJSONObject(fields, r.extra)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type messageAlias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	var alias messageAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delete(raw, "role")
	delete(raw, "content")

	m.Role = alias.Role
	m.Content = alias.Content
	m.extra = raw

	return nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	role, err := json.Marshal(m.Role)
	if err != nil {
		return nil, err
	}

	return marshalJSONObject([]jsonField{
		{name: "role", value: role},
		{name: "content", value: rawOrNull(m.Content)},
	}, m.extra)
}

func (m Message) hasField(name string) bool {
	_, ok := m.extra[name]
	return ok
}

type jsonField struct {
	name  string
	value json.RawMessage
}

func marshalJSONObject(fields []jsonField, extra map[string]json.RawMessage) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')

	wroteField := false
	writeField := func(name string, value json.RawMessage) error {
		if wroteField {
			buffer.WriteByte(',')
		}
		encodedName, err := json.Marshal(name)
		if err != nil {
			return err
		}
		buffer.Write(encodedName)
		buffer.WriteByte(':')
		buffer.Write(rawOrNull(value))
		wroteField = true
		return nil
	}

	for _, field := range fields {
		if err := writeField(field.name, field.value); err != nil {
			return nil, err
		}
	}

	if len(extra) > 0 {
		keys := make([]string, 0, len(extra))
		for key := range extra {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := writeField(key, extra[key]); err != nil {
				return nil, err
			}
		}
	}

	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func rawOrNull(value json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(value)) == 0 {
		return json.RawMessage("null")
	}
	return value
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}
