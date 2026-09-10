package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

// seedLinkedEndpoint 直接写一条已接入的网关端点记录。
// 不走 /link 接口是为了避开它顺带拉取上游模型目录（那是真实网络调用）。
func seedLinkedEndpoint(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, enabled, models, disabled_models, plugin_id)
		VALUES (?, ?, ?, ?, 1, '[]', '[]', 'workbuddy')`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey); err != nil {
		t.Fatal(err)
	}
}

func readLinkedDisabled(t *testing.T, s *Service) []string {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRowContext(context.Background(),
		`SELECT COALESCE(disabled_models,'') FROM openai_endpoints WHERE id = ?`,
		linkedEndpointID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// 模型开关必须写穿到网关端点的 disabled_models —— 网关是按那一列拦请求的，
// 只改插件自己的设置不会让网关停止把停用模型路由过来。
func TestModelToggleWritesThroughToGatewayEndpoint(t *testing.T) {
	s := newTestService(t)
	seedLinkedEndpoint(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/workbuddy/models/toggle/wb-hy3", strings.NewReader(`{"enabled":false}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("停用模型应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := s.Settings().DisabledModels; len(got) != 1 || got[0] != "wb-hy3" {
		t.Fatalf("插件设置未记录停用模型: %v", got)
	}
	if got := readLinkedDisabled(t, s); len(got) != 1 || got[0] != "wb-hy3" {
		t.Fatalf("停用模型未同步到网关端点 disabled_models: %v", got)
	}

	// 重新启用后，两端都应清空。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost,
		"/api/workbuddy/models/toggle/wb-hy3", strings.NewReader(`{"enabled":true}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("启用模型应返回 200，得到 %d", rec.Code)
	}
	if got := s.Settings().DisabledModels; len(got) != 0 {
		t.Fatalf("启用后插件设置应清空停用列表: %v", got)
	}
	if got := readLinkedDisabled(t, s); len(got) != 0 {
		t.Fatalf("启用后网关端点 disabled_models 应清空: %v", got)
	}
}

// 批量停用同样要写穿。
func TestBatchToggleWritesThrough(t *testing.T) {
	s := newTestService(t)
	seedLinkedEndpoint(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/models/toggle-batch",
		strings.NewReader(`{"enabled":false,"models":["wb-hy3","wb-glm-5.2"]}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量停用应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	got := readLinkedDisabled(t, s)
	if len(got) != 2 {
		t.Fatalf("批量停用未同步: %v", got)
	}
}

// 停用模型在转发层必须被拒（这是覆盖直连 /api/workbuddy/v1、绕过网关的那道闸）。
func TestRelayRejectsDisabledModel(t *testing.T) {
	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:        true,
		DisabledModels: []string{"wb-hy3"},
		Accounts:       []Account{},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"wb-hy3","messages":[{"role":"user","content":"hi"}]}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("停用模型应被拒绝（404），得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model_not_found") {
		t.Fatalf("错误体应是 OpenAI 的 model_not_found: %s", rec.Body.String())
	}
}

// 未停用但无可用账号 → 503（证明模型闸在前、且不会误伤正常模型）。
func TestRelayWithoutAccountReturns503(t *testing.T) {
	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:        true,
		DisabledModels: []string{"wb-hy3"},
		Accounts:       []Account{},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"hy3","messages":[{"role":"user","content":"hi"}]}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无可用账号应返回 503，得到 %d: %s", rec.Code, rec.Body.String())
	}
}

// 插件未启用时中继面整体拒绝。
func TestRelayDisabledWhenPluginOff(t *testing.T) {
	s := newTestService(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"hy3","messages":[]}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用应返回 503，得到 %d", rec.Code)
	}
}

