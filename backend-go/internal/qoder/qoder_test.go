package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

func TestAggregateSSEParsesEnvelope(t *testing.T) {
	inner1 := `{"id":"chatcmpl-1","model":"auto","created":1700000000,"choices":[{"delta":{"role":"assistant","content":"你好"}}]}`
	inner2 := `{"choices":[{"delta":{"content":"世界"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`
	sse := strings.Join([]string{
		`data: {"body":` + strconvQuote(inner1) + `}`,
		``,
		`data: {"body":` + strconvQuote(inner2) + `}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp, err := aggregateSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if resp["id"] != "chatcmpl-1" {
		t.Fatalf("id mismatch: %v", resp["id"])
	}
	choices := resp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "你好世界" {
		t.Fatalf("content = %q", msg["content"])
	}
	if _, ok := resp["usage"]; !ok {
		t.Fatalf("usage should be preserved")
	}
}

func TestAggregateSSEParsesDirectChunk(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	resp, err := aggregateSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hi" {
		t.Fatalf("content = %q", msg["content"])
	}
}

func TestStreamAndCaptureUnwrapsEnvelope(t *testing.T) {
	inner := `{"id":"chatcmpl-1","choices":[{"delta":{"content":"hi"},"index":0}]}`
	upstream := "data: " + `{"headers":{"Content-Type":["application/json"]},"body":` + strconvQuote(inner) + `,"statusCodeValue":200}` + "\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	var captured []map[string]any
	err := streamAndCapture(strings.NewReader(upstream), rec, nil, func(c map[string]any) { captured = append(captured, c) })
	if err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "statusCodeValue") || strings.Contains(body, "headers") {
		t.Fatalf("stream should be unwrapped, got %q", body)
	}
	if !strings.Contains(body, `"content":"hi"`) {
		t.Fatalf("stream should contain inner chunk, got %q", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("stream should end with [DONE]")
	}
	if len(captured) != 1 {
		t.Fatalf("captured chunks = %d", len(captured))
	}
}

func TestOffPeakWindows(t *testing.T) {
	// 静态兜底规则：Asia/Shanghai 22:00–08:00（等价旧的 UTC 14:00–24:00）。
	p := staticPromotionFor("qmodel_38max")
	if p == nil {
		t.Fatal("static promotion missing")
	}
	// 22:30 CST = 14:30 UTC 在窗口内。
	offPeak := time.Date(2026, 10, 4, 14, 30, 0, 0, time.UTC)
	// 10:00 CST = 02:00 UTC 在窗口外。
	regular := time.Date(2026, 10, 4, 2, 0, 0, 0, time.UTC)
	if !p.InWindow(offPeak) {
		t.Fatalf("14:30 UTC (22:30 CST) should be off-peak")
	}
	if p.InWindow(regular) {
		t.Fatalf("02:00 UTC (10:00 CST) should be regular")
	}
	// 边界：21:59 CST 常规，22:00 CST 错峰，07:59 CST 错峰，08:00 CST 常规。
	cases := []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, 10, 4, 13, 59, 0, 0, time.UTC), false}, // 21:59 CST
		{time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC), true},   // 22:00 CST
		{time.Date(2026, 10, 4, 23, 59, 0, 0, time.UTC), true},  // 07:59 CST
		{time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), false},   // 08:00 CST
	}
	for _, c := range cases {
		if got := p.InWindow(c.at); got != c.want {
			t.Fatalf("InWindow(%s) = %v, want %v", c.at, got, c.want)
		}
	}
	// 跨午夜窗口必须支持 24:00 与 00:00 两种写法。
	if w := p.Window(); w != "22:00–08:00" {
		t.Fatalf("window = %q", w)
	}
	// 非促销模型：窗口内也不打折。
	if staticPromotionFor("dfmodel") != nil {
		t.Fatalf("dfmodel should have no static promotion")
	}
}

// TestMultiplierAtPrefersLivePrice 验证生效倍率裁决：无促销取实时值、
// 窗口内折扣计算、以及「上游是否已把折后价写进 price_factor」两种口径。
func TestMultiplierAtPrefersLivePrice(t *testing.T) {
	offPeak := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC) // 23:00 CST
	regular := time.Date(2026, 10, 4, 5, 0, 0, 0, time.UTC)  // 13:00 CST
	f := func(v float64) *float64 { return &v }

	// 1) 无促销规则：直接用实时 price_factor（含 0，表示免费）。
	m := ModelInfo{Key: "dfmodel", CreditsMultiplier: 0.1, CreditsParsed: true, LiveMultiplier: f(0)}
	if v, disc := m.MultiplierAt(offPeak); v != 0 || disc {
		t.Fatalf("free live price = %v/%v, want 0/false", v, disc)
	}

	// 2) 有促销规则，上游已把折后价写进 price_factor（qmodel_38max 实测 0.2）。
	m = ModelInfo{Key: "qmodel_38max", CreditsParsed: true, LiveMultiplier: f(0.2), Promotion: staticPromotionFor("qmodel_38max")}
	if v, disc := m.MultiplierAt(offPeak); v != 0.2 || !disc {
		t.Fatalf("off-peak = %v/%v, want 0.2/true", v, disc)
	}
	if v, disc := m.MultiplierAt(regular); v != 0.5 || disc {
		t.Fatalf("regular = %v/%v, want 0.5/false", v, disc)
	}

	// 3) 有促销规则，但上游 price_factor 仍是原价（qmodel 实测 0.1）：按标准×折扣算。
	m = ModelInfo{Key: "qmodel", CreditsParsed: true, LiveMultiplier: f(0.1), Promotion: staticPromotionFor("qmodel")}
	if v, disc := m.MultiplierAt(offPeak); !nearlyEqual(v, 0.04) || !disc {
		t.Fatalf("off-peak = %v/%v, want 0.04/true", v, disc)
	}
	if v, disc := m.MultiplierAt(regular); v != 0.1 || disc {
		t.Fatalf("regular = %v/%v, want 0.1/false", v, disc)
	}

	// 4) 上游没给实时倍率：退回静态标准倍率，窗口内仍按规则打折。
	m = ModelInfo{Key: "qmodel_38max", CreditsMultiplier: 0.5, CreditsParsed: true, Promotion: staticPromotionFor("qmodel_38max")}
	if v, disc := m.MultiplierAt(offPeak); v != 0.2 || !disc {
		t.Fatalf("static fallback off-peak = %v/%v, want 0.2/true", v, disc)
	}

	// 5) 促销已结束（active=false）且实时值回到标准价：不再按旧规则打折。
	ended := &Promotion{Standard: 0.5, Discount: 0.4, WindowStart: "22:00", WindowEnd: "08:00", Timezone: "Asia/Shanghai", Active: false}
	m = ModelInfo{Key: "qmodel_38max", CreditsParsed: true, LiveMultiplier: f(0.5), Promotion: ended}
	if v, disc := m.MultiplierAt(offPeak); v != 0.5 || disc {
		t.Fatalf("ended promotion = %v/%v, want 0.5/false", v, disc)
	}
}

// TestPromotionWindowFollowsUpstreamTimezone 验证窗口按上游下发的时区判定，
// 而不是按 UTC 写死。
func TestPromotionWindowFollowsUpstreamTimezone(t *testing.T) {
	p := &Promotion{
		Standard:    1,
		Discount:    0.5,
		WindowStart: "00:00",
		WindowEnd:   "12:00",
		Timezone:    "UTC",
	}
	// UTC 06:00 在窗口内；同一时刻在 Asia/Shanghai 是 14:00，不在窗口内。
	at := time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)
	if !p.InWindow(at) {
		t.Fatalf("UTC window should cover 06:00 UTC")
	}
	p.Timezone = "Asia/Shanghai"
	if p.InWindow(at) {
		t.Fatalf("Asia/Shanghai window should not cover 14:00 CST")
	}
	// 非法时区退回 UTC，不能 panic。
	p.Timezone = "Not/AZone"
	if !p.InWindow(at) {
		t.Fatalf("invalid timezone should fall back to UTC")
	}
	// 非法窗口边界不视为打折。
	p.WindowStart = "25:99"
	if p.InWindow(at) {
		t.Fatalf("invalid window should never be off-peak")
	}
}

// TestAnyOffPeak 验证面板顶部的错峰状态聚合。
func TestAnyOffPeak(t *testing.T) {
	offPeak := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	regular := time.Date(2026, 10, 4, 5, 0, 0, 0, time.UTC)
	models := staticModelsFor(regionCN)
	if !anyOffPeak(models, offPeak) {
		t.Fatalf("static catalog should report off-peak at 23:00 CST")
	}
	if anyOffPeak(models, regular) {
		t.Fatalf("static catalog should not report off-peak at 13:00 CST")
	}
}

func TestNormalizeModelKey(t *testing.T) {
	cases := map[string]string{
		"":                  "auto",
		"auto":              "auto",
		"default":           "auto",
		"lite":              "lite",
		"claude-sonnet-4.5": "auto",
		"sonnet":            "auto",
		"qwen-max":          "qwen-max",
		// 官方模型名 → 上游短 key（大小写不敏感）。
		"Qwen3.8-Max":    "qmodel_38max",
		"qwen3.8-max":    "qmodel_38max",
		"DeepSeek-Flash": "dfmodel",
		// 短 key 原样透传。
		"qmodel_38max": "qmodel_38max",
	}
	for in, want := range cases {
		if got := normalizeModelKey(in); got != want {
			t.Fatalf("normalizeModelKey(%q)=%q want %q", in, got, want)
		}
	}
}

// TestMigrateDisabledModelsShortKeyToName 旧停用名单里的短 key 应迁移为官方模型名。
func TestMigrateDisabledModelsShortKeyToName(t *testing.T) {
	got := migrateDisabledModels([]string{"dfmodel", "qmodel_38max", "DeepSeek-V4-Pro"}, "")
	want := map[string]bool{"DeepSeek-Flash": true, "Qwen3.8-Max": true, "DeepSeek-V4-Pro": true}
	if len(got) != len(want) {
		t.Fatalf("migrated = %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected migrated name %q", n)
		}
	}
	// 带前缀时保持前缀。
	prefixed := migrateDisabledModels([]string{"qoder-dfmodel"}, "qoder-")
	if len(prefixed) != 1 || prefixed[0] != "qoder-DeepSeek-Flash" {
		t.Fatalf("prefixed migrate = %v", prefixed)
	}
	// 跨区域同名短 key（mmodel = MiniMax-M2.7 国内 / MiniMax-M3 国际）应两侧都保留。
	both := migrateDisabledModels([]string{"mmodel"}, "")
	wantBoth := map[string]bool{"MiniMax-M2.7": true, "MiniMax-M3": true}
	if len(both) != 2 {
		t.Fatalf("cross-region migrate = %v", both)
	}
	for _, n := range both {
		if !wantBoth[n] {
			t.Fatalf("unexpected cross-region name %q", n)
		}
	}
}

// TestLookupModelKeyResolvesOfficialName 对外官方名应解析回上游短 key。
func TestLookupModelKeyResolvesOfficialName(t *testing.T) {
	s := newTestService(t)
	if got := s.lookupModelKey("DeepSeek-Flash"); got != "dfmodel" {
		t.Fatalf("lookup official name = %q", got)
	}
	if got := s.lookupModelKey("dfmodel"); got != "dfmodel" {
		t.Fatalf("lookup short key = %q", got)
	}
	if got := s.lookupModelKey("custom-model"); got != "custom-model" {
		t.Fatalf("unknown should pass through, got %q", got)
	}
}

// TestIsModelEnabledShortKeyBypass 验证停用名单以官方名存储后，经上游短 key
// 别名请求也会被拦截（修复：短 key 反查官方名核对停用名单）。
func TestIsModelEnabledShortKeyBypass(t *testing.T) {
	s := newTestService(t)
	s.settings = Settings{DisabledModels: []string{"DeepSeek-Flash"}}
	if s.isModelEnabled("dfmodel") {
		t.Fatalf("short key for a disabled official model should be blocked")
	}
	if s.isModelEnabled("DeepSeek-Flash") {
		t.Fatalf("official name should be blocked")
	}
	if !s.isModelEnabled("Qwen3.8-Flash") {
		t.Fatalf("unrelated catalog model should stay enabled")
	}
}

// TestIsModelEnabledWhitelist 验证白名单语义：目录外的模型名一律拒绝，
// 防止上游改名后旧名/任意别名绕过停用名单。
func TestIsModelEnabledWhitelist(t *testing.T) {
	s := newTestService(t)
	// 无账号 → 默认国内版静态兜底目录。
	if !s.isModelEnabled("DeepSeek-Flash") {
		t.Fatalf("catalog model should be enabled by default")
	}
	if s.isModelEnabled("custom-model") {
		t.Fatalf("off-catalog model should be rejected")
	}
	if s.isModelEnabled("totally-made-up") {
		t.Fatalf("off-catalog model should be rejected")
	}
}

// TestModelAliasesSurviveRename 验证上游改名后，按旧名/短 key 停用依然生效：
// 用动态目录把 DeepSeek-Flash 的对外名改成 DeepSeek-V4.1-Flash（沿用短 key dfmodel），
// 停用名单里的旧官方名应通过短 key 关联到新条目。
func TestModelAliasesSurviveRename(t *testing.T) {
	s := newTestService(t)
	s.settings = Settings{DisabledModels: []string{"DeepSeek-Flash"}}
	s.storeRegionCatalog(regionCN, []ModelInfo{
		{ID: "DeepSeek-V4.1-Flash", Key: "dfmodel", Provider: "deepseek"},
		{ID: "Qwen3.8-Flash", Key: "qfmodel", Provider: "qwen"},
	})
	if s.isModelEnabled("DeepSeek-V4.1-Flash") {
		t.Fatalf("renamed model sharing the disabled short key should stay blocked")
	}
	if s.isModelEnabled("dfmodel") {
		t.Fatalf("short key should stay blocked")
	}
	if !s.isModelEnabled("Qwen3.8-Flash") {
		t.Fatalf("unrelated model should stay enabled")
	}
}

func TestLegacyEncodeRoundTrip(t *testing.T) {
	// legacyEncode 是自定义字母表变换；验证空输入与可逆性（与官方一致的标准前缀）。
	got := legacyEncode([]byte(""))
	if got != "" {
		t.Fatalf("empty encode = %q", got)
	}
	encoded := legacyEncode([]byte("hello world"))
	if encoded == "" {
		t.Fatalf("encode produced empty")
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Fatalf("encoded should use custom alphabet, got %q", encoded)
	}
}

func TestParseExpireTime(t *testing.T) {
	if got := parseExpireTime(""); got != 0 {
		t.Fatalf("empty = %d", got)
	}
	if got := parseExpireTime("2026-10-02T00:00:00Z"); got == 0 {
		t.Fatalf("rfc3339 should parse")
	}
	if got := parseExpireTime("1790438946000"); got != 1790438946 {
		t.Fatalf("millis = %d", got)
	}
	if got := parseExpireTime("1790438946"); got != 1790438946 {
		t.Fatalf("seconds = %d", got)
	}
}

func TestNormalizeRegion(t *testing.T) {
	if normalizeRegion("") != regionCN {
		t.Fatalf("empty should default to cn")
	}
	if normalizeRegion("intl") != regionIntl {
		t.Fatalf("intl")
	}
	if normalizeRegion("CN") != regionCN {
		t.Fatalf("case-insensitive cn")
	}
	if normalizeRegion("bogus") != regionCN {
		t.Fatalf("unknown should default to cn")
	}
}

func TestRegionOfDomain(t *testing.T) {
	cases := map[string]string{
		"":                         regionCN,
		"https://qoder.cn":         regionCN,
		"www.qoder.com.cn":         regionCN,
		"https://qoder.com":        regionIntl,
		"https://openapi.qoder.sh": regionIntl,
	}
	for in, want := range cases {
		if got := regionOfDomain(in); got != want {
			t.Fatalf("regionOfDomain(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRegionConfigs(t *testing.T) {
	cn := regionConfigFor(regionCN)
	if cn.openAPI != "https://openapi.qoder.com.cn" || cn.gateway != "https://gateway.qoder.com.cn" {
		t.Fatalf("cn config = %+v", cn)
	}
	if !cn.nonceDashed {
		t.Fatalf("cn nonce should be dashed")
	}
	intl := regionConfigFor(regionIntl)
	if intl.openAPI != "https://openapi.qoder.sh" || intl.gateway != "https://api3.qoder.sh" {
		t.Fatalf("intl config = %+v", intl)
	}
	if intl.nonceDashed {
		t.Fatalf("intl nonce should not be dashed")
	}
}

func TestMergeCatalogsMarksRegions(t *testing.T) {
	merged := mergeCatalogs(map[string][]ModelInfo{
		regionCN:   cnStaticModels,
		regionIntl: intlStaticModels,
	})
	byID := map[string]ModelInfo{}
	for _, m := range merged {
		byID[m.ID] = m
	}
	// auto 两区域都有 → 通用。
	if m := byID["Auto"]; len(m.Regions) != 2 {
		t.Fatalf("Auto should be in both regions, got %v", m.Regions)
	}
	// ultimate 仅国际版。
	if m := byID["Ultimate"]; !m.InternationalOnly {
		t.Fatalf("Ultimate should be internationalOnly")
	}
	// GLM-5.2 仅国内版。
	if m := byID["GLM-5.2"]; len(m.Regions) != 1 || m.Regions[0] != regionCN {
		t.Fatalf("GLM-5.2 should be cn-only, got %v", m.Regions)
	}
	// 倍率与上游 key 透传。
	if m := byID["Kimi-K3"]; m.CreditsMultiplier != 1.4 || !m.CreditsParsed || m.Key != "kmodel_latest" {
		t.Fatalf("Kimi-K3 = %+v", m)
	}
}

func TestCatalogFiltersByActiveRegion(t *testing.T) {
	s := newTestService(t)
	// 无账号：默认国内版。
	got := s.catalog()
	for _, m := range got {
		if m.ID == "Ultimate" {
			t.Fatalf("no-account catalog should not include intl-only model")
		}
		if len(m.Regions) != 1 || m.Regions[0] != regionCN {
			t.Fatalf("no-account model %s should be cn-only, got %v", m.ID, m.Regions)
		}
	}
	// 只有国内版账号：仍只国内版。
	if err := s.SaveSettings(context.Background(), Settings{Accounts: []Account{{ID: "c1", Region: regionCN}}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range s.catalog() {
		if m.ID == "Ultimate" {
			t.Fatalf("cn-only catalog should not include Ultimate")
		}
	}
	// 加国际版账号：出现国际版模型。
	if err := s.SaveSettings(context.Background(), Settings{Accounts: []Account{
		{ID: "c1", Region: regionCN}, {ID: "i1", Region: regionIntl},
	}}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.catalog() {
		if m.ID == "Ultimate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mixed catalog should include intl-only Ultimate")
	}
}

func TestAccountServesModelByRegion(t *testing.T) {
	s := newTestService(t)
	cn := Account{ID: "cn1", Region: regionCN}
	intl := Account{ID: "intl1", Region: regionIntl}
	if !s.accountServesModel(cn, "GLM-5.2") {
		t.Fatalf("cn account should serve GLM-5.2")
	}
	if s.accountServesModel(intl, "GLM-5.2") {
		t.Fatalf("intl account should not serve GLM-5.2")
	}
	if !s.accountServesModel(intl, "Ultimate") {
		t.Fatalf("intl account should serve Ultimate")
	}
	if s.accountServesModel(cn, "Ultimate") {
		t.Fatalf("cn account should not serve Ultimate")
	}
	if !s.accountServesModel(cn, "") {
		t.Fatalf("empty model should be allowed")
	}
}

func TestReconcileLinkedEndpointSyncsModels(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	// 预置一行旧端点，models 停在过期的 2 个。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, enabled, models, disabled_models, plugin_id)
		VALUES (?, 'Qoder', 'http://127.0.0.1:3000/api/qoder/v1', 'sk', 1, '["DeepSeek-Flash","Qwen3.8-Flash"]', '[]', 'qoder')`,
		linkedEndpointID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// 默认无账号 → 国内版 14 个模型，对账后端点行应更新。
	s.ReconcileLinkedEndpoint(ctx)

	db2, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var raw string
	if err := db2.QueryRowContext(ctx, `SELECT models FROM openai_endpoints WHERE id = ?`, linkedEndpointID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != len(cnStaticModels) {
		t.Fatalf("reconciled models = %d, want %d (%v)", len(models), len(cnStaticModels), models)
	}
}

// TestEnableClearsDisabledAliases 验证「启用」某模型时，停用名单里指向同一模型的
// 旧名/短 key 残留也会被清掉，避免启用后仍被别名拦截。
func TestEnableClearsDisabledAliases(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	s.storeRegionCatalog(regionCN, []ModelInfo{
		{ID: "DeepSeek-V4.1-Flash", Key: "dfmodel", Provider: "deepseek"},
	})
	// 旧名 + 短 key 都在停用名单里。
	if err := s.setModelsEnabled(ctx, []string{"DeepSeek-V4.1-Flash"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.setModelsEnabled(ctx, []string{"dfmodel"}, false); err != nil {
		t.Fatal(err)
	}
	if s.isModelEnabled("DeepSeek-V4.1-Flash") {
		t.Fatalf("model should be disabled")
	}
	// 按对外名重新启用：应一并清掉短 key 别名残留。
	if err := s.setModelsEnabled(ctx, []string{"DeepSeek-V4.1-Flash"}, true); err != nil {
		t.Fatal(err)
	}
	if !s.isModelEnabled("DeepSeek-V4.1-Flash") {
		t.Fatalf("model should be enabled after toggle-on")
	}
	if !s.isModelEnabled("dfmodel") {
		t.Fatalf("short-key alias should be cleared too")
	}
}

func TestStrategyNormalization(t *testing.T) {
	if normalizeStrategy("round-robin") != accountpick.RoundRobin {
		t.Fatalf("round-robin")
	}
	if normalizeStrategy("bogus") != accountpick.First {
		t.Fatalf("bogus should fall back to first")
	}
	if normalizeStrategy("") != accountpick.First {
		t.Fatalf("empty should be first")
	}
}

// TestParseUpstreamCatalog 验证动态目录解析：display_name 作对外 id、key 作上游 key，
// 倍率优先取上游实时 price_factor，缺失时才回填静态表。
func TestParseUpstreamCatalog(t *testing.T) {
	raw := []byte(`{
		"chat": [
			{"key":"qmodel_38max","display_name":"Qwen3.8-Max","enable":true,"is_reasoning":true,
			 "price_factor":0.2,"max_input_tokens":180000,
			 "promotion":{"active":true,"rule_id":"idle_time_model_credit_discount","discount_factor":0.4,
			   "before_promotion_price_factor":0.5,"window_start":"22:00","window_end":"08:00",
			   "timezone":"Asia/Shanghai","badge":{"zh":"错峰 4 折"}}},
			{"key":"dfmodel","display_name":"DeepSeek-V4.1-Flash","enable":true,"price_factor":0.0,
			 "original_price_factor":0.1,"is_free":true,"max_input_tokens":180000},
			{"key":"brand_new_key","display_name":"Brand-New-Model","enable":true},
			{"key":"gmodel","display_name":"GLM-5.3","enable":true,"price_factor":0.8}
		],
		"embedding": [
			{"key":"embedding-1","display_name":"Embedding-1","enable":true}
		]
	}`)
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	if _, ok := byID["Embedding-1"]; ok {
		t.Fatalf("only chat group should be published")
	}
	m, ok := byID["Qwen3.8-Max"]
	if !ok || m.Key != "qmodel_38max" {
		t.Fatalf("Qwen3.8-Max missing or wrong key: %+v", m)
	}
	// 厂商仍回填静态表；倍率取上游实时值，来源标记 live。
	if m.Provider != "qwen" || !m.CreditsParsed || m.PriceSource != priceLive {
		t.Fatalf("live metadata not applied: %+v", m)
	}
	if m.LiveMultiplier == nil || *m.LiveMultiplier != 0.2 {
		t.Fatalf("live price_factor missing: %+v", m.LiveMultiplier)
	}
	// 促销规则应完整解析（含窗口时区与 badge）。
	if m.Promotion == nil || m.Promotion.Discount != 0.4 || m.Promotion.Standard != 0.5 {
		t.Fatalf("promotion not parsed: %+v", m.Promotion)
	}
	if m.Promotion.Timezone != "Asia/Shanghai" || m.Promotion.Window() != "22:00–08:00" {
		t.Fatalf("promotion window = %+v", m.Promotion)
	}
	if m.Promotion.Badge != "错峰 4 折" {
		t.Fatalf("promotion badge = %q", m.Promotion.Badge)
	}

	// 静态表已知 key（dfmodel）但上游改了 display_name：对外 id 用上游新名；
	// price_factor 明确为 0（免费）必须保留，不能被当作「缺失」。
	d, ok := byID["DeepSeek-V4.1-Flash"]
	if !ok || d.Key != "dfmodel" || d.Provider != "deepseek" {
		t.Fatalf("renamed model not parsed from upstream: %+v", d)
	}
	if !d.CreditsParsed || !d.Free || d.PriceSource != priceLive {
		t.Fatalf("free model should keep live zero price: %+v", d)
	}
	if d.LiveMultiplier == nil || *d.LiveMultiplier != 0 {
		t.Fatalf("live zero price lost: %+v", d.LiveMultiplier)
	}
	if d.OriginalMultiplier == nil || *d.OriginalMultiplier != 0.1 {
		t.Fatalf("original price lost: %+v", d.OriginalMultiplier)
	}

	// 上游给了 price_factor 但静态表无此 key → 仍应视为已解析（live）。
	g, ok := byID["GLM-5.3"]
	if !ok || !g.CreditsParsed || g.PriceSource != priceLive || g.CreditsMultiplier != 0.8 {
		t.Fatalf("live price without static table should still parse: %+v", g)
	}
	if g.Promotion != nil {
		t.Fatalf("model without promotion should have nil promotion: %+v", g.Promotion)
	}

	// 上游完全未知的 key 且无 price_factor → 倍率未解析（前端显示「—」）。
	u, ok := byID["Brand-New-Model"]
	if !ok || u.Key != "brand_new_key" || u.CreditsParsed {
		t.Fatalf("unknown-key model should be marked unparsed: %+v", u)
	}
	if u.PriceSource != "" {
		t.Fatalf("unknown-key model should have empty price source: %q", u.PriceSource)
	}
}

// TestParseUpstreamCatalogFallsBackToStaticPromotion 验证上游未下发 promotion 时，
// 已知错峰模型仍带静态兜底规则（展示不缺失），未知模型则没有规则。
func TestParseUpstreamCatalogFallsBackToStaticPromotion(t *testing.T) {
	raw := []byte(`{"chat":[
		{"key":"qmodel","display_name":"Qwen3.7-Plus","enable":true},
		{"key":"dfmodel","display_name":"DeepSeek-Flash","enable":true,"price_factor":0.0}
	]}`)
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	if p := byID["Qwen3.7-Plus"].Promotion; p == nil || p.Discount != 0.4 {
		t.Fatalf("static promotion fallback missing: %+v", p)
	}
	// 静态表没有倍率时，上游 price_factor 是唯一来源。
	d := byID["DeepSeek-Flash"]
	if !d.CreditsParsed || d.PriceSource != priceLive || d.CreditsMultiplier != 0 {
		t.Fatalf("live price should win over static: %+v", d)
	}
}

// TestFetchUpstreamCatalog 验证签名 GET 模型目录：命中 mock 并按 chat 分组解析。
func TestFetchUpstreamCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("model list should be GET, got %s", r.Method)
		}
		if r.URL.Path != legacyModelListPath {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// 签名头必须存在，否则上游会拒绝（这里只验证已构造）。
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer COSY.") {
			t.Errorf("missing COSY authorization")
		}
		if r.Header.Get("cosy-user") == "" {
			t.Errorf("missing cosy-user")
		}
		_, _ = w.Write([]byte(`{"chat":[{"key":"qmodel","display_name":"Qwen3.7-Plus","enable":true}]}`))
	}))
	defer srv.Close()
	prev := modelAPIBaseOverride
	modelAPIBaseOverride = srv.URL
	t.Cleanup(func() { modelAPIBaseOverride = prev })

	s := newTestService(t)
	acc := Account{ID: "u1", AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	models, err := s.fetchUpstreamCatalog(context.Background(), regionCN, acc)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "Qwen3.7-Plus" || models[0].Key != "qmodel" {
		t.Fatalf("catalog = %+v", models)
	}
	// 区域标记由 storeRegionCatalog 统一补（fetchUpstreamCatalog 只解析上游条目）。
	s.storeRegionCatalog(regionCN, models)
	if got := s.regionModels(regionCN); len(got) != 1 || len(got[0].Regions) != 1 || got[0].Regions[0] != regionCN {
		t.Fatalf("region tag missing after store: %+v", got)
	}
}

// TestEnsureCatalogFallsBackToStatic 验证回源失败时目录回退静态兜底，不返回空。
func TestEnsureCatalogFallsBackToStatic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	prev := modelAPIBaseOverride
	modelAPIBaseOverride = srv.URL
	t.Cleanup(func() { modelAPIBaseOverride = prev })

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{Enabled: true, Accounts: []Account{
		{ID: "c1", Region: regionCN, AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour).Unix()},
	}}); err != nil {
		t.Fatal(err)
	}
	s.ensureCatalog(context.Background())
	if len(s.catalog()) != len(cnStaticModels) {
		t.Fatalf("catalog should fall back to static table, got %d", len(s.catalog()))
	}
}

