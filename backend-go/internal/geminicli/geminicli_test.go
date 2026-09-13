package geminicli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/openai"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

func validAccount(id, token string) Account {
	return Account{
		ID:           id,
		Email:        id + "@example.com",
		ProjectID:    "proj-" + id,
		AccessToken:  token,
		RefreshToken: "refresh-" + id,
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
}

// wrapGenerateRequest 必须产出 {project, model, request} 包裹层，且不改动内层。
func TestWrapGenerateRequest(t *testing.T) {
	inner := map[string]interface{}{
		"contents":         []interface{}{map[string]interface{}{"role": "user"}},
		"generationConfig": map[string]interface{}{"temperature": 0.5},
	}
	wrapped := wrapGenerateRequest("proj-1", "gemini-2.5-pro", inner)
	if wrapped["project"] != "proj-1" {
		t.Fatalf("project 包装错误: %v", wrapped["project"])
	}
	if wrapped["model"] != "gemini-2.5-pro" {
		t.Fatalf("model 包装错误: %v", wrapped["model"])
	}
	req, ok := wrapped["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("request 应为对象，得到 %T", wrapped["request"])
	}
	if _, has := req["contents"]; !has {
		t.Fatal("request 应保留内层 contents")
	}
	if _, has := wrapped["contents"]; has {
		t.Fatal("顶层不应直接出现 contents")
	}
	// 内层 map 不应被改动（浅拷贝语义）。
	if _, has := inner["project"]; has {
		t.Fatal("内层不应被写入 project")
	}
}

// unwrapGenerateResponse 解包 {"response": {...}}；非包装形态原样返回。
func TestUnwrapGenerateResponse(t *testing.T) {
	wrapped := []byte(`{"response":{"candidates":[{"finishReason":"STOP"}]},"traceId":"x"}`)
	got := unwrapGenerateResponse(wrapped)
	var parsed map[string]interface{}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("解包结果不是合法 JSON: %v", err)
	}
	if _, has := parsed["candidates"]; !has {
		t.Fatalf("应取出内层 response: %s", string(got))
	}
	if _, has := parsed["traceId"]; has {
		t.Fatal("不应保留外层字段")
	}

	plain := []byte(`{"candidates":[{"finishReason":"STOP"}]}`)
	got = unwrapGenerateResponse(plain)
	if string(got) != string(plain) {
		t.Fatalf("非包装形态应原样返回，得到 %s", string(got))
	}
}

