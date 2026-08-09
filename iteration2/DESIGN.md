# Iteration 2 技术设计方案

## 目标
实现流式响应支持,包括:流式转发、客户端断连传播、超时分层设计

## 核心架构设计

### 1. 请求分流:流式 vs 非流式

在 `gatewayHandler` 里先完成公共的前置处理(解析、路由、适配器选择),然后根据请求体的 `stream` 字段分流:

```go
func gatewayHandler(w http.ResponseWriter, r *http.Request) {
    // 1. 读取并解析请求体
    var unifiedReq ChatCompletionRequest
    if err := json.NewDecoder(r.Body).Decode(&unifiedReq); err != nil {
        writeJSONError(w, 400, "invalid_request", err.Error())
        return
    }
    
    // 2. 路由匹配
    route, err := MatchRoute(unifiedReq.Model, config.Routes)
    if err != nil {
        writeJSONError(w, 404, "model_not_found", err.Error())
        return
    }
    
    // 3. 获取适配器
    adapter := NewAdapter(route.Adapter)
    if adapter == nil {
        writeJSONError(w, 500, "adapter_error", "Unknown adapter")
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
```

**为什么这样设计:**
- 前置处理(解析、路由、适配器选择)两种模式都需要,放在分流前避免重复代码
- 分流点明确:检查 `stream` 字段后,流式直接 `return`,两个处理函数完全独立
- 非流式保持 Iteration 1 的逻辑不变(用 ReverseProxy 的 ModifyResponse)
- 流式需要手动逐 chunk 转发,不能用 ModifyResponse(它要等响应全部读完才执行)

---

### 2. 流式转发核心流程

```
客户端请求(stream=true) 
  ↓ 解析并路由
  ↓ 适配器转换请求体
  ↓ 创建上游请求(复用 r.Context() 实现断连传播)
  ↓ 设置分层超时(DialTimeout + TTFB + IdleTimeout)
  ↓ 发起上游请求
  ↓ 收到响应后,设置 SSE 响应头
  ↓ 创建 bufio.Scanner 按行读取上游响应
  ↓ 对每一行:
      ├ 解析 SSE 格式(data: {...})
      ├ 调用 adapter.TransformStreamChunk() 转换为 OpenAI 格式
      ├ 写入到客户端(data: {...}\n\n)
      ├ Flush 立即发送(关键!)
      └ 检测 context.Done() 处理客户端断连
  ↓ 流结束后发送 [DONE] 信号
```

---

### 3. 客户端断连传播机制

**实现原理:**

```go
func handleStreamingRequest(w http.ResponseWriter, r *http.Request, ...) {
    // 1. 复用原始请求的 context
    ctx := r.Context()
    
    // 2. 用这个 context 创建上游请求
    upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, body)
    
    // 3. 在流式转发循环中检测取消信号
    for scanner.Scan() {
        select {
        case <-ctx.Done():
            // 客户端断连,context 被取消
            log.Printf("[断连] 客户端断开连接,停止转发")
            return
        default:
            // 继续处理 chunk
        }
    }
}
```

**生效路径:**
- 客户端关闭连接(Ctrl+C / 网络断开) → `r.Context()` 被取消
- 上游请求的 `upstreamReq.Context()` 也被取消(因为用的同一个 context)
- 上游 HTTP 连接的底层 `Read()` 操作会立即返回 `context canceled` 错误
- Provider 端的请求被终止,停止生成

---

### 4. 超时分层设计

#### 4.1 三层超时的实现

