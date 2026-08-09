package main

// OpenAI Chat Completions 统一 Schema
// 这是 Gateway 对外暴露的唯一格式

// ChatCompletionRequest OpenAI 格式的请求
type ChatCompletionRequest struct {
	Model            string                 `json:"model"`
	Messages         []Message              `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	Stream           *bool                  `json:"stream,omitempty"`
	Stop             []string               `json:"stop,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	User             string                 `json:"user,omitempty"`
	N                *int                   `json:"n,omitempty"`
	Extra            map[string]interface{} `json:"-"` // 扩展字段
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"` // 消息内容
}

// ChatCompletionResponse OpenAI 格式的响应
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"` // "chat.completion"
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 响应选项
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // stop, length, content_filter, null
}

// Usage Token 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk 流式响应的单个 chunk（Iteration 2 使用）
// 本迭代先定义，不实现
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"` // "chat.completion.chunk"
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice 流式响应的选项
type ChunkChoice struct {
	Index        int           `json:"index"`
	Delta        MessageDelta  `json:"delta"`
	FinishReason *string       `json:"finish_reason"` // 可能为 null
}

// MessageDelta 流式响应的增量消息
type MessageDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ErrorResponse 统一错误响应格式
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 错误详情
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}
