package main

// ProviderAdapter 定义统一的 Provider 适配器接口
// 每个 LLM Provider 实现这个接口来做双向格式转换
type ProviderAdapter interface {
	// ToProviderRequest 将统一的 OpenAI 格式转换为 Provider 原生格式
	// 非流式请求转换，本迭代实现
	ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error)

	// FromProviderResponse 将 Provider 原生响应转换为统一的 OpenAI 格式
	// 非流式响应转换，本迭代实现
	FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error)

	// TransformStreamChunk 将 Provider 流式响应的单个 chunk 转换为统一格式
	// 流式响应转换，本迭代先占位，Iteration 2 补实现
	TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error)
}

// NewAdapter 根据适配器类型创建对应的适配器实例
func NewAdapter(adapterType string) ProviderAdapter {
	switch adapterType {
	case "ollama":
		return &OllamaAdapter{}
	case "openai":
		return &OpenAIAdapter{}
	default:
		return nil
	}
}