// TestCatalogUsesDynamicSnapshot 验证动态目录已缓存时对外视图用动态条目。
func TestCatalogUsesDynamicSnapshot(t *testing.T) {
	s := newTestService(t)
	s.storeRegionCatalog(regionCN, []ModelInfo{
		{ID: "DeepSeek-V4.1-Flash", Key: "dfmodel", Provider: "deepseek"},
	})
	got := s.catalog()
	if len(got) != 1 || got[0].ID != "DeepSeek-V4.1-Flash" {
		t.Fatalf("catalog should use dynamic snapshot, got %+v", got)
	}
	if got[0].Regions[0] != regionCN {
		t.Fatalf("dynamic snapshot should be region-tagged: %+v", got[0])
	}
}

func TestDailyCheckinClaimsAndIdempotent(t *testing.T) {
	var claimed atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sash/api/v1/me/campaigns" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"showCampaign":true,"claimable":true,"campaigns":[
				{"campaignId":"c1","campaignKey":"act-1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/claim"):
			claimed.Add(1)
			_, _ = w.Write([]byte(`{"status":"GRANTED","benefit":{"amount":100}}`))
		case r.URL.Path == "/api/v2/quota/usage":
			_, _ = w.Write([]byte(`{"userType":"personal_standard","userQuota":{"total":1000,"used":900,"remaining":100,"unit":"credits"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	acc := Account{ID: "u1", AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	res, err := s.DailyCheckin(context.Background(), acc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "success" || res.Gained != 100 {
		t.Fatalf("result = %+v", res)
	}
	if claimed.Load() != 1 {
		t.Fatalf("claim count = %d", claimed.Load())
	}
}

// TestDailyCheckinAlreadyClaimed 验证活动已领取时返回 already 而非错误。
func TestDailyCheckinAlreadyClaimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sash/api/v1/me/campaigns" {
			_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED"}]}`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	res, err := s.DailyCheckin(context.Background(), Account{ID: "u1", AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "already" {
		t.Fatalf("status = %q, want already", res.Status)
	}
}

// TestFetchQuotaParses 验证余额解析与耗尽判定。
func TestFetchQuotaParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userType":"personal_pro","isQuotaExceeded":false,"userQuota":{"total":300,"used":28,"remaining":272,"unit":"credits"}}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	q, err := s.FetchQuota(context.Background(), Account{ID: "u1", AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Limit != 300 || q.Used != 28 || q.Remaining != 272 || q.Exhausted {
		t.Fatalf("quota = %+v", q)
	}
}

// TestRefreshAccessToken 验证 deviceToken/refresh 解析。
func TestRefreshAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deviceToken/refresh" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"token":"newtok","refresh_token":"newref","expires_in":3600}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	acc := Account{ID: "u1", AccessToken: "old", RefreshToken: "r"}
	if err := s.refreshAccessToken(context.Background(), &acc); err != nil {
		t.Fatal(err)
	}
	if acc.AccessToken != "newtok" || acc.RefreshToken != "newref" || acc.ExpiresAt == 0 {
		t.Fatalf("account = %+v", acc)
	}
}

// withMockOpenAPI 把 openapi 基址指向 mock。
func withMockOpenAPI(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := openAPIBaseOverride
	openAPIBaseOverride = srv.URL
	t.Cleanup(func() { openAPIBaseOverride = prev })
}

// TestUsageTokensForCheckIsFast 守住「上下文估算不能成为热路径负担」：
// 十万字符的请求必须在毫秒级完成。曾用 go-tiktoken 实现，实测 40000 字符要
// 1.8s（超线性），一条大请求就会把连接挂住，故改为纯字符启发式。
func TestUsageTokensForCheckIsFast(t *testing.T) {
	payload := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": strings.Repeat("测试内容hello ", 10000)},
		},
	}
	if n := usageTokensForCheck(payload); n <= 0 {
		t.Fatalf("estimate should be positive, got %d", n)
	}
	start := time.Now()
	for i := 0; i < 20; i++ {
		usageTokensForCheck(payload)
	}
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("estimate too slow: 20 iterations took %s (single ~%s)", elapsed, elapsed/20)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// -----------------------------------------------------------------------------
// 思考强度（thinking_config → reasoning_effort）
// -----------------------------------------------------------------------------