```go
// 配置结构(config.yaml)
type TimeoutConfig struct {
    DialTimeout  int `yaml:"dial_timeout"`   // 连接超时,默认 5s
    TTFBTimeout  int `yaml:"ttfb_timeout"`   // 首字节超时,默认 30s
    IdleTimeout  int `yaml:"idle_timeout"`   // chunk 间隔超时,默认 60s
}

// HTTP Server 配置(用 IdleTimeout 替代自定义 deadlineReader)
func main() {
    server := &http.Server{
        Addr:        fmt.Sprintf(":%d", config.Port),
        Handler:     http.HandlerFunc(gatewayHandler),
        IdleTimeout: time.Duration(config.Timeouts.IdleTimeout) * time.Second,
        // 不设置 ReadTimeout/WriteTimeout(会掐断长流式连接)
    }
}

// HTTP Client 配置(连接超时)
func createHTTPClient(cfg TimeoutConfig) *http.Client {
    return &http.Client{
        Transport: &http.Transport{
            DialContext: (&net.Dialer{
                Timeout: time.Duration(cfg.DialTimeout) * time.Second,
            }).DialContext,
        },
        // 不设置全局 Timeout(它会包括整个流式响应时间)
    }
}

// TTFB 超时(首字节超时)
func handleStreamingRequest(...) {
    // 创建带超时的 context
    ttfbCtx, cancel := context.WithTimeout(r.Context(), ttfbTimeout)
    defer cancel()
    
    // 发起请求
    resp, err := client.Do(upstreamReq)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            // TTFB 超时
            writeStreamError(w, "ttfb_timeout", "Provider did not respond in time")
            return
        }
    }
    
    // 收到第一个字节后,取消 TTFB 超时
    // Idle 超时由 http.Server.IdleTimeout 自动处理
    cancel()
}
```

**为什么用 `http.Server.IdleTimeout` 而不是自定义 `deadlineReader`:**
- `IdleTimeout` 是 Go 标准库自带的,专门处理连接空闲超时
- 语义和"chunk 间空闲超时"完全一致
- 零额外代码,避免封装自定义 Reader
- 对学习项目完全够用(如果后续需要更精细的控制,可以再补 `deadlineReader`)

#### 4.2 为什么不用 http.Server 的 ReadTimeout/WriteTimeout?

```go
// Iteration 1 的配置
server := &http.Server{
    ReadTimeout:  30 * time.Second,  // ❌ 会掐断长流式连接
    WriteTimeout: 30 * time.Second,  // ❌ 会掐断长流式连接
}

// Iteration 2 的配置
server := &http.Server{
    IdleTimeout: 60 * time.Second,   // ✅ 只控制 chunk 间空闲,不限制总时长
    // 不设置 ReadTimeout/WriteTimeout
}
```

**原因:** 
- `WriteTimeout` 是从请求头读取完毕到响应写完的总时间。流式响应可能持续几分钟,会被这个超时掐断
- `IdleTimeout` 只在连接空闲(没有任何数据往来)时触发,不限制总响应时长,完美适配流式场景

---

### 5. 适配器流式接口实现

#### 5.1 接口已在 Iteration 1 定义(占位)

```go
type ProviderAdapter interface {
    ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error)
    FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error)
    TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error) // 本迭代补实现
}
```

#### 5.2 Ollama 适配器的流式实现

```go
// Ollama 的流式响应格式
type OllamaStreamChunk struct {
    Model     string `json:"model"`
    CreatedAt string `json:"created_at"`
    Message   struct {
        Role    string `json:"role"`
        Content string `json:"content"`
    } `json:"message"`
    Done bool `json:"done"`
}

func (a *OllamaAdapter) TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error) {
    var ollamaChunk OllamaStreamChunk
    if err := json.Unmarshal(providerChunk, &ollamaChunk); err != nil {
        return nil, err
    }
    
    // 转换为 OpenAI 格式
    return &ChatCompletionChunk{
        ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
        Object:  "chat.completion.chunk",
        Created: time.Now().Unix(),
        Model:   ollamaChunk.Model,
        Choices: []ChunkChoice{
            {
                Index: 0,
                Delta: DeltaContent{
                    Role:    ollamaChunk.Message.Role,
                    Content: ollamaChunk.Message.Content,
                },
                FinishReason: func() *string {
                    if ollamaChunk.Done {
                        reason := "stop"
                        return &reason
                    }
                    return nil
                }(),
            },
        },
    }, nil
}
```

