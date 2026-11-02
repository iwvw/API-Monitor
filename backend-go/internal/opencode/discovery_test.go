package opencode

import (
	"strings"
	"testing"
	"time"
)

// buildCatalog 构造一个带固定模型的目录样本用于测试。
func buildCatalog() openCodeCatalog {
	costFree := map[string]interface{}{"input": float64(0), "output": float64(0), "cache_read": float64(0)}
	return openCodeCatalog{
		"opencode": catalogProvider{Models: map[string]catalogModel{
			"mimo-v2.6-flash-free": {
				ID: "mimo-v2.6-flash-free", Name: "MiMo-V2.6-Flash Free",
				Cost: costFree, Limit: map[string]interface{}{"context": float64(200000), "output": float64(32000)},
				Reasoning: true, ToolCall: true,
			},
			"muse-spark-1.3-contributor-free": {
				ID: "muse-spark-1.3-contributor-free", Name: "Muse Spark 1.3 Free",
				Cost: costFree, Limit: map[string]interface{}{"context": float64(1048576), "output": float64(131072)},
				Reasoning: true, ToolCall: true,
			},
			"longcat-2.5-preview-free": {
				ID: "longcat-2.5-preview-free", Name: "Longcat 2.5 Preview Free",
				Cost: costFree, Limit: map[string]interface{}{"context": float64(1000000), "output": float64(131072)},
				Reasoning: true, ToolCall: true,
			},
			// 已废弃：应被过滤掉。
			"muse-spark-1.2-contributor-free": {
				ID: "muse-spark-1.2-contributor-free", Name: "Muse Spark 1.2 Free (deprecated)",
				Cost: costFree, Status: "deprecated",
			},
			// 付费模型（id 不含 free）：应被过滤掉。
			"claude-opus-5": {
				ID: "claude-opus-5", Name: "Claude Opus 5",
				Cost: map[string]interface{}{"input": float64(15), "output": float64(75)},
			},
			// 免费后缀但成本非零（异常数据）：应被过滤掉。
			"weird-free": {
				ID: "weird-free", Name: "Weird Free",
				Cost: map[string]interface{}{"input": float64(0), "output": float64(0.5)},
			},
		}},
		// 其他 provider 的免费模型：不属于 opencode 上游，应被过滤掉。
		"anthropic": catalogProvider{Models: map[string]catalogModel{
			"claude-sonnet-5-free": {ID: "claude-sonnet-5-free", Name: "Sonnet 5 Free", Cost: map[string]interface{}{"input": float64(0), "output": float64(0)}},
		}},
	}
}

func TestFilterCatalogFreeModels(t *testing.T) {
	out := filterCatalogFreeModels(buildCatalog())
	if len(out) != 3 {
		t.Fatalf("expected 3 free models, got %d: %+v", len(out), out)
	}
	byID := map[string]DiscoveredModel{}
	for _, d := range out {
		byID[d.ID] = d
	}
	if _, ok := byID["muse-spark-1.2-contributor-free"]; ok {
		t.Fatalf("deprecated model should be excluded")
	}
	if _, ok := byID["claude-opus-5"]; ok {
		t.Fatalf("paid model should be excluded")
	}
	if _, ok := byID["weird-free"]; ok {
		t.Fatalf("non-zero cost model should be excluded")
	}
	if _, ok := byID["claude-sonnet-5-free"]; ok {
		t.Fatalf("other-provider free model should be excluded")
	}
	if m := byID["mimo-v2.6-flash-free"]; m.Protocol != "chat" || m.ContextLength != 200000 || m.MaxOutputTokens != 32000 || !m.SupportsReasoning || !m.SupportsToolCall {
		t.Fatalf("metadata extraction wrong: %+v", m)
	}
	if m := byID["muse-spark-1.3-contributor-free"]; m.Protocol != "responses" {
		t.Fatalf("muse-spark should default to responses, got %q", m.Protocol)
	}
}

func TestMergeDiscovered(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DiscoveredModel{
		{ID: "mimo-v2.6-flash-free", Protocol: "chat", Available: true, Excluded: false, NextProbeAt: now, LastChecked: now},
		{ID: "gone-model-free", Protocol: "chat", Available: true},
	}
	fresh := []DiscoveredModel{
		{ID: "mimo-v2.6-flash-free", Protocol: "chat", DisplayName: "MiMo"},
		{ID: "new-model-free", Protocol: "chat", DisplayName: "New"},
	}
	merged := mergeDiscovered(existing, fresh)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged, got %d", len(merged))
	}
	byID := map[string]DiscoveredModel{}
	for _, d := range merged {
		byID[d.ID] = d
	}
	if _, ok := byID["gone-model-free"]; ok {
		t.Fatalf("model removed from catalog should be dropped")
	}
	m := byID["mimo-v2.6-flash-free"]
	if !m.Available || m.LastChecked != now {
		t.Fatalf("existing probe state should be preserved: %+v", m)
	}
	if m.DisplayName != "MiMo" {
		t.Fatalf("fresh metadata should win: %+v", m)
	}
	if _, ok := byID["new-model-free"]; !ok {
		t.Fatalf("new model should be added")
	}
}

