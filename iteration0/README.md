# Iteration 0: 最简反向代理

## 目标

实现最基础的反向代理功能，验证 Go 标准库做代理的可行性。

- 请求进来 → 转发到 LLM Provider → 响应返回
- 使用 `net/http/httputil.ReverseProxy`
- 不涉及任何路由/限流逻辑
- 硬编码目标地址（Ollama 本地服务）

## 技术选型

- **反向代理**: Go 标准库 `net/http/httputil.ReverseProxy`
  - 生产级实现
  - 原生支持流式响应转发
  - 无需引入第三方 Web 框架

- **配置管理**: `gopkg.in/yaml.v3`
  - 简单的 YAML 配置文件
  - 只定义 `target_url` 和 `port`

## 快速开始

### 前置条件

1. **安装 Go** (1.21+)
   ```bash
   go version
   ```

2. **安装并启动 Ollama**
   ```bash
   # 下载模型（如果还没有）
   ollama pull llama3
   
   # 启动 Ollama 服务
   ollama serve
   ```
   
   Ollama 默认监听在 `http://localhost:11434`

### 启动 Gateway

1. **安装依赖**
   ```bash
   go mod download
   ```

2. **启动 Gateway**
   ```bash
   go run .
   ```
   
   你应该看到类似输出：
   ```
   Gateway 启动中...
   监听端口: 8080
   目标地址: http://localhost:11434
   Gateway 已启动，监听地址: http://localhost:8080
   所有请求将被转发到: http://localhost:11434
   ```

### 验收测试

**方式一：使用测试脚本**

```bash
chmod +x test.sh
./test.sh
```

**方式二：手动测试**

```bash
curl -X POST http://localhost:8080/api/generate \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "prompt": "你好，请用一句话介绍你自己",
    "stream": false
  }'
```

**预期响应示例：**

```json
{
  "model": "llama3",
  "created_at": "2024-01-01T00:00:00.000000Z",
  "response": "我是一个大型语言模型...",
  "done": true
}
```

**Gateway 日志示例：**

```
[请求] POST /api/generate -> http://localhost:11434/api/generate
[响应] POST /api/generate <- 状态码: 200
```

## 验收标准

✅ curl 发送请求能返回 Ollama 的生成结果  
✅ Gateway 日志能看到请求转发记录  
✅ 响应包含 `response` 字段且内容正确

## 重要说明

### 为什么使用 Ollama 原生格式？

这一步**刻意使用** Ollama 的原生格式（`/api/generate` + `prompt` 字段），而不是 OpenAI 格式（`/v1/chat/completions` + `messages` 字段）。

**原因：**

- 如果现在就用 OpenAI 格式，新版 Ollama 自己也支持这个兼容端点
- 请求可能"意外地"能跑通，但验证的是"Ollama 认识这个格式"，不是"Gateway 做对了什么"
- **使用原生格式能明确证明**：Gateway 没做任何 Schema 转换，纯粹是管道转发

协议统一（OpenAI 格式适配）是 **Iteration 1** 要解决的问题。

## 项目结构

```
iteration0/
├── config.yaml       # 配置文件：端口和目标地址
├── config.go         # 配置读取逻辑
├── main.go           # 主程序和反向代理实现
├── test.sh           # 验收测试脚本
├── README.md         # 本文档
└── go.mod            # Go 模块定义
```

## 配置说明

`config.yaml` 配置项：

```yaml
# Gateway 监听端口
port: 8080

# 目标 LLM Provider 地址
target_url: http://localhost:11434
```

## 故障排查

**问题：Gateway 启动失败，提示 "address already in use"**

解决：端口 8080 被占用，修改 `config.yaml` 中的 `port` 为其他值（如 8081）

**问题：测试时提示 "connection refused"**

解决：
1. 检查 Ollama 是否在运行：`curl http://localhost:11434/api/tags`
2. 检查 Gateway 是否在运行：`curl http://localhost:8080`

**问题：请求超时**

解决：
1. 确认模型已下载：`ollama list`
2. 如果模型很大，首次加载可能需要较长时间

## 下一步

Iteration 1 将实现：
- 协议统一（OpenAI 格式适配）
- 多 Provider 路由
- 配置化路由规则

## 技术细节

### ReverseProxy 流式转发

`httputil.ReverseProxy` 自动处理：
- HTTP/1.1 chunked transfer encoding
- 逐块转发响应体
- 保持连接状态

这为 Iteration 2（流式响应优化）打下基础。

### 日志设计

当前日志记录：
- 请求时间戳（标准库 log 自动添加）
- 请求方法和路径
- 目标 URL
- 响应状态码
- 错误信息

足够验证转发逻辑，后续迭代会增强为结构化日志。
