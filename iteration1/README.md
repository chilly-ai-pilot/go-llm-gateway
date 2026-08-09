# Iteration 1: 协议统一 + 多 Provider 路由

## 目标

解决两个核心问题：

1. **协议统一**：不同模型在不同后端，调用方不想记多个 URL 和多套请求格式
2. **路由基础**：为后续可靠性机制（重试、熔断）打下底座——先有多个可切换的目标，才能谈切换和降级

## 核心原则

**启动时动态发现模型，配置保持简洁**

- Gateway 对外固定暴露 OpenAI Chat Completions 格式（事实标准）
- 配置中使用模式匹配（如 `llama*`、`qwen*`）定义路由规则
- **启动时自动从 Ollama 拉取实际可用模型列表**
- 将完整模型名（如 `llama3.2:latest`）精确注册到对应路由
- 请求时先精确匹配，再模式匹配
- 调用方完全无感知底层 Provider 差异

## 技术架构

### 统一 Schema + 动态模型发现

```
启动阶段：
  ├─ 加载 config.yaml
  ├─ 对于每个 fetch_models=true 的路由：
  │   ├─ 调用 Provider API 获取模型列表
  │   ├─ 用 model_pattern 过滤模型
  │   └─ 注册到精确匹配表
  └─ 构建完整的模型注册表

请求阶段：
调用方
  ↓ (OpenAI 格式)
Gateway
  ├─ 解析 model 字段
  ├─ 优先查精确匹配表 (从 Ollama 拉取的)
  ├─ 否则用模式匹配 (通配符)
  ├─ Adapter.ToProviderRequest()
  ↓ (Provider 原生格式)
Provider (Ollama/DeepSeek/...)
  ↓ (Provider 原生响应)
  ├─ Adapter.FromProviderResponse()
  ↓ (OpenAI 格式)
调用方
```

### 核心组件

| 组件 | 职责 |
|------|------|
| **Schema** | 定义 OpenAI Chat Completions 格式 |
| **Adapter 接口** | 定义双向转换方法（含流式占位） |
| **具体适配器** | Ollama、DeepSeek 的格式转换实现 |
| **OllamaClient** | Ollama API 客户端，获取模型列表 |
| **ModelRegistry** | 模型注册表，管理精确匹配和模式匹配 |
| **Router** | 路由配置和匹配逻辑 |
| **ReverseProxy** | 底层 HTTP 转发（保留流式能力） |

## 快速开始

### 前置条件

1. **Go 1.21+**
   ```bash
   go version
   ```

2. **Ollama 本地服务**
   ```bash
   # 下载模型
   ollama pull llama3.2
   
   # 启动 Ollama 服务
   ollama serve
   
   # 查看可用模型
   ./list-models.sh
   ```

3. **DeepSeek API Key**（可选，用于测试多 Provider）
   ```bash
   export DEEPSEEK_API_KEY="your-api-key"
   ```

### 启动 Gateway

```bash
cd iteration1
go mod tidy
go run .
```

预期输出：
```
Gateway 启动中...
监听端口: 8080
已配置 5 个路由:
  - llama* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - qwen* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - deepseek-r1* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - deepseek-chat -> https://api.deepseek.com/v1/chat/completions (deepseek adapter)
  - deepseek-coder -> https://api.deepseek.com/v1/chat/completions (deepseek adapter)

初始化模型注册表...
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] llama3.2:latest -> http://localhost:11434 (ollama adapter)
[成功] 路由 'llama*' 注册了 1 个模型
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] qwen2.5:3b -> http://localhost:11434 (ollama adapter)
[成功] 路由 'qwen*' 注册了 1 个模型
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] deepseek-r1:8b -> http://localhost:11434 (ollama adapter)
[注册] deepseek-r1:1.5b -> http://localhost:11434 (ollama adapter)
[成功] 路由 'deepseek-r1*' 注册了 2 个模型

已注册 4 个精确匹配的模型:
  - llama3.2:latest
  - qwen2.5:3b
  - deepseek-r1:8b
  - deepseek-r1:1.5b

Gateway 已启动，监听地址: http://localhost:8080
统一入口: POST /v1/chat/completions (OpenAI 格式)
```

### 验收测试

#### 自动测试（推荐）

```bash
./test.sh
```

测试覆盖：
- ✅ Ollama 路由（使用 OpenAI 格式请求）
- ✅ DeepSeek 路由（如果设置了 API Key）
- ✅ 响应格式一致性验证
- ✅ 错误处理（不存在的 model）

