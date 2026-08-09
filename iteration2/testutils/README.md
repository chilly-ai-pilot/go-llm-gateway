# 测试工具

这个目录包含用于测试 Gateway 超时机制的 Mock Server 和测试客户端。

## 快速开始

**推荐: 一键测试所有超时机制**

使用位于 `iteration2/testutils/test_all_timeouts.sh` 的自动化测试脚本:

```bash
# 1. 启动 Gateway (终端 1)
cd iteration2
go run .

# 2. 运行完整测试套件 (终端 2)
cd iteration2/testutils
chmod +x test_all_timeouts.sh
./test_all_timeouts.sh
```

该脚本会自动:
- ✅ 测试 Dial Timeout (5秒)
- ✅ 测试 TTFB Timeout (30秒) - 自动启动/停止 Mock Server
- ✅ 测试 Idle Timeout (60秒) - 使用已运行的 Mock Server 或自动启动
- ✅ 自动清理所有 Mock Server

**预计运行时间:** 约 100 秒 (5 + 30 + 60)

---

## 文件说明

### Mock Servers

#### 1. mock_ttfb_server.go - TTFB 超时测试

**用途:** 模拟连接成功但不返回数据的场景

**端口:** 9998

**行为:**
- 接收 HTTP 请求
- 读取请求体但不返回任何响应
- 挂起 120 秒
- 用于验证 Gateway 的 TTFB Timeout (30秒) 是否生效

**启动:**
```bash
go run testutils/mock_ttfb_server.go
```

**配置路由:**
```yaml
- model_pattern: "test-ttfb-timeout"
  target: "http://localhost:9998"
  adapter: "openai"
```

**测试:**
```bash
time curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "test-ttfb-timeout",
    "messages": [{"role": "user", "content": "test"}],
    "stream": true
  }'
```

**期望结果:** 约 30 秒后返回超时错误

---

#### 2. mock_idle_server.go - Idle 超时测试

**用途:** 模拟流式开始后中途停止的场景

**端口:** 9997

**行为:**
- 返回 SSE 响应头
- 发送第一个 chunk 并 flush
- 挂起 120 秒不发送更多 chunk
- 用于验证 Gateway 的 IdleTimeout (60秒) 是否生效

**启动:**
```bash
go run testutils/mock_idle_server.go
```

**配置路由:**
```yaml
- model_pattern: "test-idle-timeout"
  target: "http://localhost:9997"
  adapter: "openai"
```

**测试:**
使用测试客户端:
```bash
go run testutils/test_idle_client.go
```

或使用 curl (需要等待观察):
```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "test-idle-timeout",
    "messages": [{"role": "user", "content": "test"}],
    "stream": true
  }'
```

**期望结果:** 
- 立即收到第一个 chunk
- 约 60 秒后连接断开 (Gateway 检测到 idle timeout)

---

### Test Clients

#### 3. test_idle_client.go - Idle 超时测试客户端

**用途:** 自动化测试 Idle Timeout,持续接收数据直到连接断开

**功能:**
- 发起流式请求到 Gateway
- 接收并记录每个 chunk 的时间戳
- 测量连接持续时间
- 判断 IdleTimeout 是否在预期范围内触发

**使用:**
```bash
# 1. 启动 Mock Server
go run testutils/mock_idle_server.go

# 2. 确保 config.yaml 有 test-idle-timeout 路由

# 3. 运行测试客户端
go run testutils/test_idle_client.go
```

**输出示例:**
```
========================================
Idle Timeout 测试客户端
========================================
发起流式请求到 Gateway...

收到响应: status=200

[0s] data: {"id":"chatcmpl-123",...}

========================================
测试结果: 收到 1 个 chunk
连接持续时间: 60 秒
========================================
✓ IdleTimeout 在预期范围内触发 (55-70秒)
```

---

## 超时测试快速指南

### 方式一: 一键测试 (推荐)

使用 `test_all_timeouts.sh` 脚本自动测试所有三种超时机制:

```bash
# 1. 启动 Gateway
cd iteration2
go run .

# 2. 在另一个终端运行测试脚本
cd iteration2/testutils
./test_all_timeouts.sh
```

**脚本功能:**
- 自动检测 Gateway 是否运行
- 按序测试 Dial、TTFB、Idle 三种超时
- 自动启动和停止所需的 Mock Server
- 验证每个超时是否在预期时间内触发
- 自动清理测试环境

**预期输出:**
```
========================================
Iteration 2 超时测试套件
========================================
本脚本将测试以下三种超时机制:
1. Dial Timeout   - 连接超时 (5秒)
2. TTFB Timeout   - 首字节超时 (30秒)
3. Idle Timeout   - 空闲超时 (60秒)

总预计时间: 约 100 秒

========================================
测试 1: Dial Timeout (连接超时)
========================================
✓ Dial Timeout 测试通过 (5秒,预期5秒)

========================================
测试 2: TTFB Timeout (首字节超时)
========================================
✓ TTFB Timeout 测试通过 (30秒,预期30秒)

========================================
测试 3: Idle Timeout (空闲超时)
========================================
✓ Idle Timeout 测试通过

========================================
测试结果
========================================
✓ 所有测试通过! (3/3)

✓ Dial Timeout  - 5秒连接超时正常
✓ TTFB Timeout  - 30秒首字节超时正常
✓ Idle Timeout  - 60秒空闲超时正常
```

