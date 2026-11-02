package openai

import (
	"strings"
)

// pickKey 从端点全部 key 中按轮询选出一个 key，返回 (key, index)。
// key 永不冻结：triedKeys 记录本次请求内每个 key 已尝试失败的次数（map[string]int），
// 只跳过「已达到单请求最大尝试次数（maxTries）」的 key，从而允许同一 key 在
// 一个请求内被尝试 maxTries 次（默认 2 = 循环两遍），而不是一次失败即换端点。
// 全部 key 均已达到 maxTries 时返回 ("", -1)，由调用方触发端点级切换。
// 429 绝不冻结 key，只靠轮询天然分散 RPM 压力。
func (s *Service) pickKey(endpointID string, keys []string, triedKeys map[string]int, maxTries int) (string, int) {
	cleaned := cleanKeyList(keys)
	if len(cleaned) == 0 {
		return "", -1
	}
	if maxTries < 1 {
		maxTries = 1
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointKeyState()
		s.keyStateByEndpoint[endpointID] = state
	}
	if state.cursor < 0 || state.cursor >= len(cleaned) {
		state.cursor = 0
	}
	start := state.cursor
	for i := 0; i < len(cleaned); i++ {
		idx := (start + i) % len(cleaned)
		if triedKeys != nil && triedKeys[cleaned[idx]] >= maxTries {
			continue
		}
		state.cursor = (idx + 1) % len(cleaned)
		return cleaned[idx], idx
	}
	return "", -1
}

// cleanKeyList 清洗并去重 API Key 列表（保留顺序，剔除空串）。
func cleanKeyList(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