// 管理面读设置时绝不能带出 token。
func TestPublicSettingsStripsTokens(t *testing.T) {
	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{{
			ID:           "u1",
			UID:          "u1",
			Nickname:     "测试",
			AccessToken:  "secret-access",
			RefreshToken: "secret-refresh",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workbuddy/settings", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取设置应返回 200，得到 %d", rec.Code)
	}
	bodyText := rec.Body.String()
	for _, leaked := range []string{"secret-access", "secret-refresh"} {
		if strings.Contains(bodyText, leaked) {
			t.Fatalf("设置接口泄露了凭据 %q: %s", leaked, bodyText)
		}
	}
	var payload struct {
		Settings struct {
			Accounts []AccountView `json:"accounts"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Settings.Accounts) != 1 || payload.Settings.Accounts[0].ID != "u1" {
		t.Fatalf("账号视图异常: %+v", payload.Settings.Accounts)
	}
}

// 前缀归一化必须对「旧前缀 / 新前缀 / 裸名」三种输入都得到同一结果，且幂等 ——
// 前端是整对象 PUT，前缀变更时会把旧命名空间的名单和新前缀一起提交。
func TestRemapNamespaceHandlesAllPrefixShapes(t *testing.T) {
	cases := []struct{ name, newPrefix, oldPrefix, want string }{
		{"wb-hy3", "wb-", "", "wb-hy3"},    // 已是新前缀（幂等）
		{"hy3", "wb-", "", "wb-hy3"},       // 裸名
		{"wb-hy3", "cb-", "wb-", "cb-hy3"}, // 旧前缀 → 新前缀
		{"cb-hy3", "cb-", "wb-", "cb-hy3"}, // 已是新前缀
		{"wb-hy3", "", "wb-", "hy3"},       // 去掉前缀
		{"hy3", "", "", "hy3"},             // 无前缀
		{"wb-x", "wb-", "w", "wb-x"},       // 旧前缀是新前缀的前缀时取最长匹配
	}
	for _, c := range cases {
		got := remapNamespace(c.name, c.newPrefix, c.oldPrefix, c.newPrefix)
		if got != c.want {
			t.Errorf("remapNamespace(%q, new=%q, old=%q)=%q want=%q", c.name, c.newPrefix, c.oldPrefix, got, c.want)
		}
	}
}

// 被停用的模型不能出现在中继面 /v1/models 里（网关据此刷新端点模型名单）。
func TestRelayModelsFiltersDisabled(t *testing.T) {
	s := newTestService(t)
	// 直接注入目录缓存，避免真实上游调用。
	s.modelMu.Lock()
	s.modelCache = []ModelInfo{{ID: "hy3"}, {ID: "glm-5.2"}}
	s.modelCacheAt = time.Now()
	s.modelMu.Unlock()
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:        true,
		DisabledModels: []string{"hy3"},
		Accounts:       []Account{},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workbuddy/v1/models", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("模型列表应返回 200，得到 %d", rec.Code)
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0]["id"] != "glm-5.2" {
		t.Fatalf("停用模型未从 /v1/models 过滤: %+v", payload.Data)
	}
}

// 前缀变更时，端点上的停用名单与映射 key 必须整体迁移到新命名空间。
func TestPrefixChangeMigratesEndpointNamespaces(t *testing.T) {
	s := newTestService(t)
	seedLinkedEndpoint(t, s)
	ctx := context.Background()

	if err := s.SaveSettings(ctx, Settings{
		Enabled:        true,
		ModelPrefix:    "wb-",
		DisabledModels: []string{"wb-hy3"},
		Accounts:       []Account{},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readLinkedDisabled(t, s); len(got) != 1 || got[0] != "wb-hy3" {
		t.Fatalf("初始停用名单未同步: %v", got)
	}

	// 前缀 wb- → cb-
	if err := s.SaveSettings(ctx, Settings{
		Enabled:        true,
		ModelPrefix:    "cb-",
		DisabledModels: []string{"cb-hy3"},
		Accounts:       []Account{},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readLinkedDisabled(t, s); len(got) != 1 || got[0] != "cb-hy3" {
		t.Fatalf("停用名单未随前缀迁移: %v", got)
	}
}

// 回归：「保存设置」是整对象 PUT，且前端本地可能还持有切换前的旧名单，
// 绝不能把刚做的模型启停覆盖掉（现象：开关点了、保存、刷新后回到原状态）。
func TestSettingsPutDoesNotClobberModelToggles(t *testing.T) {
	s := newTestService(t)
	seedLinkedEndpoint(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/workbuddy/models/toggle/wb-hy3", strings.NewReader(`{"enabled":false}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("停用模型应返回 200，得到 %d", rec.Code)
	}

	// 模拟前端用「切换前」的旧快照整对象 PUT。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/workbuddy/settings",
		strings.NewReader(`{"enabled":true,"modelPrefix":"wb-","proxyPoolId":"","disabledModels":[],"accounts":[]}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}

	if got := s.Settings().DisabledModels; len(got) != 1 || got[0] != "wb-hy3" {
		t.Fatalf("保存设置把模型启停覆盖了: %v", got)
	}
	if got := readLinkedDisabled(t, s); len(got) != 1 || got[0] != "wb-hy3" {
		t.Fatalf("保存设置后网关端点停用名单被覆盖: %v", got)
	}
	// 响应必须回传真实状态，前端据此回填，否则界面会显示成"已启用"。
	var payload struct {
		Settings struct {
			DisabledModels []string `json:"disabledModels"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Settings.DisabledModels) != 1 || payload.Settings.DisabledModels[0] != "wb-hy3" {
		t.Fatalf("PUT 响应未回传真实停用名单: %+v", payload.Settings.DisabledModels)
	}
}

// 联调辅助（默认跳过）：验证真实上游的协议形状没有变。
// 该插件是从参考实现记录的协议事实重建的，上游一旦调整字段/编码，这里最先发现。
// 用法：WORKBUDDY_LIVE=1 go test ./internal/workbuddy/ -run TestLiveFetchCatalog -v
func TestLiveFetchCatalog(t *testing.T) {
	if os.Getenv("WORKBUDDY_LIVE") != "1" {
		t.Skip("设置 WORKBUDDY_LIVE=1 才跑真实上游联调")
	}
	s := newTestService(t)
	models, err := s.fetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("拉取真实目录失败: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("上游未返回任何可服务对话的模型")
	}
	parsed := 0
	for _, m := range models {
		if m.ContextLength <= 0 {
			t.Errorf("%s 上下文长度异常: %d", m.ID, m.ContextLength)
		}
		if m.CreditsParsed {
			parsed++
		}
	}
	t.Logf("真实上游返回 %d 个模型，其中 %d 个解析出倍率", len(models), parsed)
	if parsed == 0 {
		t.Error("没有任何倍率解析成功，上游 credits 字段格式可能已变")
	}
}

// mockCodeBuddyUpstream 起一个假的上游：记录收到的请求体与鉴权头，并回 CodeBuddy 实测的
// SSE 形状（usage 用 DeepSeek 风格命名 prompt_cache_hit_tokens + credit）。
// 注意：命中数刻意用**字符串**下发 —— 实测上游就是这么发的，而下游网关的正则只认裸数字，
// 归一化必须负责转类型，否则网关的缓存统计会恒为 0。
func mockCodeBuddyUpstream(t *testing.T, gotReq *map[string]any, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, gotReq)
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"c1","model":"deepseek-v4.1-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
			`data: {"choices":[{"delta":{"content":" there","tool_calls":[]}}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":"80","prompt_cache_miss_tokens":"20","credit":0.03}}`,
			`data: [DONE]`,
			``,
		}, "\n"))
	}))
	t.Cleanup(srv.Close)
	old := upstreamBase
	upstreamBase = srv.URL
	t.Cleanup(func() { upstreamBase = old })
	return srv
}

