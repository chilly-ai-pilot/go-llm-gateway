package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func main() {
	// 加载配置
	config, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	log.Printf("Gateway 启动中...")
	log.Printf("监听端口: %d", config.Port)
	log.Printf("目标地址: %s", config.TargetURL)

	// 解析目标 URL
	targetURL, err := url.Parse(config.TargetURL)
	if err != nil {
		log.Fatalf("目标 URL 解析失败: %v", err)
	}

	// 创建反向代理
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// 自定义 Director 以添加日志
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		log.Printf("[请求] %s %s -> %s", req.Method, req.URL.Path, req.URL.String())
	}

	// 自定义 ModifyResponse 以记录响应
	proxy.ModifyResponse = func(resp *http.Response) error {
		log.Printf("[响应] %s %s <- 状态码: %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
		return nil
	}

	// 错误处理
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[错误] %s %s - %v", r.Method, r.URL.Path, err)
		http.Error(w, fmt.Sprintf("代理错误: %v", err), http.StatusBadGateway)
	}

	// 创建 HTTP 服务器
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("Gateway 已启动，监听地址: http://localhost:%d", config.Port)
	log.Printf("所有请求将被转发到: %s", config.TargetURL)

	// 启动服务器
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
