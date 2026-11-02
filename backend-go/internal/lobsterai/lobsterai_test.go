package lobsterai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
)

func TestPrepareChatBodyForcesStream(t *testing.T) {
	in := []byte(`{"model":"deepseek-v4-pro","stream":false,"tool_choice":"none","messages":[{"role":"user","content":"hi"}]}`)
	out := prepareChatBody(in)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["stream"] != true {
		t.Fatalf("stream should be forced true, got %v", body["stream"])
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("tool_choice none should be dropped")
	}
}

func TestPrepareChatBodyKeepsToolChoiceObject(t *testing.T) {
	in := []byte(`{"model":"m","tool_choice":{"type":"function","function":{"name":"f"}}}`)
	out := prepareChatBody(in)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["tool_choice"]; !ok {
		t.Fatalf("object tool_choice should be kept")
	}
}

func TestAggregateSSEContentAndUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","model":"deepseek-v4-pro","created":1700000000,"choices":[{"delta":{"role":"assistant","content":"你好"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"世界"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_cache_hit_tokens":"3"}}`,
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
	choices, _ := resp["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("choices len = %d", len(choices))
	}
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "你好世界" {
		t.Fatalf("content = %v", msg["content"])
	}
	if choices[0].(map[string]any)["finish_reason"] != "stop" {
		t.Fatalf("finish_reason missing")
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage == nil || jsonInt(usage["prompt_tokens"]) != 10 {
		t.Fatalf("usage missing/wrong: %v", resp["usage"])
	}
}

func TestAggregateSSEToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"sf\"}"}}]},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
	}, "\n")
	resp, err := aggregateSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	calls, ok := msg["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls = %v", msg["tool_calls"])
	}
	fn := calls[0]["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Fatalf("name = %v", fn["name"])
	}
	if fn["arguments"] != `{"city":"sf"}` {
		t.Fatalf("arguments = %v", fn["arguments"])
	}
}

func TestAggregateSSEMessageFallback(t *testing.T) {
	sse := `data: {"choices":[{"message":{"content":"直接整包"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n"
	resp, err := aggregateSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "直接整包" {
		t.Fatalf("content = %v", msg["content"])
	}
}

func TestClassifyMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"credit-cn", `{"code":40001,"msg":"积分不足"}`, "credit"},
		{"credit-en", "insufficient credit", "credit"},
		{"rate-cn", "当前您在deepseek模型的使用量已超出频率限制", "rate"},
		{"session", `{"code":40100,"msg":"token rejected"}`, "session"},
		{"other", `{"code":50000,"msg":"internal"}`, "other"},
	}
	for _, c := range cases {
		if got := classifyMessage(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestParseCallbackCode(t *testing.T) {
	full := "http://127.0.0.1:33955/auth/callback?code=abc123&state=st9"
	code, state, err := parseCallbackCode(full, "")
	if err != nil || code != "abc123" || state != "st9" {
		t.Fatalf("full url: code=%q state=%q err=%v", code, state, err)
	}
	code, state, err = parseCallbackCode("code=xyz&state=st1", "")
	if err != nil || code != "xyz" || state != "st1" {
		t.Fatalf("query fragment: code=%q state=%q err=%v", code, state, err)
	}
	code, state, err = parseCallbackCode("barecode", "fallback")
	if err != nil || code != "barecode" || state != "fallback" {
		t.Fatalf("bare code: code=%q state=%q err=%v", code, state, err)
	}
	if _, _, err := parseCallbackCode("   ", ""); err == nil {
		t.Fatalf("empty should error")
	}
}

func TestParseCallbackCodeErrorParam(t *testing.T) {
	if _, _, err := parseCallbackCode("http://127.0.0.1/cb?error=access_denied&error_description=no", ""); err == nil {
		t.Fatalf("error callback should return error")
	}
}

func TestPrefixRemapNamespace(t *testing.T) {
	// 提交旧前缀名单：应归一化到新前缀。
	got := remapNamespaceList([]string{"gcli-a", "b"}, "lobster-", "lobster-", "gcli-")
	want := []string{"lobster-a", "lobster-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remap = %v want %v", got, want)
		}
	}
	// 提交新前缀名单时应幂等。
	got = remapNamespaceList([]string{"lobster-a"}, "lobster-", "lobster-", "gcli-")
	if got[0] != "lobster-a" {
		t.Fatalf("idempotent remap failed: %v", got)
	}
	if stripAnyPrefix("lobster-x", "gcli-", "lobster-") != "x" {
		t.Fatalf("longest-match strip failed")
	}
}

func TestCachedTokens(t *testing.T) {
	if v := cachedTokens(map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": float64(5)}}); v != 5 {
		t.Fatalf("details cached_tokens = %d", v)
	}
	if v := cachedTokens(map[string]any{"prompt_cache_hit_tokens": "930560"}); v != 930560 {
		t.Fatalf("string cache hit = %d", v)
	}
	if v := cachedTokens(map[string]any{"completion_tokens": float64(3)}); v != 0 {
		t.Fatalf("no cache field should be 0, got %d", v)
	}
}

func TestTokenStateAndAvailability(t *testing.T) {
	now := time.Now()
	valid := Account{ID: "u1", AccessToken: "t", ExpiresAt: now.Add(2 * time.Hour).Unix()}
	if tokenState(valid) != "valid" || !accountAvailable(valid) {
		t.Fatalf("valid account misjudged")
	}
	expiring := Account{ID: "u1", AccessToken: "t", ExpiresAt: now.Add(10 * time.Minute).Unix()}
	if tokenState(expiring) != "expiring" || !accountAvailable(expiring) {
		t.Fatalf("expiring account misjudged")
	}
	expired := Account{ID: "u1", AccessToken: "t", ExpiresAt: now.Add(-time.Minute).Unix()}
	if tokenState(expired) != "expired" || accountAvailable(expired) {
		t.Fatalf("expired account misjudged")
	}
	unknown := Account{ID: "u1", AccessToken: "t"}
	if tokenState(unknown) != "unknown" || !accountAvailable(unknown) {
		t.Fatalf("unknown expiry should stay available")
	}
	disabled := Account{ID: "u1", AccessToken: "t", Disabled: true}
	if accountAvailable(disabled) {
		t.Fatalf("disabled account should be unavailable")
	}
}

func TestResolveExpires(t *testing.T) {
	if got := resolveExpires(3600, ""); got <= time.Now().Unix() {
		t.Fatalf("expiresIn should resolve to future, got %d", got)
	}
	if got := resolveExpires(0, "not-a-jwt"); got != 0 {
		t.Fatalf("invalid token should give 0, got %d", got)
	}
}

func TestUsageDayUsesLocation(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// UTC 2026-01-01T20:00Z 在东八区属 2026-01-02。
	tm := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	if got := usageDay(tm, loc); got != "2026-01-02" {
		t.Fatalf("usageDay = %q want 2026-01-02", got)
	}
}

func TestJsonInt(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int64
	}{{float64(7), 7}, {"9", 9}, {json.Number("11"), 11}, {nil, 0}, {"bad", 0}} {
		if got := jsonInt(c.in); got != c.want {
			t.Errorf("jsonInt(%v) = %d want %d", c.in, got, c.want)
		}
	}
}

