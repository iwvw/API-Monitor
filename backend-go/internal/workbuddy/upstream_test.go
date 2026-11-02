package workbuddy

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRewriteBlockedTemplates(t *testing.T) {
	in := "You are Claude Code, Anthropic's official CLI for Claude. Main branch (you will usually use this for PRs)"
	out := rewriteBlockedTemplates(in)
	if out == in {
		t.Fatal("期望模板句被改写，实际未变")
	}
	if want := "official CLI tool for Claude"; !contains(out, want) {
		t.Fatalf("身份句未按预期改写: %q", out)
	}
	if want := "Default branch"; !contains(out, want) {
		t.Fatalf("git 注入句未按预期改写: %q", out)
	}
	// 改写必须保持语义：除两处单词外不得有其它变化。
	if contains(out, "Anthropic's official CLI for Claude.") {
		t.Fatalf("身份句仍命中黑名单: %q", out)
	}
}

// TestNormalizeThinking 验证按上游声明的能力归一思考档位。
//
// 取代旧的 TestForceMaxThinking：旧逻辑对 hy3/hy4 前缀无条件强制 high，
// 但实测 wb/intl 的 hy3 已声明 supportedEfforts ["low","high"]，
// 用户要 low 是合法的，旧行为会把它改掉。现在按 supportedEfforts 归一。
func TestNormalizeThinking(t *testing.T) {
	// 完整的档位能力（仿 kimi-k2.8-preview / gpt-5.6 系列）。
	full := &ThinkingConfig{
		CanDisable:      true,
		DefaultEffort:   "high",
		SupportsEfforts: []string{"low", "high", "max"},
	}
	// 只给默认档、无档位列表（仿旧版 hy3/hy4 的形态）。
	defaultOnly := &ThinkingConfig{DefaultEffort: "high"}
	// 不可关闭 + 只有 high（仿 hy4-preview）。
	highOnly := &ThinkingConfig{DefaultEffort: "high", SupportsEfforts: []string{"high"}}

	cases := []struct {
		name     string
		thinking *ThinkingConfig
		payload  map[string]any
		want     string
		wantHas  bool
	}{
		// 有档位列表：合法档位必须原样保留（这是修复的核心）。未改动时 changed=false。
		{"hy3 wants low", full, map[string]any{"reasoning_effort": "low"}, "low", false},
		{"hy3 wants max", full, map[string]any{"reasoning_effort": "max"}, "max", false},
		{"hy3 wants xhigh", full, map[string]any{"reasoning_effort": "xhigh"}, "max", true},
		{"hy3 wants medium", full, map[string]any{"reasoning_effort": "medium"}, "high", true},
		{"hy3 no effort", full, map[string]any{}, "high", true},
		{"hy3 already high", full, map[string]any{"reasoning_effort": "high"}, "high", false},
		// reasoning:{effort} 形态也要识别并回写到 reasoning_effort。
		{"hy3 reasoning object", full, map[string]any{"reasoning": map[string]any{"effort": "low"}}, "low", true},
		// 只有默认档（无 supportedEfforts）：**必须显式传该档位**。
		// 实测 glm-5.1 不传 reasoning_effort 会 400 / code 11150，传 medium 或 high 均 200。
		{"default-only low", defaultOnly, map[string]any{"reasoning_effort": "low"}, "high", true},
		{"default-only empty", defaultOnly, map[string]any{}, "high", true},
		{"default-only same", defaultOnly, map[string]any{"reasoning_effort": "high"}, "high", false},
		// 只有 high：结果与旧 forceMaxThinking 一致，但不再靠前缀硬判。
		{"high-only medium", highOnly, map[string]any{"reasoning_effort": "medium"}, "high", true},
		// 无 thinking 信息：完全不动 body（保持透传）。
		{"no thinking info", nil, map[string]any{"reasoning_effort": "low"}, "low", false},
	}
	for _, c := range cases {
		got := normalizeThinking(c.thinking, c.payload)
		eff, ok := c.payload["reasoning_effort"].(string)
		if !ok {
			eff = ""
		}
		if eff != c.want {
			t.Errorf("%s: reasoning_effort=%q want=%q", c.name, eff, c.want)
		}
		if got != c.wantHas {
			t.Errorf("%s: changed=%v want=%v", c.name, got, c.wantHas)
		}
	}

	// 可关闭的模型请求 none → 显式写 none 关闭。
	disable := &ThinkingConfig{CanDisable: true, DefaultEffort: "high", SupportsEfforts: []string{"low", "high"}}
	p := map[string]any{"reasoning_effort": "none"}
	if !normalizeThinking(disable, p) {
		t.Error("disabling should report change")
	}
	if p["reasoning_effort"] != "none" {
		t.Errorf("disable: got %v want none", p["reasoning_effort"])
	}
	// 不可关闭的模型请求 none → 落到默认档，不写 none。
	noDisable := &ThinkingConfig{DefaultEffort: "high", SupportsEfforts: []string{"high"}}
	p2 := map[string]any{"reasoning_effort": "none"}
	normalizeThinking(noDisable, p2)
	if p2["reasoning_effort"] != "high" {
		t.Errorf("cannot-disable: got %v want high", p2["reasoning_effort"])
	}

	// 只有默认档（无 supportedEfforts）：必须**显式**写入该档位。
	// 这条反直觉但已实测确认（2026-10-20，办公版真实账号）：
	//   glm-5.1 不带 reasoning_effort → HTTP 400 code 11150
	//     "the reasoning effort value is not supported by the current model"
	//   带 medium 或 high → HTTP 200
	// 早期实现曾推断反了（以为不能传），导致 glm-5.1 全量 400。
	defaultOnlyModel := &ThinkingConfig{DefaultEffort: "medium"}
	for _, tc := range []struct {
		name    string
		payload map[string]any
	}{
		{"no effort", map[string]any{}},
		{"other effort", map[string]any{"reasoning_effort": "high"}},
		{"same effort", map[string]any{"reasoning_effort": "medium"}},
		// reasoning:{effort:...} 形式也必须被改写为顶层字段：
		// 上游只认 reasoning_effort，单给 reasoning 对象同样 11150。
		{"reasoning object", map[string]any{"reasoning": map[string]any{"effort": "medium"}}},
	} {
		normalizeThinking(defaultOnlyModel, tc.payload)
		if got := tc.payload["reasoning_effort"]; got != "medium" {
			t.Errorf("default-only %s: reasoning_effort = %v, want medium", tc.name, got)
		}
		// reasoning 对象在改写后必须被删除（上游不认）。
		if _, ok := tc.payload["reasoning"]; ok {
			t.Errorf("default-only %s: reasoning object should be removed", tc.name)
		}
	}
}

