package posthogcode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseLoginErrorVerificationRequired(t *testing.T) {
	err := parseLoginError([]byte(`{"type":"authentication_error","code":"code_based_verification_required","detail":"user@example.com","attr":null}`))
	le, ok := err.(*loginError)
	if !ok {
		t.Fatalf("应解析为 loginError，得到 %#v", err)
	}
	if le.code != "code_based_verification_required" {
		t.Fatalf("code = %q", le.code)
	}
	if le.detail != "user@example.com" {
		t.Fatalf("detail = %q（应携带邮箱）", le.detail)
	}
}

func TestParseLoginErrorInvalidCredentials(t *testing.T) {
	err := parseLoginError([]byte(`{"type":"validation_error","code":"invalid_credentials","detail":"Invalid email or password.","attr":null}`))
	le, ok := err.(*loginError)
	if !ok {
		t.Fatalf("应解析为 loginError，得到 %#v", err)
	}
	if le.code != "invalid_credentials" {
		t.Fatalf("code = %q", le.code)
	}
}

func TestExtractCodeFromRedirect(t *testing.T) {
	code, err := extractCodeFromRedirect("http://localhost/callback?code=AUTHCODE123&state=xyz")
	if err != nil {
		t.Fatal(err)
	}
	if code != "AUTHCODE123" {
		t.Fatalf("code = %q", code)
	}
}

func TestExtractCodeFromRedirectError(t *testing.T) {
	_, err := extractCodeFromRedirect("http://localhost/callback?error=access_denied&error_description=denied")
	if err == nil {
		t.Fatal("应报错")
	}
	if got := err.Error(); got != "授权被拒绝: access_denied denied" {
		t.Fatalf("错误 = %q", got)
	}
}

func TestExtractCodeFromRedirectMissingCode(t *testing.T) {
	if _, err := extractCodeFromRedirect("http://localhost/callback?state=only"); err == nil {
		t.Fatal("缺少 code 应报错")
	}
}

func TestAutoLoginSessionLifecycle(t *testing.T) {
	s := &Service{}
	id := "sess-1"
	st := &autoLoginState{id: id, email: "a@x", region: "us", createdAt: time.Now()}
	s.saveAutoLogin(st)

	if got := s.takeAutoLogin(id); got != st {
		t.Fatal("takeAutoLogin 应取回同一会话")
	}
	if got := s.takeAutoLogin(id); got == nil {
		t.Fatal("未 drop 前应仍可取回")
	}
	s.dropAutoLogin(id)
	if got := s.takeAutoLogin(id); got != nil {
		t.Fatal("drop 后应不存在")
	}
}

func TestAutoLoginSessionExpired(t *testing.T) {
	s := &Service{}
	st := &autoLoginState{id: "old", email: "a@x", region: "us", createdAt: time.Now().Add(-autoLoginTTL - time.Minute)}
	s.saveAutoLogin(st)
	if got := s.takeAutoLogin("old"); got != nil {
		t.Fatal("过期会话应被拒绝")
	}
}

// TestAccountPasswordPersistsEncrypted 密码随账号落库，且 accountView 只暴露
// hasPassword 标记、绝不下发明文。
func TestAccountPasswordPersistsEncrypted(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	a := Account{ID: "a1", Email: "a1@x", Region: "us", Password: "secret123", Scope: "llm_gateway:read"}
	if err := s.upsertAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	stored := s.Settings()
	if len(stored.Accounts) != 1 || stored.Accounts[0].Password != "secret123" {
		t.Fatalf("密码应随账号落库，实际 %+v", stored.Accounts)
	}

	view := s.toAccountView(stored.Accounts[0])
	if !view.HasPassword {
		t.Fatal("hasPassword 应为 true")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "secret123") {
		t.Fatal("accountView 不得泄露明文密码")
	}
}

// TestBuildAccountFromTokenKeepsPassword 自动登录换码后账号保留输入的密码。
func TestAccountViewHasPasswordFalseWhenEmpty(t *testing.T) {
	s := newDBService(t)
	a := Account{ID: "a2", Email: "a2@x", Region: "us", Scope: "llm_gateway:read"}
	if v := s.toAccountView(a); v.HasPassword {
		t.Fatal("无密码账号 hasPassword 应为 false")
	}
}
