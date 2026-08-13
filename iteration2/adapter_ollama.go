package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OllamaAdapter Ollama Provider 适配器
type OllamaAdapter struct{}

// Ollama 原生请求格式
type ollamaRequest struct {
	Model       string                 `json:"model"`
	Prompt      string                 `json:"prompt"`
	Stream      bool                   `json:"stream"`
	Temperature *float64               `json:"temperature,omitempty"`
	NumPredict  *int                   `json:"num_predict,omitempty"` // Ollama 的 max_tokens
	TopP        *float64               `json:"top_p,omitempty"`
	Stop        []string               `json:"stop,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// Ollama 原生响应格式（非流式）
type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
	Context   []int  `json:"context,omitempty"`
	// Token 统计（可能不存在）
	PromptEvalCount      *int `json:"prompt_eval_count,omitempty"`
	EvalCount            *int `json:"eval_count,omitempty"`
	TotalDuration        *int `json:"total_duration,omitempty"`
	LoadDuration         *int `json:"load_duration,omitempty"`
	PromptEvalDuration   *int `json:"prompt_eval_duration,omitempty"`
	EvalDuration         *int `json:"eval_duration,omitempty"`
}

// ToProviderRequest 将 OpenAI 格式转换为 Ollama 格式
func (a *OllamaAdapter) ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error) {
	if unifiedReq == nil {
		return nil, errors.New("request cannot be nil")
	}

	// 将 messages 转换为 prompt
	// 简单策略：拼接所有消息，用换行分隔
	var promptBuilder strings.Builder
	for i, msg := range unifiedReq.Messages {
		if i > 0 {
			promptBuilder.WriteString("\n")
		}
		// 添加角色标识
		switch msg.Role {
		case "system":
			promptBuilder.WriteString("System: ")
		case "user":
			promptBuilder.WriteString("User: ")
		case "assistant":
			promptBuilder.WriteString("Assistant: ")
		}
		promptBuilder.WriteString(msg.Content)
	}

	// 构建 Ollama 请求
	ollamaReq := ollamaRequest{
		Model:       unifiedReq.Model,
		Prompt:      promptBuilder.String(),
		Stream:      unifiedReq.Stream,
		Temperature: unifiedReq.Temperature,
		TopP:        unifiedReq.TopP,
		Stop:        unifiedReq.Stop,
	}

	// MaxTokens 映射为 NumPredict
	if unifiedReq.MaxTokens != nil {
		ollamaReq.NumPredict = unifiedReq.MaxTokens
	}

	return json.Marshal(ollamaReq)
}

// FromProviderResponse 将 Ollama 响应转换为 OpenAI 格式
func (a *OllamaAdapter) FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error) {
	if len(providerResp) == 0 {
		return nil, errors.New("empty response from provider")
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(providerResp, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse ollama response: %w", err)
	}

	// 构建统一的 OpenAI 格式响应
	unifiedResp := &ChatCompletionResponse{
		ID:      generateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ollamaResp.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: ollamaResp.Response,
				},
				FinishReason: "stop", // Ollama 非流式响应都是完成状态
			},
		},
		Usage: Usage{
			PromptTokens:     getTokenCount(ollamaResp.PromptEvalCount),
			CompletionTokens: getTokenCount(ollamaResp.EvalCount),
			TotalTokens:      0, // 后面计算
		},
	}

	// 计算总 token 数
	unifiedResp.Usage.TotalTokens = unifiedResp.Usage.PromptTokens + unifiedResp.Usage.CompletionTokens

	return unifiedResp, nil
}

// TransformStreamChunk 转换 Ollama 流式 chunk 为 OpenAI 格式
func (a *OllamaAdapter) TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error) {
	// Ollama 流式响应格式
	var ollamaChunk struct {
		Model     string `json:"model"`
		CreatedAt string `json:"created_at"`
		Message   struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}

	if err := json.Unmarshal(providerChunk, &ollamaChunk); err != nil {
		return nil, fmt.Errorf("failed to parse ollama chunk: %w", err)
	}

	delta := MessageDelta{}
	if ollamaChunk.Message.Role != "" && !ollamaChunk.Done {
		delta.Role = ollamaChunk.Message.Role
	}
	if ollamaChunk.Message.Content != "" {
		delta.Content = ollamaChunk.Message.Content
	}

	chunk := &ChatCompletionChunk{
		ID:      generateID("chatcmpl"),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   ollamaChunk.Model,
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: delta,
			},
		},
	}

	if ollamaChunk.Done {
		reason := "stop"
		chunk.Choices[0].FinishReason = &reason
		chunk.Choices[0].Delta = MessageDelta{}
	}

	return chunk, nil
}

// 辅助函数：生成唯一 ID
func generateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// 辅助函数：获取 token 数量（处理 nil 指针）
func getTokenCount(count *int) int {
	if count == nil {
		return 0
	}
	return *count
}
