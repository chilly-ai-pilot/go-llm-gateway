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
