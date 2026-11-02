package geminicli

// 本文件是 Gemini CLI 的「上游协议层」：OAuth（gemini-cli client）、Cloud Code
// Assist 的 v1internal 协议（loadCodeAssist / onboardUser / generateContent /
// streamGenerateContent）与账号凭据管理。
//
// 协议事实来自 CLIProxyAPI 的 gemini-cli 实现（internal/auth/gemini、
// internal/runtime/executor/gemini_cli_executor.go，commit dd49a520）。

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// upstreamImplemented 标记上游协议层已实现（骨架期用于前端展示）。
const upstreamImplemented = true

// gemini-cli 的 OAuth client 凭据由部署方通过环境变量提供（与 antigravity 同思路）。
// 官方 gemini-cli 自带的 client 凭据属于公开常量，但仓库策略禁止把任何凭据写进代码，
// 因此这里不内置默认值：未配置时登录会返回明确错误，提示需要设置的环境变量。
const (
	// GeminiCLIOAuthClientIDEnv 是 gemini-cli OAuth client_id 的环境变量名。
	GeminiCLIOAuthClientIDEnv = "GEMINI_CLI_OAUTH_CLIENT_ID"
	// GeminiCLIOAuthClientSecretEnv 是 gemini-cli OAuth client_secret 的环境变量名。
	GeminiCLIOAuthClientSecretEnv = "GEMINI_CLI_OAUTH_CLIENT_SECRET"
	// DefaultCallbackPort 是本地回调监听端口。
	DefaultCallbackPort = 8085
)

