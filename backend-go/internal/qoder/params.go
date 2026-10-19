package qoder

// 本文件把调用方的 OpenAI 语义映射成上游的思考/上下文参数，并在转发前做校验。
//
// 与 thinking.go 的分工：thinking.go 只描述「模型支持什么」，本文件负责
// 「这次请求要什么、怎么落到上游字段上、超限时怎么拒绝」。

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

// requestOptions 是从 OpenAI 请求体里提取的、需要映射到上游的控制项。
type requestOptions struct {
	// Effort 是调用方给的思考强度（reasoning_effort / reasoning.effort / effort）。
	Effort string
	// DisableThinking 表示调用方显式要求关闭思考。
	DisableThinking bool
	// ContextWindow 是调用方指定的上下文档位（context_window / max_input_tokens）。
	ContextWindow int64
	// MaxTokens 是调用方要求的输出上限（max_completion_tokens 优先于 max_tokens）。
	MaxTokens int64
}

// parseRequestOptions 从请求体读取思考/上下文相关参数。
//
// 兼容的写法（按优先级）：
//   - 思考强度：reasoning_effort（字符串或 {effort:...}）→ reasoning.effort → effort
//   - 关闭思考：thinking.type="disabled" / reasoning.enabled=false / reasoning_effort="none"
//   - 上下文：context_window → max_input_tokens → context_length
//   - 输出上限：max_completion_tokens → max_tokens
func parseRequestOptions(payload map[string]interface{}) requestOptions {
	var opt requestOptions

	// 关闭思考的显式信号。
	if thinking, ok := payload["thinking"].(map[string]interface{}); ok {
		if strings.EqualFold(strings.TrimSpace(strValue(thinking["type"])), "disabled") {
			opt.DisableThinking = true
		}
	}
	if reasoning, ok := payload["reasoning"].(map[string]interface{}); ok {
		if enabled, ok := reasoning["enabled"].(bool); ok && !enabled {
			opt.DisableThinking = true
		}
		if effort := strings.TrimSpace(strValue(reasoning["effort"])); effort != "" {
			opt.Effort = effort
		}
	}
	// reasoning_effort 既可能是字符串，也可能是 {effort:"high"}。
	switch v := payload["reasoning_effort"].(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			opt.Effort = s
		}
	case map[string]interface{}:
		if effort := strings.TrimSpace(strValue(v["effort"])); effort != "" {
			opt.Effort = effort
		}
	}
	if opt.Effort == "" {
		if effort := strings.TrimSpace(strValue(payload["effort"])); effort != "" {
			opt.Effort = effort
		}
	}
	if isDisableEffort(opt.Effort) {
		opt.DisableThinking = true
		opt.Effort = ""
	}

	opt.ContextWindow = firstPositiveInt(payload, "context_window", "max_input_tokens", "context_length")
	opt.MaxTokens = firstPositiveInt(payload, "max_completion_tokens", "max_tokens")
	return opt
}

// firstPositiveInt 返回第一个值为正整数的字段（float64/int/json.Number 都接受）。
func firstPositiveInt(payload map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if n := jsonInt64(payload[key]); n > 0 {
			return n
		}
	}
	return 0
}

// jsonInt64 把任意 JSON 数值转成 int64（非数值返回 0）。
func jsonInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		if got, err := n.Int64(); err == nil {
			return got
		}
	}
	return 0
}

// contextCheck 校验请求的上下文长度是否超过模型上限。
//
// 返回 (是否超限, 用户可读原因)。为 0 或模型没有档位信息时不校验
// （宁可让上游报错，也不要因为本地估算误判而拒绝合法请求）。
func contextCheck(m ModelInfo, promptTokens int64, opt requestOptions) (bool, string) {
	if promptTokens <= 0 {
		return false, ""
	}
	limit := opt.ContextWindow
	if limit <= 0 {
		// 未显式指定档位：用上游发布的最大档位（而非不可靠的 max_input_tokens）。
		if len(m.ContextWindows) > 0 {
			limit = m.ContextWindows[len(m.ContextWindows)-1]
		} else {
			limit = m.MaxInputTokens
		}
	} else if len(m.ContextWindows) > 0 && limit > m.ContextWindows[len(m.ContextWindows)-1] {
		// 显式档位超过模型上限时，也按上限判（上游会拒绝）。
		limit = m.ContextWindows[len(m.ContextWindows)-1]
	}
	if limit <= 0 || promptTokens <= limit {
		return false, ""
	}
	return true, fmt.Sprintf(
		"输入过长：约 %d tokens 超过模型 %s 的上限 %d tokens（可用档位：%s）",
		promptTokens, m.ID, limit, formatContextWindows(m.ContextWindows),
	)
}