// TestThinkingConfigParse 验证上游 thinking_config 的三种形态都能正确解析。
func TestThinkingConfigParse(t *testing.T) {
	raw := []byte(`{"chat":[
		{"key":"qmodel_38max","display_name":"Qwen3.8-Max","enable":true,"is_reasoning":true,
		 "thinking_config":{"disabled":{},"enabled":{"efforts":{"xhigh":{},"low":{},"medium":{"is_default":true}},"is_default":true}}},
		{"key":"qmodel_latest","display_name":"Qwen3.7-Max","enable":true,"is_reasoning":true,
		 "thinking_config":{"disabled":{"description":"Disable thinking"},"enabled":{"description":"Enable thinking","is_default":true}}},
		{"key":"auto","display_name":"Auto","enable":true,"is_reasoning":true},
		{"key":"gmodel","display_name":"GLM-5.3","enable":true,"is_reasoning":true,
		 "thinking_config":{"enabled":{"efforts":{"high":{},"low":{},"max":{"is_default":true}},"is_default":true}}}
	]}`)
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}

	// 形态一：可关闭 + 三档，默认 medium。档位必须按强弱升序。
	q := byID["Qwen3.8-Max"]
	if q.Thinking == nil || !q.Thinking.CanDisable || len(q.Thinking.Efforts) != 3 {
		t.Fatalf("thinking = %+v", q.Thinking)
	}
	if got := strings.Join(q.Thinking.Efforts, ","); got != "low,medium,xhigh" {
		t.Fatalf("efforts order = %q", got)
	}
	if q.Thinking.DefaultEffort != "medium" {
		t.Fatalf("default effort = %q", q.Thinking.DefaultEffort)
	}

	// 形态二：可关闭但无档位（只有开关）。
	l := byID["Qwen3.7-Max"]
	if l.Thinking == nil || !l.Thinking.CanDisable || len(l.Thinking.Efforts) != 0 {
		t.Fatalf("switch-only thinking = %+v", l.Thinking)
	}

	// 形态三：完全没有 thinking_config → 无思考能力描述。
	if a := byID["Auto"]; a.Thinking != nil {
		t.Fatalf("Auto should have no thinking config, got %+v", a.Thinking)
	}

	// 形态四：不可关闭（无 disabled）+ 三档，默认 max。
	g := byID["GLM-5.3"]
	if g.Thinking == nil || g.Thinking.CanDisable {
		t.Fatalf("GLM thinking = %+v", g.Thinking)
	}
	if g.Thinking.DefaultEffort != "max" {
		t.Fatalf("GLM default = %q", g.Thinking.DefaultEffort)
	}
}