func TestEncodeRevision(t *testing.T) {
	if got := encodeRevision(float64(3)); got != "3" {
		t.Fatalf("number revision = %q", got)
	}
	if got := encodeRevision("rev-2"); got != "rev-2" {
		t.Fatalf("string revision = %q", got)
	}
	if got := encodeRevision(nil); got != "" {
		t.Fatalf("nil revision = %q", got)
	}
}

func TestIsRetryableHTTP(t *testing.T) {
	if !isRetryableHTTP(429) || !isRetryableHTTP(503) {
		t.Fatalf("429/503 should be retryable")
	}
	if isRetryableHTTP(400) || isRetryableHTTP(401) {
		t.Fatalf("4xx (non-429) should not be retryable")
	}
}

func TestFirstCreditNumber(t *testing.T) {
	if v := firstCreditNumber(map[string]any{"creditsGranted": float64(100)}); v != 100 {
		t.Fatalf("creditsGranted = %v", v)
	}
	if v := firstCreditNumber(map[string]any{"rewardCredits": "50"}); v != 50 {
		t.Fatalf("rewardCredits string = %v", v)
	}
	if v := firstCreditNumber(map[string]any{}); v != 0 {
		t.Fatalf("empty = %v", v)
	}
}

func TestAutoCheckinDefault(t *testing.T) {
	var s Service
	s.settings = defaultSettings()
	if !s.autoCheckinEnabled() {
		t.Fatalf("auto checkin should default to enabled")
	}
	off := false
	s.settings.AutoCheckin = &off
	if s.autoCheckinEnabled() {
		t.Fatalf("auto checkin should honor explicit false")
	}
}

func TestDefaultSettingsEnabled(t *testing.T) {
	st := defaultSettings()
	if !st.Enabled {
		t.Fatalf("default enabled should be true")
	}
	if st.DisabledModels == nil || st.Accounts == nil {
		t.Fatalf("slices should be non-nil")
	}
}