#### 手动测试

**测试 Ollama（llama3.2:latest）**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:latest",
    "messages": [
      {"role": "user", "content": "你好，请用一句话介绍你自己"}
    ],
    "temperature": 0.7,
    "max_tokens": 100
  }'
```

**测试 DeepSeek（deepseek-chat）**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [
      {"role": "user", "content": "你好，请用一句话介绍你自己"}
    ],
    "temperature": 0.7,
    "max_tokens": 100
  }'
```

**测试错误处理**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "nonexistent-model",
    "messages": [
      {"role": "user", "content": "test"}
    ]
  }'
```

预期返回：
```json
{
  "error": {
    "message": "no route found for model: nonexistent-model",
    "type": "model_not_found",
    "code": "404"
  }
}
```

## 配置说明

### config.yaml 结构

```yaml
port: 8080

routes:
  # Ollama 本地模型 - llama 系列
  - model_pattern: "llama*"
    target: "http://localhost:11434"
    adapter: "ollama"
    fetch_models: true  # 启动时从 Ollama 获取模型列表
    
  # Ollama 本地模型 - qwen 系列
  - model_pattern: "qwen*"
    target: "http://localhost:11434"
    adapter: "ollama"
    fetch_models: true
    
  # Ollama 本地模型 - deepseek-r1 系列（注意是本地，不是云端）
  - model_pattern: "deepseek-r1*"
    target: "http://localhost:11434"
    adapter: "ollama"
    fetch_models: true
    
  # DeepSeek 云端模型
  - model_pattern: "deepseek-chat"
    target: "https://api.deepseek.com/v1/chat/completions"
    adapter: "deepseek"
    api_key_env: "DEEPSEEK_API_KEY"
```

### 路由匹配规则

**两级匹配机制：**

1. **精确匹配（优先）**
   - 启动时从 Ollama 拉取的完整模型名
   - 例如：`llama3.2:latest`、`qwen2.5:3b`
   - O(1) 查找速度

2. **模式匹配（兜底）**
   - 配置文件中的 `model_pattern`
   - 支持通配符：`*`（任意字符）、`?`（单个字符）
   - 按配置顺序匹配
   - 用于云端模型或未启用 `fetch_models` 的路由

**Ollama 模型名格式**
- Ollama 本地模型使用 `name:tag` 格式
- 例如：`llama3.2:latest`、`qwen2.5:3b`、`deepseek-r1:8b`
- 配置中用 `llama*`、`qwen*` 等模式，启动时自动展开为完整名称

**重要：本地 vs 云端模型**
- `deepseek-r1:8b` - Ollama 本地模型（匹配 `deepseek-r1*`）
- `deepseek-chat` - DeepSeek 云端模型（精确匹配）
- 两者互不干扰

### 环境变量

- `DEEPSEEK_API_KEY`：DeepSeek API Key
- 可扩展其他 Provider 的 API Key

## 验收标准

✅ **协议统一**
- 使用统一的 OpenAI 格式请求不同 Provider
- 响应统一转换为 OpenAI 格式
- 调用方无需关心底层 Provider 差异

✅ **多 Provider 路由**
- 根据 `model` 字段自动路由到正确的 Provider
- 支持通配符匹配（如 `llama*`、`deepseek-*`）

✅ **格式一致性**
- 同一个问题在不同 Provider 下，响应结构完全一致
- 只有 `content` 内容不同，字段名和层级相同

✅ **错误处理**
- 配置不存在的 model 返回清晰的 JSON 错误
- Provider 错误被正确透传和映射

## 技术细节

### 动态模型发现机制

**工作原理：**

1. **启动阶段**
   - Gateway 读取 `config.yaml`
   - 对于 `fetch_models: true` 的路由：
     - 调用 Ollama API (`GET /api/tags`)
     - 获取所有可用模型列表
     - 用 `model_pattern` 过滤模型
     - 注册到 `ModelRegistry` 的精确匹配表

2. **请求阶段**
   - 先查精确匹配表（O(1) HashMap 查找）
   - 如果未找到，再用模式匹配（O(n) 遍历）
   - 返回匹配的路由配置

**优势：**

- ✅ 配置简洁：只需配置模式（如 `llama*`），不需要维护完整模型名列表
- ✅ 自动发现：下载新模型后重启 Gateway 即可自动识别
- ✅ 性能优化：精确匹配用 HashMap，常见请求 O(1) 时间
- ✅ 语义清晰：`llama*` 就是匹配 llama 系列，不会意外捕获其他模型
- ✅ 避免冲突：本地 `deepseek-r1:8b` 和云端 `deepseek-chat` 各自路由

**示例：**

配置：
```yaml
- model_pattern: "llama*"
  target: "http://localhost:11434"
  adapter: "ollama"
  fetch_models: true