// TestThinkingNoGuessedDefaultEffort 验证上游**没标记**默认档时不去猜最高档：
// 调用方没要求档位就不写 reasoning_effort，交给上游自己的默认——猜最高档会
// 平白拉高思考开销与延迟，属于行为漂移。
func TestThinkingNoGuessedDefaultEffort(t *testing.T) {
	raw := []byte(`{"chat":[
		{"key":"gmodel","display_name":"GLM-5.3","enable":true,
		 "thinking_config":{"enabled":{"efforts":{"high":{},"low":{}}}}}
	]}`)
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	g := byID["GLM-5.3"]
	if g.Thinking == nil {
		t.Fatal("应解析出思考能力")
	}
	if g.Thinking.DefaultEffort != "" {
		t.Errorf("上游没标默认档时不应猜一个，得到 %q", g.Thinking.DefaultEffort)
	}
	// 调用方没指定档位 → 不写任何档位。
	if lvl, dis, has := g.Thinking.Resolve(""); has || dis || lvl != "" {
		t.Errorf("未指定档位时不应产出档位，得到 (%q,%v,%v)", lvl, dis, has)
	}
	// 调用方明确要求 → 仍然归一到可用档位（向上取）。
	if lvl, _, has := g.Thinking.Resolve("medium"); !has || lvl != "high" {
		t.Errorf("medium 应向上取 high，得到 (%q,%v)", lvl, has)
	}
}

