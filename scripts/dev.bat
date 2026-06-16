@echo off
setlocal enabledelayedexpansion

echo ========================================
echo   API Monitor 开发环境一键启动
echo ========================================
echo.

REM 检查 Go 后端是否已构建
if not exist "backend-go\api-monitor.exe" (
    echo [1/3] 首次运行，构建 Go 后端...
    cd backend-go
    go build -o api-monitor.exe .\cmd\api-monitor
    if errorlevel 1 (
        echo 构建失败！
        pause
        exit /b 1
    )
    cd ..
    echo 构建完成！
    echo.
)

REM 启动 Go 后端（后台）
echo [2/3] 启动 Go 后端 (http://localhost:3000)...
start /B "" backend-go\api-monitor.exe
timeout /t 2 /nobreak >nul

REM 启动前端开发服务器
echo [3/3] 启动前端开发服务器 (http://localhost:5173)...
echo.
echo ========================================
echo   开发环境已启动！
echo   前端: http://localhost:5173
echo   后端: http://localhost:3000
echo   按 Ctrl+C 停止所有服务
echo ========================================
echo.

npm run dev

REM 清理后台进程
echo.
echo 正在停止后台服务...
taskkill /F /IM api-monitor.exe >nul 2>&1

pause
