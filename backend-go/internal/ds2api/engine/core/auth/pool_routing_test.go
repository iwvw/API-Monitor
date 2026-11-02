package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

// newMixedPoolResolver 构建一个含三种 pool_type 账号的解析器：
//   - default 账号：任何请求都可调度
//   - no_tools 账号：仅无工具请求可调度
//   - tools_only 账号：仅含工具请求可调度
func newMixedPoolResolver(t *testing.T) *Resolver {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"only@example.com","token":"tok-only","pool_type":"tools_only"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	return NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "fresh-token", nil
	})
}

func TestDetermineRoutesToolsRequestToToolsOnlyPool(t *testing.T) {
	r := newMixedPoolResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	req = req.WithContext(WithToolsPresent(req.Context(), true))

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if a.AccountID != "only@example.com" {
		t.Fatalf("expected tools_only account for tools-present request, got %q", a.AccountID)
	}
	if !a.ToolsPresent {
		t.Fatal("expected RequestAuth.ToolsPresent to reflect tools-present request")
	}
}

func TestDetermineNoToolsRequestSkipsToolsOnlyPool(t *testing.T) {
	r := newMixedPoolResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	// 未标记 tools-present，视为无工具请求；tools_only 账号不应被调度。
	req = req.WithContext(WithToolsPresent(req.Context(), false))

	if _, err := r.Determine(req); err == nil {
		t.Fatal("expected no-account error when only a tools_only account exists for a no-tools request")
	} else if err != ErrNoAccount {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineRoutesSystemToolTextToToolsOnlyPool(t *testing.T) {
	// 请求 body 顶层无 tools 数组，但 system 消息文本声明了工具（如 RikkaHub
	// workspace 应用）。经 RequestBodyHasTools 扫描判定为含工具后，应与结构化
	// tools 请求一致地路由到 tools_only 池。
	r := newMixedPoolResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "Available tools: workspace_read_file, workspace_write_file"},
			map[string]any{"role": "user", "content": "read the file"},
		},
	}
	toolsPresent := promptcompat.RequestBodyHasTools(body)
	req = req.WithContext(WithToolsPresent(req.Context(), toolsPresent))

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if a.AccountID != "only@example.com" {
		t.Fatalf("expected tools_only account for system-declared tools request, got %q", a.AccountID)
	}
	if !a.ToolsPresent {
		t.Fatal("expected RequestAuth.ToolsPresent for system-declared tools request")
	}
}

func TestDetermineDefaultsToNoToolsWhenContextUnset(t *testing.T) {
	// 未设置 tools-present 上下文时默认按无工具请求路由。
	r := newMixedPoolResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")

	if _, err := r.Determine(req); err != ErrNoAccount {
		t.Fatalf("expected ErrNoAccount for unset tools context against tools_only pool, got %v", err)
	}
}
