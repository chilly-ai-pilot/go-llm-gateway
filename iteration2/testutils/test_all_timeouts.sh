#!/bin/bash

# 超时测试脚本 - 测试三种超时机制
# 1. Dial Timeout - 连接超时
# 2. TTFB Timeout - 首字节超时
# 3. Idle Timeout - 空闲超时

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

GATEWAY_PORT=8080
TTFB_SERVER_PORT=9998
IDLE_SERVER_PORT=9997

# 打印带颜色的消息
print_header() {
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}$1${NC}"
}

# 检查 Gateway 是否运行
check_gateway() {
    print_info "检查 Gateway 是否运行在端口 $GATEWAY_PORT..."
    if lsof -Pi :$GATEWAY_PORT -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        print_success "Gateway 正在运行"
        return 0
    else
        print_error "Gateway 未运行"
        echo "请在另一个终端运行: cd iteration2 && go run ."
        return 1
    fi
}

# 检查 Mock Server 是否运行
check_mock_server() {
    local port=$1
    local name=$2
    print_info "检查 $name 是否运行在端口 $port..."
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        print_success "$name 正在运行"
        return 0
    else
        print_error "$name 未运行"
        return 1
    fi
}

# 启动 Mock Server
start_mock_server() {
    local script=$1
    local name=$2
    print_info "启动 $name..."
    go run "$script" > /tmp/${script}.log 2>&1 &
    local pid=$!
    echo $pid > /tmp/${script}.pid
    sleep 1
    
    if ps -p $pid > /dev/null 2>&1; then
        print_success "$name 已启动 (PID: $pid)"
        return 0
    else
        print_error "$name 启动失败"
        cat /tmp/${script}.log
        return 1
    fi
}

# 停止 Mock Server
stop_mock_server() {
    local script=$1
    local name=$2
    if [ -f /tmp/${script}.pid ]; then
        local pid=$(cat /tmp/${script}.pid)
        if ps -p $pid > /dev/null 2>&1; then
            print_info "停止 $name (PID: $pid)..."
            kill $pid 2>/dev/null || true
            rm /tmp/${script}.pid
            print_success "$name 已停止"
        fi
    fi
}

# 清理函数
cleanup() {
    print_info "\n清理测试环境..."
    stop_mock_server "mock_ttfb_server.go" "TTFB Mock Server"
    stop_mock_server "mock_idle_server.go" "Idle Mock Server"
    print_success "清理完成"
}

# 注册清理函数
trap cleanup EXIT INT TERM

# 测试 1: Dial Timeout (连接超时)
test_dial_timeout() {
    print_header "测试 1: Dial Timeout (连接超时)"
    
    print_info "目标: 验证连接不可达地址时,5秒内返回错误"
    print_info "配置: model=test-dial-timeout -> http://192.0.2.1:9999"
    print_info "预期: 约 5 秒后返回连接错误"
    
    echo -e "\n发起请求..."
    local start_time=$(date +%s)
    
    local response=$(curl -s -X POST http://localhost:$GATEWAY_PORT/v1/chat/completions \
        -H "Content-Type: application/json" \
        -d '{
            "model": "test-dial-timeout",
            "messages": [{"role": "user", "content": "test"}],
            "stream": true
        }' 2>&1)
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo "响应时间: ${duration} 秒"
    echo "响应内容: $response"
    
    # 验证
    if [ $duration -ge 3 ] && [ $duration -le 8 ]; then
        print_success "Dial Timeout 测试通过 (${duration}秒,预期5秒)"
        return 0
    else
        print_error "Dial Timeout 测试失败 (${duration}秒,预期约5秒)"
        return 1
    fi
}

