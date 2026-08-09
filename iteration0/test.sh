#!/bin/bash

# LLM Gateway - Iteration 0 验收测试脚本
# 测试最简反向代理功能

set -e

GATEWAY_URL="http://localhost:8080"
OLLAMA_ENDPOINT="/api/generate"

echo "=========================================="
echo "LLM Gateway Iteration 0 验收测试"
echo "=========================================="
echo ""

# 检查 Gateway 是否在运行
echo "1. 检查 Gateway 是否在运行..."
if ! curl -s --max-time 2 "${GATEWAY_URL}" > /dev/null 2>&1; then
    echo "❌ Gateway 未运行，请先启动 Gateway"
    echo "   运行: go run ."
    exit 1
fi
echo "✓ Gateway 正在运行"
echo ""

# 检查 Ollama 是否在运行
echo "2. 检查 Ollama 是否在运行..."
if ! curl -s --max-time 2 "http://localhost:11434/api/tags" > /dev/null 2>&1; then
    echo "❌ Ollama 未运行，请先启动 Ollama"
    echo "   运行: ollama serve"
    exit 1
fi
echo "✓ Ollama 正在运行"
echo ""

# 发送测试请求（使用 Ollama 原生格式）
echo "3. 发送测试请求到 Gateway..."
echo "   请求地址: ${GATEWAY_URL}${OLLAMA_ENDPOINT}"
echo "   使用 Ollama 原生格式 (prompt 字段)"
echo ""

RESPONSE=$(curl -s -X POST "${GATEWAY_URL}${OLLAMA_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "llama3",
        "prompt": "你好，请用一句话介绍你自己",
        "stream": false
    }')

# 检查响应
if [ -z "$RESPONSE" ]; then
    echo "❌ 未收到响应"
    exit 1
fi

echo "收到响应:"
echo "$RESPONSE" | head -c 500
echo ""
echo ""

# 验证响应包含预期字段
if echo "$RESPONSE" | grep -q "response"; then
    echo "✓ 响应包含 'response' 字段"
else
    echo "❌ 响应格式异常，未找到 'response' 字段"
    exit 1
fi

echo ""
echo "=========================================="
echo "✓ 所有测试通过！"
echo "=========================================="
echo ""
echo "验收标准已满足:"
echo "1. Gateway 成功转发请求到 Ollama"
echo "2. 收到 Ollama 的生成结果"
echo "3. 响应格式正确（包含 response 字段）"
echo ""
echo "请查看 Gateway 日志确认请求转发记录"
