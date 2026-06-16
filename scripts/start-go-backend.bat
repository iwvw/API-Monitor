@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

echo ========================================
echo   API Monitor Go Backend Starter
echo ========================================
echo.

REM 检查端口占用
echo [1/4] 检查端口 3000...
for /f "tokens=5" %%a in ('netstat -ano ^| findstr :3000 ^| findstr LISTENING') do (
    set pid=%%a
    echo 检测到进程 !pid! 占用端口 3000，正在停止...
    taskkill /F /PID !pid! >nul 2>&1
    timeout /t 1 /nobreak >nul
)

REM 检查二进制文件
echo [2/4] 检查 Go 程序...
if not exist "backend-go\api-monitor.exe" (
    echo 未找到 api-monitor.exe，开始编译...
    cd backend-go
    go build -o api-monitor.exe .\cmd\api-monitor
    if errorlevel 1 (
        echo 编译失败！
        pause
        exit /b 1
    )
    cd ..
)

REM 设置环境变量
echo [3/4] 配置环境变量...
set PORT=3000
set DATA_DIR=./data
set LOG_LEVEL=INFO
set NODE_ENV=production

REM 启动服务
echo [4/4] 启动 Go 后端...
echo.
echo ----------------------------------------
echo   服务地址: http://localhost:3000
echo   健康检查: http://localhost:3000/health
echo   按 Ctrl+C 停止服务
echo ----------------------------------------
echo.

cd backend-go
api-monitor.exe

pause
