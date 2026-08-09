package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      http.HandlerFunc(gatewayHandler),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("Gateway 已启动，监听地址: http://localhost:%d", config.Port)
	log.Printf("统一入口: POST /v1/chat/completions (OpenAI 格式)")

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

	log.Printf("[路由] model=%s", unifiedReq.Model)

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

	// 4. 转换请求体
	providerReqBody, err := adapter.ToProviderRequest(&unifiedReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "transformation_error", fmt.Sprintf("Failed to transform request: %v", err))
		return
	}

	log.Printf("[转换] 请求已转换为 %s 格式", route.Adapter)

	// 5. 解析目标 URL
	targetURL, err := url.Parse(route.Target)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "config_error", fmt.Sprintf("Invalid target URL: %v", err))
		return
	}

	// 根据适配器类型设置正确的 endpoint
	if route.Adapter == "ollama" {
		// Ollama: /api/generate
		targetURL.Path = "/api/generate"
	} else if route.Adapter == "openai" {
		// OpenAI 兼容: /v1/chat/completions
		targetURL.Path = "/v1/chat/completions"
	}

	// 6. 创建新请求
	proxyReq, err := http.NewRequest(http.MethodPost, targetURL.String(), bytes.NewReader(providerReqBody))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy_error", fmt.Sprintf("Failed to create proxy request: %v", err))
		return
	}

	// 复制原始请求头
	proxyReq.Header = r.Header.Clone()
	proxyReq.Header.Set("Content-Type", "application/json")

	// 注入 API Key（如果配置了）
	if route.APIKeyEnv != "" {
		apiKey := GetAPIKey(route.APIKeyEnv)
		if apiKey != "" {
			proxyReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
			log.Printf("[认证] 已注入 API Key (来自环境变量 %s)", route.APIKeyEnv)
		} else {
			log.Printf("[警告] 配置了 api_key_env=%s 但环境变量未设置", route.APIKeyEnv)
		}
	}

	// 7. 创建 ReverseProxy 并配置响应转换
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Director 已经由 proxyReq 处理，这里留空
		},
		ModifyResponse: func(resp *http.Response) error {
			// 读取 Provider 响应
			respBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to read provider response: %w", err)
			}

			log.Printf("[响应] 收到 Provider 响应: %d bytes, status=%d", len(respBody), resp.StatusCode)

			// 如果 Provider 返回错误状态码，尝试解析错误并透传
			if resp.StatusCode >= 400 {
				log.Printf("[错误] Provider 返回错误: %s", string(respBody))
				// 直接透传 Provider 的错误响应
				resp.Body = io.NopCloser(bytes.NewReader(respBody))
				return nil
			}

			// 转换响应体
			unifiedResp, err := adapter.FromProviderResponse(respBody)
			if err != nil {
				log.Printf("[错误] 响应转换失败: %v", err)
				return fmt.Errorf("failed to transform response: %w", err)
			}

			log.Printf("[转换] 响应已转换为 OpenAI 格式")

			// 序列化统一格式响应
			unifiedRespBody, err := json.Marshal(unifiedResp)
			if err != nil {
				return fmt.Errorf("failed to marshal unified response: %w", err)
			}

			// 替换响应体
			resp.Body = io.NopCloser(bytes.NewReader(unifiedRespBody))
			resp.ContentLength = int64(len(unifiedRespBody))
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(unifiedRespBody)))
			resp.Header.Set("Content-Type", "application/json")

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[错误] 代理请求失败: %v", err)
			writeJSONError(w, http.StatusBadGateway, "provider_error", fmt.Sprintf("Failed to communicate with provider: %v", err))
		},
	}

	// 8. 执行代理转发
	// 注意：这里不能直接用 proxy.ServeHTTP(w, r)，因为 r 的 Body 已经被读取
	// 需要用我们构造的 proxyReq
	proxy.ServeHTTP(w, proxyReq)

	log.Printf("[完成] 请求处理完成")
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
