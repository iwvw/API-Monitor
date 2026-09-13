package auth

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestIsHashed(t *testing.T) {
	valid, err := bcrypt.GenerateFromPassword([]byte("pw"), 4)
	if err != nil {
		t.Fatal(err)
	}
	validStr := string(valid)
	if !isHashed(validStr) {
		t.Errorf("isHashed(%q) = false, want true", validStr)
	}
	invalid := []string{
		"",
		"plaintext",
		"$2a$12$short",
		"$3a$12$" + validStr[7:],
	}
	for _, value := range invalid {
		if isHashed(value) {
			t.Errorf("isHashed(%q) = true, want false", value)
		}
	}
}

func TestVerifyPasswordBcrypt(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("correct-horse"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword("correct-horse", string(hashed)) {
		t.Fatal("verifyPassword should accept the correct bcrypt password")
	}
	if verifyPassword("wrong-password", string(hashed)) {
		t.Fatal("verifyPassword should reject a wrong bcrypt password")
	}
}

func TestVerifyPasswordPlaintextFallback(t *testing.T) {
	if !verifyPassword("abc", "abc") {
		t.Fatal("verifyPassword should accept plaintext match")
	}
	if verifyPassword("abc", "abd") {
		t.Fatal("verifyPassword should reject plaintext mismatch")
	}
	if verifyPassword("", "") {
		t.Fatal("verifyPassword should reject empty values")
	}
	if verifyPassword("abc", "") || verifyPassword("", "abc") {
		t.Fatal("verifyPassword should reject when either side is empty")
	}
}

// RFC 6238 test vectors (SHA-1, 8-digit codes truncated to 6 via %1000000).
// The shared secret is the ASCII bytes "12345678901234567890".
func TestHOTPRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := []struct {
		counter uint64
		want    string
	}{
		{1, "287082"},        // 59s
		{37037036, "081804"}, // 1111111109s
		{37037037, "050471"}, // 1111111111s
		{41152263, "005924"}, // 1234567890s
	}
	for _, tc := range cases {
		if got := hotp(key, tc.counter); got != tc.want {
			t.Errorf("hotp(key, %d) = %s, want %s", tc.counter, got, tc.want)
		}
	}
}

func TestVerifyTOTPAcceptsCurrentStepWithSkew(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()
	// token for the previous 30s step is within the +/-1 window.
	prev := totpForTest(t, secret, now.Add(-30*time.Second))
	if !verifyTOTP(secret, prev, now) {
		t.Fatal("verifyTOTP should accept a token from the previous 30s step")
	}
	// token for the current step.
	cur := totpForTest(t, secret, now)
	if !verifyTOTP(secret, cur, now) {
		t.Fatal("verifyTOTP should accept the current step token")
	}
	// A token from 2 steps in the future must be rejected.
	farFuture := totpForTest(t, secret, now.Add(2*30*time.Second))
	if verifyTOTP(secret, farFuture, now) {
		t.Fatal("verifyTOTP should reject a token outside the skew window")
	}
}

func TestVerifyTOTPRejectsBadTokens(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()
	valid := totpForTest(t, secret, now)
	if !verifyTOTP(secret, valid, now) {
		t.Fatal("verifyTOTP should accept the generated token")
	}
	if verifyTOTP(secret, "000000", now) {
		t.Fatal("verifyTOTP should reject a wrong token")
	}
	if verifyTOTP(secret, "12345", now) {
		t.Fatal("verifyTOTP should reject a 5-digit token")
	}
	if verifyTOTP(secret, "1234567", now) {
		t.Fatal("verifyTOTP should reject a 7-digit token")
	}
	if verifyTOTP("!!!invalid-secret!!!", valid, now) {
		t.Fatal("verifyTOTP should reject an invalid base32 secret")
	}
}

func TestVerifyTOTPNormalizesInput(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()
	valid := totpForTest(t, secret, now)
	if !verifyTOTP(secret, "  "+valid+"  ", now) {
		t.Fatal("verifyTOTP should trim surrounding whitespace")
	}
	// spaced groups like authenticator apps display
	spaced := valid[0:3] + " " + valid[3:6]
	if !verifyTOTP(secret, spaced, now) {
		t.Fatal("verifyTOTP should strip inner spaces")
	}
}