// TestThinkingConfigParsesUpstreamShapes 验证上游 reasoning 的三种形态。
func TestThinkingConfigParsesUpstreamShapes(t *testing.T) {
	// 形态 1：只有 effort + summary（无档位列表）。
	only := &upstreamReasoning{Effort: "medium", Summary: "auto"}
	tc := only.toThinkingConfig()
	if tc == nil || tc.DefaultEffort != "medium" || len(tc.SupportsEfforts) != 0 {
		t.Fatalf("shape1 = %+v", tc)
	}
	if tc.CanDisable {
		t.Fatal("shape1 should not allow disabling")
	}

	// 形态 2：完整能力（含 canDisableThinking 与 supportedEfforts）。
	yes := true
	full := &upstreamReasoning{
		CanDisableThinking: &yes,
		DefaultEffort:      "high",
		Summary:            "auto",
		// 故意乱序，验证会被排序。
		SupportedEfforts: []string{"max", "low", "high"},
	}
	tc = full.toThinkingConfig()
	if tc == nil || !tc.CanDisable || tc.DefaultEffort != "high" {
		t.Fatalf("shape2 = %+v", tc)
	}
	if got := strings.Join(tc.SupportsEfforts, ","); got != "low,high,max" {
		t.Fatalf("efforts should be sorted, got %q", got)
	}

	// 形态 3：无 canDisableThinking（视为不可关）。
	noFlag := &upstreamReasoning{DefaultEffort: "high", SupportedEfforts: []string{"low", "medium", "high", "xhigh"}}
	tc = noFlag.toThinkingConfig()
	if tc == nil || tc.CanDisable || len(tc.SupportsEfforts) != 4 {
		t.Fatalf("shape3 = %+v", tc)
	}

	// 缺失 reasoning 字段 → nil。
	var missing *upstreamReasoning
	if got := missing.toThinkingConfig(); got != nil {
		t.Fatalf("missing should be nil, got %+v", got)
	}

	// 默认档不在列表里 → 用最高档兜底。
	odd := &upstreamReasoning{DefaultEffort: "weird", SupportedEfforts: []string{"low", "high"}}
	tc = odd.toThinkingConfig()
	if tc.DefaultEffort != "high" {
		t.Fatalf("bogus default should fall back to highest, got %q", tc.DefaultEffort)
	}
}

