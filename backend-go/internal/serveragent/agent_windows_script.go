package serveragent

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// getWindowsAgentInstallScript generates the Windows Agent install script.
func (s *Service) getWindowsAgentInstallScript(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string, agentKey string) {
	var name, host string
	var port int
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port FROM server_accounts WHERE id = ?", accountID).Scan(&name, &host, &port)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "Account not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	storedKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}
	if agentKey != storedKey {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}

	proto, serverURL := resolveInstallOrigin(r)

	script := fmt.Sprintf(`# API Monitor Agent - Windows install script
# Host: %s (%s:%d)
# Generated at: %s

$ErrorActionPreference = "Stop"

$AGENT_VERSION = "latest"
$INSTALL_DIR = "$env:ProgramFiles\APIMonitorAgent"
$SERVER_URL = "%s://%s"
$SERVER_ID = "%s"
$AGENT_KEY = "%s"

Write-Host "Installing API Monitor Agent..."
Write-Host "Target host: %s"
Write-Host "Server: $SERVER_URL"

if (!(Test-Path $INSTALL_DIR)) {
    New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
}

$AGENT_URL = "$SERVER_URL/agent/agent-windows-amd64.exe"
$AGENT_PATH = "$INSTALL_DIRpi-monitor-agent.exe"

Write-Host "Downloading Agent..."

$oldProcess = Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue
if ($oldProcess) {
    Write-Host "Detected a running Agent process, stopping it..."
    Stop-Process -Name "api-monitor-agent" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

try {
    [System.Net.ServicePointManager]::ServerCertificateValidationCallback = {$true}
    Invoke-WebRequest -Uri $AGENT_URL -OutFile $AGENT_PATH -UseBasicParsing
    Write-Host "Agent download completed"
} catch {
    Write-Host "Error: failed to download Agent binary"
    Write-Host "URL: $AGENT_URL"
    Write-Host "Details: $_"
    exit 1
}

Write-Host "Creating launcher script..."
$VBS_PATH = "$INSTALL_DIR\launch.vbs"
$VBS_CONTENT = @"
Set WshShell = CreateObject("WScript.Shell")
WshShell.Run """$AGENT_PATH"" -s ""$SERVER_URL"" --id ""$SERVER_ID"" -k ""$AGENT_KEY"" -b", 0, False
"@

Set-Content -Path $VBS_PATH -Value $VBS_CONTENT -Encoding ASCII

Write-Host "Configuring startup..."
$REG_PATH = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$REG_NAME = "APIMonitorAgent"
$REG_VALUE = "wscript.exe ""$VBS_PATH"""

Set-ItemProperty -Path $REG_PATH -Name $REG_NAME -Value $REG_VALUE -Force

Write-Host "Starting Agent..."
Start-Process -FilePath "wscript.exe" -ArgumentList """$VBS_PATH""" -WindowStyle Hidden

Write-Host ""
Write-Host "======================================"
Write-Host "Agent installation completed."
Write-Host "======================================"
Write-Host ""
Write-Host "Mode: User startup entry"
Write-Host "Install dir: $INSTALL_DIR"
Write-Host "Launcher: $VBS_PATH"
Write-Host "Server ID: $SERVER_ID"
Write-Host ""
Write-Host "Agent is now running in the background"
Write-Host ""
Write-Host "Uninstall:"
Write-Host "  1. Run: reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v APIMonitorAgent /f"
Write-Host "  2. Delete: $INSTALL_DIR"
Write-Host ""
`,
		name, host, port,
		time.Now().Format("2006-01-02 15:04:05"),
		proto, serverURL,
		accountID,
		agentKey,
		name,
	)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%s.ps1", accountID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}