func TestEffectiveCatalogFrom(t *testing.T) {
	seedLen := len(staticModels)
	// 自动发现关闭 → 种子。
	st := Settings{AutoDetect: false, DisabledModels: []string{}}
	if got := len(effectiveCatalogFrom(st)); got != seedLen {
		t.Fatalf("autoDetect off should fall back to seed, got %d", got)
	}
	// 自动发现开启但无结果 → 种子。
	st = Settings{AutoDetect: true, DiscoveredModels: []DiscoveredModel{}, DisabledModels: []string{}}
	if got := len(effectiveCatalogFrom(st)); got != seedLen {
		t.Fatalf("empty discovered should fall back to seed, got %d", got)
	}
	// 自动发现开启且有结果 → 发现结果（排除确认退役）。
	st = Settings{AutoDetect: true, DisabledModels: []string{}, DiscoveredModels: []DiscoveredModel{
		{ID: "mimo-v2.6-flash-free", Protocol: "chat", Available: true},
		{ID: "dead-free", Protocol: "chat", Excluded: true},
		{ID: "unprobed-free", Protocol: "chat"},
	}}
	out := effectiveCatalogFrom(st)
	if len(out) != 2 {
		t.Fatalf("expected 2 effective (excluded dropped), got %d", len(out))
	}
	for _, m := range out {
		if m.ID == "dead-free" {
			t.Fatalf("excluded model should not be served")
		}
	}
}

func TestEffectiveIDsFromPrefix(t *testing.T) {
	st := Settings{
		AutoDetect:       true,
		ModelPrefix:      "oc-",
		DisabledModels:   []string{},
		DiscoveredModels: []DiscoveredModel{{ID: "mimo-v2.6-flash-free", Protocol: "chat"}},
	}
	ids := effectiveIDsFrom(st)
	if len(ids) != 1 || ids[0] != "oc-mimo-v2.6-flash-free" {
		t.Fatalf("prefixed ids wrong: %v", ids)
	}
	st.ModelPrefix = ""
	ids = effectiveIDsFrom(st)
	if len(ids) != 1 || ids[0] != "mimo-v2.6-flash-free" {
		t.Fatalf("bare ids wrong: %v", ids)
	}
}

func TestProbeDue(t *testing.T) {
	now := time.Now().UTC()
	// 空 NextProbeAt → 立即到期。
	if !probeDue(DiscoveredModel{}, now) {
		t.Fatalf("empty NextProbeAt should be due")
	}
	// 未来时间 → 未到期。
	future := now.Add(2 * time.Hour).Format(time.RFC3339)
	if probeDue(DiscoveredModel{NextProbeAt: future}, now) {
		t.Fatalf("future NextProbeAt should not be due")
	}
	// 过去时间 → 到期。
	past := now.Add(-2 * time.Hour).Format(time.RFC3339)
	if !probeDue(DiscoveredModel{NextProbeAt: past}, now) {
		t.Fatalf("past NextProbeAt should be due")
	}
	// 非法时间 → 立即到期（安全侧）。
	if !probeDue(DiscoveredModel{NextProbeAt: "garbage"}, now) {
		t.Fatalf("invalid NextProbeAt should be due")
	}
}

func TestModelMissingText(t *testing.T) {
	cases := map[string]bool{
		`{"error":{"message":"model not found"}}`:             true,
		`{"error":{"message":"Unknown model 'x-free'"}}`:       true,
		`{"error":{"message":"model does not exist"}}`:         true,
		`{"error":{"message":"OpenCode free tier expired"}}`:   false,
		`{"error":{"message":"rate limit exceeded"}}`:          false,
		`<html>Bad Gateway</html>`:                             false,
		`{"error":{"message":"upstream error"}}`:               false,
	}
	for text, want := range cases {
		if got := modelMissingText(text); got != want {
			t.Fatalf("modelMissingText(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestProbeSSEOk(t *testing.T) {
	chat := strings.Join([]string{
		`data: {"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n")
	if !probeSSEOk(chat) {
		t.Fatalf("valid chat SSE should count as ok")
	}
	responses := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		`data: {"type":"response.completed","response":{}}`,
	}, "\n")
	if !probeSSEOk(responses) {
		t.Fatalf("valid responses SSE should count as ok")
	}
	errSSE := `data: {"error":{"message":"free tier"}}`
	if probeSSEOk(errSSE) {
		t.Fatalf("error-only SSE should not count as ok")
	}
	empty := ""
	if probeSSEOk(empty) {
		t.Fatalf("empty body should not count as ok")
	}
}

func TestModelProtocol(t *testing.T) {
	s := &Service{}
	s.settings = Settings{
		AutoDetect: true,
		DisabledModels: []string{},
		DiscoveredModels: []DiscoveredModel{
			{ID: "mimo-v2.6-flash-free", Protocol: "chat"},
			{ID: "custom-responses-free", Protocol: "responses"},
		},
	}
	if got := s.modelProtocol("mimo-v2.6-flash-free"); got != "chat" {
		t.Fatalf("expected chat, got %q", got)
	}
	if got := s.modelProtocol("custom-responses-free"); got != "responses" {
		t.Fatalf("expected responses, got %q", got)
	}
	// 未在发现列表里的 muse-spark 沿用既有约定。
	if got := s.modelProtocol("muse-spark-9.9-contributor-free"); got != "responses" {
		t.Fatalf("muse-spark fallback should be responses, got %q", got)
	}
	// 未知模型默认 chat。
	if got := s.modelProtocol("brand-new-free"); got != "chat" {
		t.Fatalf("unknown model should default to chat, got %q", got)
	}
}

func TestFilterSortStable(t *testing.T) {
	cat := openCodeCatalog{"opencode": catalogProvider{Models: map[string]catalogModel{
		"b-free":  {ID: "b-free", Name: "B", Cost: map[string]interface{}{"input": float64(0), "output": float64(0)}},
		"a-free":  {ID: "a-free", Name: "A", Cost: map[string]interface{}{"input": float64(0), "output": float64(0)}},
	}}}
	out := filterCatalogFreeModels(cat)
	if len(out) != 2 || out[0].ID != "a-free" || out[1].ID != "b-free" {
		t.Fatalf("results should be sorted by id: %+v", out)
	}
}