# 测试 2: TTFB Timeout (首字节超时)
test_ttfb_timeout() {
    print_header "测试 2: TTFB Timeout (首字节超时)"
    
    print_info "目标: 验证服务器接收请求但不返回响应时,30秒内返回错误"
    print_info "配置: model=test-ttfb-timeout -> http://localhost:$TTFB_SERVER_PORT"
    print_info "预期: 约 30 秒后返回 TTFB 超时错误"
    
    # 检查或启动 Mock Server
    if ! check_mock_server $TTFB_SERVER_PORT "TTFB Mock Server"; then
        start_mock_server "mock_ttfb_server.go" "TTFB Mock Server" || return 1
    fi
    
    echo -e "\n发起请求..."
    local start_time=$(date +%s)
    
    local response=$(curl -s -X POST http://localhost:$GATEWAY_PORT/v1/chat/completions \
        -H "Content-Type: application/json" \
        -d '{
            "model": "test-ttfb-timeout",
            "messages": [{"role": "user", "content": "test"}],
            "stream": true
        }' 2>&1)
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo "响应时间: ${duration} 秒"
    echo "响应内容: $response"
    
    # 验证
    if [ $duration -ge 25 ] && [ $duration -le 35 ]; then
        print_success "TTFB Timeout 测试通过 (${duration}秒,预期30秒)"
        stop_mock_server "mock_ttfb_server.go" "TTFB Mock Server"
        return 0
    else
        print_error "TTFB Timeout 测试失败 (${duration}秒,预期约30秒)"
        stop_mock_server "mock_ttfb_server.go" "TTFB Mock Server"
        return 1
    fi
}

# 测试 3: Idle Timeout (空闲超时)
test_idle_timeout() {
    print_header "测试 3: Idle Timeout (空闲超时)"
    
    print_info "目标: 验证流式响应中 chunk 间隔过长时,60秒内断开连接"
    print_info "配置: model=test-idle-timeout -> http://localhost:$IDLE_SERVER_PORT"
    print_info "预期: 收到第一个 chunk 后,约 60 秒连接断开"
    
    # 检查或启动 Mock Server
    if ! check_mock_server $IDLE_SERVER_PORT "Idle Mock Server"; then
        start_mock_server "mock_idle_server.go" "Idle Mock Server" || return 1
    fi
    
    echo -e "\n使用测试客户端..."
    local output=$(go run test_idle_client.go 2>&1)
    echo "$output"
    
    # 验证
    if echo "$output" | grep -q "IdleTimeout 在预期范围内触发"; then
        print_success "Idle Timeout 测试通过"
        stop_mock_server "mock_idle_server.go" "Idle Mock Server"
        return 0
    else
        print_error "Idle Timeout 测试失败"
        echo "$output"
        stop_mock_server "mock_idle_server.go" "Idle Mock Server"
        return 1
    fi
}

# 主函数
main() {
    print_header "Iteration 2 超时测试套件"
    
    print_info "本脚本将测试以下三种超时机制:"
    echo "1. Dial Timeout   - 连接超时 (5秒)"
    echo "2. TTFB Timeout   - 首字节超时 (30秒)"
    echo "3. Idle Timeout   - 空闲超时 (60秒)"
    echo ""
    print_info "总预计时间: 约 100 秒"
    echo ""
    
    # 检查 Gateway
    if ! check_gateway; then
        exit 1
    fi
    
    # 运行测试
    local failed=0
    
    test_dial_timeout || failed=$((failed + 1))
    test_ttfb_timeout || failed=$((failed + 1))
    test_idle_timeout || failed=$((failed + 1))
    
    # 总结
    print_header "测试结果"
    
    if [ $failed -eq 0 ]; then
        print_success "所有测试通过! (3/3)"
        echo ""
        echo "✓ Dial Timeout  - 5秒连接超时正常"
        echo "✓ TTFB Timeout  - 30秒首字节超时正常"
        echo "✓ Idle Timeout  - 60秒空闲超时正常"
        exit 0
    else
        print_error "部分测试失败: $failed/3"
        exit 1
    fi
}

# 运行主函数
main