// TestThinkingResolveRoundsUp 验证强度档位的归一：向上取最近一档、超上限取最高、
// 关闭请求按上游是否允许关闭处理。
func TestThinkingResolveRoundsUp(t *testing.T) {
	// DeepSeek-V4-Pro 实测只有 high / max。
	deepseek := &ThinkingConfig{CanDisable: true, Efforts: []string{"high", "max"}, DefaultEffort: "max"}
	cases := []struct {
		req     string
		wantLvl string
		wantDis bool
		wantHas bool
	}{
		{"", "max", false, true},        // 未指定 → 默认
		{"max", "max", false, true},     // 命中上限
		{"xhigh", "max", false, true},   // 超上限 → 最高
		{"high", "high", false, true},   // 精确命中
		{"medium", "high", false, true}, // 中间值 → 向上取 high
		{"low", "high", false, true},    // 低于下限 → 向上取最低档
		{"none", "", true, false},       // 可关闭 → 关闭
	}
	for _, c := range cases {
		lvl, dis, has := deepseek.Resolve(c.req)
		if lvl != c.wantLvl || dis != c.wantDis || has != c.wantHas {
			t.Fatalf("Resolve(%q) = (%q,%v,%v), want (%q,%v,%v)", c.req, lvl, dis, has, c.wantLvl, c.wantDis, c.wantHas)
		}
	}

	// 不可关闭：要求关闭时不生效，保持开启。
	fixed := &ThinkingConfig{Efforts: []string{"high", "max"}, DefaultEffort: "max"}
	if lvl, dis, _ := fixed.Resolve("none"); dis || lvl != "" {
		t.Fatalf("cannot-disable model should ignore none, got (%q,%v)", lvl, dis)
	}

	// 只有开关、无档位：不返回档位。
	switchOnly := &ThinkingConfig{CanDisable: true}
	if lvl, dis, has := switchOnly.Resolve("high"); has || dis || lvl != "" {
		t.Fatalf("switch-only = (%q,%v,%v)", lvl, dis, has)
	}
}

