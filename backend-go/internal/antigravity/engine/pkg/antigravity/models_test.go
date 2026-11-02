package antigravity

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestSanitizeClaudeToolID(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	cases := map[string]string{
		"call_test|fc_test":  "call_test_fc_test",
		"call_valid-123_456": "call_valid-123_456",
		"toolu.01.abc":       "toolu_01_abc",
		"   ":                "tool_call",
		"":                   "tool_call",
		"中文ID":               "__ID",
		"call_abc::def":      "call_abc__def",
	}
	for in, want := range cases {
		got := SanitizeClaudeToolID(in)
		if got != want {
			t.Errorf("SanitizeClaudeToolID(%q) = %q, want %q", in, got, want)
		}
		if !pattern.MatchString(got) {
			t.Errorf("SanitizeClaudeToolID(%q) = %q, does not match Claude ID pattern", in, got)
		}
		if len(got) > claudeToolIDMaxLen {
			t.Errorf("SanitizeClaudeToolID(%q) length %d exceeds %d", in, len(got), claudeToolIDMaxLen)
		}
	}

	// 超长 ID 必须截断到上限。
	long := strings.Repeat("a", claudeToolIDMaxLen+20)
	if got := SanitizeClaudeToolID(long); len(got) != claudeToolIDMaxLen {
		t.Errorf("long ID length = %d, want %d", len(got), claudeToolIDMaxLen)
	}
}

func TestToolIDMapperDeduplicatesCollisions(t *testing.T) {
	m := newToolIDMapper()

	// 同一原始 ID 多次映射必须稳定。
	first := m.mapID("call_a|b")
	if second := m.mapID("call_a|b"); second != first {
		t.Fatalf("mapID not stable: %q vs %q", first, second)
	}

	// 清洗后相同的两个不同原始 ID 必须被区分开，否则上游会拒绝重复 tool id。
	other := m.mapID("call_a:b")
	if other == first {
		t.Fatalf("colliding IDs must be disambiguated, both = %q", other)
	}
	if !strings.HasPrefix(other, "call_a_b") {
		t.Errorf("disambiguated ID %q should keep the sanitized base", other)
	}

	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	third := m.mapID("call_a.b")
	for _, id := range []string{first, other, third} {
		if !pattern.MatchString(id) {
			t.Errorf("mapped ID %q violates Claude ID pattern", id)
		}
		if len(id) > claudeToolIDMaxLen {
			t.Errorf("mapped ID %q exceeds max length", id)
		}
	}
	if third == first || third == other {
		t.Fatalf("three colliding IDs should all differ: %q %q %q", first, other, third)
	}
}

func TestTransformPairsSanitizedToolIDsAcrossTurns(t *testing.T) {
	// 并行 tool_calls 的 ID 含 '|'，tool_result 用原始 ID 回传。
	// 转换后 functionCall 与 functionResponse 必须落到同一个（清洗后的）ID。
	claudeReq := &ClaudeRequest{
		Model: "claude-sonnet-4-6",
		Messages: []ClaudeMessage{
			{Role: "user", Content: []byte(`"run both tools"`)},
			{Role: "assistant", Content: []byte(`[
				{"type":"tool_use","id":"call_x|fc_1","name":"tool_one","input":{"a":1}},
				{"type":"tool_use","id":"call_x|fc_2","name":"tool_two","input":{"b":2}}
			]`)},
			{Role: "user", Content: []byte(`[
				{"type":"tool_result","tool_use_id":"call_x|fc_1","content":"ok1"},
				{"type":"tool_result","tool_use_id":"call_x|fc_2","content":"ok2"}
			]`)},
		},
	}

	body, err := TransformClaudeToGemini(claudeReq, "proj", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("TransformClaudeToGemini 失败: %v", err)
	}

	var req V1InternalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("解析转换结果失败: %v (%s)", err, body)
	}

	var callIDs, responseIDs []string
	for _, content := range req.Request.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				callIDs = append(callIDs, part.FunctionCall.ID)
			}
			if part.FunctionResponse != nil {
				responseIDs = append(responseIDs, part.FunctionResponse.ID)
			}
		}
	}

	if len(callIDs) != 2 || len(responseIDs) != 2 {
		t.Fatalf("expected 2 function calls and 2 responses, got %v / %v", callIDs, responseIDs)
	}
	for i := range callIDs {
		if callIDs[i] != responseIDs[i] {
			t.Errorf("call/response ID mismatch at %d: %q vs %q", i, callIDs[i], responseIDs[i])
		}
	}
	if callIDs[0] == callIDs[1] {
		t.Errorf("parallel tool calls must not share an ID: %v", callIDs)
	}

	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for _, id := range callIDs {
		if !pattern.MatchString(id) {
			t.Errorf("upstream tool ID %q violates Claude ID pattern", id)
		}
	}
}

