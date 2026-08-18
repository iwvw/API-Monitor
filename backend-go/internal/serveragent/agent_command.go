package serveragent

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// getAgentInstallCommand returns Agent install commands for the frontend modal.
func (s *Service) getAgentInstallCommand(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
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

	agentKey, err := s.getOrGenerateAgentKeyForServer(r.Context(), db, accountID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	proto, serverURL := s.resolveInstallOrigin(r.Context(), db, r, "")
	baseURL := fmt.Sprintf("%s://%s", proto, serverURL)
	installScriptURL := appendInstallProtocol(fmt.Sprintf("%s/api/server/agent/install/linux/%s/%s", baseURL, accountID, agentKey), proto)
	winInstallURL := appendInstallProtocol(fmt.Sprintf("%s/api/server/agent/install/win/%s/%s", baseURL, accountID, agentKey), proto)

	installCommand := fmt.Sprintf(`curl -fsSL %s | bash`, installScriptURL)
	winInstallCommand := fmt.Sprintf(`powershell -c "irm %s | iex"`, winInstallURL)
	manualCommand := fmt.Sprintf(`# Download and run Agent
wget %s -O install-agent.sh
chmod +x install-agent.sh
sudo ./install-agent.sh`, installScriptURL)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"serverName":        name,
			"serverHost":        host,
			"serverPort":        port,
			"serverId":          accountID,
			"agentKey":          agentKey,
			"baseUrl":           baseURL,
			"apiUrl":            baseURL,
			"installScriptUrl":  installScriptURL,
			"installCommand":    installCommand,
			"winInstallCommand": winInstallCommand,
			"manualCommand":     manualCommand,
			"curlCommand":       installCommand,
			"timestamp":         time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}

// handleAgentExecCommand POST /api/server/agent/command/{id} 向在线 Agent 下发
// shell 命令并同步等待执行结果。复用 RUN_COMMAND（type=1）任务链路与危险命令检测。
func (s *Service) handleAgentExecCommand(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"` // 秒，可选
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		response.Error(w, http.StatusBadRequest, "command required")
		return
	}

	// 危险命令拦截（对原始明文检测，防 base64 绕过）
	danger := DetectDangerousCommand(command)
	if danger.Dangerous {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{
			"success":       false,
			"error":         "dangerous command rejected: " + strings.Join(danger.Reasons, ", "),
			"dangerous":     true,
			"dangerReasons": danger.Reasons,
		})
		return
	}

	// PowerShell 命令改写为 -EncodedCommand 形式：Agent 端经 cmd /C 执行，
	// base64 只含字母数字，可避免 cmd 解析管道符 / 引号导致命令被拆坏。
	command = normalizeExecCommand(command)

	// 超时：默认 30s，上限 300s（先钳制再换算，防止巨大值溢出成负 duration）
	timeout := defaultExecTimeout
	if req.Timeout > 0 {
		seconds := int64(req.Timeout)
		if seconds > int64(maxExecTimeout/time.Second) {
			seconds = int64(maxExecTimeout / time.Second)
		}
		timeout = time.Duration(seconds) * time.Second
	}

	conn, online := s.registry.Get(serverID)
	if !online {
		response.Error(w, http.StatusBadGateway, "agent offline: "+serverID)
		return
	}

	task := s.taskRegistry.Create(serverID, "shell", command)
	eventCh := task.Subscribe()

	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    1, // RUN_COMMAND
		"data":    command,
		"timeout": int(timeout.Seconds()),
	}); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	output, status, timedOut := waitAgentTaskResult(s.taskRegistry, task, eventCh, timeout)
	if timedOut {
		// 任务已下发且不可取消：超时后 Agent 端命令仍会继续执行到完成，
		// 记录超时历史并提示调用方用 task_id 跟踪后续终态。
		s.recordExecCommandHistory(r.Context(), db, serverID, command, "timeout", "task timeout after "+timeout.String())
		response.JSON(w, http.StatusGatewayTimeout, map[string]interface{}{
			"success": false,
			"error":   "task timeout after " + timeout.String() + "；命令可能仍在 Agent 上执行，可凭 task_id 查询任务终态",
			"task_id": task.ID,
		})
		return
	}

	// 记录命令历史（复用片段历史写入逻辑，executionMode=api）
	s.recordExecCommandHistory(r.Context(), db, serverID, command, status, output)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": status == "success",
		"output":  output,
		"task_id": task.ID,
		"status":  status,
	})
}

// recordExecCommandHistory 将 API 发起的命令执行写入命令历史表
func (s *Service) recordExecCommandHistory(ctx context.Context, db *sql.DB, serverID, command, status, output string) {
	summary := output
	if len(summary) > 500 {
		// 按 rune 截断，避免从多字节 UTF-8 字符中间切断产生乱码
		runes := []rune(summary)
		summary = string(runes[:500])
	}
	_, _, _ = s.insertSnippetHistory(ctx, db, nil, &serverID, command, command, "api", status, &summary)
}

// normalizeExecCommand 将 PowerShell 前缀命令改写为 -EncodedCommand 形式。
// Agent 端（agent-rust execute_command）在 Windows 上经 cmd /C 执行命令，
// cmd 会解析管道符 / 引号等元字符导致复杂 PowerShell 命令被拆坏。
// -EncodedCommand 的参数是纯 base64（仅字母数字），cmd 无法拆解，
// PowerShell 解码后按原生语义执行，管道与引号完整生效。
// 非 PowerShell 命令（cmd 内建等）原样返回，不影响既有行为。
func normalizeExecCommand(command string) string {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)

	rest := ""
	matched := false
	for _, p := range []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"} {
		if lower == p {
			rest = ""
			matched = true
			break
		}
		if strings.HasPrefix(lower, p+" ") {
			rest = strings.TrimSpace(trimmed[len(p):])
			matched = true
			break
		}
	}
	if !matched {
		return command
	}

	script, ok := extractPowerShellScript(rest)
	if !ok {
		return command
	}
	encoded := base64.StdEncoding.EncodeToString(utf16LEBytes(script))
	return "powershell -NoProfile -ExecutionPolicy Bypass -EncodedCommand " + encoded
}

// extractPowerShellScript 提取 powershell -Command/-c 之后的脚本内容。
// 返回 false 表示不存在 -Command 参数（如 -File 调用），交由原样执行。
func extractPowerShellScript(rest string) (string, bool) {
	fields := strings.Fields(rest)
	idx := -1
	for i, f := range fields {
		lf := strings.ToLower(f)
		if lf == "-command" || lf == "-c" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(fields) {
		return "", false
	}
	script := strings.Join(fields[idx+1:], " ")

	// 若脚本整体被一对引号包裹（如 -Command "..."），剥掉外壳引号，
	// 避免 PowerShell 把带引号的字符串当作命令名执行。
	if strings.HasPrefix(script, `"`) {
		if closeIdx := strings.Index(script[1:], `"`); closeIdx >= 0 {
			closePos := closeIdx + 1
			if strings.TrimSpace(script[closePos+1:]) == "" {
				script = script[1:closePos]
			}
		}
	}
	script = strings.TrimSpace(script)
	if script == "" {
		return "", false
	}
	return script, true
}

// utf16LEBytes 将字符串编码为 UTF-16LE 字节（PowerShell -EncodedCommand 要求）。
func utf16LEBytes(s string) []byte {
	runes := []rune(s)
	out := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		if r > 0xFFFF {
			r1, r2 := utf16.EncodeRune(r)
			out = append(out, byte(r1), byte(r1>>8), byte(r2), byte(r2>>8))
		} else {
			out = append(out, byte(r), byte(r>>8))
		}
	}
	return out
}