// TestResolveThinkingToUpstream 验证落字段：is_reasoning 与 parameters.reasoning_effort。
func TestResolveThinkingToUpstream(t *testing.T) {
	qwen := ModelInfo{
		ID:          "Qwen3.8-Max",
		IsReasoning: true,
		Thinking:    &ThinkingConfig{CanDisable: true, Efforts: []string{"low", "medium", "xhigh"}, DefaultEffort: "medium"},
	}
	if r, e := resolveThinking(qwen, requestOptions{Effort: "xhigh"}); !r || e != "xhigh" {
		t.Fatalf("xhigh = (%v,%q)", r, e)
	}
	if r, e := resolveThinking(qwen, requestOptions{Effort: "max"}); !r || e != "xhigh" {
		t.Fatalf("max should clamp to xhigh, got (%v,%q)", r, e)
	}
	if r, e := resolveThinking(qwen, requestOptions{DisableThinking: true}); r || e != "" {
		t.Fatalf("disable = (%v,%q)", r, e)
	}
	// 无 thinking_config 时沿用上游 is_reasoning，不写档位。
	auto := ModelInfo{ID: "Auto", IsReasoning: true}
	if r, e := resolveThinking(auto, requestOptions{Effort: "high"}); !r || e != "" {
		t.Fatalf("no-config model = (%v,%q)", r, e)
	}
}