#### 5.3 OpenAI 适配器的流式实现

```go
func (a *OpenAIAdapter) TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error) {
    // OpenAI 格式直接透传,不需要转换
    var chunk ChatCompletionChunk
    if err := json.Unmarshal(providerChunk, &chunk); err != nil {
        return nil, err
    }
    return &chunk, nil
}
```

---

### 6. SSE 格式处理

#### 6.1 SSE 响应头设置

```go
func handleStreamingRequest(w http.ResponseWriter, ...) {
    // 设置 SSE 必需的响应头
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no") // 禁用 nginx 缓冲
    w.WriteHeader(http.StatusOK)
    
    // 获取 Flusher(立即发送数据的接口)
    flusher, ok := w.(http.Flusher)
    if !ok {
        writeJSONError(w, 500, "streaming_not_supported", "Streaming not supported")
        return
    }
}
```

#### 6.2 SSE 数据格式解析和写入

```go
// 读取上游响应
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
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
        break
    }
    
    // 调用适配器转换
    unifiedChunk, err := adapter.TransformStreamChunk([]byte(jsonData))
    if err != nil {
        log.Printf("[错误] chunk 转换失败: %v", err)
        continue
    }
    
    // 序列化并写入
    chunkJSON, _ := json.Marshal(unifiedChunk)
    fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
    
    // 立即发送(关键!)
    flusher.Flush()
}
```

---

### 7. 配置文件扩展

```yaml
# config.yaml
port: 8080

# 超时配置(新增)
timeouts:
  dial_timeout: 5      # 连接超时(秒)
  ttfb_timeout: 30     # 首字节超时(秒)
  idle_timeout: 60     # chunk 间隔超时(秒)

routes:
  - model_pattern: "llama*"
    target: "http://localhost:11434"
    adapter: "ollama"
  - model_pattern: "deepseek-*"
    target: "https://api.deepseek.com"
    adapter: "openai"
    api_key_env: "DEEPSEEK_API_KEY"
```

---

## 文件结构

```
iteration2/
├── main.go                # 主入口 + 请求分流逻辑
├── handler_stream.go      # 流式请求处理(新增)
├── handler_nonstream.go   # 非流式请求处理(从 main.go 拆分)
├── adapter.go             # 适配器接口(不变)
├── adapter_ollama.go      # Ollama 适配器 + 流式实现
├── adapter_openai.go      # OpenAI 适配器 + 流式实现
├── router.go              # 路由逻辑(不变)
├── schema.go              # Schema 定义 + 新增 ChatCompletionChunk
├── config.go              # 配置加载 + 新增 TimeoutConfig
├── config.yaml            # 配置文件 + 新增 timeouts 段
├── test.sh                # 验收测试脚本
├── go.mod
├── go.sum
└── README.md              # 使用文档
```

**文件变化说明:**
- 去掉了 `timeout.go`(用 `http.Server.IdleTimeout` 替代自定义 `deadlineReader`)
- `handler_stream.go` 和 `handler_nonstream.go` 是新增文件,从 `main.go` 拆分出来

---

## 关键代码片段

### handler_stream.go 核心逻辑