// TestTruncateKeepsUTF8Boundary 截断必须落在 UTF-8 字符边界上：
// 上游报错/响应含中文时，按字节硬切会产生非法 UTF-8 进入错误信息与日志。
func TestTruncateKeepsUTF8Boundary(t *testing.T) {
	s := strings.Repeat("限", 300) // 每个汉字 3 字节
	got := truncate(s, 100)
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if len(got) > 100 {
		t.Fatalf("截断后长度应 <= 100，得到 %d", len(got))
	}
	if !strings.HasPrefix(s, got) {
		t.Fatal("截断结果应是原串前缀")
	}
	// 98 字节落在第 33 个汉字内部（起始字节在 99），应退到 96 字节的完整字符。
	if got := truncate(s, 98); !utf8.ValidString(got) || len(got) != 96 {
		t.Fatalf("非对齐边界应退到完整字符，得到 len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	// 边界：n<=0 与无需截断时原样返回。
	if got := truncate(s, 0); got != s {
		t.Fatal("n<=0 应原样返回")
	}
	if got := truncate("短", 10); got != "短" {
		t.Fatal("长度足够时应原样返回")
	}
}

// TestNormalizeUpstreamTime 上游裸时间（北京时间、无时区后缀）必须归一化为
// RFC3339 UTC，否则前端按浏览器本地时区解析，非东八区用户看到偏移时刻。
func TestNormalizeUpstreamTime(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 北京 2026-09-29 19:34:18 == UTC 2026-09-29 11:34:18
		{"2026-09-29T19:34:18", "2026-09-29T11:34:18Z"},
		// 已是 RFC3339 的按原样归一化，不重复偏移。
		{"2026-09-29T11:34:18Z", "2026-09-29T11:34:18Z"},
		{"", ""},
		// 无法解析的原样返回，交前端兜底。
		{"not-a-time", "not-a-time"},
	}
	for _, c := range cases {
		if got := normalizeUpstreamTime(c.in); got != c.want {
			t.Errorf("normalizeUpstreamTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 选号：三种策略各自行为正确，且都跳过不可用与冷却账号。
func TestPickAccount(t *testing.T) {
	s := &Service{
		cooldownUntil: map[string]time.Time{},
		quotaSnap:     accountpick.NewSnapshot(),
		settings: Settings{
			AccountStrategy: accountpick.First,
			Accounts: []Account{
				{ID: "u1", AccessToken: "t1", ExpiresAt: time.Now().Add(time.Hour).Unix()},
				{ID: "u2", AccessToken: "t2", ExpiresAt: time.Now().Add(time.Hour).Unix()},
			},
		},
	}

	// first：固定取列表序首个。
	acc, ok := s.pickAccount(nil)
	if !ok || acc.ID != "u1" {
		t.Fatalf("first 应取 u1，得到 %v/%v", acc.ID, ok)
	}

	// round-robin：依次轮换。
	s.settings.AccountStrategy = accountpick.RoundRobin
	s.rr = accountpick.Cursor{}
	seq := []string{}
	for i := 0; i < 4; i++ {
		a, ok := s.pickAccount(nil)
		if !ok {
			t.Fatal("round-robin 应能选中")
		}
		seq = append(seq, a.ID)
	}
	want := []string{"u1", "u2", "u1", "u2"}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("round-robin 序列错误：%v want %v", seq, want)
		}
	}

	// least-used：选剩余积分最多的。
	s.settings.AccountStrategy = accountpick.LeastUsed
	s.quotaSnap.Set("u1", 10)
	s.quotaSnap.Set("u2", 50)
	if acc, _ = s.pickAccount(nil); acc.ID != "u2" {
		t.Fatalf("least-used 应选积分更多的 u2，得到 %v", acc.ID)
	}

	// 冷却账号被跳过，且全冷却时回退到最早恢复者。
	s.settings.AccountStrategy = accountpick.First
	s.setCooldown("u1", time.Minute)
	if acc, _ = s.pickAccount(nil); acc.ID != "u2" {
		t.Fatalf("冷却账号应被跳过，得到 %v", acc.ID)
	}
	if acc, ok = s.pickAccount(map[string]bool{"u2": true}); !ok || acc.ID != "u1" {
		t.Fatalf("候选全冷却时应回退 u1，得到 %v/%v", acc.ID, ok)
	}

	// 全不可用 → false。
	s.settings = Settings{Accounts: []Account{{ID: "x", Disabled: true}}}
	if _, ok := s.pickAccount(nil); ok {
		t.Fatal("无可用账号应返回 false")
	}
}
