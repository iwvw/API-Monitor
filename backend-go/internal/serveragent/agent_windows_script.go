package serveragent

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
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

	storedKey, err := s.getOrGenerateAgentKeyForServer(r.Context(), db, accountID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}
	if agentKey != storedKey {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}

	proto, serverURL := s.resolveInstallOrigin(r.Context(), db, r, "")
	serverBaseURL := fmt.Sprintf("%s://%s", proto, serverURL)
	agentDownloadBaseURL := s.resolveAgentDownloadBaseURL(r.Context(), db, serverBaseURL)
	installScriptURL := fmt.Sprintf("%s/api/server/agent/install/win/%s/%s?protocol=%s&base_url=%s", serverBaseURL, accountID, agentKey, proto, url.QueryEscape(serverBaseURL))

	script := fmt.Sprintf(`# API Monitor Agent - Windows install script
# Host: %s (%s:%d)
# Generated at: %s

$ErrorActionPreference = "Stop"

$AGENT_VERSION = "latest"
$INSTALL_DIR = "$env:ProgramFiles\APIMonitorAgent"
$LOG_DIR = "$env:ProgramData\APIMonitorAgent"
$INSTALL_LOG = "$LOG_DIR\install.log"
$DISPATCH_LOG = "$LOG_DIR\detached.log"
$SERVER_URL = "%s://%s"
$AGENT_DOWNLOAD_BASE_URL = "%s"
$SERVER_ID = "%s"
$AGENT_KEY = "%s"
$INSTALL_SCRIPT_URL = "%s"

Write-Host "Installing API Monitor Agent..."
Write-Host "Target host: %s"
Write-Host "Server: $SERVER_URL"

# A script launched inside the Agent terminal must outlive the Agent process
# that it is about to replace. Start a hidden, independent PowerShell updater
# before stopping the current Agent, then return from the terminal-bound copy.
# The detached process writes its own transcript so a failure is diagnosable
# instead of being silently swallowed.
if ($env:API_MONITOR_AGENT_INSTALL_DETACHED -ne "1" -and (Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue)) {
    Write-Host "Detected a running Agent process. Scheduling detached installer..."
    if (!(Test-Path $LOG_DIR)) {
        New-Item -ItemType Directory -Path $LOG_DIR -Force | Out-Null
    }
    $DETACHED_COMMAND = '$env:API_MONITOR_AGENT_INSTALL_DETACHED = "1"; $__src = $null; try { $__src = irm "' + $INSTALL_SCRIPT_URL + '" } catch { Write-Output ("Install failed while fetching the installer: " + $_.Exception.Message); exit 1 }; if ($__src) { $__src | iex }'
    $ENCODED_COMMAND = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($DETACHED_COMMAND))
    # Redirect the detached process's own stdout/stderr to the log dir. This covers
    # failures that happen *before* the fetched script can start its own transcript
    # (e.g. the server is unreachable), which would otherwise leave no trace at all.
    Start-Process -FilePath "powershell.exe" -ArgumentList "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", $ENCODED_COMMAND -WindowStyle Hidden -RedirectStandardOutput $DISPATCH_LOG -RedirectStandardError "$DISPATCH_LOG.err"
    Write-Host "Detached installer scheduled. The Agent terminal may disconnect; installation will continue in the background."
    Write-Host "Install log: $INSTALL_LOG"
    Write-Host "If the Agent does not come back online, open the log above (and $DISPATCH_LOG) for the failure reason."
    return
}

# Detached run: mirror all output to a log file so silent failures are visible
# (the parent process has already exited and cannot report the child's exit code).
if ($env:API_MONITOR_AGENT_INSTALL_DETACHED -eq "1") {
    if (!(Test-Path $LOG_DIR)) {
        New-Item -ItemType Directory -Path $LOG_DIR -Force | Out-Null
    }
    try {
        Start-Transcript -Path $INSTALL_LOG -Force | Out-Null
    } catch {
        # Transcript is best-effort; never block installation on logging.
    }
}

if (!(Test-Path $INSTALL_DIR)) {
    New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
}

$AGENT_URL = "$AGENT_DOWNLOAD_BASE_URL/agent-windows-amd64.exe"
$AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.exe"
# 每次安装用唯一临时名：固定名会被上一轮残留的安装器/旧进程占用，导致
# “being used by another process” 反复失败且无法自愈。
$TEMP_AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.$([Guid]::NewGuid().ToString('N')).download.exe"
$CONFIG_PATH = "$INSTALL_DIR\config.json"

Write-Host "Downloading Agent..."

$oldProcess = Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue
if ($oldProcess) {
    Write-Host "Detected a running Agent process, stopping it..."
    Stop-Process -Name "api-monitor-agent" -Force -ErrorAction SilentlyContinue

    $stopped = $false
    for ($i = 0; $i -lt 10; $i++) {
        Start-Sleep -Milliseconds 500
        if (!(Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue)) {
            $stopped = $true
            break
        }
    }

    # Stop-Process can fail to terminate an Agent whose handle is held elsewhere
    # (e.g. a stale process object or an elevated session). Fall back to taskkill
    # before giving up, so an upgrade is not silently blocked.
    if (!$stopped) {
        Write-Host "Stop-Process did not terminate the Agent; retrying with taskkill..."
        foreach ($p in @(Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue)) {
            & taskkill.exe /F /T /PID $p.Id 2>&1 | Out-Host
        }
        for ($i = 0; $i -lt 10; $i++) {
            Start-Sleep -Milliseconds 500
            if (!(Get-Process -Name "api-monitor-agent" -ErrorAction SilentlyContinue)) {
                $stopped = $true
                break
            }
        }
    }

    if (!$stopped) {
        Write-Host "Error: existing Agent process is still running"
        Write-Host "Please stop it manually (Task Manager -> api-monitor-agent) and re-run the installer."
        exit 1
    }
}

# 清掉历史遗留的临时下载文件（可能是旧版本的固定名或本版本中断留下的）。
Get-ChildItem -Path $INSTALL_DIR -Filter "api-monitor-agent*.download.exe" -ErrorAction SilentlyContinue |
    Remove-Item -Force -ErrorAction SilentlyContinue
# 清掉历史升级备份二进制（.prev.exe / .previous），避免安装目录长期堆积。
Get-ChildItem -Path $INSTALL_DIR -Filter "api-monitor-agent*.prev.exe" -ErrorAction SilentlyContinue |
    Remove-Item -Force -ErrorAction SilentlyContinue
Get-ChildItem -Path $INSTALL_DIR -Filter "api-monitor-agent*.previous" -ErrorAction SilentlyContinue |
    Remove-Item -Force -ErrorAction SilentlyContinue

if (Test-Path $CONFIG_PATH) {
    Write-Host "Removing old Agent config..."
    Remove-Item -Path $CONFIG_PATH -Force -ErrorAction SilentlyContinue
}

try {
    Invoke-WebRequest -Uri $AGENT_URL -OutFile $TEMP_AGENT_PATH -UseBasicParsing

    $DOWNLOADED_VERSION = (& $TEMP_AGENT_PATH --version 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $DOWNLOADED_VERSION -notmatch '^api-monitor-agent\s+\S+$') {
        throw "Downloaded file is not a valid API Monitor Agent binary: $DOWNLOADED_VERSION"
    }

    if (Test-Path $AGENT_PATH) {
        Remove-Item -Path $AGENT_PATH -Force
    }

    Move-Item -Path $TEMP_AGENT_PATH -Destination $AGENT_PATH -Force
    Write-Host "Agent download completed: $DOWNLOADED_VERSION"
} catch {
    Write-Host "Error: failed to download Agent binary"
    Write-Host "URL: $AGENT_URL"
    Write-Host "Details: $_"
    Remove-Item -Path $TEMP_AGENT_PATH -Force -ErrorAction SilentlyContinue
    exit 1
}

Write-Host "Creating launcher script..."
$VBS_PATH = "$INSTALL_DIR\launch.vbs"
$FILE_ROOTS_VALUE = "%s"
$VBS_CONTENT = @"
Set WshShell = CreateObject("WScript.Shell")
Set WshEnv = WshShell.Environment("PROCESS")
If "$FILE_ROOTS_VALUE" <> "" Then WshEnv("API_MONITOR_FILE_ROOTS") = "$FILE_ROOTS_VALUE"
WshShell.Run """$AGENT_PATH"" -s ""$SERVER_URL"" --id ""$SERVER_ID"" -k ""$AGENT_KEY"" -b", 0, False
"@

Set-Content -Path $VBS_PATH -Value $VBS_CONTENT -Encoding ASCII

Write-Host "Writing Agent config..."
$CONFIG_CONTENT = @"
{
  "agentKey": "$AGENT_KEY",
  "debug": false,
  "reportInterval": 1500,
  "serverId": "$SERVER_ID",
  "serverUrl": "$SERVER_URL"
}
"@

Set-Content -Path $CONFIG_PATH -Value $CONFIG_CONTENT -Encoding ASCII

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

if ($env:API_MONITOR_AGENT_INSTALL_DETACHED -eq "1") {
    try { Stop-Transcript | Out-Null } catch { }
}
`,
		name, host, port,
		time.Now().Format("2006-01-02 15:04:05"),
		proto, serverURL,
		agentDownloadBaseURL,
		accountID,
		agentKey,
		installScriptURL,
		name,
		s.agentFileRootsEnvValue(true),
	)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%s.ps1", accountID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}