// TestPromotionIndex 验证限时促销的解析：时间窗过滤、enabled 过滤、目录类条目排除、
// priority 取高。
func TestPromotionIndex(t *testing.T) {
	enabled := true
	disabled := false
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	items := []upstreamPromotion{
		{
			ID: "active-free", Kind: "discount", Enabled: &enabled, Priority: 200,
			ModelIDs: []string{"hy3"},
			Discount: struct {
				Factor            float64 `json:"factor"`
				DiscountedCredits string  `json:"discountedCredits"`
				DisplayMode       string  `json:"displayMode"`
			}{Factor: 0, DisplayMode: "replace"},
			Schedule: struct {
				Timezone   string `json:"timezone"`
				ValidFrom  string `json:"validFrom"`
				ValidUntil string `json:"validUntil"`
			}{ValidFrom: "2026-10-01T00:00:00+08:00", ValidUntil: "2026-11-01T00:00:00+08:00"},
			Badge: struct {
				Label string `json:"label"`
			}{Label: "Free now"},
		},
		{
			// 已过期：不该再生效。
			ID: "expired", Kind: "discount", Enabled: &enabled, Priority: 300,
			ModelIDs: []string{"old-model"},
			Discount: struct {
				Factor            float64 `json:"factor"`
				DiscountedCredits string  `json:"discountedCredits"`
				DisplayMode       string  `json:"displayMode"`
			}{Factor: 0},
			Schedule: struct {
				Timezone   string `json:"timezone"`
				ValidFrom  string `json:"validFrom"`
				ValidUntil string `json:"validUntil"`
			}{ValidFrom: "2026-08-01T00:00:00+08:00", ValidUntil: "2026-09-01T00:00:00+08:00"},
		},
		{
			// 已停用：不该生效。
			ID: "off", Kind: "discount", Enabled: &disabled, Priority: 400,
			ModelIDs: []string{"off-model"},
		},
		{
			// 目录类条目（无 kind/discount/schedule）：绝不能当免费。
			ID: "gpt-image-2.5-sunburst",
		},
	}
	idx := buildPromotionIndex(items, now)
	if p := idx["hy3"]; p == nil || !p.Free() || p.Label != "Free now" {
		t.Fatalf("hy3 promotion = %+v", p)
	}
	if _, ok := idx["old-model"]; ok {
		t.Fatal("expired promotion should be filtered out")
	}
	if _, ok := idx["off-model"]; ok {
		t.Fatal("disabled promotion should be filtered out")
	}
	if _, ok := idx["gpt-image-2.5-sunburst"]; ok {
		t.Fatal("catalog entry without discount must not be treated as promotion")
	}
}

// TestContextCheckWorkbuddy 验证上下文预检。
func TestContextCheckWorkbuddy(t *testing.T) {
	m := ModelInfo{ID: "deepseek-v3-1", ContextLength: 96000}
	if over, _ := contextCheck(m, 50000); over {
		t.Fatal("50k should pass under 96k")
	}
	over, reason := contextCheck(m, 120000)
	if !over || !strings.Contains(reason, "96000") || !strings.Contains(reason, "deepseek-v3-1") {
		t.Fatalf("should reject over limit, got over=%v reason=%q", over, reason)
	}
	// 上限未知时不校验。
	if over, _ := contextCheck(ModelInfo{ID: "x"}, 999999); over {
		t.Fatal("unknown limit should not reject")
	}
	// 估算必须是字符启发式且很快（曾用 tiktoken 会拖慢热路径）。
	big := strings.Repeat("测试hello ", 20000)
	start := time.Now()
	n := estimateTokens(big)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("estimate too slow: %s", elapsed)
	}
	if n <= 0 {
		t.Fatalf("estimate = %d", n)
	}
}

func TestRelayEmitsSSEFrame(t *testing.T) {
	// 本项目的中继面是真实 HTTP 端点、只说 OpenAI 协议，必须恒发标准 SSE 分帧。
	if !relayEmitsSSEFrame() {
		t.Fatal("中继面必须给 chunk 加 data: 分帧")
	}
}