// cloudaicompanionProject 支持 string 与 {id} 两种形状。
func TestExtractProjectID(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{"proj-a", "proj-a"},
		{"  proj-b  ", "proj-b"},
		{map[string]interface{}{"id": "proj-c"}, "proj-c"},
		{map[string]interface{}{"id": ""}, ""},
		{nil, ""},
		{42, ""},
	}
	for _, c := range cases {
		if got := extractProjectID(c.in); got != c.want {
			t.Errorf("extractProjectID(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// PKCE S256 是确定性的 base64url(sha256(verifier))。
func TestCodeChallengeS256(t *testing.T) {
	// RFC 7636 附录 B 的测试向量。
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := codeChallengeS256(verifier); got != want {
		t.Fatalf("codeChallengeS256 = %q, want %q", got, want)
	}
	if codeChallengeS256(verifier) != codeChallengeS256(verifier) {
		t.Fatal("S256 应确定性")
	}
}

// 授权 URL 必须带上 gemini-cli client、S256、offline 与三个 scope。
func TestStartLoginURL(t *testing.T) {
	// client_id 改为部署方通过环境变量提供，测试里注入一个占位值。
	t.Setenv(GeminiCLIOAuthClientIDEnv, "test-client-id.apps.googleusercontent.com")
	s := newTestService(t)
	state, authURL, err := s.startLogin("")
	if err != nil {
		// 端口被占用等环境问题不应让测试失败在断言上。
		t.Skipf("startLogin 环境不可用: %v", err)
	}
	defer s.forgetLoginState(state)
	if state == "" {
		t.Fatal("state 不应为空")
	}
	for _, frag := range []string{"code_challenge_method=S256", "access_type=offline", "client_id=test-client-id.apps.googleusercontent.com", "response_type=code", "state=" + state} {
		if !strings.Contains(authURL, frag) {
			t.Errorf("授权 URL 缺少 %q: %s", frag, authURL)
		}
	}
	lst := s.loginStates[state]
	if lst == nil || lst.codeVerifier == "" {
		t.Fatal("loginState 应记录 code_verifier")
	}
	if got := codeChallengeS256(lst.codeVerifier); !strings.Contains(authURL, got) {
		t.Error("授权 URL 的 code_challenge 与 verifier 不匹配")
	}
}

// 模型目录固定 7 项，ID 与上下文长度符合规格。
func TestStaticCatalog(t *testing.T) {
	want := []string{
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite",
		"gemini-3-pro-preview", "gemini-3.1-pro-preview",
		"gemini-3-flash-preview", "gemini-3.1-flash-lite-preview",
	}
	models := staticModels
	if len(models) != len(want) {
		t.Fatalf("模型数 = %d, want %d", len(models), len(want))
	}
	for i, id := range want {
		if models[i].ID != id {
			t.Errorf("第 %d 项 = %q, want %q", i, models[i].ID, id)
		}
		if models[i].ContextLength != 1048576 {
			t.Errorf("%s contextLength = %d, want 1048576", id, models[i].ContextLength)
		}
		if models[i].MaxOutputTokens != 65536 {
			t.Errorf("%s maxOutputTokens = %d, want 65536", id, models[i].MaxOutputTokens)
		}
	}
}

// token 过期判定与可用性。
func TestTokenStateAndAvailability(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		acc    Account
		state  string
		usable bool
	}{
		{"valid", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(time.Hour).Unix()}, "valid", true},
		{"expiring", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(10 * time.Minute).Unix()}, "expiring", true},
		{"expired", Account{ID: "a", AccessToken: "t", ExpiresAt: now.Add(-time.Minute).Unix()}, "expired", false},
		{"no-token", Account{ID: "a"}, "unknown", false},
		{"no-expiry", Account{ID: "a", AccessToken: "t"}, "unknown", true},
	}
	for _, c := range cases {
		if got := tokenState(c.acc); got != c.state {
			t.Errorf("%s: tokenState = %q, want %q", c.name, got, c.state)
		}
		if got := accountAvailable(c.acc); got != c.usable {
			t.Errorf("%s: accountAvailable = %v, want %v", c.name, got, c.usable)
		}
	}
	disabled := validAccount("d", "t")
	disabled.Disabled = true
	if accountAvailable(disabled) {
		t.Error("停用账号不可用")
	}
}

// 选号：跳过停用/过期/无 token/冷却账号，在候选中取调用次数最少者。
func TestPickAccount(t *testing.T) {
	s := newTestService(t)
	a1 := validAccount("a1", "t1")
	a2 := validAccount("a2", "t2")
	s.settings = Settings{Accounts: []Account{a1, a2}}

	// 零调用 → 取列表序首个。
	acc, ok := s.pickAccount(nil)
	if !ok || acc.ID != "a1" {
		t.Fatalf("零调用应取列表序首个，得到 %v/%v", acc.ID, ok)
	}

	// a1 调用更多 → 选 a2。
	s.callBase["a1"] = 10
	acc, ok = s.pickAccount(nil)
	if !ok || acc.ID != "a2" {
		t.Fatalf("应选调用更少的 a2，得到 %v", acc.ID)
	}

	// a2 冷却中 → 回落到 a1，并把 a2 排除。
	s.setCooldown("a2", time.Minute)
	acc, ok = s.pickAccount(nil)
	if !ok || acc.ID != "a1" {
		t.Fatalf("冷却账号应被跳过，得到 %v", acc.ID)
	}

	// 显式排除 a1 后只剩冷却中的 a2 → 回退到它（全部冷却时不空手而归）。
	acc, ok = s.pickAccount(map[string]bool{"a1": true})
	if !ok || acc.ID != "a2" {
		t.Fatalf("候选全冷却时应回退到 a2，得到 %v/%v", acc.ID, ok)
	}

	// 全部不可用 → false。
	expired := validAccount("e1", "te")
	expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	s.settings = Settings{Accounts: []Account{expired}}
	if _, ok := s.pickAccount(nil); ok {
		t.Fatal("无可用账号应返回 false")
	}
}