// TestParseRequestOptions 验证 OpenAI 侧各种写法的解析。
func TestParseRequestOptions(t *testing.T) {
	p := func(raw string) requestOptions {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		return parseRequestOptions(payload)
	}

	if o := p(`{"reasoning_effort":"high"}`); o.Effort != "high" {
		t.Fatalf("string effort = %q", o.Effort)
	}
	if o := p(`{"reasoning_effort":{"effort":"low"}}`); o.Effort != "low" {
		t.Fatalf("object effort = %q", o.Effort)
	}
	if o := p(`{"reasoning":{"effort":"medium","enabled":true}}`); o.Effort != "medium" {
		t.Fatalf("reasoning.effort = %q", o.Effort)
	}
	// reasoning_effort="none" → 关闭思考。
	if o := p(`{"reasoning_effort":"none"}`); !o.DisableThinking || o.Effort != "" {
		t.Fatalf("none = %+v", o)
	}
	// thinking.type=disabled → 关闭。
	if o := p(`{"thinking":{"type":"disabled"}}`); !o.DisableThinking {
		t.Fatalf("thinking disabled = %+v", o)
	}
	// reasoning.enabled=false → 关闭。
	if o := p(`{"reasoning":{"enabled":false}}`); !o.DisableThinking {
		t.Fatalf("reasoning disabled = %+v", o)
	}
	// 上下文与输出上限，注意 max_completion_tokens 优先。
	if o := p(`{"context_window":400000,"max_tokens":100,"max_completion_tokens":200}`); o.ContextWindow != 400000 || o.MaxTokens != 200 {
		t.Fatalf("limits = %+v", o)
	}
	// 非法/负数忽略。
	if o := p(`{"context_window":-1,"max_tokens":0}`); o.ContextWindow != 0 || o.MaxTokens != 0 {
		t.Fatalf("invalid limits = %+v", o)
	}
}

