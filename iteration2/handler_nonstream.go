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
)

// handleNonStreamingRequest 处理非流式请求
// 使用 ReverseProxy 的 ModifyResponse 进行响应转换
func handleNonStreamingRequest(
	w http.ResponseWriter,
	r *http.Request,
	unifiedReq *ChatCompletionRequest,
	route *Route,
	adapter ProviderAdapter,
) {
	log.Printf("[非流式] 处理非流式请求")

	// 1. 转换请求体
	providerReqBody, err := adapter.ToProviderRequest(unifiedReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "transformation_error", fmt.Sprintf("Failed to transform request: %v", err))
		return
	}

	log.Printf("[转换] 请求已转换为 %s 格式", route.Adapter)

	// 2. 解析目标 URL
	targetURL, err := url.Parse(route.Target)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "config_error", fmt.Sprintf("Invalid target URL: %v", err))
		return
	}

	// 根据适配器类型设置正确的 endpoint
	if route.Adapter == "ollama" {
		targetURL.Path = "/api/generate"
	} else if route.Adapter == "openai" {
		targetURL.Path = "/v1/chat/completions"
	}

	// 3. 创建代理请求
	proxyReq, err := http.NewRequest(http.MethodPost, targetURL.String(), bytes.NewReader(providerReqBody))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy_error", fmt.Sprintf("Failed to create proxy request: %v", err))
		return
	}

	// 复制原始请求头
	proxyReq.Header = r.Header.Clone()
	proxyReq.Header.Set("Content-Type", "application/json")

	// 注入 API Key
	if route.APIKeyEnv != "" {
		apiKey := GetAPIKey(route.APIKeyEnv)
		if apiKey != "" {
			proxyReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
			log.Printf("[认证] 已注入 API Key (来自环境变量 %s)", route.APIKeyEnv)
		} else {
			log.Printf("[警告] 配置了 api_key_env=%s 但环境变量未设置", route.APIKeyEnv)
		}
	}

	// 4. 创建 ReverseProxy 并配置响应转换
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

	// 5. 执行代理转发
	proxy.ServeHTTP(w, proxyReq)

	log.Printf("[完成] 非流式请求处理完成")
}
