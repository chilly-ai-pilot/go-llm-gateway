package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var (
	config *Config
)

func main() {
	// 加载配置
	var err error
	config, err = LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	log.Printf("Gateway 启动中...")
	log.Printf("监听端口: %d", config.Port)
	log.Printf("已配置 %d 个路由:", len(config.Routes))
	for _, route := range config.Routes {
		log.Printf("  - %s -> %s (%s adapter)", route.ModelPattern, route.Target, route.Adapter)
	}
	log.Printf("")

	// 创建 HTTP 服务器
	server := &http.Server{
		Addr:        fmt.Sprintf(":%d", config.Port),
		Handler:     http.HandlerFunc(gatewayHandler),
		IdleTimeout: time.Duration(config.Timeouts.IdleTimeout) * time.Second,
		// 不设置 ReadTimeout/WriteTimeout(会掐断长流式连接)
	}

	log.Printf("Gateway 已启动，监听地址: http://localhost:%d", config.Port)
	log.Printf("统一入口: POST /v1/chat/completions (OpenAI 格式)")
	log.Printf("超时配置: Dial=%ds, TTFB=%ds, Idle=%ds",
		config.Timeouts.DialTimeout,
		config.Timeouts.TTFBTimeout,
		config.Timeouts.IdleTimeout)

	// 启动服务器
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// gatewayHandler Gateway 核心处理函数
func gatewayHandler(w http.ResponseWriter, r *http.Request) {
	// 只接受 POST 请求
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST method is allowed")
		return
	}

	// 记录请求
	log.Printf("[请求] %s %s", r.Method, r.URL.Path)

	// 1. 读取并解析 OpenAI 格式请求
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var unifiedReq ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &unifiedReq); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("Failed to parse request: %v", err))
		return
	}

	// 验证必需字段
	if unifiedReq.Model == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "model field is required")
		return
	}
	if len(unifiedReq.Messages) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "messages field cannot be empty")
		return
	}

	log.Printf("[路由] model=%s, stream=%v", unifiedReq.Model, unifiedReq.Stream)

	// 2. 根据 model 匹配路由
	route, err := MatchRoute(unifiedReq.Model, config.Routes)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "model_not_found", err.Error())
		return
	}

	log.Printf("[匹配] 路由匹配成功: %s -> %s (%s)", route.ModelPattern, route.Target, route.Adapter)

	// 3. 获取适配器
	adapter := NewAdapter(route.Adapter)
	if adapter == nil {
		writeJSONError(w, http.StatusInternalServerError, "adapter_error", fmt.Sprintf("Unknown adapter type: %s", route.Adapter))
		return
	}

	// 4. 根据 stream 字段分流
	if unifiedReq.Stream {
		handleStreamingRequest(w, r, &unifiedReq, route, adapter)
		return
	}

	// 5. 非流式走 Iteration 1 的逻辑
	handleNonStreamingRequest(w, r, &unifiedReq, route, adapter)
}

// writeJSONError 写入 JSON 格式的错误响应
func writeJSONError(w http.ResponseWriter, statusCode int, errorType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errResp := ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errorType,
			Code:    fmt.Sprintf("%d", statusCode),
		},
	}

	json.NewEncoder(w).Encode(errResp)
	log.Printf("[错误响应] %d - %s: %s", statusCode, errorType, message)
}
