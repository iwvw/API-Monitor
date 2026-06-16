@echo off
setlocal

echo ========================================
echo   API Monitor Go Backend Starter
echo ========================================
echo.

REM Check port 3000
echo [1/4] Checking port 3000...
for /f "tokens=5" %%a in ('netstat -ano ^| findstr :3000 ^| findstr LISTENING') do (
    echo Stopping process %%a on port 3000...
    taskkill /F /PID %%a >nul 2>&1
    timeout /t 1 /nobreak >nul
)

REM Check binary
echo [2/4] Checking Go binary...
if not exist "backend-go\api-monitor.exe" (
    echo Building Go backend...
    cd backend-go
    go build -o api-monitor.exe .\cmd\api-monitor
    if errorlevel 1 (
        echo Build failed!
        pause
        exit /b 1
    )
    cd ..
)

REM Set environment
echo [3/4] Setting environment...
set PORT=3000
set DATA_DIR=./data
set LOG_LEVEL=INFO

REM Start server
echo [4/4] Starting Go backend...
echo.
echo ----------------------------------------
echo   URL: http://localhost:3000
echo   Health: http://localhost:3000/health
echo   Press Ctrl+C to stop
echo ----------------------------------------
echo.

cd backend-go
api-monitor.exe

pause