---

### 方式二: 单独测试各个超时 (用于调试)

#### 1. 测试 Idle Timeout

**步骤:**

1. **启动 Mock Server** (终端 1)
   ```bash
   cd iteration2
   go run testutils/mock_idle_server.go
   ```

2. **确保配置路由** (编辑 config.yaml)
   ```yaml
   routes:
     - model_pattern: "test-idle-timeout"
       target: "http://localhost:9997"
       adapter: "openai"
   ```

3. **启动 Gateway** (终端 2)
   ```bash
   cd iteration2
   go run .
   ```

4. **运行测试** (终端 3)
   ```bash
   cd iteration2
   go run testutils/test_idle_client.go
   ```

**验收标准:**
- ✅ 收到第一个 chunk
- ✅ 连接持续约 60 秒
- ✅ Gateway 日志显示 `i/o timeout`
- ✅ 测试客户端显示 "IdleTimeout 在预期范围内触发"

---

#### 2. 测试 TTFB Timeout

**步骤:**

1. **启动 Mock Server** (终端 1)
   ```bash
   cd iteration2
   go run testutils/mock_ttfb_server.go
   ```

2. **配置路由**
   ```yaml
   - model_pattern: "test-ttfb-timeout"
     target: "http://localhost:9998"
     adapter: "openai"
   ```

3. **启动 Gateway 并测试**
   ```bash
   time curl -X POST http://localhost:8080/v1/chat/completions \
     -H "Content-Type: application/json" \
     -d '{
       "model": "test-ttfb-timeout",
       "messages": [{"role": "user", "content": "test"}],
       "stream": true
     }'
   ```

**验收标准:**
- ✅ 约 30 秒后返回错误
- ✅ Gateway 日志不显示 "收到响应"

---

#### 3. 测试连接超时 (Dial Timeout)

**不需要 Mock Server,使用不可达地址:**

1. **配置路由**
   ```yaml
   - model_pattern: "test-dial-timeout"
     target: "http://192.0.2.1:9999"  # TEST-NET-1 保留地址
     adapter: "openai"
   ```

2. **测试**
   ```bash
   time curl -X POST http://localhost:8080/v1/chat/completions \
     -H "Content-Type: application/json" \
     -d '{
       "model": "test-dial-timeout",
       "messages": [{"role": "user", "content": "test"}],
       "stream": true
     }'
   ```

**验收标准:**
- ✅ 约 5 秒内返回错误
- ✅ 错误信息包含 "dial" 或 "connection"

---

## 注意事项

1. **Mock Server 必须单独运行**
   - 不要和 Gateway 一起 `go run .`
   - 必须在单独的终端运行

2. **端口占用**
   - mock_ttfb_server: 9998
   - mock_idle_server: 9997
   - Gateway: 8080

3. **配置路由**
   - 测试前必须在 config.yaml 添加对应路由
   - 修改配置后需要重启 Gateway

4. **清理**
   - 测试完成后按 Ctrl+C 停止 Mock Server
   - 可以从 config.yaml 移除测试路由 (可选)

---

## 故障排查

### 问题: Idle Timeout 没有触发

**症状:** 连接持续 120 秒直到 Mock Server 发送 [DONE]

**可能原因:**
- Gateway 没有正确捕获底层 net.Conn
- idleTimeoutReader 没有调用 SetReadDeadline

**检查:**
1. 查看 Gateway 日志,确认没有 "设置读取截止时间失败" 的警告
2. 确认 conn_tracker.go 文件存在
3. 确认 handler_stream.go 中使用了 capturedConn

### 问题: 测试客户端立即退出

**症状:** test_idle_client 显示 "连接持续时间: 0 秒"

**可能原因:**
- Mock Server 未运行
- 配置路由错误
- Gateway 返回了错误响应

**检查:**
1. 确认 Mock Server 在运行: `lsof -i :9997`
2. 检查 config.yaml 中的路由配置
3. 查看 Gateway 日志的错误信息

---

## 实现原理

### Idle Timeout 的实现

**核心机制:**
1. 使用 `connTracker` 在 `DialContext` 时捕获底层 `net.Conn`
2. 将连接传递给 `idleTimeoutReader`
3. 每次读取前调用 `conn.SetReadDeadline(now + 60s)`
4. 如果 60 秒内没有新数据,返回 `i/o timeout`

**代码位置:**
- `conn_tracker.go`: connTracker 结构体
- `handler_stream.go`: DialContext 钩子 + idleTimeoutReader

**为什么不用 `http.Server.IdleTimeout`:**
- `IdleTimeout` 只对 Gateway 和客户端之间的连接生效
- 不能控制 Gateway 和上游之间的连接
- 需要在读取上游响应时设置超时

---

## 总结

这个目录提供了完整的超时测试工具:
- **2 个 Mock Server** - 模拟不同的故障场景
- **1 个测试客户端** - 自动化验证 Idle Timeout
- **完整的测试指南** - 快速验收所有超时机制

通过这些工具,可以验证 Gateway 在各种故障场景下都能及时返回错误,不会无限挂起。
