package chat

import (
	internalopenai "github.com/rillanai/rillan/internal/openai"
)

type Request = internalopenai.ChatCompletionRequest
type Message = internalopenai.Message
type RetrievalOptions = internalopenai.RetrievalOptions

type ProviderRequest struct {
	Request Request
	RawBody []byte
}

func MessageText(message Message) (string, error) {
	return internalopenai.MessageText(message)
}

func RequiredCapabilities(req Request) []string {
	return internalopenai.RequiredCapabilities(req)
}