```

启动时：
```
[Ollama] 从 http://localhost:11434 获取到 4 个模型
  - qwen2.5:3b          (不匹配 llama*, 跳过)
  - llama3.2:latest     (匹配!)
  - llama3.1:latest     (匹配!)
  - deepseek-r1:8b      (不匹配 llama*, 跳过)

[注册] llama3.2:latest -> http://localhost:11434 (ollama adapter)
[注册] llama3.1:latest -> http://localhost:11434 (ollama adapter)
[成功] 路由 'llama*' 注册了 2 个模型
```

请求时：
```go
// model="llama3.2:latest"
// 1. 查精确匹配表 -> 找到! 返回路由
// 2. (无需模式匹配)

// model="llama3.5:latest"  (假设是新下载的，但未重启 Gateway)
// 1. 查精确匹配表 -> 未找到
// 2. 模式匹配 "llama*" -> 匹配! 返回路由
```

### Adapter 接口设计

```go
type ProviderAdapter interface {
    // 非流式转换（本迭代实现）
    ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error)
    FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error)
    
    // 流式转换（本迭代占位，Iteration 2 实现）
    TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error)
}
```

**为什么提前定义流式接口？**

- 接口先行：避免 Iteration 2 改接口签名
- 已有适配器无需重构
- 占位实现返回 "not implemented: see Iteration 2"

### 保留 ReverseProxy 的原因

❌ **不要自己重写 HTTP 转发**
- `ReverseProxy` 已处理连接池、超时、流式转发
- 自己用 `http.Client` 重写会遗漏边界条件

✅ **在 ReverseProxy 外层包裹 Schema 转换**
- `Director`：修改请求（Body、URL、API Key）
- `ModifyResponse`：转换响应体
- `ErrorHandler`：统一错误处理

### Ollama 格式转换示例

**OpenAI → Ollama**

```json
// OpenAI 格式
{
  "model": "llama3",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant"},
    {"role": "user", "content": "你好"}
  ],
  "temperature": 0.7,
  "max_tokens": 100
}

// 转换为 Ollama 格式
{
  "model": "llama3",
  "prompt": "System: You are a helpful assistant\nUser: 你好",
  "stream": false,
  "temperature": 0.7,
  "num_predict": 100
}
```

**Ollama → OpenAI**

```json
// Ollama 响应
{
  "model": "llama3",
  "response": "你好！我是一个AI助手...",
  "done": true,
  "prompt_eval_count": 15,
  "eval_count": 28
}