func TestNormalizeBase32Secret(t *testing.T) {
	if got := normalizeBase32Secret(" jbswy3dpehpk3pxp "); got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("normalizeBase32Secret = %q", got)
	}
	if got := normalizeBase32Secret("JBSWY3DPEHPK3PXP=="); got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("normalizeBase32Secret with padding = %q", got)
	}
}

func TestOtpAuthURL(t *testing.T) {
	got := otpAuthURL("JBSWY3DPEHPK3PXP", "admin")
	want := `otpauth://totp/API-Monitor:admin?secret=JBSWY3DPEHPK3PXP&issuer=API-Monitor`
	if got != want {
		t.Fatalf("otpAuthURL = %q, want %q", got, want)
	}
	// label with spaces should be percent-encoded with %20 (not +)
	escaped := otpAuthURL("JBSWY3DPEHPK3PXP", "my label")
	if !strings.Contains(escaped, `API-Monitor:my%20label`) {
		t.Fatalf("otpAuthURL did not encode label with spaces: %q", escaped)
	}
}

func TestQueryEscape(t *testing.T) {
	if got := queryEscape("a b+c"); got != "a%20b%2Bc" {
		t.Fatalf("queryEscape = %q", got)
	}
}

func TestSessionFingerprintIsStableAndShort(t *testing.T) {
	a := sessionFingerprint("session-id-123")
	b := sessionFingerprint("session-id-123")
	if a != b {
		t.Fatalf("sessionFingerprint not deterministic: %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("sessionFingerprint len = %d, want 16 hex chars", len(a))
	}
	if sessionFingerprint("session-id-123") == sessionFingerprint("session-id-124") {
		t.Fatal("sessionFingerprint should differ for different inputs")
	}
}

func TestFormatAndParseDBTimeRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	formatted := formatTime(now)
	parsed, err := parseDBTime(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(now) {
		t.Fatalf("round trip mismatch: %v vs %v", parsed, now)
	}
}

func TestParseDBTimeFormats(t *testing.T) {
	valid := []string{
		"2026-07-12T08:00:00Z",
		"2026-07-12T08:00:00.123456789Z",
		"2026-07-12 08:00:00",
		"2026-07-12T08:00:00.000Z",
		"1783760000",
	}
	for _, value := range valid {
		if _, err := parseDBTime(value); err != nil {
			t.Errorf("parseDBTime(%q) error: %v", value, err)
		}
	}
	if _, err := parseDBTime(""); err == nil {
		t.Error("parseDBTime empty should error")
	}
	if _, err := parseDBTime("not-a-time"); err == nil {
		t.Error("parseDBTime garbage should error")
	}
}

func TestIsTrustedProxy(t *testing.T) {
	service := New(config.Config{
		TrustedProxyCIDRs: []string{"203.0.113.0/24", "198.51.100.7"},
	})
	if !service.isTrustedProxy(net.ParseIP("203.0.113.9")) {
		t.Fatal("CIDR member should be trusted")
	}
	if !service.isTrustedProxy(net.ParseIP("198.51.100.7")) {
		t.Fatal("exact IP should be trusted")
	}
	if service.isTrustedProxy(net.ParseIP("203.0.114.9")) {
		t.Fatal("outside CIDR should not be trusted")
	}
	if service.isTrustedProxy(nil) {
		t.Fatal("nil IP should not be trusted")
	}
	// invalid configured entries are ignored
	service.cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}
	if service.isTrustedProxy(net.ParseIP("203.0.113.9")) {
		t.Fatal("invalid config entry should be ignored")
	}
}

