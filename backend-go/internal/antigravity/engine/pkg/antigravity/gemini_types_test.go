package antigravity

import (
	"encoding/json"
	"testing"
)

// TestGeminiPartForceEmptyTextSerialization 覆盖 forceEmptyText 的三条序列化路径。
//
// 边界归一化补的空 user turn 必须真发 {"text":""}：omitempty 会把空串整字段丢掉，
// 上游收到的就是 {"parts":[{}]}，反被判为非法 part。
func TestGeminiPartForceEmptyTextSerialization(t *testing.T) {
	// 常见形态：只有空 text。
	raw, err := json.Marshal(GeminiPart{Text: "", forceEmptyText: true})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if string(raw) != `{"text":""}` {
		t.Fatalf("synthetic empty part = %s, want {\"text\":\"\"}", raw)
	}

	// 兜底形态：标记了 forceEmptyText 但还带其它字段时，不能丢字段。
	rawMixed, err := json.Marshal(GeminiPart{
		Text:             "",
		Thought:          true,
		ThoughtSignature: "sig",
		forceEmptyText:   true,
	})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	var mixed map[string]any
	if err := json.Unmarshal(rawMixed, &mixed); err != nil {
		t.Fatalf("结果不是合法 JSON: %v (%s)", err, rawMixed)
	}
	if mixed["text"] != "" {
		t.Errorf("text 键应被补成空串, got %v", mixed["text"])
	}
	if mixed["thought"] != true || mixed["thoughtSignature"] != "sig" {
		t.Errorf("兜底路径不应丢弃其它字段, got %s", rawMixed)
	}

	// 未标记的普通 part 仍走 omitempty。
	rawPlain, err := json.Marshal(GeminiPart{Text: ""})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if string(rawPlain) != `{}` {
		t.Fatalf("plain empty part = %s, want {}", rawPlain)
	}
}
