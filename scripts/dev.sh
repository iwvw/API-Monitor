#!/bin/bash

echo "========================================"
echo "  API Monitor 开发环境一键启动"
echo "========================================"
echo ""

# 检查 Go 后端是否已构建
if [ ! -f "backend-go/api-monitor" ]; then
    echo "[1/3] 首次运行，构建 Go 后端..."
    cd backend-go
    go build -o api-monitor ./cmd/api-monitor
    if [ $? -ne 0 ]; then
        echo "构建失败！"
        exit 1
    fi
    cd ..
    echo "构建完成！"
    echo ""
fi

# 启动 Go 后端（后台）
echo "[2/3] 启动 Go 后端 (http://localhost:3000)..."
./backend-go/api-monitor &
BACKEND_PID=$!
sleep 2

# 启动前端开发服务器
echo "[3/3] 启动前端开发服务器 (http://localhost:5173)..."
echo ""
echo "========================================"
echo "  开发环境已启动！"
echo "  前端: http://localhost:5173"
echo "  后端: http://localhost:3000"
echo "  按 Ctrl+C 停止所有服务"
echo "========================================"
echo ""

# 设置退出时清理
trap "echo ''; echo '正在停止后台服务...'; kill $BACKEND_PID 2>/dev/null; exit" INT TERM

npm run dev

# 清理后台进程
kill $BACKEND_PID 2>/dev/null