```go
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
    w.Header().Set("X-Accel-Buffering", "no")
    w.WriteHeader(http.StatusOK)
    
    flusher, ok := w.(http.Flusher)
    if !ok {
        writeJSONError(w, 500, "streaming_not_supported", "Streaming not supported")
        return
    }
    
    // 2. 转换请求体
    providerReqBody, err := adapter.ToProviderRequest(unifiedReq)
    if err != nil {
        writeStreamError(w, "transformation_error", err.Error())
        return
    }
    
    // 3. 创建上游请求(复用 context 实现断连传播)
    ctx := r.Context()
    upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, route.Target, bytes.NewReader(providerReqBody))
    if err != nil {
        writeStreamError(w, "request_error", err.Error())
        return
    }
    
    // 4. 发起请求(带 TTFB 超时)
    client := createStreamingHTTPClient()
    resp, err := client.Do(upstreamReq)
    if err != nil {
        writeStreamError(w, "provider_error", err.Error())
        return
    }
    defer resp.Body.Close()
    
    // 5. 逐 chunk 转发
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        // 检测客户端断连
        select {
        case <-ctx.Done():
            log.Printf("[断连] 客户端断开,停止转发")
            return
        default:
        }
        
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") {
            continue
        }
        
        jsonData := strings.TrimPrefix(line, "data: ")
        if jsonData == "[DONE]" {
            fmt.Fprintf(w, "data: [DONE]\n\n")
            flusher.Flush()
            break
        }
        
        // 转换 chunk
        unifiedChunk, err := adapter.TransformStreamChunk([]byte(jsonData))
        if err != nil {
            log.Printf("[错误] chunk 转换失败: %v", err)
            continue
        }
        
        // 写入并立即发送
        chunkJSON, _ := json.Marshal(unifiedChunk)
        fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
        flusher.Flush()
    }
    
    if err := scanner.Err(); err != nil {
        log.Printf("[错误] 流式读取失败: %v", err)
    }
}
```

---

## 验收测试设计

### test.sh 脚本内容

```bash
#!/bin/bash

echo "=== Iteration 2 验收测试 ==="
echo ""

# 测试 1: 流式转发实时性
echo "测试 1: 流式转发实时性"
echo "验证 chunk 不被缓冲,逐行实时返回"
time curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "messages": [{"role": "user", "content": "用5句话介绍人工智能"}],
    "stream": true
  }' | while IFS= read -r line; do
    echo "[$(date +%H:%M:%S.%3N)] $line"
  done

echo ""
echo "期望:每个 chunk 的时间戳应该有间隔,不是一次性全部返回"
echo ""
read -p "按回车继续下一个测试..."

# 测试 2: 客户端断连传播
echo ""
echo "测试 2: 客户端断连传播"
echo "启动流式请求,3秒后主动断开"
timeout 3s curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "messages": [{"role": "user", "content": "详细解释量子计算的原理"}],
    "stream": true
  }'

echo ""
echo "期望:Gateway 日志应该显示 [断连] 消息"
echo "期望:Ollama 日志应该显示请求被取消(如果可见)"
echo ""
read -p "按回车继续下一个测试..."

# 测试 3: TTFB 超时
echo ""
echo "测试 3: TTFB 超时"
echo "模拟上游连接上但不返回数据的场景"
echo ""
echo "启动 mock server (另一个终端运行):"
echo "go run -<<'EOF'"
echo "package main"
echo "import (\"net/http\"; \"time\")"
echo "func main() {"
echo "  http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {"
echo "    time.Sleep(120 * time.Second)"
echo "  })"
echo "  http.ListenAndServe(\":9999\", nil)"
echo "}"
echo "EOF"
echo ""
echo "然后修改 config.yaml 添加测试路由:"
echo "  - model_pattern: \"test-hang\""
echo "    target: \"http://localhost:9999\""
echo "    adapter: \"openai\""
echo ""
echo "再运行:"
echo "curl http://localhost:8080/v1/chat/completions \\"
echo "  -H 'Content-Type: application/json' \\"
echo "  -d '{\"model\": \"test-hang\", \"messages\": [{\"role\": \"user\", \"content\": \"hi\"}], \"stream\": true}'"
echo ""
echo "期望:30秒后返回错误 (TTFB 超时)"
echo ""
read -p "按回车继续下一个测试..."

# 测试 4: Idle 超时
echo ""
echo "测试 4: Idle 超时"
echo "模拟上游开始返回数据,但中途卡住不再发送的场景"
echo ""
echo "启动 mock server (另一个终端运行):"
echo "go run -<<'EOF'"
echo "package main"
echo "import (\"fmt\"; \"net/http\"; \"time\")"
echo "func main() {"
echo "  http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {"
echo "    w.Header().Set(\"Content-Type\", \"text/event-stream\")"
echo "    w.WriteHeader(200)"
echo "    fmt.Fprintf(w, \"data: {\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"hi\\\"}}]}\\n\\n\")"
echo "    w.(http.Flusher).Flush()"
echo "    time.Sleep(120 * time.Second) // 发完第一个 chunk 后挂起"
echo "  })"
echo "  http.ListenAndServe(\":9998\", nil)"
echo "}"
echo "EOF"
echo ""
echo "修改 config.yaml 添加测试路由 (target: http://localhost:9998)"
echo "期望:60秒后连接被 IdleTimeout 断开"

echo ""
echo "=== 所有测试完成 ==="
```

