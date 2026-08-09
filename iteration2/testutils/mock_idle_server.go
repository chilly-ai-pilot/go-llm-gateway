package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// 读取并丢弃请求体
		io.Copy(io.Discard, r.Body)
		r.Body.Close()

		log.Printf("[Mock] 收到请求，准备发送第一个 chunk...")

		// 设置 SSE 响应头
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		// 发送第一个 chunk
		chunk1 := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"第一个chunk"},"finish_reason":null}]}` + "\n\n"
		fmt.Fprint(w, chunk1)

		// 立即 flush
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
			log.Printf("[Mock] 第一个 chunk 已发送并 flush")
		} else {
			log.Printf("[Mock] 警告: Flusher 不可用")
		}

		// 挂起 120 秒，不发送更多 chunk
		log.Printf("[Mock] 挂起 120 秒，模拟 idle timeout...")
		time.Sleep(120 * time.Second)

		// 恢复后发送结束信号
		log.Printf("[Mock] 恢复，发送 [DONE]")
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		log.Printf("[Mock] 请求处理完成")
	})

	log.Println("========================================")
	log.Println("Mock Idle Timeout Server")
	log.Println("========================================")
	log.Println("监听端口: :9997")
	log.Println("端点: /v1/chat/completions")
	log.Println("")
	log.Println("行为:")
	log.Println("1. 立即返回第一个 chunk")
	log.Println("2. 挂起 120 秒不发送更多数据")
	log.Println("3. 恢复后发送 [DONE]")
	log.Println("")
	log.Println("用于测试 Gateway 的 IdleTimeout (60秒)")
	log.Println("========================================")
	log.Println("")

	if err := http.ListenAndServe(":9997", nil); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
