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
	// UTC 14:00 起进入错峰；UTC 00:00–14:00 为常规。
	offPeak := time.Date(2026, 10, 4, 14, 30, 0, 0, time.UTC)
	regular := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	if !isOffPeak(offPeak) {
		t.Fatalf("14:30 UTC should be off-peak")
	}
	if isOffPeak(regular) {
		t.Fatalf("10:00 UTC should be regular")
	}
	// 边界：13:59 常规，14:00 错峰，23:59 错峰。
	if isOffPeak(time.Date(2026, 10, 4, 13, 59, 0, 0, time.UTC)) {
		t.Fatalf("13:59 UTC should be regular")
	}
	if !isOffPeak(time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("14:00 UTC should be off-peak")
	}
	if !isOffPeak(time.Date(2026, 10, 4, 23, 59, 0, 0, time.UTC)) {
		t.Fatalf("23:59 UTC should be off-peak")
	}
}

func TestEffectiveMultiplier(t *testing.T) {
	offPeak := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	regular := time.Date(2026, 10, 4, 5, 0, 0, 0, time.UTC)

	// Qwen3.8-Max：标准 0.5，错峰 0.2。
	if v, disc := effectiveMultiplier("qmodel_38max", 0.5, offPeak); v != 0.2 || !disc {
		t.Fatalf("qmodel_38max off-peak = %v disc=%v, want 0.2/true", v, disc)
	}
	if v, disc := effectiveMultiplier("qmodel_38max", 0.5, regular); v != 0.5 || disc {
		t.Fatalf("qmodel_38max regular = %v disc=%v, want 0.5/false", v, disc)
	}
	// 非折扣模型：任何时段都是标准倍率。
	if v, disc := effectiveMultiplier("dfmodel", 0.1, offPeak); v != 0.1 || disc {
		t.Fatalf("dfmodel should stay 0.1/false, got %v/%v", v, disc)
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
	}
	for in, want := range cases {
		if got := normalizeModelKey(in); got != want {
			t.Fatalf("normalizeModelKey(%q)=%q want %q", in, got, want)
		}
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
	if m := byID["auto"]; len(m.Regions) != 2 {
		t.Fatalf("auto should be in both regions, got %v", m.Regions)
	}
	// ultimate 仅国际版。
	if m := byID["ultimate"]; !m.InternationalOnly {
		t.Fatalf("ultimate should be internationalOnly")
	}
	// gm51model（GLM-5.2）仅国内版。
	if m := byID["gm51model"]; len(m.Regions) != 1 || m.Regions[0] != regionCN {
		t.Fatalf("gm51model should be cn-only, got %v", m.Regions)
	}
	// 倍率透传。
	if m := byID["kmodel_latest"]; m.CreditsMultiplier != 1.4 || !m.CreditsParsed {
		t.Fatalf("kmodel_latest multiplier = %v parsed=%v", m.CreditsMultiplier, m.CreditsParsed)
	}
}

func TestCatalogFiltersByActiveRegion(t *testing.T) {
	s := newTestService(t)
	// 无账号：默认国内版。
	got := s.catalog()
	for _, m := range got {
		if m.ID == "ultimate" {
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
		if m.ID == "ultimate" {
			t.Fatalf("cn-only catalog should not include ultimate")
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
		if m.ID == "ultimate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mixed catalog should include intl-only ultimate")
	}
}

func TestAccountServesModelByRegion(t *testing.T) {
	s := newTestService(t)
	cn := Account{ID: "cn1", Region: regionCN}
	intl := Account{ID: "intl1", Region: regionIntl}
	if !s.accountServesModel(cn, "gm51model") {
		t.Fatalf("cn account should serve gm51model")
	}
	if s.accountServesModel(intl, "gm51model") {
		t.Fatalf("intl account should not serve gm51model")
	}
	if !s.accountServesModel(intl, "ultimate") {
		t.Fatalf("intl account should serve ultimate")
	}
	if s.accountServesModel(cn, "ultimate") {
		t.Fatalf("cn account should not serve ultimate")
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
		VALUES (?, 'Qoder', 'http://127.0.0.1:3000/api/qoder/v1', 'sk', 1, '["dfmodel","qfmodel"]', '[]', 'qoder')`,
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

// TestDailyCheckinClaimsAndIdempotent 验证签到：列出活动 → 领取可领取项；已领取幂等。
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

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