// formatContextWindows 把档位列表渲染成 "200K/400K/1M" 形式。
func formatContextWindows(windows []int64) string {
	if len(windows) == 0 {
		return "上游未声明"
	}
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		parts = append(parts, formatTokenCount(w))
	}
	return strings.Join(parts, "/")
}

// formatTokenCount 把 token 数渲染成 200K / 1M 这样的短标签。
func formatTokenCount(n int64) string {
	switch {
	case n <= 0:
		return "0"
	case n%(1<<20) == 0:
		return fmt.Sprintf("%dM", n/(1<<20))
	case n >= 1000000:
		// 上游用十进制 1M = 1000000，优先按十进制渲染。
		if n%1000000 == 0 {
			return fmt.Sprintf("%dM", n/1000000)
		}
		return fmt.Sprintf("%dk", n/1000)
	case n%1000 == 0:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// resolveThinking 把调用方取值落成上游字段。
//
// 返回 (isReasoning, effort)：
//   - isReasoning 写进 model_config.is_reasoning 与 chat_context.extra.modelConfig.is_reasoning；
//   - effort 非空时写进 parameters.reasoning_effort。
//
// 模型不支持思考时两者都为空/原样（不写入任何思考字段）。
func resolveThinking(m ModelInfo, opt requestOptions) (isReasoning bool, effort string) {
	if m.Thinking == nil {
		// 无 thinking_config：沿用上游 is_reasoning 声明，不主动改。
		return m.IsReasoning, ""
	}
	level, disable, hasLevel := m.Thinking.Resolve(opt.Effort)
	if opt.DisableThinking {
		if m.Thinking.CanDisable || disable {
			return false, ""
		}
		// 不能关就保持开启，但也不加档位。
		return true, ""
	}
	if hasLevel {
		return true, level
	}
	// 只有开关能力：默认开启。
	return true, ""
}

// usageTokensForCheck 估算请求的输入 token 数，用于上下文校验。
//
// 用 ds2api 的 CountPromptTokens（tiktoken，按模型选编码，失败回退启发式），
// 与网关其它模块口径一致。messages 和 tools 都计入——工具定义往往占数千 token。
func usageTokensForCheck(payload map[string]interface{}, model string) int64 {
	text := requestTextForTokenCount(payload)
	if text == "" {
		return 0
	}
	return int64(util.CountPromptTokens(text, tokenizerModelFor(model)))
}

// tokenizerModelFor 把 Qoder 模型名映射到 tokenizer 可识别的编码族。
//
// Qoder 的模型名（qmodel_38max / dmodel / ...）不在 tiktoken 的模型表里，
// CountPromptTokens 内部会对未知模型回退启发式估算（EstimateTokens），
// 那本身可用，但精确度差。这里尽量贴近真实编码：Qwen/GLM/Kimi/MiniMax
// 的 BPE 更接近 cl100k_base（gpt-4o 族），DeepSeek 同理。
// 统一返回 gpt-4o，让 tiktoken 走 o200k_base；与上游实际分词仍有差异，
// 但上下文校验只需保守估计（宁可高估一点点以便尽早拦截明显超限）。
func tokenizerModelFor(model string) string {
	return "gpt-4o"
}

// requestTextForTokenCount 把 messages 与 tools 拼成待计数的文本。
func requestTextForTokenCount(payload map[string]interface{}) string {
	var parts []string
	if msgs, ok := payload["messages"].([]interface{}); ok {
		for _, raw := range msgs {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if s := strings.TrimSpace(strValue(msg["content"])); s != "" {
				parts = append(parts, s)
			}
			// 多模态 parts 数组里的文本也要计入。
			if arr, ok := msg["content"].([]interface{}); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]interface{}); ok {
						if s := strings.TrimSpace(strValue(m["text"])); s != "" {
							parts = append(parts, s)
						}
					}
				}
			}
			if !rawJSONIsEmptyValue(msg["tool_calls"]) {
				if b, err := json.Marshal(msg["tool_calls"]); err == nil {
					parts = append(parts, string(b))
				}
			}
		}
	}
	if !rawJSONIsEmptyValue(payload["tools"]) {
		if b, err := json.Marshal(payload["tools"]); err == nil {
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}
