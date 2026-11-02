package workbuddy

// 本文件实现转发前的上下文长度预检。
//
// 为什么要做：上游对超长输入的处理不统一——可能返回 400（浪费一次往返），
// 也可能接受后截断（静默丢内容，更糟）。目录里每个模型都带 maxInputTokens，
// 直接用它拦下明显超限的请求，报错明确指出上限与模型。
//
// **估算方式**：用字符启发式而非分词器。原因见 qoder 插件同处的记录：
// go-tiktoken 在本项目的调用方式下耗时超线性（40000 字符约 1.8s），
// 放在转发热路径上会明显拖慢每次请求。上下文预检只需判断「是否明显超限」，
// 启发式（ASCII 约 4 字符/token、CJK 约 1.3）已经足够保守。

import (
	"encoding/json"
	"fmt"
	"strings"
)

// estimateTokens 估算文本的 token 数（纯字符遍历）。
// 非 ASCII 字符按 1.3 字符/token、ASCII 按 4 字符/token 折算，与项目内
// ds2api/util.EstimateTokens 的口径一致。
func estimateTokens(text string) int64 {
	if text == "" {
		return 0
	}
	var ascii, nonASCII int64
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			nonASCII++
		}
	}
	n := ascii/4 + (nonASCII*10+7)/13
	if n < 1 {
		return 1
	}
	return n
}

// strValue 返回字符串值（非字符串返回空串）。
func strValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// requestTextForTokenCount 把 messages 与 tools 拼成待估算的文本。
// tools 也计入——工具定义往往占数千 token，漏算会让校验形同虚设。
func requestTextForTokenCount(payload map[string]any) string {
	var parts []string
	if msgs, ok := payload["messages"].([]any); ok {
		for _, raw := range msgs {
			msg, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch c := msg["content"].(type) {
			case string:
				if s := strings.TrimSpace(c); s != "" {
					parts = append(parts, s)
				}
			case []any:
				for _, item := range c {
					if m, ok := item.(map[string]any); ok {
						if s := strings.TrimSpace(strValue(m["text"])); s != "" {
							parts = append(parts, s)
						}
					}
				}
			}
			if tc, ok := msg["tool_calls"]; ok && tc != nil {
				if b, err := json.Marshal(tc); err == nil {
					parts = append(parts, string(b))
				}
			}
		}
	}
	if tools, ok := payload["tools"]; ok && tools != nil {
		if b, err := json.Marshal(tools); err == nil {
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}

// contextCheck 校验请求是否超过模型的输入上限。
//
// 返回 (是否超限, 给调用方的原因)。上限缺失（<=0）时不校验——宁可让上游
// 自己判断，也不要因本地缺数据而误拒。
func contextCheck(m ModelInfo, promptTokens int64) (bool, string) {
	if promptTokens <= 0 || m.ContextLength <= 0 {
		return false, ""
	}
	if promptTokens <= m.ContextLength {
		return false, ""
	}
	return true, fmt.Sprintf(
		"输入过长：约 %d tokens 超过模型 %s 的上限 %d tokens",
		promptTokens, m.ID, m.ContextLength,
	)
}

// checkContext 是 relay 侧的入口：解析请求体、估算、校验。
// 返回非空字符串表示应当拒绝并回 400。
func (s *Service) checkContext(body []byte, model string) string {
	entry, ok := s.catalogEntryFor(model)
	if !ok {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	over, reason := contextCheck(entry, estimateTokens(requestTextForTokenCount(payload)))
	if over {
		return reason
	}
	return ""
}
