package antigravity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// stubUpstream 起一个假上游，捕获实际发出的请求（UA + 路径 + body）。
type stubUpstream struct {
	server *httptest.Server
	ua     string
	path   string
	body   []byte
}

func newStubUpstream(t *testing.T) *stubUpstream {
	t.Helper()
	stub := &stubUpstream{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.ua = r.Header.Get("User-Agent")
		stub.path = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取上游请求体失败: %v", err)
		}
		stub.body = raw
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// useTempBaseURLs 把引擎的 base URL 指向假上游。
func useTempBaseURLs(t *testing.T, url string) {
	t.Helper()
	origURLs := engineag.BaseURLs
	origURL := engineag.BaseURL
	engineag.BaseURLs = []string{url}
	engineag.BaseURL = url
	t.Cleanup(func() {
		engineag.BaseURLs = origURLs
		engineag.BaseURL = origURL
	})
}

// TestUpstreamPayloadToolPairingAndBoundary 端到端校验真正发到上游的字节：
// UA 形态、tool id 合法且成对、contents 不以 model 结尾。
func TestUpstreamPayloadToolPairingAndBoundary(t *testing.T) {
	stub := newStubUpstream(t)
	useTempBaseURLs(t, stub.server.URL)

	claudeReq := &engineag.ClaudeRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
		Messages: []engineag.ClaudeMessage{
			{Role: "user", Content: []byte(`"run the tool"`)},
			// 网关侧常见形态：ID 含 '|'，Claude 上游不接受。
			{Role: "assistant", Content: []byte(`[{"type":"tool_use","id":"call_a|b","name":"t","input":{"x":1}}]`)},
			{Role: "user", Content: []byte(`[{"type":"tool_result","tool_use_id":"call_a|b","content":"done"}]`)},
		},
		Tools: []engineag.ClaudeTool{{
			Name:        "t",
			Description: "a tool",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "number"},
				},
			},
		}},
	}

	body, err := engineag.TransformClaudeToGemini(claudeReq, "proj", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("转换请求失败: %v", err)
	}

	client, err := engineag.NewClient("")
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	req, err := engineag.NewAPIRequestWithURL(context.Background(), stub.server.URL, "generateContent", "test-token", body)
	if err != nil {
		t.Fatalf("构造上游请求失败: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("执行上游请求失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// UA 必须是 Hub 形态，不能退回旧的 windows/amd64 写法。
	if !strings.HasPrefix(stub.ua, "antigravity/hub/") {
		t.Errorf("上游 UA 应为 Hub 形态, got %q", stub.ua)
	}
	if strings.Contains(stub.ua, "windows/amd64") {
		t.Errorf("上游 UA 不应是旧 windows 形态, got %q", stub.ua)
	}
	if !strings.HasSuffix(stub.path, "/v1internal:generateContent") {
		t.Errorf("上游路径不匹配: %q", stub.path)
	}

	var payload map[string]any
	if err := json.Unmarshal(stub.body, &payload); err != nil {
		t.Fatalf("上游请求体不是合法 JSON: %v", err)
	}
	inner, ok := payload["request"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 request 字段: %s", stub.body)
	}
	contents, ok := inner["contents"].([]any)
	if !ok || len(contents) == 0 {
		t.Fatalf("缺少 contents: %s", stub.body)
	}

	last := contents[len(contents)-1].(map[string]any)
	if last["role"] == "model" {
		t.Errorf("contents 不能以 model 结尾: %v", last)
	}

	var callIDs, responseIDs []string
	for _, raw := range contents {
		content := raw.(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, rawPart := range parts {
			part := rawPart.(map[string]any)
			if fc, ok := part["functionCall"].(map[string]any); ok {
				callIDs = append(callIDs, fc["id"].(string))
			}
			if fr, ok := part["functionResponse"].(map[string]any); ok {
				responseIDs = append(responseIDs, fr["id"].(string))
			}
		}
	}
	if len(callIDs) != 1 || len(responseIDs) != 1 {
		t.Fatalf("期望 1 个 functionCall + 1 个 functionResponse, got %v / %v", callIDs, responseIDs)
	}
	if callIDs[0] != responseIDs[0] {
		t.Errorf("functionCall/functionResponse ID 未配对: %q vs %q", callIDs[0], responseIDs[0])
	}
	if callIDs[0] != "call_a_b" {
		t.Errorf("tool ID = %q, want call_a_b（'|' 应被清洗为 '_'）", callIDs[0])
	}
	if strings.Contains(callIDs[0], "|") {
		t.Errorf("tool ID 仍含非法字符: %q", callIDs[0])
	}

	// 工具 schema 必须经过清洗（$ref 展开、不支持字段剥离）。
	toolsRaw, _ := json.Marshal(inner["tools"])
	if strings.Contains(string(toolsRaw), "$ref") {
		t.Errorf("tool schema 中的 $ref 未被展开: %s", toolsRaw)
	}
	if !strings.Contains(string(toolsRaw), `"functionDeclarations"`) {
		t.Errorf("缺少 functionDeclarations: %s", toolsRaw)
	}
}

// TestUpstreamPayloadHasNoTrailingModelTurn 覆盖 Claude 目标：只丢尾部纯 thought
// turn，不注入空 text turn（Claude 适配层拒绝空文本 part）。
func TestUpstreamPayloadHasNoTrailingModelTurn(t *testing.T) {
	stub := newStubUpstream(t)
	useTempBaseURLs(t, stub.server.URL)

	claudeReq := &engineag.ClaudeRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
		Messages: []engineag.ClaudeMessage{
			{Role: "user", Content: []byte(`"hi"`)},
			{Role: "assistant", Content: []byte(`[{"type":"thinking","thinking":"let me think"}]`)},
		},
	}

	body, err := engineag.TransformClaudeToGemini(claudeReq, "proj", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("转换请求失败: %v", err)
	}

	client, _ := engineag.NewClient("")
	req, err := engineag.NewAPIRequestWithURL(context.Background(), stub.server.URL, "generateContent", "tok", body)
	if err != nil {
		t.Fatalf("构造上游请求失败: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("执行上游请求失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	var payload engineag.V1InternalRequest
	if err := json.Unmarshal(stub.body, &payload); err != nil {
		t.Fatalf("解析上游请求体失败: %v", err)
	}
	for _, content := range payload.Request.Contents {
		if content.Role != "model" {
			continue
		}
		for _, part := range content.Parts {
			if part.Thought && strings.TrimSpace(part.Text) != "" {
				t.Fatalf("尾部纯 thought 的 model turn 未被丢弃: %q", part.Text)
			}
		}
	}
	// Claude 目标不应出现空文本 part。
	for _, content := range payload.Request.Contents {
		for _, part := range content.Parts {
			if part.Text == "" && part.FunctionCall == nil && part.FunctionResponse == nil &&
				part.InlineData == nil && !part.Thought {
				t.Fatalf("Claude 目标出现空文本 part: %+v", content)
			}
		}
	}
}
