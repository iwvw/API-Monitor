#!/bin/bash
# API Monitor Go Backend 启动脚本

echo "==================================="
echo "  API Monitor Go Backend"
echo "==================================="
echo ""

# 检查二进制文件
if [ ! -f "backend-go/api-monitor.exe" ]; then
    echo "正在构建 Go 后端..."
    cd backend-go
    go build -o api-monitor.exe ./cmd/api-monitor
    cd ..
    echo "✓ 构建完成"
    echo ""
fi

# 设置环境变量
export PORT=3000
export DATA_DIR=./data
export DB_NAME=data.db
export LOG_LEVEL=INFO

# 如果 Node legacy 需要运行，设置端口
export LEGACY_PORT=3001

echo "配置信息："
echo "  - Go 端口: $PORT"
echo "  - 数据目录: $DATA_DIR"
echo "  - 数据库: $DB_NAME"
echo "  - 日志级别: $LOG_LEVEL"
echo ""

# 检查是否需要启动 Node sidecar
START_NODE=false
if [ "$1" == "--with-node" ]; then
    START_NODE=true
    echo "⚠️  将同时启动 Node sidecar (端口 $LEGACY_PORT)"
    echo ""
fi

# 启动 Node sidecar (如果需要)
if [ "$START_NODE" = true ]; then
    echo "启动 Node legacy sidecar..."
    PORT=$LEGACY_PORT npm run server:low-memory &
    NODE_PID=$!
    echo "✓ Node sidecar 启动 (PID: $NODE_PID, 端口: $LEGACY_PORT)"
    echo ""

    # 等待 Node 启动
    sleep 2

    # 设置 Go 的 legacy proxy URL
    export LEGACY_BASE_URL="http://localhost:$LEGACY_PORT"
fi

# 启动 Go 后端
echo "启动 Go 后端..."
echo ""
echo "==================================="
echo ""

cd backend-go
./api-monitor.exe

# 清理
if [ "$START_NODE" = true ] && [ ! -z "$NODE_PID" ]; then
    echo ""
    echo "正在停止 Node sidecar..."
    kill $NODE_PID 2>/dev/null
fi
