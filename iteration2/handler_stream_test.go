package main

import "testing"

func TestExtractStreamPayload(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "sse", input: "data: {\"model\":\"llama3\"}\n\n", want: "{\"model\":\"llama3\"}"},
		{name: "ndjson", input: "{\"model\":\"llama3\",\"message\":{\"role\":\"assistant\",\"content\":\"你好\"},\"done\":false}\n", want: "{\"model\":\"llama3\",\"message\":{\"role\":\"assistant\",\"content\":\"你好\"},\"done\":false}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractStreamPayload(tt.input)
			if !ok {
				t.Fatalf("expected payload to be extracted from %q", tt.input)
			}
			if got != tt.want {
				t.Fatalf("extractStreamPayload(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOllamaTransformStreamChunk(t *testing.T) {
	adapter := &OllamaAdapter{}

	contentChunk, err := adapter.TransformStreamChunk([]byte("{\"model\":\"llama3.2:latest\",\"message\":{\"role\":\"assistant\",\"content\":\"你好\"},\"done\":false}"))
	if err != nil {
		t.Fatalf("TransformStreamChunk(content) error: %v", err)
	}
	if contentChunk.Choices[0].Delta.Content != "你好" {
		t.Fatalf("content chunk should carry content, got %+v", contentChunk.Choices[0].Delta)
	}
	if contentChunk.Choices[0].FinishReason != nil {
		t.Fatalf("content chunk should not end with finish_reason, got %+v", contentChunk.Choices[0].FinishReason)
	}

	finalChunk, err := adapter.TransformStreamChunk([]byte("{\"model\":\"llama3.2:latest\",\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true}"))
	if err != nil {
		t.Fatalf("TransformStreamChunk(final) error: %v", err)
	}
	if finalChunk.Choices[0].Delta.Role != "" || finalChunk.Choices[0].Delta.Content != "" {
		t.Fatalf("final stop chunk should be empty delta, got %+v", finalChunk.Choices[0].Delta)
	}
	if finalChunk.Choices[0].FinishReason == nil || *finalChunk.Choices[0].FinishReason != "stop" {
		t.Fatalf("final stop chunk should set finish_reason=stop, got %+v", finalChunk.Choices[0].FinishReason)
	}
}

func TestOllamaToProviderRequestUsesChatMessagesInStreamMode(t *testing.T) {
	adapter := &OllamaAdapter{}
	unifiedReq := &ChatCompletionRequest{
		Model:  "llama3.2:latest",
		Stream: true,
		Messages: []Message{
			{Role: "user", Content: "你好"},
		},
	}

	body, err := adapter.ToProviderRequest(unifiedReq)
	if err != nil {
		t.Fatalf("ToProviderRequest error: %v", err)
	}

	if string(body) == "" || len(body) == 0 {
		t.Fatal("body should not be empty")
	}

	if !containsString(string(body), "\"messages\":[") {
		t.Fatalf("streaming Ollama request should use messages field, got %s", body)
	}
	if containsString(string(body), "\"prompt\":") {
		t.Fatalf("streaming Ollama request should not send prompt field, got %s", body)
	}
}

func containsString(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})())
}
