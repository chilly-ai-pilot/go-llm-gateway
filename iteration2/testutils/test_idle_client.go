package main

import (
	"bufio"
	"bytes"
	"log"
	"net/http"
	"time"
)

func main() {
	reqBody := `{"model":"test-idle-timeout","messages":[{"role":"user","content":"test"}],"stream":true}`

	log.Println("========================================")
	log.Println("Idle Timeout 测试客户端")
	log.Println("========================================")
	log.Println("发起流式请求到 Gateway...")
	log.Println("")

	start := time.Now()

	resp, err := http.Post(
		"http://localhost:8080/v1/chat/completions",
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		log.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("收到响应: status=%d", resp.StatusCode)
	log.Println("")

	scanner := bufio.NewScanner(resp.Body)
	chunkCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		elapsed := time.Since(start).Seconds()

		if line == "" {
			// 空行,SSE 事件分隔符
			continue
		}

		log.Printf("[%.0fs] %s", elapsed, line)
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		elapsed := time.Since(start).Seconds()
		log.Printf("[%.0fs] 连接断开: %v", elapsed, err)
	}

	total := time.Since(start).Seconds()

	log.Println("")
	log.Println("========================================")
	log.Printf("测试结果: 收到 %d 个 chunk", chunkCount)
	log.Printf("连接持续时间: %.0f 秒", total)
	log.Println("========================================")

	if total >= 55 && total <= 70 {
		log.Println("✓ IdleTimeout 在预期范围内触发 (55-70秒)")
	} else if total < 10 {
		log.Println("✗ 连接过早断开")
	} else {
		log.Printf("⚠  连接时长 %.0f秒 (期望约60秒)", total)
	}
}
