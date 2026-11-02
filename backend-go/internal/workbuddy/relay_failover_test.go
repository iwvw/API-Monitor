package workbuddy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// failoverUpstream 起一个按鉴权头分流的假上游：每个 token 返回 (状态码, 响应体)。
// 状态码非 200 时回纯文本错误；200 时按 SSE 下发。返回请求鉴权头的收集函数。
func failoverUpstream(t *testing.T, perToken func(token string) (int, string)) (func() []string, *httptest.Server) {
	t.Helper()
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		auths = append(auths, auth)
		mu.Unlock()
		code, body := perToken(auth)
		if code != http.StatusOK {
			http.Error(w, body, code)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := upstreamBaseOverride
	upstreamBaseOverride = srv.URL
	t.Cleanup(func() { upstreamBaseOverride = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), auths...)
	}, srv
}

func serveChat(t *testing.T, s *Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
		strings.NewReader(body))
	s.ServeHTTP(rec, req)
	return rec
}

const okSSE = "data: {\"id\":\"c1\",\"model\":\"hy3\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n" +
	"data: [DONE]\n\n"

// 首号上游返回 429 → 换下一个号 → 成功。下游只看到一次成功。
func TestRelayFailoverSwitchesAccountOn429(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		if token == "Bearer t2" {
			return http.StatusOK, okSSE
		}
		return http.StatusTooManyRequests, "quota exceeded"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("换号后应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("响应内容不对: %s", rec.Body.String())
	}
	want := []string{"Bearer t1", "Bearer t2"}
	if got := getAuths(); len(got) != len(want) {
		t.Fatalf("上游应收到 %v，实际 %v", want, got)
	}
	for i := range want {
		if got := getAuths(); got[i] != want[i] {
			t.Fatalf("换号顺序错误：第 %d 次 %q want %q（完整 %v）", i+1, got[i], want[i], got)
		}
	}
}

// 所有账号都返回可重试错误 → 每个账号只试一次，最终把最后一次错误抛给调用方（502）。
func TestRelayFailoverExhaustsAccounts(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusServiceUnavailable, "upstream down"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("账号耗尽应返回 502，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream_error") {
		t.Fatalf("错误体应是 upstream_error: %s", rec.Body.String())
	}
	got := getAuths()
	if len(got) != 2 || got[0] != "Bearer t1" || got[1] != "Bearer t2" {
		t.Fatalf("每个账号应恰好试一次，实际 %v", got)
	}
}

// 4xx 属于请求侧问题，换号无意义 → 不重试，直接抛错（上游只收到一次请求）。
func TestRelayNoFailoverOnNonRetryableError(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusBadRequest, "bad request"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("应返回 502，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := getAuths(); len(got) != 1 || got[0] != "Bearer t1" {
		t.Fatalf("4xx 不应换号重试，实际上游收到 %v", got)
	}
}

// 渠道抽样校验拦截（HTTP 400 + code 11128）是特例：不是 payload 语法错误，
// 换号/重发常能通过 → 必须换号重试，而不是把整个请求打穿。
func TestRelayFailoverOnChannelVerificationBlock(t *testing.T) {
	blocked := `{"code":11128,"msg":"request illegal","displayTips":{"zh":"这次调用没通过渠道校验，可能不是官方客户端发出的。"}}`
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		if token == "Bearer t1" {
			return http.StatusBadRequest, blocked
		}
		return http.StatusOK, okSSE
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("渠道校验命中应换号成功返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := getAuths(); len(got) != 2 || got[0] != "Bearer t1" || got[1] != "Bearer t2" {
		t.Fatalf("应换到第二个号，实际上游收到 %v", got)
	}
}

// 11133（模型提供方拒绝参数）是真正的 payload 错误：即使正文带通用 displayMsg，
// 也必须保持不可重试，避免对必然失败的请求反复换号。
func TestIsChannelVerificationBlockOnlyFor11128(t *testing.T) {
	blocked := `{"code":11128,"msg":"request illegal","displayMsg":{"zh":"请求被拦截，请重新发送"},"displayTips":{"zh":"这次调用没通过渠道校验，可能不是官方客户端发出的。"}}`
	if !isChannelVerificationBlock(blocked) {
		t.Error("11128 渠道校验应判为可重试")
	}
	paramErr := `{"code":11133,"msg":"the request parameters were rejected by the model provider","displayMsg":{"zh":"请求被拦截，请重新发送"},"extError":{"code":"invalid_request_error"}}`
	if isChannelVerificationBlock(paramErr) {
		t.Error("11133 参数错误不应判为渠道校验（会被误换号重试）")
	}
	// 纯文本错误页（解析不出业务码）走文案匹配。
	if !isChannelVerificationBlock("400 conversation id missing") {
		t.Error("无信封的纯文本渠道校验文案应命中")
	}
}

// 没有账号级冷却：一次失败不会被记到下一个请求。上一个请求 u1 撞 429 换号成功后，
// 下一个请求仍按选号策略从 u1 重新开始（而不是把它晾几分钟）。
func TestRelayDoesNotRememberFailureAcrossRequests(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		if token == "Bearer t2" {
			return http.StatusOK, okSSE
		}
		return http.StatusTooManyRequests, "quota exceeded"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应换号成功返回 200，得到 %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	got := getAuths()
	want := []string{"Bearer t1", "Bearer t2", "Bearer t1", "Bearer t2"}
	if len(got) != len(want) {
		t.Fatalf("两次请求各自从 u1 开始换号，期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("换号顺序错误：第 %d 次 %q want %q（完整 %v）", i+1, got[i], want[i], got)
		}
	}
}
