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
		Confirm bool   `json:"confirm"` // 危险命令二次确认（敏感模式下软拦截要求）
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

	// 危险命令门控（对原始明文检测，防 base64 绕过）。判定依据调用来源：
	//   full / 管理 AI 完全批准 → 放行
	//   sensitive（敏感模式）   → 软拦截：未显式确认时返回确认提示，确认后放行
	//   standard（标准模式）及其他来源 → 硬拦截
	danger := DetectDangerousCommand(command)
	if danger.Dangerous {
		switch s.dangerousCommandDecision(r) {
		case dangerousAllow:
			// 放行
		case dangerousConfirm:
			if !req.Confirm {
				response.JSON(w, http.StatusOK, map[string]interface{}{
					"success":               false,
					"requires_confirmation": true,
					"dangerous":             true,
					"dangerReasons":         danger.Reasons,
					"error":                 "该命令被判定为危险操作（" + strings.Join(danger.Reasons, "、") + "）。当前为敏感模式：如确认执行，请带 confirm=true 重新调用；如不需要，请改用更安全的命令。",
				})
				return
			}
		default:
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{
				"success":       false,
				"error":         "dangerous command rejected: " + strings.Join(danger.Reasons, ", "),
				"dangerous":     true,
				"dangerReasons": danger.Reasons,
			})
			return
		}
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

	// 失败时把实际输出塞进 error，避免 AI 工具层 EnvelopeError 只看到 info 为空的
	// "success:false" 而误判为「未知业务错误」，导致无法诊断命令为何失败。
	resp := map[string]interface{}{
		"success": status == "success",
		"output":  output,
		"task_id": task.ID,
		"status":  status,
	}
	if status != "success" {
		reason := strings.TrimSpace(output)
		if reason == "" {
			reason = "命令在目标主机上执行失败，未返回输出"
		}
		if len([]rune(reason)) > 1000 {
			reason = string([]rune(reason)[:1000])
		}
		resp["error"] = "命令执行失败: " + reason
	}
	response.JSON(w, http.StatusOK, resp)
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
	// -OutputFormat Text 抑制非交互场景下 PowerShell 把 Error/Progress 流序列化成
	// CLIXML XML（即返回里出现的 "#< CLIXML" 噪声），让 stdout/stderr 回到纯文本。
	return "powershell -NoProfile -NonInteractive -OutputFormat Text -ExecutionPolicy Bypass -EncodedCommand " + encoded
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

// dangerousDecision 危险命令的处理决策。
type dangerousDecision int

const (
	dangerousReject  dangerousDecision = iota // 硬拦截
	dangerousConfirm                          // 软拦截：需调用方显式确认
	dangerousAllow                            // 直接放行
)

// dangerousCommandDecision 依据调用来源决定危险命令的处理方式。
// 判定优先级：管理 AI 完全批准 > 会话内 AI 接入权限模式 > 默认硬拦截。
// 权限模式标记只能由服务端内部（server/ai_caller.go）注入到 request context，
// 外部 HTTP 请求无法伪造，因此不会成为绕过拦截的路径。
func (s *Service) dangerousCommandDecision(r *http.Request) dangerousDecision {
	if AdminAIFullApprove(r.Context()) {
		return dangerousAllow
	}
	switch AIPolicyMode(r.Context()) {
	case aiPolicyFull:
		return dangerousAllow
	case aiPolicySensitive:
		return dangerousConfirm
	default:
		return dangerousReject
	}
}

// allowDangerousFromAdminAI 保留旧语义：仅「管理 AI 完全批准」时放行危险命令。
func (s *Service) allowDangerousFromAdminAI(r *http.Request) bool {
	return AdminAIFullApprove(r.Context())
}

// adminAIFullApproveKey 是内部只读标记的 context key 类型，避免与外部冲突。
type adminAIFullApproveKey struct{}

// WithAdminAIFullApprove 返回带「管理 AI 完全批准」标记的 context。
// 只在服务端内部（server/ai_caller.go 处理内部 AI 调用）使用，
// 普通 HTTP 请求不可能携带该 context 值。
func WithAdminAIFullApprove(ctx context.Context) context.Context {
	return context.WithValue(ctx, adminAIFullApproveKey{}, true)
}

// AdminAIFullApprove 判断 context 是否携带「管理 AI 完全批准」内部标记。
func AdminAIFullApprove(ctx context.Context) bool {
	v, _ := ctx.Value(adminAIFullApproveKey{}).(bool)
	return v
}

// aiPolicyMode 是 AI 接入权限模式在本模块的镜像值，避免反向依赖 system 包。
type aiPolicyMode string

const (
	aiPolicyMinimal   aiPolicyMode = "minimal"
	aiPolicySensitive aiPolicyMode = "sensitive"
	aiPolicyStandard  aiPolicyMode = "standard"
	aiPolicyFull      aiPolicyMode = "full"
)

// aiPolicyModeKey 是 AI 接入权限模式的 context key 类型。
type aiPolicyModeKey struct{}

// WithAIPolicyMode 把当前 AI 接入权限模式写入 context。仅由服务端内部
// （server/ai_caller.go）在转发 AI 调用时注入；外部请求无法伪造。
func WithAIPolicyMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, aiPolicyModeKey{}, aiPolicyMode(mode))
}

// AIPolicyMode 读取 context 中的 AI 接入权限模式，缺省视为 standard（最保守）。
func AIPolicyMode(ctx context.Context) aiPolicyMode {
	if v, ok := ctx.Value(aiPolicyModeKey{}).(aiPolicyMode); ok {
		return v
	}
	return aiPolicyStandard
}
