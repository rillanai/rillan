package stdio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

func TestClientChatCompletionsRunsCommandAndReturnsSyntheticResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}

	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeExecutableScript(t, `#!/bin/sh
set -eu
cat > "$REQUEST_PATH"
printf '%s' '{"status_code":200,"headers":{"Content-Type":["application/json"]},"body":{"id":"resp_123"}}'
`)
	t.Setenv("REQUEST_PATH", requestPath)

	client := New([]string{script})
	response, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{Model: "demo-model"}, []byte(`{"model":"demo-model"}`))
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()

	if got, want := response.StatusCode, 200; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := string(body), `{"id":"resp_123"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	requestData, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got, want := string(requestData), `{"request":{"model":"demo-model","messages":null},"raw_body":{"model":"demo-model"}}`; got != want {
		t.Fatalf("request payload = %s, want %s", got, want)
	}
}

func TestClientChatCompletionsReturnsStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}

	script := writeExecutableScript(t, "#!/bin/sh\necho boom >&2\nexit 3\n")
	client := New([]string{script})

	if _, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{Model: "demo-model"}, []byte(`{"model":"demo-model"}`)); err == nil {
		t.Fatal("expected ChatCompletions to fail")
	}
}

func TestClientReadyRejectsMissingCommand(t *testing.T) {
	client := New([]string{"definitely-missing-rillan-stdio-provider"})
	if err := client.Ready(context.Background()); err == nil {
		t.Fatal("expected Ready to fail for missing command")
	}
}

func TestClientChatCompletionsRejectsInvalidStatusCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}

	script := writeExecutableScript(t, `#!/bin/sh
printf '%s' '{"status_code":42,"body":{"id":"resp_123"}}'
`)
	client := New([]string{script})

	if _, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{Model: "demo-model"}, []byte(`{"model":"demo-model"}`)); err == nil {
		t.Fatal("expected ChatCompletions to reject invalid status_code")
	}
}

func writeExecutableScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}