// enableWithAccount 打开插件并放一个可用账号（token 未过期）。
func enableWithAccount(t *testing.T, s *Service) {
	t.Helper()
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{{
			ID: "u1", UID: "u1", AccessToken: "tok",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

// 端到端（流式）：mock 上游 → 插件中继 → 下游字节。
// ① 发上游的请求必须带 stream + stream_options.include_usage（否则上游不回完整 usage）；
// ② 下游 chunk 里必须出现网关唯一认得的 usage.prompt_tokens_details.cached_tokens，
//
//	由上游的 prompt_cache_hit_tokens 归一而来 —— 这就是"缓存统计恒为 0"的修复点。
func TestRelayEndToEndStreamNormalizesCacheForGateway(t *testing.T) {
	var gotReq map[string]any
	var gotAuth string
	mockCodeBuddyUpstream(t, &gotReq, &gotAuth)

	s := newTestService(t)
	enableWithAccount(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("中继应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("上游鉴权头不对: %q", gotAuth)
	}
	if gotReq["stream"] != true {
		t.Errorf("发给上游的请求必须 stream=true，得到 %v", gotReq["stream"])
	}
	opts, _ := gotReq["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Errorf("必须注入 stream_options.include_usage=true，得到 %v", gotReq["stream_options"])
	}

	out := rec.Body.String()
	// 网关的 cachedTokensRegex 就是扫这个字面量，必须是标准形状。
	if !strings.Contains(out, `"cached_tokens":80`) {
		t.Fatalf("下游字节里没有标准 cached_tokens，网关仍会统计为 0。实际:\n%s", out)
	}
	if !strings.Contains(out, `"content":"hi"`) || !strings.Contains(out, `"content":" there"`) {
		t.Errorf("正文内容被破坏:\n%s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Errorf("缺少 SSE 终止符:\n%s", out)
	}
	// 诊断快照应认为上游已上报缓存。
	if info := s.upstreamUsageInfo(); info == nil || info["cacheReported"] != true {
		t.Errorf("upstreamUsage 快照异常: %+v", info)
	}

	// 记账闭环：这次调用必须被计入用量（账号 u1 / 模型 deepseek-v4.1-flash），
	// 且 credit 与缓存命中都要落下来 —— 这是"谁在烧额度"的数据来源。
	urec := httptest.NewRecorder()
	s.ServeHTTP(urec, httptest.NewRequest(http.MethodGet, "/api/workbuddy/usage?days=7", nil))
	if urec.Code != http.StatusOK {
		t.Fatalf("用量接口应返回 200，得到 %d: %s", urec.Code, urec.Body.String())
	}
	var accounting struct {
		Totals    usageTotals      `json:"totals"`
		ByAccount []map[string]any `json:"byAccount"`
		ByModel   []map[string]any `json:"byModel"`
	}
	if err := json.Unmarshal(urec.Body.Bytes(), &accounting); err != nil {
		t.Fatal(err)
	}
	if accounting.Totals.Requests != 1 ||
		accounting.Totals.PromptTokens != 100 ||
		accounting.Totals.CompletionTokens != 5 ||
		accounting.Totals.CachedTokens != 80 {
		t.Errorf("用量合计不对: %+v", accounting.Totals)
	}
	if math.Abs(accounting.Totals.Credit-0.03) > 1e-9 {
		t.Errorf("扣费合计=%.6f want=0.03", accounting.Totals.Credit)
	}
	if len(accounting.ByAccount) != 1 || accounting.ByAccount[0]["accountId"] != "u1" {
		t.Errorf("按账号聚合异常: %+v", accounting.ByAccount)
	}
	if len(accounting.ByModel) != 1 || accounting.ByModel[0]["model"] != "deepseek-v4.1-flash" {
		t.Errorf("按模型聚合异常: %+v", accounting.ByModel)
	}
}

// 端到端（非流式）：客户端要非流式时，本层仍按流式请求上游，再聚合成整包回给下游，
// 聚合结果里同样必须带标准 cached_tokens。
func TestRelayEndToEndNonStreamAggregatesWithCache(t *testing.T) {
	var gotReq map[string]any
	var gotAuth string
	mockCodeBuddyUpstream(t, &gotReq, &gotAuth)

	s := newTestService(t)
	enableWithAccount(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if gotReq["stream"] != true {
		t.Errorf("即使客户端要非流式，发给上游也必须 stream=true，得到 %v", gotReq["stream"])
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("非流式响应不是单个 JSON: %v\n%s", err, rec.Body.String())
	}
	if payload["object"] != "chat.completion" {
		t.Errorf("object=%v want=chat.completion", payload["object"])
	}
	usage, _ := payload["usage"].(map[string]any)
	details, _ := usage["prompt_tokens_details"].(map[string]any)
	if details == nil || details["cached_tokens"] != float64(80) {
		t.Fatalf("聚合后的 usage 缺标准 cached_tokens: %v", payload["usage"])
	}
	choices := payload["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hi there" {
		t.Errorf("聚合内容不对: %v", msg["content"])
	}
}

// 配了模型前缀时，发往上游的 body 必须是**剥离前缀后**的真实模型名。
// 剥前缀只用于判定 hy3/hy4 而不写回 body，会让上游收到未知模型名（如 wb-hy3）。
func TestRelayStripsModelPrefixBeforeUpstream(t *testing.T) {
	var gotReq map[string]any
	var gotAuth string
	mockCodeBuddyUpstream(t, &gotReq, &gotAuth)

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:     true,
		ModelPrefix: "wb-",
		Accounts:    []Account{validAccount("u1", "tok")},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(`{"model":"wb-deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if gotReq["model"] != "deepseek-v4.1-flash" {
		t.Fatalf("发往上游的 model 应为剥前缀后的 %q，实际 %v", "deepseek-v4.1-flash", gotReq["model"])
	}
}
