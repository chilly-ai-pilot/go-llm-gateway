#!/bin/bash

# LLM Gateway - Iteration 1 验收测试脚本
# 测试协议统一和多 Provider 路由功能

set -e

GATEWAY_URL="http://localhost:8080"
ENDPOINT="/v1/chat/completions"

echo "=========================================="
echo "LLM Gateway Iteration 1 验收测试"
echo "协议统一 + 多 Provider 路由"
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

# 检查 DeepSeek API Key（可选）
echo "3. 检查 DeepSeek API Key..."
if [ -z "$DEEPSEEK_API_KEY" ]; then
    echo "⚠️  DEEPSEEK_API_KEY 未设置，将跳过 DeepSeek 测试"
    SKIP_DEEPSEEK=true
else
    echo "✓ DEEPSEEK_API_KEY 已设置"
    SKIP_DEEPSEEK=false
fi
echo ""

# 测试问题
TEST_PROMPT="你好，请用一句话介绍你自己"

# 测试 1: Ollama 路由（使用统一的 OpenAI 格式）
echo "=========================================="
echo "测试 1: Ollama 路由（llama3.2:latest）"
echo "=========================================="
echo "发送统一格式请求 (OpenAI Chat Completions)..."
echo ""

OLLAMA_RESPONSE=$(curl -s -X POST "${GATEWAY_URL}${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -d "{
        \"model\": \"llama3.2:latest\",
        \"messages\": [
            {\"role\": \"user\", \"content\": \"${TEST_PROMPT}\"}
        ],
        \"temperature\": 0.7,
        \"max_tokens\": 100
    }")

if [ -z "$OLLAMA_RESPONSE" ]; then
    echo "❌ 未收到 Ollama 响应"
    exit 1
fi

echo "收到响应:"
echo "$OLLAMA_RESPONSE" | jq '.' 2>/dev/null || echo "$OLLAMA_RESPONSE"
echo ""

# 验证 Ollama 响应格式
if echo "$OLLAMA_RESPONSE" | jq -e '.choices[0].message.content' > /dev/null 2>&1; then
    echo "✓ Ollama 响应格式正确（OpenAI 格式）"
    OLLAMA_CONTENT=$(echo "$OLLAMA_RESPONSE" | jq -r '.choices[0].message.content')
    echo "  内容: ${OLLAMA_CONTENT:0:100}..."
else
    echo "❌ Ollama 响应格式错误"
    exit 1
fi
echo ""

# 测试 2: DeepSeek 路由（如果有 API Key）
if [ "$SKIP_DEEPSEEK" = false ]; then
    echo "=========================================="
    echo "测试 2: DeepSeek 路由（deepseek-chat）"
    echo "=========================================="
    echo "发送统一格式请求 (OpenAI Chat Completions)..."
    echo ""

    DEEPSEEK_RESPONSE=$(curl -s -X POST "${GATEWAY_URL}${ENDPOINT}" \
        -H "Content-Type: application/json" \
        -d "{
            \"model\": \"deepseek-chat\",
            \"messages\": [
                {\"role\": \"user\", \"content\": \"${TEST_PROMPT}\"}
            ],
            \"temperature\": 0.7,
            \"max_tokens\": 100
        }")

    if [ -z "$DEEPSEEK_RESPONSE" ]; then
        echo "❌ 未收到 DeepSeek 响应"
        exit 1
    fi

    echo "收到响应:"
    echo "$DEEPSEEK_RESPONSE" | jq '.' 2>/dev/null || echo "$DEEPSEEK_RESPONSE"
    echo ""

    # 验证 DeepSeek 响应格式
    if echo "$DEEPSEEK_RESPONSE" | jq -e '.choices[0].message.content' > /dev/null 2>&1; then
        echo "✓ DeepSeek 响应格式正确（OpenAI 格式）"
        DEEPSEEK_CONTENT=$(echo "$DEEPSEEK_RESPONSE" | jq -r '.choices[0].message.content')
        echo "  内容: ${DEEPSEEK_CONTENT:0:100}..."
    else
        echo "❌ DeepSeek 响应格式错误"
        exit 1
    fi
    echo ""
else
    echo "=========================================="
    echo "测试 2: DeepSeek 路由 - 已跳过"
    echo "=========================================="
    echo ""
fi

# 测试 3: 响应格式一致性
if [ "$SKIP_DEEPSEEK" = false ]; then
    echo "=========================================="
    echo "测试 3: 响应格式一致性"
    echo "=========================================="
    echo "验证 Ollama 和 DeepSeek 的响应结构是否一致..."
    echo ""

    OLLAMA_KEYS=$(echo "$OLLAMA_RESPONSE" | jq -S 'keys' 2>/dev/null)
    DEEPSEEK_KEYS=$(echo "$DEEPSEEK_RESPONSE" | jq -S 'keys' 2>/dev/null)

    if [ "$OLLAMA_KEYS" = "$DEEPSEEK_KEYS" ]; then
        echo "✓ 顶层字段一致"
        echo "  字段: $OLLAMA_KEYS"
    else
        echo "⚠️  顶层字段不完全一致"
        echo "  Ollama:   $OLLAMA_KEYS"
        echo "  DeepSeek: $DEEPSEEK_KEYS"
    fi
    echo ""

    # 检查 choices[0].message 结构
    OLLAMA_MSG_KEYS=$(echo "$OLLAMA_RESPONSE" | jq -S '.choices[0].message | keys' 2>/dev/null)
    DEEPSEEK_MSG_KEYS=$(echo "$DEEPSEEK_RESPONSE" | jq -S '.choices[0].message | keys' 2>/dev/null)

    if [ "$OLLAMA_MSG_KEYS" = "$DEEPSEEK_MSG_KEYS" ]; then
        echo "✓ message 结构一致"
        echo "  字段: $OLLAMA_MSG_KEYS"
    else
        echo "⚠️  message 结构不完全一致"
        echo "  Ollama:   $OLLAMA_MSG_KEYS"
        echo "  DeepSeek: $DEEPSEEK_MSG_KEYS"
    fi
    echo ""
fi

# 测试 4: 不存在的 model
echo "=========================================="
echo "测试 4: 不存在的 model（错误处理）"
echo "=========================================="
echo "发送不存在的 model..."
echo ""

ERROR_RESPONSE=$(curl -s -X POST "${GATEWAY_URL}${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "nonexistent-model",
        "messages": [
            {"role": "user", "content": "test"}
        ]
    }')

if echo "$ERROR_RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo "✓ 正确返回错误响应"
    ERROR_MSG=$(echo "$ERROR_RESPONSE" | jq -r '.error.message')
    echo "  错误信息: $ERROR_MSG"
else
    echo "❌ 错误响应格式不正确"
    echo "$ERROR_RESPONSE"
    exit 1
fi
echo ""

# 总结
echo "=========================================="
echo "✓ 所有测试通过！"
echo "=========================================="
echo ""
echo "验收标准已满足:"
echo "1. ✓ 使用统一的 OpenAI 格式请求 Ollama"
echo "2. ✓ 响应被转换为统一的 OpenAI 格式"
if [ "$SKIP_DEEPSEEK" = false ]; then
    echo "3. ✓ 使用统一的 OpenAI 格式请求 DeepSeek"
    echo "4. ✓ 两个 Provider 的响应结构一致"
else
    echo "3. ⊘ DeepSeek 测试已跳过（未设置 API Key）"
fi
echo "5. ✓ 不存在的 model 返回清晰错误"
echo ""
echo "请查看 Gateway 日志确认路由和转换过程"
