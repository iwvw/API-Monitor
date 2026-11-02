package serveragent

import (
	"strconv"
	"strings"
)

// minAgentFileRealPathVersion 是第一个支持文件任务 `realPath=true` 的 Agent 版本。
//
// 背景：旧版 Agent 的 handle_file_read/write 会把绝对路径锚定到自身沙箱根再落盘，
// 面板如果只是「多传一个 realPath 字段」，旧 Agent 会直接忽略它，于是又回到
// 「界面显示保存成功、主机真实文件没变」的静默数据丢失。因此面板必须先比对
// 版本：不达标的 Agent 一律不允许走 Agent 文件通道写主机文件。
const minAgentFileRealPathVersion = "0.6.37"

// agentVersionAtLeast 判断连接上报的 agent 版本是否 >= want（形如 0.6.37）。
// 版本缺失或无法解析时返回 false——宁可回退到 SSH，也不能静默写错位置。
func agentVersionAtLeast(conn *AgentConnection, want string) bool {
	if conn == nil {
		return false
	}
	meta := conn.GetMetadata()
	raw := ""
	for _, key := range []string{"agent_version", "version"} {
		if value, ok := meta[key].(string); ok && strings.TrimSpace(value) != "" {
			raw = strings.TrimSpace(value)
			break
		}
	}
	if raw == "" {
		return false
	}
	return compareAgentVersion(raw, want) >= 0
}

// compareAgentVersion 逐段比较点分版本号，返回 -1 / 0 / 1。
// 允许前缀 v 与后缀（如 0.6.37-beta.1 取 0.6.37 参与比较）。
func compareAgentVersion(a, b string) int {
	left := splitAgentVersion(a)
	right := splitAgentVersion(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l > r {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitAgentVersion(value string) []int {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if text == "" {
		return nil
	}
	// 截断预发布/构建后缀：0.6.37-beta.1 → 0.6.37
	if idx := strings.IndexAny(text, "-+"); idx >= 0 {
		text = text[:idx]
	}
	parts := strings.Split(text, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			// 出现非数字段时停止解析，剩余段按 0 处理。
			break
		}
		out = append(out, n)
	}
	return out
}

// agentSupportsFileRealPath 判断该主机当前的 Agent 是否已支持 realPath=true。
// 未连接 Agent 时返回 false（调用方本来就走 SSH）。
func (s *Service) agentSupportsFileRealPath(serverID string) bool {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return false
	}
	return agentVersionAtLeast(conn, minAgentFileRealPathVersion)
}