// 冷却写入后 inCooldown 为真，时长过后自动失效，clear 立即生效。
func TestCooldownLifecycle(t *testing.T) {
	s := newTestService(t)
	if s.inCooldown("a1", time.Now()) {
		t.Fatal("初始不应冷却")
	}
	s.setCooldown("a1", 50*time.Millisecond)
	if !s.inCooldown("a1", time.Now()) {
		t.Fatal("设置后应冷却")
	}
	time.Sleep(60 * time.Millisecond)
	if s.inCooldown("a1", time.Now()) {
		t.Fatal("冷却到期后应失效")
	}
	s.setCooldown("a1", time.Minute)
	s.clearCooldown("a1")
	if s.inCooldown("a1", time.Now()) {
		t.Fatal("clearCooldown 后不应冷却")
	}
}

// 模型启停会写入 disabledModels，并同步到已接入端点的 disabled_models 列。
func TestSetModelsEnabled(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	if err := s.setModelsEnabled(ctx, []string{"gemini-2.5-pro"}, false); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if s.isModelEnabled("gemini-2.5-pro") {
		t.Fatal("停用后应不可用")
	}
	if !s.isModelEnabled("gemini-2.5-flash") {
		t.Fatal("其它模型仍应可用")
	}

	if err := s.setModelsEnabled(ctx, []string{"gemini-2.5-pro"}, true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if !s.isModelEnabled("gemini-2.5-pro") {
		t.Fatal("启用后应可用")
	}
}

// 模型前缀：套前缀、剥前缀、带前缀的启停判定。
func TestModelPrefix(t *testing.T) {
	s := newTestService(t)
	s.settings = Settings{ModelPrefix: "gcli-"}
	if got := s.prefixModel("gemini-2.5-pro"); got != "gcli-gemini-2.5-pro" {
		t.Fatalf("prefixModel = %q", got)
	}
	if got := s.stripModelPrefix("gcli-gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Fatalf("stripModelPrefix = %q", got)
	}
	if got := s.stripModelPrefix("gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Fatalf("无前缀应原样返回，得到 %q", got)
	}
}

// 端到端：OpenAI 请求 → 标准 Gemini 请求 → 包成 v1internal 请求体。
func TestOpenAIToV1InternalWrapping(t *testing.T) {
	payload := map[string]interface{}{
		"model": "gemini-2.5-pro",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "you are helpful"},
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"temperature": 0.3,
	}
	inner, err := openai.OpenAIChatToGeminiGenerate(payload)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if _, has := inner["contents"]; !has {
		t.Fatalf("内层应含 contents: %#v", inner)
	}
	wrapped := wrapGenerateRequest("proj-x", "gemini-2.5-pro", inner)
	raw, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	req := back["request"].(map[string]interface{})
	if _, has := req["contents"]; !has {
		t.Fatal("包好后 request.contents 丢失")
	}
	if back["project"] != "proj-x" {
		t.Fatalf("project 丢失: %v", back["project"])
	}
}

// 端到端：v1internal 响应（带 response 外层）→ OpenAI chat 响应。
func TestV1InternalResponseToOpenAI(t *testing.T) {
	inner := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi there"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`
	wrapped := `{"response":` + inner + `}`
	out, err := openai.GeminiGenerateToOpenAIChat(unwrapGenerateResponse([]byte(wrapped)), "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("解析 OpenAI 响应失败: %v\n%s", err, string(out))
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hi there" {
		t.Fatalf("正文转换错误: %s", string(out))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason 错误: %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("usage 错误: %+v", resp.Usage)
	}
}