// 转换为 OpenAI 格式
{
  "id": "chatcmpl-1234567890",
  "object": "chat.completion",
  "created": 1234567890,
  "model": "llama3",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "你好！我是一个AI助手..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 28,
    "total_tokens": 43
  }
}
```

## 项目结构

```
iteration1/
├── config.yaml            # 路由配置（支持 fetch_models）
├── config.go              # 配置读取 + Route 结构
├── main.go                # HTTP Handler + ReverseProxy + 错误处理
├── schema.go              # OpenAI 统一 Schema 定义
├── adapter.go             # ProviderAdapter 接口定义
├── adapter_ollama.go      # Ollama 适配器（messages→prompt 转换）
├── adapter_deepseek.go    # DeepSeek 适配器（OpenAI 兼容）
├── ollama.go              # Ollama API 客户端（获取模型列表）
├── router.go              # ModelRegistry（模型注册表）+ 路由逻辑
├── test.sh                # 验收测试脚本（含格式一致性）
├── list-models.sh         # 列出可用的 Ollama 模型
├── README.md              # 本文档
├── go.mod
└── go.sum
```

## 与 Iteration 0 的对比

| 特性 | Iteration 0 | Iteration 1 |
|------|-------------|-------------|
| 请求格式 | Provider 原生格式 | 统一 OpenAI 格式 |
| 响应格式 | Provider 原生格式 | 统一 OpenAI 格式 |
| 路由 | 硬编码单一目标 | 配置化多 Provider 路由 |
| Schema 转换 | 无（纯转发） | 双向转换 |
| 错误处理 | 简单日志 | JSON 格式错误响应 |
| API Key | 不支持 | 环境变量注入 |

## 故障排查

**问题：Gateway 启动时提示 "配置错误: routes 不能为空"**

解决：检查 `config.yaml` 格式，确保 `routes` 字段正确配置

**问题：请求返回 "no route found for model: xxx"**

解决：
1. 检查模型是否在启动日志的"已注册模型"列表中
2. 运行 `./list-models.sh` 查看 Ollama 可用模型
3. 如果模型存在但未注册，检查 `config.yaml` 中的 `model_pattern` 是否匹配
4. 如果是新下载的模型，重启 Gateway 重新拉取模型列表
5. 通配符规则：
   - `llama*` 匹配 `llama3.2:latest`、`llama3.1:latest` 等
   - `qwen*` 匹配 `qwen2.5:3b`、`qwen2:7b` 等

**问题：DeepSeek 返回 401 Unauthorized**

解决：
1. 确认 `DEEPSEEK_API_KEY` 环境变量已设置
2. 检查 API Key 是否有效
3. 查看 Gateway 日志确认 API Key 是否被注入

**问题：响应格式不一致**

解决：
1. 运行 `./test.sh` 查看格式一致性测试结果
2. 检查适配器的 `FromProviderResponse()` 实现
3. 查看 Gateway 日志确认转换过程

## 下一步

**Iteration 2** 将实现：
- 流式响应支持（SSE）
- 实现 `TransformStreamChunk()` 方法
- 逐块转发，降低首字延迟
- 处理流式错误和中断

## 设计权衡

### 为什么不支持 passthrough？

**当前不实现 passthrough（跳过转换、原样透传）的原因：**

- 现在没有任何具体场景需要 Provider 的专有参数
- 为假设性需求搭建配置、解析、测试是过早通用化
- 保持设计简洁，专注核心价值："统一入口"

**什么时候会加入？**

当接入一个有专有能力、统一 Schema 确实覆盖不了的 Provider 时，需求会变得具体，那时再加这条路径。

### 为什么选择 OpenAI 格式？

1. **事实标准**：大部分 SDK 和工具默认支持
2. **生态丰富**：LangChain、LlamaIndex 等框架原生支持
3. **文档完善**：开发者熟悉度高
4. **易于扩展**：字段设计考虑了多种场景

## 日志示例

```
Gateway 启动中...
监听端口: 8080
已配置 5 个路由:
  - llama* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - qwen* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - deepseek-r1* -> http://localhost:11434 (ollama adapter) [启动时获取模型列表]
  - deepseek-chat -> https://api.deepseek.com/v1/chat/completions (deepseek adapter)
  - deepseek-coder -> https://api.deepseek.com/v1/chat/completions (deepseek adapter)

初始化模型注册表...
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] llama3.2:latest -> http://localhost:11434 (ollama adapter)
[成功] 路由 'llama*' 注册了 1 个模型
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] qwen2.5:3b -> http://localhost:11434 (ollama adapter)
[成功] 路由 'qwen*' 注册了 1 个模型
[Ollama] 从 http://localhost:11434 获取到 4 个模型
[注册] deepseek-r1:8b -> http://localhost:11434 (ollama adapter)
[注册] deepseek-r1:1.5b -> http://localhost:11434 (ollama adapter)
[成功] 路由 'deepseek-r1*' 注册了 2 个模型

已注册 4 个精确匹配的模型:
  - llama3.2:latest
  - qwen2.5:3b
  - deepseek-r1:8b
  - deepseek-r1:1.5b

Gateway 已启动，监听地址: http://localhost:8080
统一入口: POST /v1/chat/completions (OpenAI 格式)

[请求] POST /v1/chat/completions
[路由] model=llama3.2:latest
[匹配] 路由匹配成功: llama* -> http://localhost:11434 (ollama)
[转换] 请求已转换为 ollama 格式
[响应] 收到 Provider 响应: 513 bytes, status=200
[转换] 响应已转换为 OpenAI 格式
[完成] 请求处理完成
```

## 贡献指南

### 添加新 Provider

1. 实现 `ProviderAdapter` 接口（新建 `adapter_xxx.go`）
2. 在 `NewAdapter()` 中注册适配器
3. 在 `config.yaml` 中添加路由
4. 在 `test.sh` 中添加测试用例
5. 更新本 README

### 适配器实现注意事项

- 处理 nil 指针（可选字段）
- 提供合理的默认值
- 错误信息清晰
- 保持 OpenAI 格式的字段完整性