---

## 实现顺序

1. **复制 iteration1 → iteration2**
2. **扩展 schema.go**:新增 `ChatCompletionChunk` 和 `DeltaContent` 结构体
3. **扩展 config.go**:新增 `TimeoutConfig` 字段
4. **修改 main.go**:
   - 移除 `ReadTimeout`/`WriteTimeout`
   - 添加 `IdleTimeout`
   - 实现请求分流逻辑(解析 → 路由 → 检查 `stream` → 分流)
5. **拆分 handler_nonstream.go**:把 Iteration 1 的非流式逻辑拆出来
6. **实现 handler_stream.go**:流式请求处理(SSE 响应头、逐 chunk 转发、Flush、断连检测)
7. **补全适配器流式方法**:`adapter_ollama.go` 和 `adapter_openai.go` 的 `TransformStreamChunk()`
8. **更新 config.yaml**:新增 `timeouts` 段
9. **编写 test.sh**:4 个验收测试脚本(含 mock server 代码)
10. **更新 README.md**:使用文档和验收说明

---

## 风险和注意事项

### 1. Flusher 可用性
某些中间件(如某些 Go Web 框架)可能不支持 `http.Flusher`。本项目直接用 `http.Server`,没有这个问题。

### 2. Nginx 反向代理缓冲
如果 Gateway 前面有 Nginx,需要配置:
```nginx
proxy_buffering off;
```
否则 SSE 会被 Nginx 缓冲,客户端无法实时收到 chunk。

### 3. 不同 Provider 的流式格式差异
- Ollama: 每个 chunk 都包含完整的 message 对象,需要提取增量 content
- OpenAI: 使用 delta 字段表示增量内容
- 适配器需要正确处理这些差异

### 4. 错误处理的特殊性
流式场景下,HTTP 状态码已经返回 200,中途出错无法改状态码。可以:
- 发送一个特殊的 error chunk
- 直接断开连接(客户端能感知到流异常终止)

本设计采用第二种方式,更简单且符合 SSE 规范。

---

## 总结

这个设计完整解决了 Iteration 2 的三大目标:

1. **流式转发**:手动逐 chunk 读取、转换、写入、Flush
2. **断连传播**:复用 `r.Context()`,利用 Go 的 context 取消机制自动传播
3. **超时分层**:DialTimeout + TTFB(用 context.WithTimeout) + IdleTimeout(用 SetReadDeadline)

架构上保持了与 Iteration 1 的一致性:
- 适配器接口不变(只补实现)
- 非流式逻辑不变
- 配置文件向后兼容(只新增字段)

为 Iteration 3(重试、熔断)留下了清晰的扩展点:
- 流式请求能否重试?设计已经给出答案:不能(一旦开始流式返回,无法回滚)
- 但可以在"发起上游请求前"做路由级重试(如果第一个 Provider 连接失败,立即切换到备用)
