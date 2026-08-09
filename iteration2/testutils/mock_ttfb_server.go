package main

import (
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

		log.Printf("[Mock] 收到请求，挂起 120 秒不返回任何数据...")

		// 关键: 不写任何响应，只是挂起
		time.Sleep(120 * time.Second)

		// 120 秒后才返回 (但此时客户端应该已经超时了)
		log.Printf("[Mock] 恢复，发送响应")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("too late"))
	})

	log.Println("========================================")
	log.Println("Mock TTFB Timeout Server")
	log.Println("========================================")
	log.Println("监听端口: :9998")
	log.Println("端点: /v1/chat/completions")
	log.Println("")
	log.Println("行为:")
	log.Println("1. 接收连接和请求")
	log.Println("2. 挂起 120 秒不返回任何数据 (包括响应头)")
	log.Println("3. 120 秒后返回 (但此时应该已超时)")
	log.Println("")
	log.Println("用于测试 Gateway 的 TTFB Timeout (30秒)")
	log.Println("========================================")
	log.Println("")

	if err := http.ListenAndServe(":9998", nil); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