// usage 记录：从内层响应取 usageMetadata 并按账号/模型聚合。
func TestRecordUsageFromResponse(t *testing.T) {
	s := newTestService(t)
	inner := []byte(`{"usageMetadata":{"promptTokenCount":10,"toolUsePromptTokenCount":2,"candidatesTokenCount":20,"thoughtsTokenCount":5,"cachedContentTokenCount":4}}`)
	s.recordUsageFromResponse("acc-1", "gemini-2.5-pro", inner)
	keys := s.sortedUsageKeys()
	if len(keys) != 1 {
		t.Fatalf("应产生 1 个用量键，得到 %d", len(keys))
	}
	d := s.usagePending[keys[0]]
	if d.promptTokens != 12 {
		t.Errorf("prompt tokens = %d, want 12", d.promptTokens)
	}
	if d.completionTokens != 25 {
		t.Errorf("completion tokens = %d, want 25", d.completionTokens)
	}
	if d.cachedTokens != 4 {
		t.Errorf("cached tokens = %d, want 4", d.cachedTokens)
	}
	if d.requests != 1 {
		t.Errorf("requests = %d, want 1", d.requests)
	}

	// 无 usage 的响应不应产生增量。
	s.recordUsageFromResponse("acc-1", "gemini-2.5-pro", []byte(`{"candidates":[]}`))
	if len(s.sortedUsageKeys()) != 1 {
		t.Error("空 usage 不应新增键")
	}
}

// 中继面未启用时直接拒绝。
func TestServeRelayDisabled(t *testing.T) {
	s := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/geminicli/v1/models", nil)
	rec := httptest.NewRecorder()
	s.serveRelay(rec, req, "/v1/models")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用应返回 503，得到 %d", rec.Code)
	}
}

// 中继面模型列表：带前缀并过滤停用项。
func TestServeRelayModels(t *testing.T) {
	s := newTestService(t)
	s.settings = Settings{Enabled: true, ModelPrefix: "gcli-", DisabledModels: []string{"gcli-gemini-2.5-pro"}}
	rec := httptest.NewRecorder()
	s.serveRelayModels(rec)
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" {
		t.Fatalf("object = %q", body.Object)
	}
	if len(body.Data) != len(staticModels)-1 {
		t.Fatalf("停用后应剩 %d 项，得到 %d", len(staticModels)-1, len(body.Data))
	}
	for _, d := range body.Data {
		if d.ID == "gcli-gemini-2.5-pro" {
			t.Error("停用模型不应出现在列表")
		}
		if !strings.HasPrefix(d.ID, "gcli-") {
			t.Errorf("模型应带前缀: %s", d.ID)
		}
	}
}

// generateURL 的流式/非流式路径与 alt=sse。
func TestGenerateURL(t *testing.T) {
	if got := generateURL(false); !strings.HasSuffix(got, ":generateContent") {
		t.Fatalf("非流式 URL 错误: %s", got)
	}
	got := generateURL(true)
	if !strings.HasSuffix(got, ":streamGenerateContent?alt=sse") {
		t.Fatalf("流式 URL 错误: %s", got)
	}
	if !strings.Contains(got, "cloudcode-pa.googleapis.com") {
		t.Fatalf("主机错误: %s", got)
	}
}

