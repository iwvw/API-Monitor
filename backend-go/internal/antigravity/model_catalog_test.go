package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// TestQuotaBucketZeroIsSerialized 钉住「真 0%」与「字段缺失」的区别。
//
// RemainingFraction 之前是 float64 + omitempty：上游把窗口用到 0 时返回的
// 就是数字 0，omitempty 会把 0 当空值整个删掉，前端于是拿到 undefined 显示
// 「—」，看起来像没数据而不是已耗尽。
func TestQuotaBucketZeroIsSerialized(t *testing.T) {
	zero := 0.0
	raw, err := json.Marshal(engineag.UserQuotaBucket{DisplayName: "Weekly", RemainingFraction: &zero})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if !strings.Contains(string(raw), `"remainingFraction":0`) {
		t.Fatalf("已耗尽的 bucket 必须序列化出 0, got %s", raw)
	}

	// 字段缺失时不应凭空造出 0。
	rawMissing, err := json.Marshal(engineag.UserQuotaBucket{DisplayName: "Weekly"})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if strings.Contains(string(rawMissing), "remainingFraction") {
		t.Fatalf("缺失的 fraction 不应出现在 JSON 里, got %s", rawMissing)
	}
}

// TestQuotaWeightOfKeepsExhaustedBucket 确认「已耗尽」不会被当成「无数据」跳过。
func TestQuotaWeightOfKeepsExhaustedBucket(t *testing.T) {
	zero := 0.0
	view := &QuotaView{Groups: []engineag.UserQuotaGroup{{
		Buckets: []engineag.UserQuotaBucket{{RemainingFraction: &zero}},
	}}}
	// 只剩一个 0% 窗口时，权重必须是 0 而不是「无数据」。
	if got := quotaWeightOf(view); got != 0 {
		t.Fatalf("quotaWeightOf = %v, want 0", got)
	}

	// 混入缺失字段的 bucket 时，应按已有数据算最小比例，不被 nil 拉低成 0。
	half := 0.5
	mixed := &QuotaView{Groups: []engineag.UserQuotaGroup{{
		Buckets: []engineag.UserQuotaBucket{
			{RemainingFraction: nil},
			{RemainingFraction: &half},
		},
	}}}
	if got := quotaWeightOf(mixed); got != 0.5 {
		t.Fatalf("quotaWeightOf(mixed) = %v, want 0.5（nil 不应被当成 0）", got)
	}
}

// TestFilterExposedModelsDropsInternalCodenames 覆盖模型目录交集。
//
// 上游 fetchAvailableModels 会把内部实验代号一起返回（例如
// tab_jump_flash_lite_preview），直接透传会被客户端当成正式模型。
func TestFilterExposedModelsDropsInternalCodenames(t *testing.T) {
	upstream := map[string]engineag.ModelInfo{
		"claude-sonnet-4-6":                 {DisplayName: "Claude Sonnet 4.6"},
		"gemini-4-argon-high":               {DisplayName: "Gemini 4 Argon"},
		"gpt-oss-120b-medium":               {DisplayName: "GPT-OSS 120B"},
		"tab_jump_flash_lite_preview":       {DisplayName: "internal"},
		"tab_flash_lite_preview":            {DisplayName: "internal"},
		"gemini-2.5-flash-thinking":         {DisplayName: "Claude/Gemini thinking"},
		"some-undocumented-experimental-id": {DisplayName: "internal"},
	}

	got := engineag.FilterExposedModels(upstream)

	for _, keep := range []string{"claude-sonnet-4-6", "gemini-4-argon-high", "gpt-oss-120b-medium"} {
		if _, ok := got[keep]; !ok {
			t.Errorf("白名单模型 %q 不应被过滤掉", keep)
		}
	}
	for _, drop := range []string{"tab_jump_flash_lite_preview", "tab_flash_lite_preview", "some-undocumented-experimental-id"} {
		if _, ok := got[drop]; ok {
			t.Errorf("内部实验代号 %q 不应对外暴露", drop)
		}
	}

	// 不得修改入参（调用方还可能在别处用它）。
	if len(upstream) != 7 {
		t.Errorf("FilterExposedModels 不应修改入参, got %d entries", len(upstream))
	}
}

// TestFilterExposedModelsEmpty 空输入原样返回，不 panic。
func TestFilterExposedModelsEmpty(t *testing.T) {
	if got := engineag.FilterExposedModels(nil); got != nil {
		t.Fatalf("nil 输入应返回 nil, got %v", got)
	}
	if got := engineag.FilterExposedModels(map[string]engineag.ModelInfo{}); len(got) != 0 {
		t.Fatalf("空 map 应返回空 map, got %v", got)
	}
}

// TestIsExposedModelCaseInsensitive 上游模型 ID 大小写不一定稳定。
func TestIsExposedModelCaseInsensitive(t *testing.T) {
	if !engineag.IsExposedModel("Claude-Sonnet-4-6") {
		t.Error("大小写不同的白名单模型应被识别")
	}
	if engineag.IsExposedModel("tab_jump_flash_lite_preview") {
		t.Error("内部代号不应在白名单内")
	}
	if engineag.IsExposedModel("") {
		t.Error("空 ID 不应在白名单内")
	}
}
