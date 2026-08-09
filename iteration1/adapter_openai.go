package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// OpenAIAdapter OpenAI 兼容格式适配器
// 适用于所有使用 OpenAI Chat Completions API 格式的 Provider：
// - OpenAI (gpt-3.5-turbo, gpt-4, etc.)
// - DeepSeek (deepseek-chat, deepseek-coder)
// - 其他 OpenAI 兼容的 Provider
type OpenAIAdapter struct{}

// ToProviderRequest 将统一格式转换为 OpenAI 兼容格式
// 由于已经是 OpenAI 格式，主要是参数过滤和标准化
func (a *OpenAIAdapter) ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error) {
	if unifiedReq == nil {
		return nil, errors.New("request cannot be nil")
	}

	// OpenAI 兼容格式，直接序列化
	// 构建请求体，只包含支持的字段
	reqBody := map[string]interface{}{
		"model":    unifiedReq.Model,
		"messages": unifiedReq.Messages,
	}

	// 可选参数
	if unifiedReq.Temperature != nil {
		reqBody["temperature"] = *unifiedReq.Temperature
	}
	if unifiedReq.MaxTokens != nil {
		reqBody["max_tokens"] = *unifiedReq.MaxTokens
	}
	if unifiedReq.TopP != nil {
		reqBody["top_p"] = *unifiedReq.TopP
	}
	if len(unifiedReq.Stop) > 0 {
		reqBody["stop"] = unifiedReq.Stop
	}
	if unifiedReq.FrequencyPenalty != nil {
		reqBody["frequency_penalty"] = *unifiedReq.FrequencyPenalty
	}
	if unifiedReq.PresencePenalty != nil {
		reqBody["presence_penalty"] = *unifiedReq.PresencePenalty
	}
	if unifiedReq.N != nil {
		reqBody["n"] = *unifiedReq.N
	}
	if unifiedReq.User != "" {
		reqBody["user"] = unifiedReq.User
	}

	// 强制设置 stream=false（本迭代不支持流式）
	reqBody["stream"] = false

	return json.Marshal(reqBody)
}

// FromProviderResponse 将 OpenAI 兼容响应转换为统一格式
// OpenAI 格式的响应本身就是标准格式，直接解析即可
func (a *OpenAIAdapter) FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error) {
	if len(providerResp) == 0 {
		return nil, errors.New("empty response from provider")
	}

	var unifiedResp ChatCompletionResponse
	if err := json.Unmarshal(providerResp, &unifiedResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI-compatible response: %w", err)
	}

	// 确保必需字段存在
	if unifiedResp.Object == "" {
		unifiedResp.Object = "chat.completion"
	}

	return &unifiedResp, nil
}

// TransformStreamChunk 流式转换（本迭代占位，Iteration 2 实现）
func (a *OpenAIAdapter) TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error) {
	return nil, errors.New("not implemented: see Iteration 2")
}