// geminiCLIClientID 返回配置的 OAuth client_id；未配置时返回错误。
func geminiCLIClientID() (string, error) {
	if v := strings.TrimSpace(os.Getenv(GeminiCLIOAuthClientIDEnv)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("未配置 %s：请在部署环境中设置 gemini-cli 的 OAuth client_id", GeminiCLIOAuthClientIDEnv)
}

// geminiCLIClientSecret 返回配置的 OAuth client_secret；未配置时返回错误。
func geminiCLIClientSecret() (string, error) {
	if v := strings.TrimSpace(os.Getenv(GeminiCLIOAuthClientSecretEnv)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("未配置 %s：请在部署环境中设置 gemini-cli 的 OAuth client_secret", GeminiCLIOAuthClientSecretEnv)
}

// Scopes 是 gemini-cli 请求的 OAuth 授权范围。
var Scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

const (
	// authEndpoint 是 Google OAuth 授权端点。
	authEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
	// tokenEndpoint 是 Google OAuth token 端点。
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	// userInfoEndpoint 是 Google 用户信息端点（注意是 v1）。
	userInfoEndpoint = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"

	// codeAssistEndpoint 是 Cloud Code Assist 主机。
	codeAssistEndpoint = "https://cloudcode-pa.googleapis.com"
	// codeAssistVersion 是 Cloud Code Assist API 版本。
	codeAssistVersion = "v1internal"

	// geminiCLIVersion 是 User-Agent 里上报的版本。
	geminiCLIVersion = "0.34.0"
	// geminiCLIApiClient 是 X-Goog-Api-Client 头。
	geminiCLIApiClient = "google-genai-sdk/1.41.0 gl-node/v22.19.0"
	// defaultUserAgentModel 是无具体请求模型时的 UA 占位模型。
	defaultUserAgentModel = "gemini-2.5-pro"
	// defaultTierID 是 onboardUser 缺省使用的 tier（与 CLIProxyAPI gemini-cli 一致：
	// 个人账号走 legacy-tier 完成初始化；allowedTiers 里声明了 default 则优先用它）。
	defaultTierID = "legacy-tier"

	// fallbackProjectID 是自动获取项目全部失败时的最后兜底。
	// 部分 Google 账号（个人免费）没有可列出的 GCP 项目，但 Cloud Code Assist
	// 的 v1internal 请求需要项目归属；此值与 CLIProxyAPI 对同类账号实际使用的
	// 项目一致。用户可在登录时手动填项目 ID 覆盖它。
	fallbackProjectID = "aicode-consumers"

	// loginTTL 是一次 OAuth 登录会话的有效期。
	loginTTL = 10 * time.Minute
	// callbackPath 是本地回调路径。
	callbackPath = "/oauth2callback"
)

// -----------------------------------------------------------------------------
// 数据模型
// -----------------------------------------------------------------------------

// ModelInfo 是模型目录里的一项。
type ModelInfo struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	ContextLength     int64  `json:"contextLength,omitempty"`
	MaxOutputTokens   int64  `json:"maxOutputTokens,omitempty"`
	SupportsImages    bool   `json:"supportsImages,omitempty"`
	SupportsReasoning bool   `json:"supportsReasoning,omitempty"`
	SupportsToolCall  bool   `json:"supportsToolCall,omitempty"`
	Description       string `json:"description,omitempty"`
}

// Account 是一个 gemini-cli OAuth 登录凭据。
// 含 token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	// ID 是账号稳定标识，取 email；email 缺失时回退为昵称。
	ID        string `json:"id"`
	Email     string `json:"email,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	ProjectID string `json:"projectId,omitempty"`

	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt 是 access token 过期时刻（Unix 秒）。
	ExpiresAt int64 `json:"expiresAt,omitempty"`

	Disabled bool `json:"disabled,omitempty"`
	// CreatedAt / LastRefreshAt 为 RFC3339（UTC）。
	CreatedAt     string `json:"createdAt,omitempty"`
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	// LastError 记录最近一次上游失败原因，便于排障。
	LastError string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图：不含任何 token。
type AccountView struct {
	ID               string `json:"id"`
	Email            string `json:"email,omitempty"`
	Nickname         string `json:"nickname,omitempty"`
	ProjectID        string `json:"projectId,omitempty"`
	Disabled         bool   `json:"disabled"`
	TokenState       string `json:"tokenState"`
	ExpiresAt        int64  `json:"expiresAt,omitempty"`
	ExpiresInSeconds int64  `json:"expiresInSeconds,omitempty"`
	Available        bool   `json:"available"`
	CallCount        int64  `json:"callCount"`
	LastError        string `json:"lastError,omitempty"`
}

// loginState 是一次进行中的 OAuth 登录会话。
// 同一个 state 绑定 code_verifier、回调结果与本地监听。
type loginState struct {
	codeVerifier string
	redirectURI  string
	expires      time.Time

	// srv 是本地回调监听；登录完成/过期后经 shutdown 关闭，释放端口。
	srv          *http.Server
	shutdownOnce sync.Once

	// 回调结果（由本地 HTTP server 写入）。
	mu     sync.Mutex
	code   string
	errMsg string
	done   bool
	// consumed 标记授权码是否已被取走用于换 token。
	// 授权码只能用一次：poll 多次取用同一个码会 invalid_grant，故取走即置位。
	consumed bool
	// terminalErr 记录换 token 失败后的最终错误：会话保留（不立即丢弃），
	// 让后续 poll 反复返回同一原因，便于用户看到真实失败信息而不是「会话已过期」。
	terminalErr string

	// requestedProject 是用户登录时可选填的 GCP 项目 ID：账号无自动分配项目时用它兜底。
	requestedProject string
}

// shutdown 关闭本地回调监听，幂等（可重复调用）。
func (l *loginState) shutdown() {
	l.shutdownOnce.Do(func() {
		if l.srv != nil {
			_ = l.srv.Close()
		}
	})
}

// callbackSink 收集本地回调结果。
func (l *loginState) setResult(code, errMsg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return
	}
	l.code = code
	l.errMsg = errMsg
	l.done = true
}

func (l *loginState) result() (code, errMsg string, done bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.code, l.errMsg, l.done
}

// takeResult 原子地取走回调结果：首次调用返回 done=true 并标记已消费，
// 之后再次调用返回 done=false（避免用同一个授权码重复换取 token）。
func (l *loginState) takeResult() (code, errMsg string, done bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.done || l.consumed {
		return "", "", false
	}
	l.consumed = true
	return l.code, l.errMsg, true
}

// claim 原子地把会话标记为已消费（不依赖本地回调是否完成），
// 供「粘贴回调地址」路径防重复换取；已被消费时返回 false。
func (l *loginState) claim() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.consumed {
		return false
	}
	l.consumed = true
	return true
}

// setTerminalErr / getTerminalErr 读写换 token 的最终失败原因。
func (l *loginState) setTerminalErr(msg string) {
	l.mu.Lock()
	l.terminalErr = msg
	l.mu.Unlock()
}

func (l *loginState) getTerminalErr() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.terminalErr
}

// upstreamError 是分类后的上游错误。
type upstreamError struct {
	status     int
	retryable  bool
	msg        string
	authBroken bool
}

func (e *upstreamError) Error() string { return e.msg }

// isRetryableHTTP 判断 HTTP 状态码是否值得换号重试。
func isRetryableHTTP(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500
}

// -----------------------------------------------------------------------------
// HTTP 客户端
// -----------------------------------------------------------------------------

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 返回不带代理池的共享客户端。
// Transport 显式走 ProxyFromEnvironment：Go 不读 Windows 系统代理，但认
// HTTPS_PROXY / HTTP_PROXY / NO_PROXY 环境变量，这对本机开发（全局代理）很关键。
func sharedHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		sharedClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConnsPerHost: 5,
			},
		}
	})
	return sharedClient
}

// httpClientFor 返回本次出网要用的客户端：配置了代理池时经 ProxyPoolSelector 选出口，
// 否则用共享客户端（仍受 HTTPS_PROXY 环境变量影响）。选路失败时静默回退，避免代理池
// 故障直接打断登录/转发。
func (s *Service) httpClientFor(sessionKey string) *http.Client {
	poolID := strings.TrimSpace(s.Settings().ProxyPoolID)
	if poolID == "" || s.externalPool == nil {
		return sharedHTTPClient()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := s.externalPool.SelectProxy(ctx, poolID, sessionKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return sharedHTTPClient()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return sharedHTTPClient()
	}
	return &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(u),
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 5,
		},
	}
}

// -----------------------------------------------------------------------------
// OAuth：授权 URL / PKCE / 回调
// -----------------------------------------------------------------------------

// randomURLSafe 生成 n 字节的 URL-safe 随机串（无填充）。
func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// codeChallengeS256 计算 PKCE code_challenge（S256）。
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// callbackPort 返回本地回调端口（默认 8085）。
func callbackPort() int {
	return DefaultCallbackPort
}

// redirectURI 返回本地回调地址。
// 必须与 CLIProxyAPI 一致用 localhost：gemini-cli 这个 Google OAuth client
// 注册的回调是 localhost，授权码绑定该值，换 token 时用 127.0.0.1 会 invalid_grant。
func redirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", callbackPort(), callbackPath)
}

// startLogin 生成 state/verifier，尽力启动本地回调监听，返回授权 URL。
//
// 本地监听失败（如部署在远程服务器、或 8085 被占用）**不是致命错误**：
// 此时自动回调不可用，但用户仍可通过「粘贴回调地址」完成登录。因此这里
// 降级为仅记录告警，照常返回授权 URL。
func (s *Service) startLogin(requestedProject string) (state, authURL string, err error) {
	clientID, err := geminiCLIClientID()
	if err != nil {
		return "", "", err
	}
	state, err = randomURLSafe(24)
	if err != nil {
		return "", "", fmt.Errorf("生成 state 失败: %w", err)
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return "", "", fmt.Errorf("生成 code_verifier 失败: %w", err)
	}
	lst := &loginState{
		codeVerifier:     verifier,
		redirectURI:      redirectURI(),
		expires:          time.Now().Add(loginTTL),
		requestedProject: strings.TrimSpace(requestedProject),
	}

	s.loginMu.Lock()
	s.loginStates[state] = lst
	s.loginMu.Unlock()

	// 绑全网卡（与 CLIProxyAPI 一致）：redirect 用 localhost，浏览器可能解析到
	// 127.0.0.1 或 ::1，只绑 IPv4 会漏掉 IPv6 回调。
	if ln, listenErr := net.Listen("tcp", fmt.Sprintf(":%d", callbackPort())); listenErr != nil {
		// 自动回调不可用：常见于远程部署时浏览器到不了服务器的回环地址。
		applog.Warn(context.Background(), "geminicli", "oauth callback listener unavailable, use paste mode",
			"port", callbackPort(), "error", listenErr.Error())
	} else {
		mux := http.NewServeMux()
		mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if errParam := q.Get("error"); errParam != "" {
				lst.setResult("", errParam+": "+q.Get("error_description"))
				fmt.Fprint(w, "登录失败，可以关闭此页面。")
				lst.shutdown()
				return
			}
			if q.Get("state") != state {
				lst.setResult("", "state 不匹配")
				http.Error(w, "state 不匹配", http.StatusBadRequest)
				lst.shutdown()
				return
			}
			lst.setResult(q.Get("code"), "")
			fmt.Fprint(w, "登录成功，可以关闭此页面。")
			lst.shutdown()
		})

		srv := &http.Server{Handler: mux}
		lst.srv = srv
		go func() {
			_ = srv.Serve(ln)
		}()
		// 会话过期后关闭监听，避免端口长期被占用（正常完成时回调处理里已提前关闭）。
		time.AfterFunc(loginTTL, lst.shutdown)
	}

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", lst.redirectURI)
	params.Set("scope", strings.Join(Scopes, " "))
	params.Set("state", state)
	params.Set("code_challenge", codeChallengeS256(verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	return state, authEndpoint + "?" + params.Encode(), nil
}

// pollLogin 查询登录会话：未完成返回 done=false；完成后换取 token、
// 拉取 project_id 与 email 并组装账号。
func (s *Service) pollLogin(ctx context.Context, state string) (Account, bool, error) {
	s.loginMu.Lock()
	lst := s.loginStates[state]
	s.loginMu.Unlock()
	if lst == nil {
		return Account{}, false, fmt.Errorf("登录会话不存在或已过期")
	}
	if time.Now().After(lst.expires) {
		s.forgetLoginState(state)
		return Account{}, false, fmt.Errorf("登录会话已过期")
	}
	if errMsgTerminal := lst.getTerminalErr(); errMsgTerminal != "" {
		return Account{}, false, fmt.Errorf("%s", errMsgTerminal)
	}
	code, errMsg, done := lst.takeResult()
	if !done {
		return Account{}, false, nil
	}
	if errMsg != "" {
		lst.setTerminalErr("授权失败: " + errMsg)
		return Account{}, false, fmt.Errorf("授权失败: %s", errMsg)
	}
	acc, err := s.exchangeAndBuildAccount(ctx, code, lst.codeVerifier, lst.redirectURI, lst.requestedProject)
	if err != nil {
		// 授权码是一次性的：换失败后旧码已失效，不再重复尝试；保留会话并把
		// 真实原因记为终态，后续 poll 返回同一信息（不掩盖成「会话已过期」）。
		applog.Warn(context.Background(), "geminicli", "oauth token exchange failed",
			"redirect", lst.redirectURI, "error", err.Error())
		lst.setTerminalErr(err.Error())
		return Account{}, false, err
	}
	return acc, true, nil
}

// parseCallbackCode 从用户粘贴的内容里解析出授权码与 state。
// 支持整条回调 URL（含 query）或裸 code；URL 里的 state 优先于入参 state。
func parseCallbackCode(raw, fallbackState string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("内容为空")
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "?") || strings.Contains(raw, "code=") {
		u, perr := url.Parse(raw)
		if perr == nil {
			q := u.Query()
			if e := q.Get("error"); e != "" {
				return "", "", fmt.Errorf("授权失败: %s %s", e, q.Get("error_description"))
			}
			code = strings.TrimSpace(q.Get("code"))
			if st := strings.TrimSpace(q.Get("state")); st != "" {
				state = st
			}
		}
	}
	if code == "" {
		// 裸 code：可能是纯 code，也可能是 "code=xxx&state=yyy" 片段。
		if strings.Contains(raw, "=") {
			if vals, perr := url.ParseQuery(raw); perr == nil {
				code = strings.TrimSpace(vals.Get("code"))
				if st := strings.TrimSpace(vals.Get("state")); st != "" {
					state = st
				}
			}
		}
		if code == "" {
			code = raw
		}
	}
	if state == "" {
		state = fallbackState
	}
	if code == "" {
		return "", "", fmt.Errorf("未能从粘贴内容中解析出授权码")
	}
	return code, state, nil
}

// completeLoginByPaste 用用户粘贴的回调 URL/授权码完成登录：
// 定位对应 state 的会话，取其 code_verifier 换取 token 并组装账号。
func (s *Service) completeLoginByPaste(ctx context.Context, state, raw string) (Account, error) {
	state = strings.TrimSpace(state)
	code, st, err := parseCallbackCode(raw, state)
	if err != nil {
		return Account{}, err
	}
	if st == "" {
		return Account{}, fmt.Errorf("缺少 state：请粘贴完整的回调地址")
	}

	s.loginMu.Lock()
	lst := s.loginStates[st]
	s.loginMu.Unlock()
	if lst == nil {
		return Account{}, fmt.Errorf("登录会话不存在或已过期，请重新发起登录")
	}
	if time.Now().After(lst.expires) {
		s.forgetLoginState(st)
		return Account{}, fmt.Errorf("登录会话已过期，请重新发起登录")
	}
	if state != "" && state != st {
		return Account{}, fmt.Errorf("state 不匹配，请重新发起登录")
	}
	if terminal := lst.getTerminalErr(); terminal != "" {
		return Account{}, fmt.Errorf("%s", terminal)
	}

	// 与 poll 路径共用一次性消费：标记会话已用，避免同一授权码被换两次。
	if !lst.claim() {
		return Account{}, fmt.Errorf("该登录会话的授权码已被使用，请重新发起登录")
	}
	acc, err := s.exchangeAndBuildAccount(ctx, code, lst.codeVerifier, lst.redirectURI, lst.requestedProject)
	if err != nil {
		// 保留会话（不立即丢弃）并把真实原因记为终态，便于用户看到具体失败原因。
		applog.Warn(context.Background(), "geminicli", "oauth token exchange failed (paste)",
			"redirect", lst.redirectURI, "error", err.Error())
		lst.setTerminalErr(err.Error())
		return Account{}, err
	}
	s.forgetLoginState(st)
	return acc, nil
}

// exchangeAndBuildAccount 用授权码换 token，随后拉取 project_id 与 email。
func (s *Service) exchangeAndBuildAccount(ctx context.Context, code, verifier, redirect, requestedProject string) (Account, error) {
	clientID, err := geminiCLIClientID()
	if err != nil {
		return Account{}, err
	}
	clientSecret, err := geminiCLIClientSecret()
	if err != nil {
		return Account{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", redirect)
	form.Set("code_verifier", verifier)
	client := s.httpClientFor("")
	tok, err := postTokenForm(ctx, client, form)
	if err != nil {
		return Account{}, err
	}
	acc := Account{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix(),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	projectID, err := fetchProjectID(ctx, client, acc.AccessToken, requestedProject)
	if err != nil {
		return Account{}, fmt.Errorf("获取 project_id 失败: %w", err)
	}
	acc.ProjectID = projectID
	email, err := fetchUserInfo(ctx, client, acc.AccessToken)
	if err == nil {
		acc.Email = email
	}
	acc.ID = strings.TrimSpace(firstNonEmpty(acc.Email, projectID))
	acc.Nickname = acc.Email
	return acc, nil
}

// tokenResponse 是 Google token 端点的响应。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// postTokenForm 以 x-www-form-urlencoded 提交 token 请求。
func postTokenForm(ctx context.Context, client *http.Client, form url.Values) (tokenResponse, error) {
	var out tokenResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return out, fmt.Errorf("请求 token 端点失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("token 端点返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if out.AccessToken == "" {
		return out, fmt.Errorf("token 响应缺少 access_token")
	}
	return out, nil
}

// refreshAccessToken 用 refresh_token 换取新的 access token 并回写账号。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("账号缺少 refresh_token")
	}
	clientID, err := geminiCLIClientID()
	if err != nil {
		return err
	}
	clientSecret, err := geminiCLIClientSecret()
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", acc.RefreshToken)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	tok, err := postTokenForm(ctx, s.httpClientFor(acc.ID), form)
	if err != nil {
		return err
	}
	acc.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	return nil
}

// fetchUserInfo 拉取 Google 用户信息并返回 email。
func fetchUserInfo(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("userinfo 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", err
	}
	return strings.TrimSpace(info.Email), nil
}

// -----------------------------------------------------------------------------
// Cloud Code Assist：project_id
// -----------------------------------------------------------------------------

// codeAssistHeaders 设置 v1internal 请求的公共头（原样对齐 CLIProxyAPI）。
func codeAssistHeaders(req *http.Request, accessToken, model string) {
	if strings.TrimSpace(model) == "" {
		model = defaultUserAgentModel
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("GeminiCLI/%s/%s (%s; %s; terminal)", geminiCLIVersion, model, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-Goog-Api-Client", geminiCLIApiClient)
}

// codeAssistMetadata 是 loadCodeAssist / onboardUser 共用的 metadata。
// 字段与 CLIProxyAPI 的 gemini-cli 实现对齐，缺字段会影响上游判定与 provisioning。
func codeAssistMetadata() map[string]string {
	return map[string]string{
		"ideType":    "IDE_UNSPECIFIED",
		"platform":   "PLATFORM_UNSPECIFIED",
		"pluginType": "GEMINI",
	}
}

// projectSelectionRequiredError 表示账号无法自动分配 Cloud Code 项目，
// 需要用户提供 GCP 项目 ID（对齐 CLIProxyAPI 的 projectSelectionRequiredError）。
type projectSelectionRequiredError struct {
	tierID string
}

func (e *projectSelectionRequiredError) Error() string {
	return "该 Google 账号未自动分配 Cloud Code 项目，请在登录时填写一个 Google Cloud 项目 ID" +
		"（tier=" + e.tierID + "）"
}

// fetchProjectID 选择账号的 Cloud Code 项目并把项目注册/启用好。
//
// 选择顺序（混合 CatieCli 与 CLIProxyAPI）：
//  1. 用户显式填写的项目；
//  2. Cloud Resource Manager 列出的用户项目（优先含 default）——CatieCli 主路径；
//  3. loadCodeAssist / onboardUser 自动发现 —— CLIProxyAPI 路径；
//  4. 兜底项目（登录时可手动覆盖）。
//
// 选定后统一做 provisioning：先 onboardUser 注册项目（CLIProxyAPI 做法，缺这一步
// 上游会对该项目的模型/配额请求回 403 PERMISSION_DENIED），再启用相关服务（CatieCli 做法）。
func fetchProjectID(ctx context.Context, client *http.Client, accessToken, requestedProject string) (string, error) {
	requestedProject = strings.TrimSpace(requestedProject)
	metadata := codeAssistMetadata()
	tierID := defaultTierID
	var loadResp map[string]interface{}

	// 取一次 loadCodeAssist：拿 allowedTiers（决定 onboarding 的 tier）。
	if err := callCodeAssist(ctx, client, accessToken, "loadCodeAssist",
		map[string]interface{}{"metadata": metadata}, &loadResp); err == nil {
		// 该客户端（Gemini CLI）的个人免费层可能已被上游停用：此时 ineligibleTiers
		// 会给出 UNSUPPORTED_CLIENT，任何后续调用都会 403。直接透传原因，避免用户
		// 在项目/代理上反复折腾。
		if reason := unsupportedClientReason(loadResp); reason != "" {
			return "", fmt.Errorf("Gemini CLI 客户端已不受上游支持：%s", reason)
		}
		if tiers, ok := loadResp["allowedTiers"].([]interface{}); ok {
			for _, rawTier := range tiers {
				tier, ok := rawTier.(map[string]interface{})
				if !ok {
					continue
				}
				if isDefault, ok := tier["isDefault"].(bool); ok && isDefault {
					if id, ok := tier["id"].(string); ok && strings.TrimSpace(id) != "" {
						tierID = strings.TrimSpace(id)
						break
					}
				}
			}
		}
	}

	projectID := requestedProject
	if projectID == "" {
		if pid := extractProjectID(loadResp["cloudaicompanionProject"]); pid != "" {
			projectID = pid
		}
	}
	if projectID == "" {
		if pid := fetchGCPProjectFallback(ctx, client, accessToken); pid != "" {
			applog.Warn(context.Background(), "geminicli", "using GCP project from resource manager", "project", pid)
			projectID = pid
		}
	}
	if projectID == "" {
		if discovered, err := onboardAutoDiscover(ctx, client, accessToken, tierID, metadata); err == nil && discovered != "" {
			projectID = discovered
		}
	}
	if projectID == "" {
		applog.Warn(context.Background(), "geminicli", "using fallback project", "project", fallbackProjectID)
		projectID = fallbackProjectID
	}

	// 统一 provisioning：注册项目 + 启用服务。注册失败（尤其是账号无 Code Assist
	// 许可的 403 SUBSCRIPTION_REQUIRED）要把真实原因透出，否则登录会以含糊错误失败。
	if finalID, err := onboardWithProject(ctx, client, accessToken, tierID, metadata, projectID); err != nil {
		if isLicenseError(err) {
			return "", err
		}
	} else if strings.TrimSpace(finalID) != "" {
		projectID = finalID
	}
	enableGCPProjectServices(ctx, client, accessToken, projectID)
	return projectID, nil
}

// isLicenseError 判断 onboarding 错误是否属于「账号无 Code Assist 许可」。
// 这类错误必须透传给用户，因为它不是配置问题，换项目/换 token 都无法绕过。
func isLicenseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SUBSCRIPTION_REQUIRED") ||
		strings.Contains(msg, "valid license") ||
		strings.Contains(msg, "PERMISSION_DENIED")
}

// unsupportedClientReason 从 loadCodeAssist 响应里提取「客户端已不受支持」的原因。
// 命中时返回提示文案（含上游建议），否则返回空串。
func unsupportedClientReason(loadResp map[string]interface{}) string {
	tiers, ok := loadResp["ineligibleTiers"].([]interface{})
	if !ok {
		return ""
	}
	for _, raw := range tiers {
		tier, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		code, _ := tier["reasonCode"].(string)
		msg, _ := tier["reasonMessage"].(string)
		if strings.EqualFold(code, "UNSUPPORTED_CLIENT") || strings.Contains(msg, "no longer supported") {
			if strings.TrimSpace(msg) != "" {
				return msg
			}
			return "该客户端已不再受支持，请迁移到 Antigravity（https://antigravity.google）"
		}
	}
	return ""
}

// codeAssistServiceIDs 是 Cloud Code Assist 依赖的 GCP 服务。
var codeAssistServiceIDs = []string{
	"cloudaicompanion.googleapis.com",
	"geminicloudassist.googleapis.com",
}

// enableGCPProjectServices 幂等地启用项目上的 Cloud Code Assist 相关服务。
// 失败不致命：服务可能已启用，或账号无权限（交由后续请求报错）。
func enableGCPProjectServices(ctx context.Context, client *http.Client, accessToken, projectID string) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return
	}
	for _, svc := range codeAssistServiceIDs {
		url := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/%s:enable", projectID, svc)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			applog.Warn(context.Background(), "geminicli", "enable GCP service failed",
				"project", projectID, "service", svc,
				"status", resp.StatusCode, "body", truncate(strings.TrimSpace(string(body)), 200))
		}
	}
}

// extractProjectID 从 project 字段提取 project_id（string 或 {id}）。
func extractProjectID(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]interface{}:
		for _, key := range []string{"id", "projectId", "project"} {
			if id, ok := t[key].(string); ok && strings.TrimSpace(id) != "" {
				return strings.TrimSpace(id)
			}
		}
	}
	return ""
}

// extractOnboardProjectID 从 onboardUser 的响应里尽力取出 project_id：
// 依次尝试 response.cloudaicompanionProject、顶层 cloudaicompanionProject，
// 以及 response 里的 projectId/project，兼容不同上游返回形态。
func extractOnboardProjectID(data map[string]interface{}) string {
	if respData, ok := data["response"].(map[string]interface{}); ok {
		for _, key := range []string{"cloudaicompanionProject", "projectId", "project"} {
			if pid := extractProjectID(respData[key]); pid != "" {
				return pid
			}
		}
	}
	for _, key := range []string{"cloudaicompanionProject", "projectId", "project"} {
		if pid := extractProjectID(data[key]); pid != "" {
			return pid
		}
	}
	return ""
}

// callCodeAssist 调一次 Cloud Code Assist 方法（loadCodeAssist / onboardUser）。
func callCodeAssist(ctx context.Context, client *http.Client, accessToken, method string, body map[string]interface{}, out interface{}) error {
	raw, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/%s:%s", codeAssistEndpoint, codeAssistVersion, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	codeAssistHeaders(req, accessToken, defaultUserAgentModel)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s 返回 %d: %s", method, resp.StatusCode, truncate(strings.TrimSpace(string(respBody)), 500))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w", method, err)
	}
	return nil
}

// onboardAutoDiscover 不带项目调 onboardUser 轮询至 done，返回上游自动分配的项目 ID
// （拿不到返回空串，不视为错误）。整体上限约 30 秒，与 CLIProxyAPI 一致。
func onboardAutoDiscover(ctx context.Context, client *http.Client, accessToken, tierID string, metadata map[string]string) (string, error) {
	body := map[string]interface{}{"tierId": tierID, "metadata": metadata}
	deadline := time.Now().Add(30 * time.Second)
	for {
		var resp map[string]interface{}
		if err := callCodeAssist(ctx, client, accessToken, "onboardUser", body, &resp); err != nil {
			return "", fmt.Errorf("自动发现 onboardUser: %w", err)
		}
		if done, ok := resp["done"].(bool); ok && done {
			return extractOnboardProjectID(resp), nil
		}
		if time.Now().After(deadline) {
			return "", nil
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// onboardWithProject 带 cloudaicompanionProject 调 onboardUser 轮询至 done。
// 返回上游确认的项目 ID；response 未给时返回空串（调用方用传入项目兜底）。
func onboardWithProject(ctx context.Context, client *http.Client, accessToken, tierID string, metadata map[string]string, projectID string) (string, error) {
	body := map[string]interface{}{
		"tierId":                  tierID,
		"metadata":                metadata,
		"cloudaicompanionProject": projectID,
	}
	const maxAttempts = 20
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var resp map[string]interface{}
		if err := callCodeAssist(ctx, client, accessToken, "onboardUser", body, &resp); err != nil {
			return "", fmt.Errorf("onboardUser: %w", err)
		}
		if done, ok := resp["done"].(bool); ok && done {
			return extractOnboardProjectID(resp), nil
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("onboardUser 轮询超时")
}

// fetchGCPProjectFallback 用 Cloud Resource Manager 列出用户可访问的 GCP 项目，
// 优先取 projectId 含 "default" 的，否则取第一个。取不到返回空串。
// （对比 CatieCli/gcli2api：Cloud Code Assist 需要项目归属，而部分账号的
// loadCodeAssist/onboardUser 不会返回项目，此时回退到用户自己的项目。）
func fetchGCPProjectFallback(ctx context.Context, client *http.Client, accessToken string) string {
	const listURL = "https://cloudresourcemanager.googleapis.com/v1/projects"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		applog.Warn(context.Background(), "geminicli", "list GCP projects failed",
			"status", resp.StatusCode, "body", truncate(strings.TrimSpace(string(body)), 300))
		return ""
	}
	var parsed struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
			Name      string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Projects) == 0 {
		return ""
	}
	first := ""
	for _, p := range parsed.Projects {
		id := strings.TrimSpace(p.ProjectID)
		if id == "" {
			continue
		}
		if first == "" {
			first = id
		}
		if strings.Contains(strings.ToLower(id), "default") {
			return id
		}
	}
	return first
}

// -----------------------------------------------------------------------------
// Cloud Code Assist：对话
// -----------------------------------------------------------------------------

// wrapGenerateRequest 把标准 Gemini GenerateContentRequest 包成 v1internal 请求体：
// {"project": ..., "model": ..., "request": {...}}。
func wrapGenerateRequest(projectID, model string, inner map[string]interface{}) map[string]interface{} {
	req := map[string]interface{}{}
	for k, v := range inner {
		req[k] = v
	}
	return map[string]interface{}{
		"project": projectID,
		"model":   model,
		"request": req,
	}
}

// unwrapGenerateResponse 从 v1internal 响应体里取出内层 response。
// 非包装形态（直接就是 Gemini 响应）时原样返回。
func unwrapGenerateResponse(body []byte) []byte {
	var probe struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && len(probe.Response) > 0 {
		return probe.Response
	}
	return body
}

// generateURL 返回非流式/流式 generateContent 的地址。
func generateURL(stream bool) string {
	if stream {
		return fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse", codeAssistEndpoint, codeAssistVersion)
	}
	return fmt.Sprintf("%s/%s:generateContent", codeAssistEndpoint, codeAssistVersion)
}

// doGenerate 向 v1internal 发起一次 generateContent 调用。
// 返回响应体与 HTTP 状态码；网络错误直接返回 error。
func (s *Service) doGenerate(ctx context.Context, acc Account, model string, inner map[string]interface{}, stream bool) (*http.Response, error) {
	payload, err := json.Marshal(wrapGenerateRequest(acc.ProjectID, model, inner))
	if err != nil {
		return nil, fmt.Errorf("请求体序列化失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, generateURL(stream), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	codeAssistHeaders(req, acc.AccessToken, model)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return s.httpClientFor(acc.ID).Do(req)
}

// classifyUpstreamResponse 读入响应体并做错误分类。
func classifyUpstreamResponse(resp *http.Response) ([]byte, *upstreamError) {
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return body, &upstreamError{
		status:     resp.StatusCode,
		retryable:  isRetryableHTTP(resp.StatusCode),
		authBroken: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		msg:        fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, msg),
	}
}

// truncate 截断字符串到 n 字节以内（仅用于日志，避免超长原文刷屏）。
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}