func TestStripDataPrefix(t *testing.T) {
	cases := map[string]string{
		"data: {\"a\":1}":      "{\"a\":1}",
		"data:data: {\"a\":1}": "{\"a\":1}",
		"{\"a\":1}":            "{\"a\":1}",
		"   data: [DONE]  ":    "[DONE]",
		"":                     "",
	}
	for in, want := range cases {
		if got := stripDataPrefix(in); got != want {
			t.Errorf("stripDataPrefix(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestAggregateCompletion(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"abc","model":"hy3","created":1700000000,"choices":[{"delta":{"role":"assistant","content":"你"}}]}`,
		`data: {"choices":[{"delta":{"content":"好","tool_calls":[]}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"思考"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"c1","type":"function"}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	out, err := aggregateCompletion(strings.NewReader(sse), "hy3", nil)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("聚合结果不是合法 JSON: %v", err)
	}
	if obj["object"] != "chat.completion" {
		t.Errorf("object=%v want=chat.completion", obj["object"])
	}
	if obj["id"] != "abc" || obj["model"] != "hy3" {
		t.Errorf("id/model 未回填: %v / %v", obj["id"], obj["model"])
	}
	choices := obj["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("choices 数量应为 1，得到 %d", len(choices))
	}
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason=%v want=tool_calls", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != "你好" {
		t.Errorf("content 累加错误: %v", msg["content"])
	}
	if msg["reasoning_content"] != "思考" {
		t.Errorf("reasoning_content 应保留: %v", msg["reasoning_content"])
	}
	if tcs, ok := msg["tool_calls"].([]any); !ok || len(tcs) != 1 {
		t.Errorf("tool_calls 应聚合为 1 条: %v", msg["tool_calls"])
	}
	if _, ok := obj["usage"]; !ok {
		t.Error("usage 应透传")
	}
}

func TestAggregateCompletionFallsBackToModelAndDefaults(t *testing.T) {
	out, err := aggregateCompletion(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n"), "glm-5.2", nil)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["model"] != "glm-5.2" {
		t.Errorf("上游未回 model 时应回填请求模型，得到 %v", obj["model"])
	}
	if obj["id"] != "chatcmpl-workbuddy" {
		t.Errorf("上游未回 id 时应使用兜底 id，得到 %v", obj["id"])
	}
	choice := obj["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason 兜底应为 stop，得到 %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Errorf("role 兜底应为 assistant，得到 %v", msg["role"])
	}
}

// 上游中途断流时不得把截断的正文当成完整回答返回：scanner 的读错误必须上抛，
// 否则非流式调用方会收到「内容不全但 finish_reason=stop」的假成功。
func TestAggregateCompletionReportsUpstreamTruncation(t *testing.T) {
	// 模拟传输层在读完首个完整 chunk 后中断。
	stream := strings.NewReader(`data: {"id":"abc","choices":[{"delta":{"content":"前半段"}}]}` + "\n")
	r := io.MultiReader(stream, errReader{})

	if _, err := aggregateCompletion(r, "hy3", nil); err == nil {
		t.Fatal("上游读取中断时应返回错误，实际返回成功（截断正文被当成完整回答）")
	}
}

// errReader 始终返回一个非 EOF 错误，模拟上游连接中断。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }

// 缓存命中字段各家命名不同，必须都归一到网关唯一认得的
// usage.prompt_tokens_details.cached_tokens，否则统计恒为 0。
func TestNormalizeUsageCache(t *testing.T) {
	cases := []struct {
		name  string
		usage map[string]any
		want  any
	}{
		{"标准形状不动", map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": 123}}, int64(123)},
		{"DeepSeek / CodeBuddy 命名", map[string]any{"prompt_cache_hit_tokens": 456}, int64(456)},
		{"腾讯协议文档命名", map[string]any{"cache_read_tokens": 321}, int64(321)},
		{"写入缓存不算命中", map[string]any{"cache_write_tokens": 999}, nil},
		{"顶层 cached_tokens", map[string]any{"cached_tokens": 789}, int64(789)},
		{"Gemini 原生", map[string]any{"total_cached_tokens": 11}, int64(11)},
		{"别名在 details 内", map[string]any{"prompt_tokens_details": map[string]any{"prompt_cache_hit_tokens": 42}}, int64(42)},
		{"无缓存字段", map[string]any{"prompt_tokens": 5, "completion_tokens": 2}, nil},
		{"显式 0 保留（缓存未命中是有效值）", map[string]any{"prompt_cache_hit_tokens": 0}, int64(0)},
	}
	for _, c := range cases {
		normalizeUsageCache(c.usage)
		details, _ := c.usage["prompt_tokens_details"].(map[string]any)
		var got any
		if details != nil {
			got = details["cached_tokens"]
		}
		if c.want == nil {
			if got != nil {
				t.Errorf("%s: 上游没报缓存字段时不应写入 cached_tokens，得到 %v", c.name, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: cached_tokens=%v want=%v", c.name, got, c.want)
		}
	}
}

// 上游把命中数发成**字符串**时必须转成裸数字 —— 否则下游正则读不到（网关统计恒 0）。
func TestNormalizeUsageCacheCoercesStringNumbers(t *testing.T) {
	cases := []struct {
		name  string
		usage map[string]any
		want  any
	}{
		{"字符串别名", map[string]any{"prompt_cache_hit_tokens": "930560"}, int64(930560)},
		{"字符串标准字段", map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": "42"}}, int64(42)},
		{"数字别名", map[string]any{"prompt_cache_hit_tokens": float64(7)}, int64(7)},
		{"小数字符串", map[string]any{"cache_read_tokens": "1.5"}, 1.5},
		{"非数字字符串应丢弃", map[string]any{"cache_read_tokens": "n/a"}, nil},
		{"对象应丢弃", map[string]any{"cache_read_tokens": map[string]any{"v": 1}}, nil},
	}
	for _, c := range cases {
		normalizeUsageCache(c.usage)
		details, _ := c.usage["prompt_tokens_details"].(map[string]any)
		var got any
		if details != nil {
			got = details["cached_tokens"]
		}
		if c.want == nil {
			if got != nil {
				t.Errorf("%s: 不应产出 cached_tokens，得到 %#v", c.name, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: cached_tokens=%#v (%T) want=%#v (%T)", c.name, got, got, c.want, c.want)
		}
	}
}

// firstCachedTokensInJSON 复刻网关的取数语义：整段字节流里**第一个**
// `"cached_tokens":<整数>`（网关用的是字面量正则 + FindStringSubmatch）。
// 刻意不用正则实现，避免与生产代码同源而掩盖问题。
func firstCachedTokensInJSON(t *testing.T, s string) int {
	t.Helper()
	const key = `"cached_tokens":`
	idx := strings.Index(s, key)
	if idx < 0 {
		t.Fatal("输出里没有 cached_tokens")
	}
	rest := s[idx+len(key):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		t.Fatalf("cached_tokens 后面不是裸数字（网关正则读不到）: %.40q", rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// 用 _diag 抓到的**真实上游报文**做回归。这是整条链路上最要命的一处：
// 上游在顶层放了恒 0 的同名占位字段 `cached_tokens`，而网关的正则取第一个匹配 ——
// 归一化若只写 prompt_tokens_details，网关永远读到 0（插件账本读 details 却是对的）。
func TestNormalizeUsageCacheBeatsTopLevelDecoy(t *testing.T) {
	usage := `{"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cached_tokens":0,` +
		`"completion_thinking_tokens":1107,"completion_tokens":1261,` +
		`"completion_tokens_details":{"cached_tokens":0,"reasoning_tokens":1107},` +
		`"credit":0.28,"prompt_cache_hit_tokens":131712,"prompt_cache_miss_tokens":2246,` +
		`"prompt_cache_write_tokens":0,"prompt_tokens":133958,` +
		`"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":129664,"reasoning_tokens":1107},` +
		`"total_tokens":135219}`
	out := cleanChunkJSON(`{"choices":[],"usage":` + usage + `}`)

	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	u := obj["usage"].(map[string]any)
	details := u["prompt_tokens_details"].(map[string]any)

	if u["cached_tokens"] != float64(129664) {
		t.Errorf("顶层 cached_tokens 未覆盖成真值: %v", u["cached_tokens"])
	}
	if details["cached_tokens"] != float64(129664) {
		t.Errorf("details cached_tokens 不对: %v", details["cached_tokens"])
	}
	// 其它字段必须原样保留，别被归一化改坏。
	if u["credit"] != float64(0.28) || u["prompt_tokens"] != float64(133958) ||
		u["completion_tokens"] != float64(1261) || u["total_tokens"] != float64(135219) {
		t.Errorf("其它 usage 字段被改坏: %v", u)
	}

	// 关键断言：按网关语义取第一个匹配，必须是真值。
	if got := firstCachedTokensInJSON(t, out); got != 129664 {
		t.Fatalf("网关会读到 %d，应为 129664（被顶层占位字段抢先匹配）", got)
	}
}

// 全为 0（真实未命中）时不能凭空造值。
func TestNormalizeUsageCacheAllZeroStaysZero(t *testing.T) {
	out := cleanChunkJSON(`{"choices":[],"usage":{"cached_tokens":0,"prompt_cache_hit_tokens":0,"prompt_tokens_details":{"cached_tokens":0}}}`)
	if got := firstCachedTokensInJSON(t, out); got != 0 {
		t.Fatalf("全 0 时首个匹配应为 0，得到 %d", got)
	}
}

func TestCleanChunkJSONNormalizesCacheFields(t *testing.T) {
	in := `{"usage":{"prompt_tokens":10,"completion_tokens":1,"prompt_cache_hit_tokens":8},` +
		`"choices":[{"delta":{"content":"hi","tool_calls":[]}}]}`
	out := cleanChunkJSON(in)
	if !strings.Contains(out, `"cached_tokens":8`) {
		t.Fatalf("未把别名归一为标准 cached_tokens: %s", out)
	}
	// 空值 delta 清理不能因为新增逻辑而失效。
	if strings.Contains(out, `"tool_calls":[]`) {
		t.Fatalf("空值 delta 字段应被清理: %s", out)
	}
}

func TestUsageReportsCacheDetectsAliases(t *testing.T) {
	cases := []struct {
		name  string
		usage map[string]any
		want  bool
	}{
		{"标准", map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": 1}}, true},
		{"DeepSeek", map[string]any{"prompt_cache_hit_tokens": 1}, true},
		{"没有", map[string]any{"prompt_tokens": 9}, false},
		{"空对象", map[string]any{}, false},
	}
	for _, c := range cases {
		if got := usageReportsCache(c.usage); got != c.want {
			t.Errorf("%s: usageReportsCache=%v want=%v", c.name, got, c.want)
		}
	}
}

// 诊断快照：正常调用即可暴露上游真实上报的 usage 字段，无需额外探测请求。
func TestObserveUsageSnapshot(t *testing.T) {
	s := &Service{}
	if s.upstreamUsageInfo() != nil {
		t.Fatal("尚无调用时不应有快照")
	}

	s.observeUsage(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"cached_tokens":7}}`)
	info := s.upstreamUsageInfo()
	if info == nil {
		t.Fatal("应记录 usage 快照")
	}
	if info["cacheReported"] != true {
		t.Errorf("上游带了 cached_tokens，cacheReported 应为 true: %+v", info)
	}

	// CodeBuddy 实测的真实形状：DeepSeek 风格命名 + credit，必须被识别为"已上报"。
	s.observeUsage(`{"usage":{"prompt_tokens":65717,"completion_tokens":499,"total_tokens":66216,` +
		`"prompt_cache_hit_tokens":31020,"prompt_cache_miss_tokens":34697,"credit":0.03}}`)
	info = s.upstreamUsageInfo()
	if info["cacheReported"] != true {
		t.Errorf("CodeBuddy 带 prompt_cache_hit_tokens，cacheReported 应为 true: %+v", info)
	}
	keys, _ := info["keys"].([]string)
	if len(keys) != 6 {
		t.Errorf("keys=%v，应为 6 个字段（含 credit）", keys)
	}

	// 完全不带缓存字段的 usage 才是 false。
	s.observeUsage(`{"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	info = s.upstreamUsageInfo()
	if info["cacheReported"] != false {
		t.Errorf("上游未上报缓存字段时 cacheReported 应为 false: %+v", info)
	}

	// 不含 usage 的 chunk 不应覆盖已有快照。
	s.observeUsage(`{"choices":[{"delta":{"content":"hi"}}]}`)
	if info2 := s.upstreamUsageInfo(); info2["sample"] != info["sample"] {
		t.Error("无 usage 的 chunk 不应覆盖快照")
	}
}

// 倍率解析失败时必须是「未知」而不是 0 —— 0 会被误读成免费。
func TestParseCredits(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"x0.79 credits", 0.79, true},
		{"x2.00 credits", 2, true},
		{"X5.00 credits", 5, true},
		{"x0.00 credits", 0, true},
		// 国际版格式：无 "credits" 后缀，甚至无前导 x。
		{"x0.00", 0, true},
		{"x6.67", 6.67, true},
		{"  0.51  ", 0.51, true},
		{"", 0, false},
		{"credits", 0, false},
		{"x- credits", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCredits(c.in)
		if ok != c.ok {
			t.Errorf("parseCredits(%q) ok=%v want=%v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseCredits(%q)=%v want=%v", c.in, got, c.want)
		}
	}
}

func TestCleanChunkJSON(t *testing.T) {
	in := `{"id":"x","choices":[{"delta":{"content":"hi","tool_calls":[],"function_call":null,"reasoning_content":""}}]}`
	out := cleanChunkJSON(in)
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	delta := obj["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if _, ok := delta["tool_calls"]; ok {
		t.Error("空数组 tool_calls 应被清理")
	}
	if _, ok := delta["function_call"]; ok {
		t.Error("null function_call 应被清理")
	}
	if _, ok := delta["reasoning_content"]; ok {
		t.Error("空字符串 reasoning_content 应被清理")
	}
	if delta["content"] != "hi" {
		t.Errorf("非空字段不应被清理: %v", delta["content"])
	}
}

func TestCleanChunkJSONKeepsInvalidInput(t *testing.T) {
	in := "not json"
	if got := cleanChunkJSON(in); got != in {
		t.Errorf("非法 JSON 应原样返回，得到 %q", got)
	}
}

func TestForceStreamBody(t *testing.T) {
	out := forceStreamBody([]byte(`{"model":"hy3","stream":false}`))
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if obj["stream"] != true {
		t.Errorf("stream 应被强制为 true，得到 %v", obj["stream"])
	}
	if obj["model"] != "hy3" {
		t.Errorf("其它字段应保留，得到 %v", obj["model"])
	}
	// 必须补 include_usage，否则上游不回 usage，缓存命中与用量统计全是 0。
	opts, ok := obj["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Errorf("应注入 stream_options.include_usage=true，得到 %v", obj["stream_options"])
	}
}

// 客户端已带 stream_options 时要合并，不能整体覆盖掉它的其它选项。
func TestForceStreamBodyMergesStreamOptions(t *testing.T) {
	out := forceStreamBody([]byte(`{"stream":true,"stream_options":{"foo":"bar"}}`))
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	opts := obj["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Errorf("include_usage 未注入: %v", opts)
	}
	if opts["foo"] != "bar" {
		t.Errorf("既有 stream_options 选项被覆盖: %v", opts)
	}
}

func TestRewriteForUpstreamHandlesStringAndParts(t *testing.T) {
	body, err := decodeMessages([]any{
		map[string]any{"role": "system", "content": "You are Claude Code, Anthropic's official CLI for Claude."},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "Main branch (you will usually use this for PRs)"},
		}},
	}, "hy3")
	if err != nil {
		t.Fatal(err)
	}
	out := rewriteForUpstream(body, "hy3")
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	msgs := obj["messages"].([]any)
	if got := msgs[0].(map[string]any)["content"].(string); !contains(got, "CLI tool for Claude") {
		t.Errorf("字符串 content 未改写: %q", got)
	}
	parts := msgs[1].(map[string]any)["content"].([]any)
	if got := parts[0].(map[string]any)["text"].(string); !contains(got, "Default branch") {
		t.Errorf("多模态 parts 未改写: %q", got)
	}
	// 无思考能力信息时不写 reasoning_effort（保持透传，改写只做模板清理）。
	if _, ok := obj["reasoning_effort"]; ok {
		t.Errorf("thinking 未知时不应写入 reasoning_effort，得到 %v", obj["reasoning_effort"])
	}
}

func TestRewriteForUpstreamNoopWhenNothingToChange(t *testing.T) {
	// 首条已是 system：此时没有模板句、没有思考档位、模型名无需写回，
	// 唯一会触发改写的「补 system」也不成立，因此必须原样返回字节。
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}]}`)
	out := rewriteForUpstream(body, "deepseek-v4-pro")
	if string(out) != string(body) {
		// 无改动时必须原样返回，避免无意义的重编码改动上游看到的字节。
		t.Errorf("无改动时不应重写 body\n got=%s\nwant=%s", out, body)
	}
}

func TestRewriteForUpstreamKeepsInvalidInput(t *testing.T) {
	in := []byte("not json")
	if got := rewriteForUpstream(in, "hy3"); string(got) != string(in) {
		t.Errorf("非法 JSON 应原样返回，得到 %q", got)
	}
}

func TestTokenStateAndAvailability(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		acc   Account
		state string
		avail bool
	}{
		{"无 token", Account{ID: "a"}, "unknown", false},
		{"无过期信息", Account{ID: "a", AccessToken: "t"}, "unknown", true},
		{"有效", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(2 * time.Hour).Unix()}, "valid", true},
		{"即将过期", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(5 * time.Minute).Unix()}, "expiring", true},
		{"已过期", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(-time.Minute).Unix()}, "expired", false},
		{"停用", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(time.Hour).Unix(), Disabled: true}, "valid", false},
	}
	for _, c := range cases {
		if got := tokenState(c.acc); got != c.state {
			t.Errorf("%s: tokenState=%q want=%q", c.name, got, c.state)
		}
		if got := accountAvailable(c.acc); got != c.avail {
			t.Errorf("%s: accountAvailable=%v want=%v", c.name, got, c.avail)
		}
	}
}