func TestRequestClientIPTrustedProxyForwardedHeader(t *testing.T) {
	service := New(config.Config{
		TrustedProxyCIDRs: []string{"203.0.113.0/24"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.42, 203.0.113.1")
	got := service.requestClientIP(req)
	if got != "198.51.100.42" {
		t.Fatalf("requestClientIP = %q, want 198.51.100.42", got)
	}

	// Untrusted source IP must not honor X-Forwarded-For.
	req2 := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req2.RemoteAddr = "198.51.100.9:1234"
	req2.Header.Set("X-Forwarded-For", "192.0.2.66")
	if got := service.requestClientIP(req2); got != "198.51.100.9" {
		t.Fatalf("untrusted source should keep RemoteAddr, got %q", got)
	}
}

func TestRequestClientIPFallbacks(t *testing.T) {
	service := New(config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.RemoteAddr = "198.51.100.9"
	if got := service.requestClientIP(req); got != "198.51.100.9" {
		t.Fatalf("bare addr fallback = %q", got)
	}
	req.RemoteAddr = ""
	if got := service.requestClientIP(req); got != "unknown" {
		t.Fatalf("empty addr fallback = %q", got)
	}
}

func TestGitHubStateVerifierCookieBinding(t *testing.T) {
	flow := githubOAuthStateFlow{BaseURL: "https://panel.example.com", StateVerifier: hashStateVerifier("verifier-abc")}

	// 缺少 Cookie 时必须拒绝（跨站发起的 OAuth）。
	noCookie := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	if stateVerifierMatches(noCookie, flow.StateVerifier) {
		t.Fatal("missing cookie must not match")
	}

	// Cookie 与签发时不匹配必须拒绝。
	wrong := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	wrong.AddCookie(&http.Cookie{Name: githubOAuthStateCookie, Value: "verifier-xyz"})
	if stateVerifierMatches(wrong, flow.StateVerifier) {
		t.Fatal("mismatched cookie must not match")
	}

	// 匹配的 Cookie 才放行。
	right := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	right.AddCookie(&http.Cookie{Name: githubOAuthStateCookie, Value: "verifier-abc"})
	if !stateVerifierMatches(right, flow.StateVerifier) {
		t.Fatal("matching cookie should pass")
	}

	// 旧 flow（无 verifier 摘要）即使带 Cookie 也必须拒绝，避免向后兼容漏洞。
	legacy := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	legacy.AddCookie(&http.Cookie{Name: githubOAuthStateCookie, Value: "anything"})
	if stateVerifierMatches(legacy, "") {
		t.Fatal("flow without verifier digest must not match")
	}
}

func TestLoginAttemptLockout(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	// maxLoginAttempts = 5: the 5th wrong password triggers the lock itself.
	for i := 0; i < maxLoginAttempts-1; i++ {
		res := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"wrong"}`, nil)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("wrong login #%d status = %d body=%s", i+1, res.Code, res.Body.String())
		}
	}

	// 5th failure locks the account and returns 429 immediately.
	blocked := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"wrong"}`, nil)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("5th wrong login status = %d body=%s, want 429", blocked.Code, blocked.Body.String())
	}
	// 锁定响应不得回传 lockUntil 或剩余尝试次数，避免辅助爆破节奏控制。
	if strings.Contains(blocked.Body.String(), "lockUntil") || strings.Contains(blocked.Body.String(), "还剩") {
		t.Fatalf("blocked login body leaks lock details: %s", blocked.Body.String())
	}

	// Even the correct password is now blocked while locked.
	locked := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password while locked status = %d body=%s, want 429", locked.Code, locked.Body.String())
	}
}

func TestLoginAttemptsResetOnSuccess(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	// Two failed attempts, then a successful login must reset the counter.
	for i := 0; i < 2; i++ {
		performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"wrong"}`, nil)
	}
	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("successful login status = %d body=%s", res.Code, res.Body.String())
	}

	// Fail again: remaining attempts should be back to max-1, not decremented further.
	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"wrong"}`, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("login after reset status = %d body=%s", res.Code, res.Body.String())
	}
	// 单次失败不得回传剩余次数，避免泄露计数状态。
	if strings.Contains(res.Body.String(), "还剩") {
		t.Fatalf("wrong login body leaks remaining attempts: %s", res.Body.String())
	}
}

