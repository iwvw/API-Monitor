package antigravity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// withMockUpstream 把 engine 的 BaseURLs 指到本地 mock，测试结束恢复。
func withMockUpstream(t *testing.T, url string) {
	t.Helper()
	origBaseURLs, origBaseURL := engineag.BaseURLs, engineag.BaseURL
	engineag.BaseURLs = []string{url}
	engineag.BaseURL = url
	t.Cleanup(func() {
		engineag.BaseURLs = origBaseURLs
		engineag.BaseURL = origBaseURL
	})
}

func newUnionTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

// futureUnix 返回 1 小时后的 Unix 秒（token 未过期）。
func futureUnix() int64 { return time.Now().Add(time.Hour).Unix() }

// TestFetchModelNamesUnionAcrossAccounts 钉住「模型目录按账号取并集」这条口径。
//
// 背景（真实故障）：不同 Antigravity 账号的模型权限不同 —— 有的账号有
// gemini-4-argon，有的没有。旧实现 `FetchModels(ctx, "")` 只取 pickAccount()
// 选中的**那一个**账号，于是别的账号独有的模型永远进不了端点列表，表现是
// 「某个模型明明能用，端点列表里却看不到」。
func TestFetchModelNamesUnionAcrossAccounts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// 按 Authorization 区分账号：a 只给 argon，b 只给 claude；两者共享 gemini-3-flash。
		auth := r.Header.Get("Authorization")
		var models string
		switch {
		case strings.Contains(auth, "token-a"):
			models = `{"models":{"gemini-4-argon-high":{"displayName":"Gemini 4 Argon"},"gemini-3-flash":{"displayName":"Gemini 3 Flash"}}}`
		case strings.Contains(auth, "token-b"):
			models = `{"models":{"claude-sonnet-4-6":{"displayName":"Claude Sonnet 4.6"},"gemini-3-flash":{"displayName":"Gemini 3 Flash"}}}`
		default:
			models = `{"models":{}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(models))
	}))
	defer srv.Close()
	withMockUpstream(t, srv.URL)

	s := newUnionTestService(t)
	st := s.Settings()
	st.Accounts = []Account{
		{Email: "a@example.com", AccessToken: "token-a", ExpiresAt: futureUnix(), ProjectID: "p1"},
		{Email: "b@example.com", AccessToken: "token-b", ExpiresAt: futureUnix(), ProjectID: "p2"},
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	names := s.fetchModelNamesUnion(context.Background())
	sort.Strings(names)

	want := []string{"claude-sonnet-4-6", "gemini-3-flash", "gemini-4-argon-high"}
	if len(names) != len(want) {
		t.Fatalf("并集 = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("并集 = %v, want %v", names, want)
		}
	}
	// 两个账号都必须被访问过（不能只取一个）。
	if calls.Load() < 2 {
		t.Errorf("应至少访问 2 次上游（每账号一次），实际 %d 次", calls.Load())
	}
}

// TestFetchModelNamesUnionSkipsFailingAccount 单个账号失败不影响其它账号的模型。
func TestFetchModelNamesUnionSkipsFailingAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "bad") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-high":{"displayName":"Gemini 4 Argon"}}}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv.URL)

	s := newUnionTestService(t)
	st := s.Settings()
	st.Accounts = []Account{
		{Email: "bad@example.com", AccessToken: "bad", ExpiresAt: futureUnix(), ProjectID: "p1"},
		{Email: "good@example.com", AccessToken: "good", ExpiresAt: futureUnix(), ProjectID: "p2"},
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	names := s.fetchModelNamesUnion(context.Background())
	if len(names) != 1 || names[0] != "gemini-4-argon-high" {
		t.Fatalf("失败账号不应影响其它账号的模型, got %v", names)
	}
}

// TestFetchModelsUnionSinglePass 钉住「元数据与 id 同一次遍历取回」这条约束。
//
// 背景：曾经写成「先 fetchModelNamesUnion 拿 id，再遍历一遍账号补 displayName」，
// 上游请求数直接翻倍（2N）。Antigravity 的上游对目录请求有频率限制，
// 翻倍会明显提高被限流的概率。
func TestFetchModelsUnionSinglePass(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-high":{"displayName":"Gemini 4 Argon"}}}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv.URL)

	s := newUnionTestService(t)
	st := s.Settings()
	st.Accounts = []Account{
		{Email: "a@example.com", AccessToken: "a", ExpiresAt: futureUnix(), ProjectID: "p1"},
		{Email: "b@example.com", AccessToken: "b", ExpiresAt: futureUnix(), ProjectID: "p2"},
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	models := s.fetchModelsUnion(context.Background())
	if len(models) != 1 {
		t.Fatalf("并集 = %v", models)
	}
	// 元数据必须在同一次遍历里拿到（否则 displayName 会空）。
	if got := models["gemini-4-argon-high"].DisplayName; got != "Gemini 4 Argon" {
		t.Errorf("displayName = %q, 应在同一次遍历里取到", got)
	}
	// 2 个账号 → 恰好 2 次上游请求，不能是 4 次。
	if n := calls.Load(); n != 2 {
		t.Errorf("上游请求数 = %d, want 2（每账号一次，不能为补元数据再遍历一遍）", n)
	}
}

// TestFetchModelsUnionSkipsAccountWithoutProject 验证缺 ProjectID 的账号被提前跳过
// （FetchModels 必须带 ProjectID，否则必然失败并刷 warn 噪音）。
func TestFetchModelsUnionSkipsAccountWithoutProject(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-high":{"displayName":"Gemini 4 Argon"}}}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv.URL)

	s := newUnionTestService(t)
	st := s.Settings()
	st.Accounts = []Account{
		// 有 token 但没 ProjectID：必须被跳过，不该发请求。
		{Email: "noproj@example.com", AccessToken: "x", ExpiresAt: futureUnix()},
		{Email: "ok@example.com", AccessToken: "y", ExpiresAt: futureUnix(), ProjectID: "p2"},
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	if models := s.fetchModelsUnion(context.Background()); len(models) != 1 {
		t.Fatalf("并集 = %v", models)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("上游请求数 = %d, want 1（缺 ProjectID 的账号应被跳过）", n)
	}
}

func TestFetchModelNamesUnionAllFailReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withMockUpstream(t, srv.URL)

	s := newUnionTestService(t)
	st := s.Settings()
	st.Accounts = []Account{
		{Email: "a@example.com", AccessToken: "a", ExpiresAt: futureUnix(), ProjectID: "p1"},
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	if names := s.fetchModelNamesUnion(context.Background()); len(names) != 0 {
		t.Fatalf("全部失败时应返回空切片, got %v", names)
	}
}
