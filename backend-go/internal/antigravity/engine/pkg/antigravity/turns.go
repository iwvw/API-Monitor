package antigravity

// 请求 contents 的边界归一化。
//
// Antigravity 上游（Gemini 系）对 contents 的首尾形态敏感：
//   - 首轮若是 model，会被判为会话不完整；补一个空 user turn 即可。
//   - 尾轮若是 model，上游期望模型继续生成；但只要尾轮「没有可回复内容」就会被拒。
//     历史重放时最容易踩的是「尾部只剩 thinking 的 model turn」——上游把它当作
//     纯思考、无可回复内容，于是以 model 结尾报 400（CLIProxyAPI #6530）。
//     正确做法是先丢掉这种尾部 thought-only turn，再补空 user turn。
//   - 尾轮是带 functionResponse 的 model turn 时要保留：上游正是要靠它继续生成。
//
// 这里在结构化转换之后、序列化之前对 []GeminiContent 直接操作，
// 不走 JSON 补丁路径，避免与 schema 清洗互相干扰。

import "strings"

// normalizeContentsBoundary 归一化 contents 的首尾形态。
//
// syntheticUserTurn 决定是否允许补「空 user turn」：Gemini 系上游接受空文本
// part，可以靠补空 turn 修正边界；Claude 系上游的适配层会拒绝空文本 part，
// 因此对 Claude 模型只做「丢弃尾部纯思考 turn」，不注入空 turn。
func normalizeContentsBoundary(contents []GeminiContent, allowSyntheticUserTurn bool) []GeminiContent {
	contents = dropTrailingThoughtOnlyTurns(contents)
	if !allowSyntheticUserTurn {
		return contents
	}
	contents = ensureLeadingUserTurn(contents)
	contents = ensureTrailingUserTurn(contents)
	return contents
}

// isThoughtOnlyTurn 判断一条 content 是否「只包含思考内容」。
// 空 parts 也视为无效内容。
func isThoughtOnlyTurn(content GeminiContent) bool {
	if len(content.Parts) == 0 {
		return true
	}
	for _, part := range content.Parts {
		if !part.Thought {
			return false
		}
		// 带函数调用/内联数据的 part 即使标了 thought 也不是纯思考，
		// 不能丢（丢了会破坏工具配对）。
		if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
			return false
		}
		if strings.TrimSpace(part.Text) != "" {
			continue
		}
	}
	return true
}

// hasFunctionResponse 判断 content 是否包含 functionResponse。
func hasFunctionResponse(content GeminiContent) bool {
	for _, part := range content.Parts {
		if part.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// hasFunctionResponseMedia 判断 functionResponse 是否携带嵌套媒体
// （inlineData / fileData）。上游拒绝以这种尾轮结束（CLIProxyAPI #6524）。
func hasFunctionResponseMedia(content GeminiContent) bool {
	for _, part := range content.Parts {
		if part.FunctionResponse == nil {
			continue
		}
		if _, ok := part.FunctionResponse.Response["parts"]; ok {
			return true
		}
	}
	return false
}

// dropTrailingThoughtOnlyTurns 丢弃尾部的纯思考 model turn，至少保留一条。
func dropTrailingThoughtOnlyTurns(contents []GeminiContent) []GeminiContent {
	for len(contents) > 1 {
		last := contents[len(contents)-1]
		if !isModelRole(last.Role) || !isThoughtOnlyTurn(last) {
			break
		}
		contents = contents[:len(contents)-1]
	}
	return contents
}

// syntheticEmptyUserTurn 构造用于补齐边界形态的空 user turn。
func syntheticEmptyUserTurn() GeminiContent {
	return GeminiContent{
		Role:  "user",
		Parts: []GeminiPart{{Text: "", forceEmptyText: true}},
	}
}

// ensureLeadingUserTurn 保证 contents 以 user turn 开头。
func ensureLeadingUserTurn(contents []GeminiContent) []GeminiContent {
	if len(contents) == 0 || contents[0].Role != "model" {
		return contents
	}
	return append([]GeminiContent{syntheticEmptyUserTurn()}, contents...)
}

// ensureTrailingUserTurn 保证 contents 以 user turn 结尾。
//
// 尾轮为 model 且带 functionResponse（不含嵌套媒体）时保留原样：上游要靠它
// 继续生成。其余 model 结尾一律补一个空 user turn。
func ensureTrailingUserTurn(contents []GeminiContent) []GeminiContent {
	if len(contents) == 0 {
		return contents
	}
	last := contents[len(contents)-1]
	if !isModelRole(last.Role) {
		return contents
	}
	if hasFunctionResponse(last) && !hasFunctionResponseMedia(last) {
		return contents
	}
	return append(contents, syntheticEmptyUserTurn())
}

// isModelRole 判断角色是否代表模型侧（兼容 model / assistant 两种写法）。
func isModelRole(role string) bool {
	return role == "model" || role == "assistant"
}
