package openai

import "encoding/json"

// 本文件把 Vertex 上游使用的标准 Gemini GenerateContent 转换器导出，供
// geminicli 插件复用。Cloud Code Assist 的 v1internal 协议在请求体中承载
// 标准 GeminiGenerateContentRequest（contents / systemInstruction /
// generationConfig，camelCase），响应体承载标准 GeminiGenerateContentResponse
// （candidates / usageMetadata），与 Vertex 完全同构，故直接复用。

// OpenAIChatToGeminiGenerate 将 OpenAI chat 请求体转换为标准 Gemini
// GenerateContent 请求体（contents / systemInstruction / generationConfig）。
func OpenAIChatToGeminiGenerate(body map[string]interface{}) (map[string]interface{}, error) {
	return openAIChatToVertex(body)
}

// GeminiGenerateToOpenAIChat 将标准 Gemini GenerateContent 响应体转换为
// OpenAI chat.completions 响应。
func GeminiGenerateToOpenAIChat(body []byte, fallbackModel string) ([]byte, error) {
	return vertexToOpenAIChat(body, fallbackModel)
}

// GeminiGenerateSSETransformer 把标准 Gemini SSE 事件转换为 OpenAI
// chat.completion.chunk 序列。每个输入事件应是去壳后的 Gemini 事件 JSON
// （即 v1internal 流式分片里的 response 字段），输出为带 "data: " 前缀的
// SSE 行；流结束时调用 Finish 补发收尾与 [DONE]。
type GeminiGenerateSSETransformer struct {
	inner *vertexSSETransformer
}

// NewGeminiGenerateSSETransformer 构造标准 Gemini 流式转换器。
func NewGeminiGenerateSSETransformer(model string) *GeminiGenerateSSETransformer {
	return &GeminiGenerateSSETransformer{inner: newVertexSSETransformer(model)}
}

// Consume 处理一个 Gemini 事件 JSON，返回需写出的 OpenAI SSE chunk。
func (t *GeminiGenerateSSETransformer) Consume(data []byte) [][]byte {
	if t == nil || t.inner == nil {
		return nil
	}
	return t.inner.consume(data)
}

// Finish 在流结束时补发 finish chunk 与 [DONE]。
func (t *GeminiGenerateSSETransformer) Finish() [][]byte {
	if t == nil || t.inner == nil {
		return nil
	}
	return t.inner.finish()
}

// GeminiGenerateUsage 解析标准 Gemini 响应的 usageMetadata 为 OpenAI usage 结构。
func GeminiGenerateUsage(body []byte) (map[string]interface{}, bool) {
	var parsed struct {
		UsageMetadata vertexUsage `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false
	}
	u := parsed.UsageMetadata
	if u.TotalTokenCount == 0 && u.PromptTokenCount == 0 && u.CandidatesTokenCount == 0 {
		return nil, false
	}
	return vertexOpenAIUsage(&u), true
}
