package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// handleStreamingRequest 处理流式请求
// 逐 chunk 转发,支持客户端断连传播和 TTFB 超时
func handleStreamingRequest(
	w http.ResponseWriter,
	r *http.Request,
	unifiedReq *ChatCompletionRequest,
	route *Route,
	adapter ProviderAdapter,
) {
	log.Printf("[流式] 处理流式请求")

	// 1. 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 禁用 nginx 缓冲
	w.WriteHeader(http.StatusOK)

	// 获取 Flusher(立即发送数据的接口)
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[错误] 响应不支持 Flusher")
		writeStreamError(w, "streaming_not_supported", "Streaming not supported")
		return
	}

	// 2. 转换请求体
	providerReqBody, err := adapter.ToProviderRequest(unifiedReq)
	if err != nil {
		log.Printf("[错误] 请求转换失败: %v", err)
		writeStreamError(w, "transformation_error", err.Error())
		return
	}

	log.Printf("[转换] 请求已转换为 %s 格式", route.Adapter)

	// 3. 解析目标 URL
	targetURL, err := url.Parse(route.Target)
	if err != nil {
		log.Printf("[错误] 目标 URL 解析失败: %v", err)
		writeStreamError(w, "config_error", err.Error())
		return
	}

	// 根据适配器类型设置正确的 endpoint
	if route.Adapter == "ollama" {
		targetURL.Path = "/api/chat"
	} else if route.Adapter == "openai" {
		targetURL.Path = "/v1/chat/completions"
	}

	// 4. 获取原始 context(用于断连传播)
	ctx := r.Context()

	// 创建上游请求(使用原始 context)
	// TTFB 超时通过 http.Transport.ResponseHeaderTimeout 实现
	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL.String(), bytes.NewReader(providerReqBody))
	if err != nil {
		log.Printf("[错误] 创建上游请求失败: %v", err)
		writeStreamError(w, "request_error", err.Error())
		return
	}

	// 复制请求头
	upstreamReq.Header = r.Header.Clone()
	upstreamReq.Header.Set("Content-Type", "application/json")

	// 注入 API Key
	if route.APIKeyEnv != "" {
		apiKey := GetAPIKey(route.APIKeyEnv)
		if apiKey != "" {
			upstreamReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
			log.Printf("[认证] 已注入 API Key")
		}
	}

	// 5. 创建 HTTP Client(设置连接超时和 TTFB 超时)
	// 先声明一个变量来捕获底层连接
	var capturedConn net.Conn
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				d := &net.Dialer{
					Timeout: time.Duration(config.Timeouts.DialTimeout) * time.Second,
				}
				rawConn, err := d.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				// 用 tracker 包装，捕获原始连接
				tracker := &connTracker{Conn: rawConn, conn: &capturedConn}
				capturedConn = rawConn
				return tracker, nil
			},
			ResponseHeaderTimeout: time.Duration(config.Timeouts.TTFBTimeout) * time.Second, // TTFB 超时
		},
	}

	// 6. 发起上游请求
	log.Printf("[上游] 发起流式请求到 %s", targetURL.String())
	resp, err := client.Do(upstreamReq)
	if err != nil {
		log.Printf("[错误] 上游请求失败: %v", err)
		// 判断是否是 TTFB 超时
		if ctx.Err() == context.DeadlineExceeded {
			writeStreamError(w, "ttfb_timeout", "Provider did not respond in time")
		} else {
			writeStreamError(w, "provider_error", err.Error())
		}
		return
	}
	defer resp.Body.Close()

	log.Printf("[上游] 收到响应: status=%d", resp.StatusCode)

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		log.Printf("[错误] 上游返回错误状态: %d", resp.StatusCode)
		// 读取错误响应
		errBody, _ := bufio.NewReader(resp.Body).ReadString('\n')
		writeStreamError(w, "provider_error", fmt.Sprintf("Provider returned %d: %s", resp.StatusCode, errBody))
		return
	}

	// 7. 创建带 Idle 超时的 Reader
	idleTimeout := time.Duration(config.Timeouts.IdleTimeout) * time.Second
	// 直接使用捕获到的底层连接来设置超时
	deadlineReader := &idleTimeoutReader{
		reader:      resp.Body,
		idleTimeout: idleTimeout,
		conn:        capturedConn, // 传入捕获的底层连接
	}

	// 逐 chunk 转发
	scanner := bufio.NewScanner(deadlineReader)
	chunkCount := 0

	for scanner.Scan() {
		// 检测客户端断连
		select {
		case <-ctx.Done():
			log.Printf("[断连] 客户端断开连接,停止转发")
			return
		default:
		}

		line := scanner.Text()

		// SSE 格式: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// 提取 JSON 部分
		jsonData := strings.TrimPrefix(line, "data: ")

		// 特殊处理: [DONE] 信号
		if jsonData == "[DONE]" {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			log.Printf("[流式] 收到 [DONE] 信号,流结束")
			break
		}

		// 调用适配器转换 chunk
		unifiedChunk, err := adapter.TransformStreamChunk([]byte(jsonData))
		if err != nil {
			log.Printf("[错误] chunk 转换失败: %v, 原始数据: %s", err, jsonData)
			continue
		}

		// 序列化并写入
		chunkJSON, err := json.Marshal(unifiedChunk)
		if err != nil {
			log.Printf("[错误] chunk 序列化失败: %v", err)
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", chunkJSON)

		// 立即发送(关键!)
		flusher.Flush()

		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[错误] 流式读取失败: %v", err)
	}

	log.Printf("[完成] 流式请求处理完成,共转发 %d 个 chunk", chunkCount)
}

// writeStreamError 在流式响应中写入错误
// 注意: 流式场景下 HTTP 状态码已经返回 200,只能通过 SSE 发送错误 chunk
func writeStreamError(w http.ResponseWriter, errorType, message string) {
	errorChunk := map[string]interface{}{
		"error": map[string]string{
			"type":    errorType,
			"message": message,
		},
	}
	errorJSON, _ := json.Marshal(errorChunk)
	fmt.Fprintf(w, "data: %s\n\n", errorJSON)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// idleTimeoutReader 包装 io.Reader,在每次读取前设置超时
// 用于检测流式响应中 chunk 间隔过长的情况
type idleTimeoutReader struct {
	reader      io.Reader
	idleTimeout time.Duration
	conn        net.Conn // 底层的 net.Conn,用于设置读超时
}

// Read 实现 io.Reader 接口
// 在每次读取前刷新读取截止时间
func (r *idleTimeoutReader) Read(p []byte) (n int, err error) {
	if r.conn != nil {
		deadline := time.Now().Add(r.idleTimeout)
		// 直接在捕获到的 net.Conn 上设置读超时
		if err := r.conn.SetReadDeadline(deadline); err != nil {
			log.Printf("[超时] 设置读取截止时间失败: %v", err)
		}
	}

	return r.reader.Read(p)
}
