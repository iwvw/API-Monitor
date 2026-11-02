package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// summarizeToolResult 把工具返回值序列化为紧凑 JSON 文本（供 LLM 读取）。
// 超长结果走 trimToolJSON 智能压缩（保标识字段 + 截断标注），不给模型残缺 JSON。
func summarizeToolResult(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return truncateContent(s)
	}
	text := toJSONText(v)
	if len(text) > contentSizeLimit {
		return trimToolJSON(v)
	}
	return text
}

// trimToolJSON 把超长 JSON 结果压到 contentSizeLimit×3/4 预算内（Anthropic 工具
// 结果治理原则：截断必须显式标注，且保留模型下轮调用所需的 id/name 标识字段）：
// 顶层数组按序保留若干完整条目并标注总条数，条目内大对象由 compactToolEntry 压缩；
// 顶层对象逐字段压缩；仍超预算才字符截断（此时 JSON 残缺，但标注可让模型知道要缩小范围）。
// 只应在结果超预算时调用；输出始终是完整可读文本，不会超预算太多。
func trimToolJSON(v interface{}) string {
	budget := contentSizeLimit * 3 / 4
	switch t := v.(type) {
	case []interface{}:
		total := len(t)
		if total == 0 {
			return "[]"
		}
		var kept []interface{}
		size := 0
		for _, it := range t {
			entry := compactToolEntry(it)
			b, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			if len(kept) > 0 && size+len(b) > budget {
				break
			}
			kept = append(kept, entry)
			size += len(b)
			if size > budget {
				break
			}
		}
		out, _ := json.Marshal(kept)
		if len(kept) < total {
			return string(out) + fmt.Sprintf("\n[已截断：共 %d 条，仅保留前 %d 条（条目含 id/name 标识，可直接作为后续调用的过滤/定位参数）；需要完整数据时可用筛选参数缩小范围]", total, len(kept))
		}
		return string(out)
	case map[string]interface{}:
		compressed := make(map[string]interface{}, len(t))
		for k, val := range t {
			compressed[k] = compactToolEntry(val)
		}
		if b, err := json.Marshal(compressed); err == nil && len(b) <= budget {
			return string(b)
		}
		// 压缩后仍超预算：只保留顶层标识键（id/name…）并显式标注，不让字符截断把标识切掉。
		top := compactToolEntry(t)
		b, _ := json.Marshal(top)
		return string(b) + "\n[已截断：结果过大，仅保留顶层标识字段]"
	}
	return truncateContent(toJSONText(v))
}

// compactToolEntry 压缩单个结果条目：对象保留 id/ID/name/host/type/status 等标识键
// 完整不动（模型下轮要靠它们定位资源），其余字段被省略并显式标注；非对象原样返回。
func compactToolEntry(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := make(map[string]interface{}, 6)
	for _, k := range []string{"id", "ID", "name", "host", "type", "status"} {
		if val, has := m[k]; has {
			out[k] = val
		}
	}
	if len(out) != len(m) {
		out["_truncated"] = "该条大量字段已省略"
	}
	return out
}

