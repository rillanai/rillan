// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/rillanai/rillan/internal/ollama"
)

// QueryRewriter transforms a raw user query before embedding.
type QueryRewriter interface {
	Rewrite(ctx context.Context, query string) (string, error)
}

const rewritePrompt = `Rewrite the following user query into a concise search query optimized for semantic similarity search over a code repository. Return only the rewritten query, nothing else.

User query:
%s`

// OllamaQueryRewriter uses a local model to rewrite queries for better retrieval.
type OllamaQueryRewriter struct {
	client *ollama.Client
	model  string
}

// NewOllamaQueryRewriter creates a rewriter backed by an Ollama instance.
func NewOllamaQueryRewriter(client *ollama.Client, model string) *OllamaQueryRewriter {
	return &OllamaQueryRewriter{client: client, model: model}
}

func (r *OllamaQueryRewriter) Rewrite(ctx context.Context, query string) (string, error) {
	prompt := fmt.Sprintf(rewritePrompt, query)
	result, err := r.client.Generate(ctx, r.model, prompt)
	if err != nil {
		return "", fmt.Errorf("rewrite query: %w", err)
	}

	rewritten := strings.TrimSpace(result)
	if rewritten == "" {
		return query, nil
	}

	return rewritten, nil
}