// TestContextCheck 验证上下文超限拦截与档位选择。
func TestContextCheck(t *testing.T) {
	m := ModelInfo{
		ID:                   "DeepSeek-V4-Pro",
		MaxInputTokens:       96000,
		ContextWindows:       []int64{200000, 400000, 1000000},
		DefaultContextWindow: 200000,
	}
	// 未指定档位：按最大档位 1M 判，90k 远低于上限 → 放行。
	if over, reason := contextCheck(m, 90000, requestOptions{}); over {
		t.Fatalf("should pass under 1M: %s", reason)
	}
	// 1.1M 超过最大档位 → 拦截。
	over, reason := contextCheck(m, 1100000, requestOptions{})
	if !over || !strings.Contains(reason, "1000000") {
		t.Fatalf("should reject over 1M, got over=%v reason=%q", over, reason)
	}
	if !strings.Contains(reason, "200k/400k/1M") {
		t.Fatalf("reason should list windows: %q", reason)
	}
	// 指定 200K 档位时，300k 应被拦截（按用户选择的档位，而不是最大档）。
	if over, _ := contextCheck(m, 300000, requestOptions{ContextWindow: 200000}); !over {
		t.Fatalf("should reject 300k against requested 200K window")
	}
	// 未指定且模型无档位信息时用 max_input_tokens：100k 超过 96000 → 拦截。
	noCtx := ModelInfo{ID: "X", MaxInputTokens: 96000}
	if over, _ := contextCheck(noCtx, 100000, requestOptions{}); !over {
		t.Fatalf("should fall back to max_input_tokens")
	}
	// 完全无信息时不校验（避免误拒）。
	if over, _ := contextCheck(ModelInfo{ID: "Y"}, 5000000, requestOptions{}); over {
		t.Fatalf("no limits should never reject")
	}
}

// TestParseUpstreamCatalogCapabilities 验证上下文档位与能力元数据的解析。
func TestParseUpstreamCatalogCapabilities(t *testing.T) {
	raw := []byte(`{"chat":[
		{"key":"qmodel_38max","display_name":"Qwen3.8-Max","enable":true,"is_vl":true,"is_reasoning":true,
		 "is_new":true,"icon":"QoderQwenAiFill","max_input_tokens":180000,
		 "context_config":{"1M":{"token_count":1000000},"200K":{"token_count":200000,"is_default":true},"400K":{"token_count":400000}}},
		{"key":"mmodel","display_name":"MiniMax-M3.1-Flash","enable":true,"is_vl":true,"is_reasoning":false,
		 "max_input_tokens":180000,"context_config":{"200K":{"token_count":200000,"is_default":true}}},
		{"key":"auto","display_name":"Auto","enable":true,"is_default":true,"is_vl":true,"max_input_tokens":200000}
	]}`)
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}

	q := byID["Qwen3.8-Max"]
	if got := q.ContextWindows; len(got) != 3 || got[0] != 200000 || got[1] != 400000 || got[2] != 1000000 {
		t.Fatalf("context windows = %v", got)
	}
	if q.DefaultContextWindow != 200000 {
		t.Fatalf("default window = %d", q.DefaultContextWindow)
	}
	if !q.IsVL || !q.IsNew || !q.IsReasoning {
		t.Fatalf("flags = vl:%v new:%v reasoning:%v", q.IsVL, q.IsNew, q.IsReasoning)
	}
	if q.Icon != "QoderQwenAiFill" {
		t.Fatalf("icon = %q", q.Icon)
	}

	// 单一档位。
	if m := byID["MiniMax-M3.1-Flash"]; len(m.ContextWindows) != 1 || m.DefaultContextWindow != 200000 {
		t.Fatalf("MiniMax windows = %v default=%d", m.ContextWindows, m.DefaultContextWindow)
	}
	// 无 context_config → 空档位。
	if a := byID["Auto"]; len(a.ContextWindows) != 0 || !a.IsDefault {
		t.Fatalf("Auto = %+v", a)
	}
}

// TestBuildLegacyRequestWritesThinking 验证请求体里落到上游的字段。
func TestBuildLegacyRequestWritesThinking(t *testing.T) {
	acc := Account{UserType: "personal_pro"}
	model := ModelInfo{
		ID:             "Qwen3.8-Max",
		Key:            "qmodel_38max",
		IsVL:           true,
		IsReasoning:    true,
		MaxInputTokens: 180000,
		Thinking:       &ThinkingConfig{CanDisable: true, Efforts: []string{"low", "medium", "xhigh"}, DefaultEffort: "medium"},
	}
	raw := []byte(`{"model":"qmodel_38max","reasoning_effort":"xhigh","max_tokens":4096,
		"messages":[{"role":"user","content":"hi"}]}`)

	body, err := buildLegacyRequestWithModel(acc, raw, model, true)
	if err != nil {
		t.Fatal(err)
	}
	params := body["parameters"].(map[string]any)
	if params["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %v", params["reasoning_effort"])
	}
	if params["max_tokens"] != int64(4096) {
		t.Fatalf("max_tokens = %v", params["max_tokens"])
	}
	mc := body["model_config"].(map[string]any)
	if mc["is_reasoning"] != true || mc["is_vl"] != true {
		t.Fatalf("model_config = %+v", mc)
	}
	if mc["max_input_tokens"] != int64(180000) {
		t.Fatalf("max_input_tokens = %v", mc["max_input_tokens"])
	}
	if mc["display_name"] != "Qwen3.8-Max" {
		t.Fatalf("display_name = %v", mc["display_name"])
	}
	// chat_context.extra.modelConfig 必须与 model_config 同步。
	extra := body["chat_context"].(map[string]any)["extra"].(map[string]any)
	if extra["modelConfig"].(map[string]any)["is_reasoning"] != true {
		t.Fatalf("chat_context modelConfig not synced")
	}

	// 关闭思考时：is_reasoning=false 且不写 reasoning_effort。
	off, err := buildLegacyRequestWithModel(acc, []byte(`{"model":"qmodel_38max","reasoning_effort":"none","messages":[{"role":"user","content":"hi"}]}`), model, true)
	if err != nil {
		t.Fatal(err)
	}
	offMC := off["model_config"].(map[string]any)
	if offMC["is_reasoning"] != false {
		t.Fatalf("disabled thinking should set is_reasoning=false, got %v", offMC["is_reasoning"])
	}
	if _, ok := off["parameters"].(map[string]any)["reasoning_effort"]; ok {
		t.Fatalf("disabled thinking should not write reasoning_effort")
	}

	// modelKnown=false 时保持旧行为（不写思考字段）。
	plain, err := buildLegacyRequest(acc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["parameters"].(map[string]any)["reasoning_effort"]; ok {
		t.Fatalf("unknown model should not write reasoning_effort")
	}
}