func TestBuildGenerationConfigRespectsModelLimit(t *testing.T) {
	// opus-5-5-high 允许 128000 输出；旧上限是 64000，会把额度截半。
	temperature := 0.5
	req := &ClaudeRequest{
		Model:       "claude-opus-5-5-high",
		MaxTokens:   200000,
		Temperature: &temperature,
	}
	cfg := buildGenerationConfig(req)
	if cfg.MaxOutputTokens != 128000 {
		t.Errorf("MaxOutputTokens = %d, want 128000", cfg.MaxOutputTokens)
	}

	// 未知 Claude 模型沿用旧上限，避免行为突变。
	unknown := buildGenerationConfig(&ClaudeRequest{Model: "claude-unknown-1", MaxTokens: 200000})
	if unknown.MaxOutputTokens != maxOutputTokensClaude {
		t.Errorf("unknown model MaxOutputTokens = %d, want %d", unknown.MaxOutputTokens, maxOutputTokensClaude)
	}
}

func TestBuildGenerationConfigDropsThinkingBelowMinimum(t *testing.T) {
	// claude 系最小 thinking budget 是 1024；低于该值上游直接 400，
	// 因此应整体移除 thinkingConfig 而不是透传。
	req := &ClaudeRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
		Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: 128},
	}
	cfg := buildGenerationConfig(req)
	if cfg.ThinkingConfig != nil {
		t.Fatalf("thinkingConfig should be dropped for sub-minimum budget, got %+v", cfg.ThinkingConfig)
	}
}

func TestBuildGenerationConfigClampsThinkingToModelMax(t *testing.T) {
	// gemini-2.5-flash 的 thinking 上限是 24576；更小的预算应被裁剪。
	req := &ClaudeRequest{
		Model:     "gemini-2.5-flash",
		MaxTokens: 64000,
		Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: 60000},
	}
	cfg := buildGenerationConfig(req)
	if cfg.ThinkingConfig == nil {
		t.Fatal("thinkingConfig should be kept")
	}
	if cfg.ThinkingConfig.ThinkingBudget != Gemini25FlashThinkingBudgetLimit {
		t.Errorf("ThinkingBudget = %d, want %d", cfg.ThinkingConfig.ThinkingBudget, Gemini25FlashThinkingBudgetLimit)
	}
}

func TestBuildGenerationConfigKeepsMaxAboveThinkingBudget(t *testing.T) {
	// max_tokens 必须严格大于 budget；裁剪后也不能反过来违反该约束。
	req := &ClaudeRequest{
		Model:     "claude-opus-5-5-high",
		MaxTokens: 2048,
		Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: 1024},
	}
	cfg := buildGenerationConfig(req)
	if cfg.ThinkingConfig == nil {
		t.Fatal("thinkingConfig should be kept")
	}
	if cfg.MaxOutputTokens <= cfg.ThinkingConfig.ThinkingBudget {
		t.Errorf("MaxOutputTokens %d must exceed ThinkingBudget %d",
			cfg.MaxOutputTokens, cfg.ThinkingConfig.ThinkingBudget)
	}
	if cfg.MaxOutputTokens > 128000 {
		t.Errorf("MaxOutputTokens %d exceeds model limit", cfg.MaxOutputTokens)
	}
}

func TestMaxCompletionTokensForKnownModels(t *testing.T) {
	cases := map[string]int{
		"claude-opus-5-5-high":     128000,
		"claude-sonnet-5-5-high":   128000,
		"claude-opus-4-6-thinking": 64000,
		"gemini-3.8-flash-high":    65536,
		"gemini-4-argon-high":      1048576,
		"gpt-oss-120b-medium":      32768,
	}
	for model, want := range cases {
		if got := MaxCompletionTokensFor(model); got != want {
			t.Errorf("MaxCompletionTokensFor(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestDefaultModelsIncludesUpstreamCatalog(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range DefaultModels() {
		ids[m.ID] = true
	}
	for _, want := range []string{
		"claude-opus-5-5-high",
		"claude-sonnet-5-5-high",
		"gemini-4-argon-high",
		"gemini-3.8-flash-high",
		"gemini-3.1-flash-lite",
		"gemini-3.5-flash-lite",
		"gpt-oss-120b-medium",
	} {
		if !ids[want] {
			t.Errorf("DefaultModels missing %q", want)
		}
	}
}