// toJSONText 尽力把任意值序列化为 JSON，失败时回退 fmt 文本。
func toJSONText(v interface{}) string {
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// toolErrorHint 给工具失败回喂附加修正引导（与 retryableToolError 的关键词判定解耦，
// 只影响进入 LLM 上下文/审计的 summary，不影响失败重试逻辑）。审批类等待是正常流程，
// 不加引导；参数类错误给出可执行动作，避免模型用相同参数盲目重试。
func toolErrorHint(toolName string, args map[string]interface{}, msg string) string {
	if msg == "" {
		return msg
	}
	if strings.Contains(msg, "审批") || strings.Contains(msg, "未启用") {
		return msg
	}
	hint := ""
	switch toolName {
	case "call_api":
		// 上游 ai_caller 的错误建议对「外部 MCP 客户端」写的是 find_api，
		// 但本引擎没有该工具：显式澄清，避免模型照着不存在的工具继续绕路。
		if strings.Contains(msg, "find_api") {
			msg += "（说明：本环境没有 find_api 工具，请忽略该建议；直接对照系统提示词内置的接口清单选择真实路径与方法，禁止猜测路径）"
		}
		switch {
		case strings.Contains(msg, "unsupported action"):
			// 主机预设动作接口不支持任意命令：比 HTTP 4 分支更具体，
			// 必须在 HTTP 4 判定之前命中（真实错误文本形如
			// "unsupported action (HTTP 400)"）。直接在引导中点名正确
			// 通道，避免模型在多个候选接口间反复试错（审计实证：
			// taskkill 在 /api/server/action 上撞墙多轮才放弃）。
			hint = "该接口仅支持预设动作（reboot/restart/shutdown），不支持任意命令或进程操作；执行任意命令（如关闭进程、修改配置）请改用 POST /api/server/agent/command/{id} 向在线 Agent 下发命令，一次发一条命令并携带 timeout"
		case strings.Contains(msg, "HTTP 4"):
			hint = "请求未通过校验：先调用 get_route 读取该接口契约（路径参数/请求体字段类型、必填项、枚举），修正后重试；确认 path 里的大括号参数已替换为真实值；若返回是路径不存在（404），请对照系统提示词中的接口清单改用真实路径，不要继续猜测或拼凑路径"
		case strings.Contains(msg, "HTTP 5"), strings.Contains(msg, "连接"), strings.Contains(msg, "超时"):
			hint = "服务端临时故障：可稍后重试，或换用同类聚合/状态接口确认结果"
		case strings.Contains(msg, "业务失败"):
			hint = "业务校验未通过：按错误信息修正参数（如 cron 需 5 段表达式、资源已存在/不存在、余额或配额不足），必要时先读一次对应资源状态再操作"
		}
	case "unknown tool":
		hint = "工具名不存在：请改用系统提示词内置清单中的工具名"
	default:
		if strings.Contains(msg, "未知工具") {
			hint = "工具名不存在：请改用系统提示词内置清单中的工具名"
		} else {
			hint = "请检查参数是否完整且符合工具 schema（必填项、类型、枚举）后重试；不确定时先调用 get_route 读取契约"
		}
	}
	if hint == "" {
		return msg
	}
	return msg + "\n[修正引导] " + hint
}

// aiCallResult 把 AICallResponse 转换为模型友好的工具结果：
// 非 2xx 视为错误；成功时优先返回解码后的 JSON Body，其次为原始文本。
func aiCallResult(resp systemmetrics.AICallResponse) (interface{}, error) {
	if resp.StatusCode >= 400 {
		raw := resp.Raw
		if raw == "" && resp.Body != nil {
			if b, err := json.Marshal(resp.Body); err == nil {
				raw = string(b)
			}
		}
		return nil, fmt.Errorf("接口返回 HTTP %d: %s", resp.StatusCode, truncateContent(raw))
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if businessErr := systemmetrics.EnvelopeError(resp.Body); businessErr != "" {
			return nil, fmt.Errorf("接口返回 HTTP %d 但业务失败: %s", resp.StatusCode, businessErr)
		}
	}
	if resp.Body != nil {
		return resp.Body, nil
	}
	return resp.Raw, nil
}

// isSessionWriteEnabled 读取会话级写授权标记（“允许此对话”授予后为 1）。
// 带时效：write_enabled_until 早于当前时间时视为未授权并幂等清零
// （admin_ai_session_write_ttl_hours 控制；0=不自动过期）。
func (s *Service) isSessionWriteEnabled(ctx context.Context, db *sql.DB, sessionID string) (bool, error) {
	var value int
	var until string
	err := db.QueryRowContext(ctx, "SELECT write_enabled, COALESCE(write_enabled_until,'') FROM admin_ai_sessions WHERE id = ?", sessionID).Scan(&value, &until)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if value != 1 {
		return false, nil
	}
	if until != "" {
		if deadline, perr := time.Parse(time.RFC3339, until); perr == nil && time.Now().After(deadline) {
			_, _ = db.ExecContext(ctx, "UPDATE admin_ai_sessions SET write_enabled = 0, write_enabled_until = '' WHERE id = ?", sessionID)
			return false, nil
		}
	}
	return true, nil
}
