# Iteration 2: 流式响应 + 超时管理

## 概述

Iteration 2 在 Iteration 1 的基础上,实现了完整的流式响应支持和三层超时管理机制。

## 核心功能

### 1. 流式响应

- **请求分流**: 根据 `stream` 字段自动分流到流式/非流式处理器
- **SSE 格式**: 符合 Server-Sent Events 标准的流式输出
- **逐 chunk 转发**: 收到一个 chunk 立即转发,不缓冲
- **立即 Flush**: 确保客户端实时接收数据
- **适配器支持**: Ollama 和 OpenAI 格式的流式转换

### 2. 客户端断连传播

- 使用 `context.Context` 实现断连信号传播
- 客户端取消请求时,上游请求立即终止
- 避免浪费 Provider 资源和计费

### 3. 三层超时管理

| 超时类型 | 默认值 | 实现方式 | 作用 |
|---------|--------|---------|------|
| **连接超时** (Dial Timeout) | 5秒 | `net.Dialer.Timeout` | TCP 连接建立超时 |
| **首字节超时** (TTFB) | 30秒 | `http.Transport.ResponseHeaderTimeout` | 等待响应头超时 |
| **空闲超时** (Idle Timeout) | 60秒 | `net.Conn.SetReadDeadline` | chunk 间隔超时 |

## 技术亮点

### Idle Timeout 的实现

通过三个组件协作实现:

1. **connTracker** (`conn_tracker.go`)
   - 在 `DialContext` 时捕获底层 `net.Conn`
   - 包装连接,记录连接对象引用

2. **自定义 DialContext** (`handler_stream.go`)
   - 使用闭包捕获 `net.Conn`
   - 将连接传递给后续处理逻辑

3. **idleTimeoutReader** (`handler_stream.go`)
   - 每次读取前调用 `SetReadDeadline(now + 60s)`
   - 60 秒内无数据则返回 `i/o timeout`

## 文件结构

```
iteration2/
├── main.go                    # 入口 + 请求分流
├── handler_stream.go          # 流式处理 + Idle超时
├── handler_nonstream.go       # 非流式处理
├── conn_tracker.go            # net.Conn 捕获
├── config.go                  # 配置加载 + TimeoutConfig
├── config.yaml                # 超时配置
├── adapter.go                 # 适配器接口
├── adapter_ollama.go          # Ollama 适配器 + 流式
├── adapter_openai.go          # OpenAI 适配器 + 流式
├── router.go                  # 路由匹配
├── schema.go                  # OpenAI Schema + 流式结构
├── test.sh                    # Iteration 1 的验收测试
├── DESIGN.md                  # 技术设计文档
└── testutils/                 # 测试工具
    ├── README.md              # 测试指南
    ├── mock_ttfb_server.go    # TTFB 超时测试
    ├── mock_idle_server.go    # Idle 超时测试
    └── test_idle_client.go    # 测试客户端
```

## 配置

### config.yaml

```yaml
port: 8080

# 超时配置 (新增)
timeouts:
  dial_timeout: 5   # 连接超时(秒)
  ttfb_timeout: 30  # 首字节超时(秒)
  idle_timeout: 60  # chunk 间隔超时(秒)

routes:
  - model_pattern: "llama*"
    target: "http://localhost:11434"
    adapter: "ollama"
  # ... 其他路由 ...
```

## 使用示例

### 非流式请求 (与 Iteration 1 一致)

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:latest",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'
```

### 流式请求 (新增)

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

**输出:**
```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk",...,"choices":[{"delta":{"content":"你"}}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk",...,"choices":[{"delta":{"content":"好"}}]}

data: [DONE]
```

## 验收测试

### 快速测试: 一键运行所有超时测试

```bash
# 1. 启动 Gateway
cd iteration2
go run .

# 2. 在另一个终端运行测试脚本
cd iteration2/testutils
./test_all_timeouts.sh
```

该脚本会自动测试所有三种超时机制 (Dial、TTFB、Idle),预计 100 秒完成。

### 详细测试

### 1. 基础功能测试 (Iteration 1)

```bash
./test.sh
```

验证:
- ✅ 非流式请求正常工作
- ✅ 多 Provider 路由正常
- ✅ Schema 转换正确

### 2. Idle Timeout 测试

```bash
# 终端 1: 启动 Mock Server
go run testutils/mock_idle_server.go

# 终端 2: 启动 Gateway  
go run .

# 终端 3: 运行测试
go run testutils/test_idle_client.go
```

**期望结果:**
```
收到响应: status=200
[0s] data: {...第一个chunk...}
连接持续时间: 60 秒
✓ IdleTimeout 在预期范围内触发 (55-70秒)
```

详细测试指南见 [`testutils/README.md`](testutils/README.md)

## 关键日志

### 正常流式请求

```
[请求] POST /v1/chat/completions
[路由] model=llama3, stream=true
[匹配] 路由匹配成功: llama* -> http://localhost:11434 (ollama)
[流式] 处理流式请求
[转换] 请求已转换为 ollama 格式
[上游] 发起流式请求到 http://localhost:11434/api/chat
[上游] 收到响应: status=200
[完成] 流式请求处理完成,共转发 15 个 chunk
```

### Idle Timeout 触发

```
[上游] 收到响应: status=200
(60秒后)
[错误] 流式读取失败: read tcp [::1]:60590->[::1]:9997: i/o timeout
[完成] 流式请求处理完成,共转发 1 个 chunk
```

## 与 Iteration 1 的兼容性

- ✅ 非流式请求完全兼容
- ✅ 所有 Iteration 1 的测试通过
- ✅ 配置文件向后兼容 (超时配置有默认值)
- ✅ 路由规则不变

## 下一步

Iteration 2 完成了流式支持和超时管理,为 Iteration 3 的可靠性机制打下基础:
- 重试策略
- 熔断机制
- 限流控制

## 参考文档

- [DESIGN.md](DESIGN.md) - 技术设计方案
- [testutils/README.md](testutils/README.md) - 测试指南
- [../docs/iteration-plan.md](../docs/iteration-plan.md) - 整体迭代计划
