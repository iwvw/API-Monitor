package qoder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestAccountRefreshEndpoint 验证手动续期接口：成功时回写新 token 与签发时刻。
func TestAccountRefreshEndpoint(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deviceToken/refresh" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"token":"newtok","refresh_token":"newref","expires_in":3600}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	acc := Account{ID: "u1", AccessToken: "old", RefreshToken: "r"}
	if err := s.upsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, s, http.MethodPost, "/api/qoder/accounts/u1/refresh", "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d", calls.Load())
	}
	got, _ := s.findAccount("u1")
	if got.AccessToken != "newtok" || got.RefreshToken != "newref" {
		t.Fatalf("account = %+v", got)
	}
	if got.TokenIssuedAt == 0 || got.ExpiresAt == 0 {
		t.Fatalf("token timestamps not recorded: %+v", got)
	}
}

// TestAccountRefreshWithoutRefreshToken 缺 refresh_token 时必须明确报错，
// 而不是发一次注定失败的请求。
func TestAccountRefreshWithoutRefreshToken(t *testing.T) {
	s := newTestService(t)
	if err := s.upsertAccount(context.Background(), Account{ID: "u1", AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s, http.MethodPost, "/api/qoder/accounts/u1/refresh", "{}")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "refresh_token") {
		t.Fatalf("error should mention refresh_token: %s", rec.Body.String())
	}
}

// TestRefreshAllEndpoint 验证全量续期会逐个处理并把失败原因分开返回。
func TestRefreshAllEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := url.ParseQuery("")
		_ = body
		_, _ = w.Write([]byte(`{"token":"t","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	ctx := context.Background()
	_ = s.upsertAccount(ctx, Account{ID: "ok", AccessToken: "a", RefreshToken: "r"})
	_ = s.upsertAccount(ctx, Account{ID: "no-rt", AccessToken: "b"})

	rec := doJSON(t, s, http.MethodPost, "/api/qoder/refresh", "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"accountId":"ok"`) || !strings.Contains(body, `"success":false`) {
		t.Fatalf("results should mix success and failure: %s", body)
	}
}

// TestUnknownAccountSubrouteIs404 守住「路由不存在不能报成方法不对」：
// 旧版后端缺少 /refresh 时，前端拿到 405 会把「后端没这个接口」误读成「HTTP 方法用错了」，
// 让人无从下手。未匹配的子动作必须明确 404 并带上具体路径。
func TestUnknownAccountSubrouteIs404(t *testing.T) {
	s, _ := newTestServiceWithAccount(t)
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/qoder/accounts/u1/no-such-action"},
		{http.MethodPatch, "/api/qoder/accounts/u1"},
	}
	for _, c := range cases {
		rec := doJSON(t, s, c.method, c.path, "{}")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s => %d, want 404 (body=%s)", c.method, c.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "账号子接口不存在") {
			t.Fatalf("%s %s should explain the missing sub-route: %s", c.method, c.path, rec.Body.String())
		}
	}

	// DELETE 仍必须走删除，不能被上面的兜底吃掉。
	rec := doJSON(t, s, http.MethodDelete, "/api/qoder/accounts/u1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE should still delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := s.findAccount("u1"); ok {
		t.Fatalf("account should be gone")
	}
}

// TestRefreshStaleAccountsNoExpiresAtDoesNotLoop 守住：缺少 ExpiresAt 的旧账号
// 不能每次扫描都续期一次。早期实现用 `tokenState != valid` 判定，unknown 也算
// 需要续期，于是每 5 分钟对同一账号发一次换 token 请求（浪费配额、refresh_token
// 轮换异常）。现在只有「进入 3 天续期窗口」或「已过期」才续期。
func TestRefreshStaleAccountsNoExpiresAtDoesNotLoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"token":"t","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	ctx := context.Background()
	// 无 ExpiresAt 且没有签发时刻：首次允许尝试一次。
	_ = s.upsertAccount(ctx, Account{ID: "u1", AccessToken: "a", RefreshToken: "r"})
	s.RefreshStaleAccounts(ctx)
	if calls.Load() != 1 {
		t.Fatalf("first scan should attempt refresh once, calls = %d", calls.Load())
	}

	// 续期成功后写回了 ExpsesAt/TokenIssuedAt，后续扫描不应再打上游。
	acc, _ := s.findAccount("u1")
	if acc.ExpiresAt == 0 || acc.TokenIssuedAt == 0 {
		t.Fatalf("refresh should record timestamps: %+v", acc)
	}
	for i := 0; i < 3; i++ {
		s.RefreshStaleAccounts(ctx)
	}
	if calls.Load() != 1 {
		t.Fatalf("fresh token must not be refreshed repeatedly, calls = %d", calls.Load())
	}
}

// TestRefreshStaleAccountsRespectsSwitch 守住「自动续期开关缺省开、显式关则不动」。
func TestRefreshStaleAccountsRespectsSwitch(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"token":"t","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	ctx := context.Background()
	_ = s.upsertAccount(ctx, Account{ID: "u1", AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(5 * 24 * time.Hour).Unix()})

	// 剩余寿命 > 续期窗口（3 天）→ 不续期。
	s.RefreshStaleAccounts(ctx)
	if calls.Load() != 0 {
		t.Fatalf("fresh token should be skipped, calls = %d", calls.Load())
	}

	// 剩余寿命落入窗口 → 续期。
	acc, _ := s.findAccount("u1")
	acc.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	_ = s.upsertAccount(ctx, acc)
	s.RefreshStaleAccounts(ctx)
	if calls.Load() != 1 {
		t.Fatalf("token within refresh window should be renewed, calls = %d", calls.Load())
	}

	// 开关显式关闭 → 不再续期。
	off := false
	st := s.Settings()
	st.AutoRefreshToken = &off
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	acc, _ = s.findAccount("u1")
	acc.ExpiresAt = time.Now().Add(time.Hour).Unix()
	_ = s.upsertAccount(ctx, acc)
	s.RefreshStaleAccounts(ctx)
	if calls.Load() != 1 {
		t.Fatalf("switch off must stop refresh, calls = %d", calls.Load())
	}
}
