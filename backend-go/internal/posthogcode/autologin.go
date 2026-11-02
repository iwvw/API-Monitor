package posthogcode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// autoLoginTTL 是待完成自动登录会话的有效期。
const autoLoginTTL = 15 * time.Minute

// 自动登录会话状态。
const (
	autoLoginAwaitingCode = "awaiting_code"
	autoLoginAwaitingTOTP = "awaiting_totp"
	autoLoginLoggedIn     = "logged_in"
	autoLoginFailed       = "failed"
)

// autoLoginState 是账号密码自动登录的中间状态。
// 每个会话持有独立的 cookie jar，隔离不同账号的登录环境——
// PostHog 按 cookie 会话区分登录者，互不共享，天然满足隔离。
type autoLoginState struct {
	id         string
	region     string
	email      string
	password   string
	accountID  string
	jar        *cookiejar.Jar
	client     *http.Client
	verifier   string
	oauthToken *tokenResponse
	createdAt  time.Time
	// status 记录会话当前状态：等待验证码 / 等待 TOTP / 已完成 / 已失败。
	status string
	// result 与 lastError 保存完成后的账号视图或失败原因，供状态查询读取。
	result    *AccountView
	lastError string
}

// autologinMu 串行化自动登录会话的读写。
var autologinMu sync.Mutex

// autoLoginID 生成新的自动登录会话 id。
func autoLoginID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// posthogLoginStep1 用账号密码发起 PostHog 登录。
// 返回登录是否完成，需要验证码/2FA 时返回对应标识与提示。
// 成功时 session cookie 已写入 jar，可继续走 OAuth authorize。
func (s *Service) posthogLoginStep1(ctx context.Context, st *autoLoginState) error {
	base := cloudBaseURL(st.region)

	// 1. GET 登录页拿 CSRF token（Django 要求）。
	csrf, err := fetchCSRF(ctx, st.client, base)
	if err != nil {
		return err
	}

	// 2. 提交邮箱+密码（DRF 接受 JSON body，与实测一致）。
	payload, _ := json.Marshal(map[string]string{
		"email":    st.email,
		"password": st.password,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/login", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", base+"/login")
	req.Header.Set("X-CSRFToken", csrf)

	resp, err := st.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		return nil // 登录成功，session 已落 cookie
	case http.StatusUnauthorized:
		return parseLoginError(raw)
	case http.StatusBadRequest, http.StatusForbidden:
		return parseLoginError(raw)
	default:
		return fmt.Errorf("登录请求返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

// posthogLoginVerify 提交邮箱验证码，完成登录。
func (s *Service) posthogLoginVerify(ctx context.Context, st *autoLoginState, code string) error {
	base := cloudBaseURL(st.region)
	csrf, err := fetchCSRF(ctx, st.client, base)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"code":  code,
		"email": st.email,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/login/code-based-verification", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", base+"/login")
	req.Header.Set("X-CSRFToken", csrf)

	resp, err := st.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return parseLoginError(raw)
	default:
		return fmt.Errorf("验证码请求返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

// posthogOAuthAuthorize 在已登录 session 下访问 authorize，拿到授权码 code。
// PostHog 对 first-party 应用在登录态下 GET /oauth/authorize 直接 302 带 code，
// 无需同意页；对其它客户端可退化为 POST allow=true（见 autoAuthorizeWithConsent）。
// 该方法会取回 buildAuthorizeURL 存入的 PKCE verifier 并写到 st 上，供后续换码。
func (s *Service) posthogOAuthAuthorize(ctx context.Context, st *autoLoginState, redirectURI string) (string, error) {
	// accountID 留空：以登录后 userinfo 解析出的邮箱/用户 id 作为账号标识，
	// 与已有同账号时自然覆盖。若想替换指定账号，由上层在 state 中锚定。
	authURL, state, err := s.buildAuthorizeURL(st.region, redirectURI, "")
	if err != nil {
		return "", err
	}
	sess := s.takeOAuthState(state)
	if sess != nil {
		st.verifier = sess.verifier
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	// 不要跟随重定向：授权成功会 302 到 redirect_uri 带 code，需拦截 Location。
	client := *st.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		return extractCodeFromRedirect(loc)
	}
	// 非 302：可能是同意页（非 first-party）。读一次 body 以失败提示。
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return "", fmt.Errorf("授权未自动完成（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}

// extractCodeFromRedirect 从授权重定向地址解析 code（兼可忽略 state）。
func extractCodeFromRedirect(loc string) (string, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("授权重定向地址无法解析: %w", err)
	}
	code := strings.TrimSpace(u.Query().Get("code"))
	if code == "" {
		// 可能带 error，回显便于排障。
		if e := u.Query().Get("error"); e != "" {
			return "", fmt.Errorf("授权被拒绝: %s %s", e, u.Query().Get("error_description"))
		}
		return "", errors.New("授权重定向缺少 code")
	}
	return code, nil
}

// fetchCSRF 拉取登录页 cookie 中的 csrftoken。
func fetchCSRF(ctx context.Context, client *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/login", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "posthog_csrftoken" {
			return c.Value, nil
		}
	}
	for _, c := range client.Jar.Cookies(mustParseURL(base)) {
		if c.Name == "posthog_csrftoken" {
			return c.Value, nil
		}
	}
	return "", errors.New("登录页未返回 CSRF cookie")
}

// parseLoginError 解析登录失败响应（结构形如 {type,code,detail,attr}）。
func parseLoginError(raw []byte) error {
	var e struct {
		Type   string `json:"type"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Attr   string `json:"attr"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Code == "" {
		return fmt.Errorf("登录失败: %s", strings.TrimSpace(string(raw)))
	}
	detail := e.Detail
	if detail == "" {
		detail = e.Code
	}
	return &loginError{code: e.Code, detail: detail, attr: e.Attr}
}

// mustParseURL 解析 URL（失败时返回空对象，仅用于 cookie jar 查询兜底）。
func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

// loginError 是结构化登录失败。
type loginError struct {
	code   string
	detail string
	attr   string
}

func (e *loginError) Error() string { return e.detail }
