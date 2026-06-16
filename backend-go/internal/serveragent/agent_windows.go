package serveragent

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// getWindowsAgentInstallScript 生成 Windows Agent 安装脚本
func (s *Service) getWindowsAgentInstallScript(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string, agentKey string) {
	// 查询账号信息
	var name, host string
	var port int
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port FROM server_accounts WHERE id = ?", accountID).Scan(&name, &host, &port)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 验证 Agent Key（可选，用于额外安全性）
	storedKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	if agentKey != storedKey {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}

	// 获取服务器地址
	serverURL := r.Header.Get("X-Forwarded-Host")
	if serverURL == "" {
		serverURL = r.Host
	}

	// 判断协议
	proto := "https"
	if r.Header.Get("X-Forwarded-Proto") == "http" || strings.HasPrefix(serverURL, "localhost") || strings.HasPrefix(serverURL, "127.0.0.1") {
		proto = "http"
	}

	// 生成 PowerShell 安装脚本
	script := fmt.Sprintf(`# API Monitor Agent - Windows 安装脚本
# 主机: %s (%s:%d)
# 生成时间: %s

$ErrorActionPreference = "Stop"

$AGENT_VERSION = "latest"
$INSTALL_DIR = "$env:ProgramFiles\APIMonitorAgent"
$SERVER_URL = "%s://%s"
$SERVER_ID = "%s"
$AGENT_KEY = "%s"

Write-Host "正在安装 API Monitor Agent..."
Write-Host "目标主机: %s"
Write-Host "服务器: $SERVER_URL"

# 创建安装目录
if (!(Test-Path $INSTALL_DIR)) {
    New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
}

# 下载 Agent 程序
$AGENT_URL = "$SERVER_URL/agent/agent-windows-amd64.exe"
$AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.exe"

Write-Host "正在下载 Agent..."

# 停止可能正在运行的旧进程
$oldProcess = Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue
if ($oldProcess) {
    Write-Host "检测到正在运行的 Agent，正在停止..."
    Stop-Process -Name "api-monitor-agent" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

try {
    [System.Net.ServicePointManager]::ServerCertificateValidationCallback = {$true}
    Invoke-WebRequest -Uri $AGENT_URL -OutFile $AGENT_PATH -UseBasicParsing
    Write-Host "Agent 下载完成"
} catch {
    Write-Host "错误: 无法下载 Agent 程序"
    Write-Host "URL: $AGENT_URL"
    Write-Host "错误信息: $_"
    exit 1
}

# 创建启动脚本（隐藏窗口运行）
Write-Host "正在创建启动脚本..."
$VBS_PATH = "$INSTALL_DIR\launch.vbs"
$VBS_CONTENT = @"
Set WshShell = CreateObject("WScript.Shell")
WshShell.Run """$AGENT_PATH"" -s ""$SERVER_URL"" --id ""$SERVER_ID"" -k ""$AGENT_KEY"" -b", 0, False
"@

Set-Content -Path $VBS_PATH -Value $VBS_CONTENT -Encoding ASCII

# 添加到用户开机自启动（注册表）
Write-Host "正在设置开机自启动..."
$REG_PATH = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$REG_NAME = "APIMonitorAgent"
$REG_VALUE = "wscript.exe ""$VBS_PATH"""

Set-ItemProperty -Path $REG_PATH -Name $REG_NAME -Value $REG_VALUE -Force

# 立即启动 Agent
Write-Host "正在启动 Agent..."
Start-Process -FilePath "wscript.exe" -ArgumentList """$VBS_PATH""" -WindowStyle Hidden

Write-Host ""
Write-Host "======================================"
Write-Host "✓ Agent 安装完成！"
Write-Host "======================================"
Write-Host ""
Write-Host "安装方式: 用户级开机自启动"
Write-Host "安装目录: $INSTALL_DIR"
Write-Host "启动脚本: $VBS_PATH"
Write-Host "服务器ID: $SERVER_ID"
Write-Host ""
Write-Host "Agent 已在后台运行"
Write-Host ""
Write-Host "卸载方法:"
Write-Host "  1. 运行: reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v APIMonitorAgent /f"
Write-Host "  2. 删除: $INSTALL_DIR"
Write-Host ""
`,
		name, host, port,
		time.Now().Format("2006-01-02 15:04:05"),
		proto, serverURL,
		accountID,
		agentKey,
		name,
	)

	// 返回 PowerShell 脚本
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%s.ps1", accountID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}