func TestChangePasswordFlow(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/change-password", `{"oldPassword":"wrong","newPassword":"newpass456"}`, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("change with wrong old password status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/change-password", `{"oldPassword":"secret123","newPassword":"short"}`, nil)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("change with short new password status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/change-password", `{"oldPassword":"secret123","newPassword":"newpass456"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("change password status = %d body=%s", res.Code, res.Body.String())
	}

	// Old password must no longer work, new password must.
	if res := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d", res.Code)
	}
	if res := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"newpass456"}`, nil); res.Code != http.StatusOK {
		t.Fatalf("new password login status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestSetPasswordRejectsDuplicate(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	if res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil); res.Code != http.StatusOK {
		t.Fatalf("first set-password status = %d body=%s", res.Code, res.Body.String())
	}
	if res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"other123"}`, nil); res.Code != http.StatusBadRequest {
		t.Fatalf("duplicate set-password status = %d body=%s", res.Code, res.Body.String())
	}
	if res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"short"}`, nil); res.Code != http.StatusBadRequest {
		t.Fatalf("short set-password status = %d body=%s", res.Code, res.Body.String())
	}
}

func Test2FALoginFlowWithGeneratedSecret(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("ENCRYPTION_KEY_TEST", "") // no-op, keep env clean

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	if res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil); res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}
	loginRes := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRes.Code, loginRes.Body.String())
	}
	sid := findCookie(loginRes.Result().Cookies(), "sid")
	if sid == nil {
		t.Fatal("login did not set sid cookie")
	}

	// Setup 2FA requires an active session.
	setup := performAuthRequest(service, http.MethodPost, "/api/auth/2fa/setup", "", sid)
	if setup.Code != http.StatusOK {
		t.Fatalf("2fa/setup status = %d body=%s", setup.Code, setup.Body.String())
	}
	var payload struct {
		Success bool   `json:"success"`
		Secret  string `json:"secret"`
	}
	mustDecodeAuth(t, setup, &payload)
	if !payload.Success || payload.Secret == "" {
		t.Fatalf("setup payload = %#v", payload)
	}

	// Enabling with a wrong token must fail.
	badEnable := performAuthRequest(service, http.MethodPost, "/api/auth/2fa/enable", `{"secret":"`+payload.Secret+`","token":"000000"}`, sid)
	if badEnable.Code != http.StatusBadRequest {
		t.Fatalf("2fa/enable wrong token status = %d body=%s", badEnable.Code, badEnable.Body.String())
	}

	// Correct token enables 2FA.
	validToken := totpForTest(t, payload.Secret, time.Now())
	enable := performAuthRequest(service, http.MethodPost, "/api/auth/2fa/enable", `{"secret":"`+payload.Secret+`","token":"`+validToken+`"}`, sid)
	if enable.Code != http.StatusOK {
		t.Fatalf("2fa/enable status = %d body=%s", enable.Code, enable.Body.String())
	}

	// Logout, then login without a token must require 2FA.
	performAuthRequest(service, http.MethodPost, "/api/auth/logout", "", sid)
	no2FA := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if no2FA.Code != http.StatusOK {
		t.Fatalf("login-no-2fa status = %d body=%s", no2FA.Code, no2FA.Body.String())
	}
	var no2FAPayload struct {
		Require2FA bool `json:"require2FA"`
	}
	mustDecodeAuth(t, no2FA, &no2FAPayload)
	if !no2FAPayload.Require2FA {
		t.Fatalf("expected require2FA, body=%s", no2FA.Body.String())
	}

	// Login with the correct TOTP succeeds.
	loginToken := totpForTest(t, payload.Secret, time.Now())
	with2FA := performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123","totpToken":"`+loginToken+`"}`, nil)
	if with2FA.Code != http.StatusOK {
		t.Fatalf("login-with-2fa status = %d body=%s", with2FA.Code, with2FA.Body.String())
	}
	sid2 := findCookie(with2FA.Result().Cookies(), "sid")
	if sid2 == nil {
		t.Fatal("2FA login did not set sid cookie")
	}
}

func TestSaveAdminPasswordHashesPlaintext(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.openDB(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := service.saveAdminPassword(t.Context(), db, "plaintext-password"); err != nil {
		t.Fatal(err)
	}
	saved, err := service.getConfig(t.Context(), db, "admin_password")
	if err != nil {
		t.Fatal(err)
	}
	if saved == "plaintext-password" {
		t.Fatal("admin password stored as plaintext")
	}
	if !isHashed(saved) {
		t.Fatalf("stored password not a bcrypt hash: %q", saved)
	}
	if !verifyPassword("plaintext-password", saved) {
		t.Fatal("saved hash should verify against the original password")
	}
}