// v1internal 请求头必须匹配 gemini-cli 的 UA 与 X-Goog-Api-Client。
func TestCodeAssistHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	codeAssistHeaders(req, "tok-123", "gemini-2.5-flash")
	if got := req.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("X-Goog-Api-Client"); got != geminiCLIApiClient {
		t.Errorf("X-Goog-Api-Client = %q", got)
	}
	wantUA := "GeminiCLI/0.34.0/gemini-2.5-flash"
	if got := req.Header.Get("User-Agent"); !strings.Contains(got, wantUA) {
		t.Errorf("User-Agent = %q, want 含 %q", got, wantUA)
	}
	// 无模型时用默认占位模型。
	req2 := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	codeAssistHeaders(req2, "tok", "")
	if got := req2.Header.Get("User-Agent"); !strings.Contains(got, "GeminiCLI/0.34.0/"+defaultUserAgentModel) {
		t.Errorf("默认 UA 错误: %q", got)
	}
}

func TestParseCallbackCode(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		fallb   string
		wantCod string
		wantSt  string
		wantErr bool
	}{
		{name: "完整回调 URL", raw: "http://127.0.0.1:8085/oauth2callback?code=abc123&state=st-1&scope=x", fallb: "", wantCod: "abc123", wantSt: "st-1"},
		{name: "裸 code", raw: "4/0AX4XfWh", fallb: "st-2", wantCod: "4/0AX4XfWh", wantSt: "st-2"},
		{name: "query 片段", raw: "code=zzz&state=st-3", fallb: "", wantCod: "zzz", wantSt: "st-3"},
		{name: "URL 无 state 用兜底", raw: "http://localhost:8085/oauth2callback?code=c9", fallb: "st-4", wantCod: "c9", wantSt: "st-4"},
		{name: "error 参数", raw: "http://127.0.0.1:8085/oauth2callback?error=access_denied&error_description=nope", fallb: "", wantErr: true},
		{name: "空内容", raw: "   ", fallb: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, state, err := parseCallbackCode(tc.raw, tc.fallb)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望错误，得到 code=%q state=%q", code, state)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if code != tc.wantCod || state != tc.wantSt {
				t.Fatalf("code=%q state=%q, want code=%q state=%q", code, state, tc.wantCod, tc.wantSt)
			}
		})
	}
}

func TestCompleteLoginByPasteMissingState(t *testing.T) {
	s := &Service{loginStates: map[string]*loginState{}}
	if _, err := s.completeLoginByPaste(context.Background(), "", "http://127.0.0.1:8085/oauth2callback?code=x&state=unknown"); err == nil {
		t.Fatal("未知 state 应报错")
	}
}

func TestRemapNamespaceListNormalizesBothNamespaces(t *testing.T) {
	// 两种输入（旧前缀名单、新前缀名单）都要归一化到新前缀，且重复提交幂等。
	got := remapNamespaceList([]string{"old-a", "new-b", "c"}, "new-", "old-", "new-")
	want := []string{"new-a", "new-b", "new-c"}
	if len(got) != len(want) {
		t.Fatalf("长度不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项 = %q, want %q（完整 %v）", i, got[i], want[i], got)
		}
	}
	// 幂等
	again := remapNamespaceList(got, "new-", "old-", "new-")
	for i := range want {
		if again[i] != want[i] {
			t.Fatalf("重复提交应幂等，得到 %v", again)
		}
	}
}

func TestSaveSettingsRemapsDisabledModelsOnPrefixChange(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if err := s.SaveSettings(ctx, Settings{Enabled: true, ModelPrefix: "gcli-", DisabledModels: []string{"gcli-gemini-2.5-pro"}}); err != nil {
		t.Fatal(err)
	}
	// 前端整对象 PUT：带旧前缀的停用名单 + 新前缀一起提交。
	if err := s.SaveSettings(ctx, Settings{Enabled: true, ModelPrefix: "new-", DisabledModels: []string{"gcli-gemini-2.5-pro"}}); err != nil {
		t.Fatal(err)
	}
	got := s.Settings().DisabledModels
	if len(got) != 1 || got[0] != "new-gemini-2.5-pro" {
		t.Fatalf("前缀变更后停用名单应迁移到新命名空间，得到 %v", got)
	}
	if s.isModelEnabled("new-gemini-2.5-pro") {
		t.Fatal("迁移后的模型应仍处于停用态")
	}
}