func TestRemapPrefixedName(t *testing.T) {
	cases := []struct{ name, oldP, newP, want string }{
		{"ds2-x", "ds2-", "wb-", "wb-x"},
		{"x", "", "wb-", "wb-x"},
		{"wb-x", "wb-", "", "x"},
		{"x", "", "", "x"},
	}
	for _, c := range cases {
		if got := remapPrefixedName(c.name, c.oldP, c.newP); got != c.want {
			t.Errorf("remapPrefixedName(%q,%q,%q)=%q want=%q", c.name, c.oldP, c.newP, got, c.want)
		}
	}
}

func TestDefaultSettingsNotEmpty(t *testing.T) {
	st := defaultSettings()
	if st.DisabledModels == nil || st.Accounts == nil {
		t.Fatal("默认设置的切片不得为 nil（JSON 序列化会变成 null，前端需按数组处理）")
	}
	if st.Enabled {
		t.Fatal("默认应关闭")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestEnsureSystemPrompt 验证首条消息不是 system 时自动补一条。
//
// 上游硬性要求（实测 2026-10-20，连续 3 次稳定复现，不是抽样校验）：
//
//	HTTP 400 {"code":11128,"msg":"first message is not system prompt"}
//
// 补一条 system 后立即 200。这条验证的正是该补齐逻辑。
func TestEnsureSystemPrompt(t *testing.T) {
	// 首条是 user → 插入 system，其余顺序不变。
	payload := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
		},
	}
	if !ensureSystemPrompt(payload) {
		t.Fatal("should insert system prompt")
	}
	msgs := payload["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if role := msgs[0].(map[string]any)["role"]; role != "system" {
		t.Fatalf("first role = %v, want system", role)
	}
	if role := msgs[1].(map[string]any)["role"]; role != "user" {
		t.Fatalf("second role = %v, want user (原顺序必须保持)", role)
	}

	// 首条已是 system → 不动。
	withSystem := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "custom"},
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	if ensureSystemPrompt(withSystem) {
		t.Fatal("should not touch existing system prompt")
	}
	// 大小写与前后空白也应识别为 system。
	oddCase := map[string]any{
		"messages": []any{map[string]any{"role": " System ", "content": "x"}},
	}
	if ensureSystemPrompt(oddCase) {
		t.Fatal("role matching should be case/space insensitive")
	}
	// 空 messages / 缺失 messages → 不动（交上游判非法）。
	if ensureSystemPrompt(map[string]any{}) {
		t.Fatal("missing messages should be untouched")
	}
	if ensureSystemPrompt(map[string]any{"messages": []any{}}) {
		t.Fatal("empty messages should be untouched")
	}
}

// TestRewriteAddsSystemPrompt 验证改写链路里确实会补 system。
func TestRewriteAddsSystemPrompt(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}]}`)
	out := rewriteForUpstreamWithThinking(body, "deepseek-v4.1-flash", nil)
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	msgs := obj["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("expected injected system prompt, got %v", msgs)
	}
}
