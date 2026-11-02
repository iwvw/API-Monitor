package lobsterai

// 本文件是 LobsterAI 的「上游协议层」：OAuth 登录（本地回环回调 + 粘贴回调）、
// token 换发与刷新、对话转发（强制流式 SSE）、动态模型目录、额度明细与每日签到。
//
// 协议事实来自开源反代 lobsterai2api（xinxinshuhao-create/lobsterai2api）与其
// 配套部署文章，字段名与路径按上游实测记录照抄。

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

const (
	// 默认上游基址与登录门户（可用环境变量覆盖）。
	defaultUpstreamBase  = "https://lobsterai-server.youdao.com"
	defaultLoginPortal   = "https://lobsterai.youdao.com"
	envUpstreamBase      = "LOBSTERAI_UPSTREAM_BASE"
	envLoginPortal       = "LOBSTERAI_LOGIN_PORTAL"
	defaultClientVersion = "0.1.0"

	// updateAPI 是官方更新接口：用于取 clientVersion（签到/请求 UA 必带）。
	updateAPI = "https://api-overmind.youdao.com/openapi/get/luna/hardware/lobsterai/prod/update"

	// callbackPath / callbackHost 是 OAuth 本地回调的路径与主机。
	callbackPath = "/auth/callback"
	callbackHost = "127.0.0.1"

	// loginTTL 是一次 OAuth 登录会话的有效期。
	loginTTL = 10 * time.Minute

	// tokenExpiringWindow 是判定 access token「即将过期」的阈值。
	tokenExpiringWindow = 30 * time.Minute
)

// serverBase 返回上游 API 基址（去尾部斜杠）。
func serverBase() string {
	if v := strings.TrimSpace(os.Getenv(envUpstreamBase)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultUpstreamBase
}

// loginPortalURL 返回登录门户基址（去尾部斜杠）。
func loginPortalURL() string {
	if v := strings.TrimSpace(os.Getenv(envLoginPortal)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultLoginPortal
}

// -----------------------------------------------------------------------------
// 数据模型
// -----------------------------------------------------------------------------

// ModelInfo 是模型目录里的一项。
type ModelInfo struct {
	ID       string  `json:"id"`
	Name     string  `json:"name,omitempty"`
	Provider string  `json:"provider,omitempty"`
	Cost     float64 `json:"costMultiplier,omitempty"`
}

// Account 是一个 LobsterAI OAuth 登录凭据。
// 含 token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	// ID 是账号稳定标识，取上游 uid。
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	UserID   string `json:"userId,omitempty"`
	// UUID 是安装标识（exchange/refresh 载荷用）。
	UUID string `json:"uuid,omitempty"`
	// FirstKeyfrom / LatestKeyfrom 是首次登录与最近活动时间戳（毫秒字符串）。
	FirstKeyfrom  string `json:"firstKeyfrom,omitempty"`
	LatestKeyfrom string `json:"latestKeyfrom,omitempty"`

	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt 是 access token 过期时刻（Unix 秒）。
	ExpiresAt int64 `json:"expiresAt,omitempty"`

	// Credits 是最近一次查询到的剩余积分（仅缓存展示，权威值以额度接口为准）。
	Credits float64 `json:"credits,omitempty"`

	Disabled bool `json:"disabled,omitempty"`
	// CreatedAt / LastRefreshAt / LastCheckinAt 为 RFC3339（UTC）。
	CreatedAt     string `json:"createdAt,omitempty"`
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	LastCheckinAt string `json:"lastCheckinAt,omitempty"`
	// LastError 记录最近一次上游失败原因，便于排障。
	LastError string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图：不含任何 token。
type AccountView struct {
	ID               string  `json:"id"`
	Nickname         string  `json:"nickname,omitempty"`
	UserID           string  `json:"userId,omitempty"`
	Credits          float64 `json:"credits,omitempty"`
	Disabled         bool    `json:"disabled"`
	TokenState       string  `json:"tokenState"`
	ExpiresAt        int64   `json:"expiresAt,omitempty"`
	ExpiresInSeconds int64   `json:"expiresInSeconds,omitempty"`
	Available        bool    `json:"available"`
	Cooling          bool    `json:"cooling"`
	CallCount        int64   `json:"callCount"`
	LastCheckinAt    string  `json:"lastCheckinAt,omitempty"`
	LastError        string  `json:"lastError,omitempty"`
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
// clientVersion
// -----------------------------------------------------------------------------

// clientVersionTTL 是上游客户端版本号的缓存时长。
const clientVersionTTL = time.Hour

// resolveClientVersion 取上游客户端版本号：优先环境变量 LOBSTERAI_CLIENT_VERSION，
// 其次官方更新接口（缓存 1 小时），最后回退默认值。签到与 UA 都依赖它。
func (s *Service) resolveClientVersion(ctx context.Context) string {
	if v := strings.TrimSpace(os.Getenv("LOBSTERAI_CLIENT_VERSION")); v != "" {
		return v
	}
	s.versionMu.Lock()
	if s.clientVersion != "" && time.Since(s.versionFetched) < clientVersionTTL {
		v := s.clientVersion
		s.versionMu.Unlock()
		return v
	}
	s.versionMu.Unlock()

	v := fetchClientVersion(ctx, s.httpClientFor(""))
	if v == "" {
		s.versionMu.Lock()
		if s.clientVersion != "" {
			v = s.clientVersion
		} else {
			v = defaultClientVersion
			s.clientVersion = v
			s.versionFetched = time.Now()
		}
		s.versionMu.Unlock()
		return v
	}
	s.versionMu.Lock()
	s.clientVersion, s.versionFetched = v, time.Now()
	s.versionMu.Unlock()
	return v
}

// fetchClientVersion 调官方更新接口取 version；失败返回空串。
func fetchClientVersion(ctx context.Context, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateAPI, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var parsed struct {
		Data struct {
			Value struct {
				Version string `json:"version"`
			} `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Data.Value.Version)
}

// -----------------------------------------------------------------------------
// 上游请求公共件
// -----------------------------------------------------------------------------

// apiEnvelope 是上游统一信封：{code, msg/ message, data}。
type apiEnvelope struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// envMsg 返回信封里的错误说明（兼容 msg / message 两种字段名）。
func (e apiEnvelope) envMsg() string {
	return firstNonEmpty(e.Msg, e.Message)
}

// chatHeaders 设置 chat completions 请求头。
func chatHeaders(req *http.Request, a Account, version string) {
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", "LobsterAI/"+version)
	req.Header.Set("X-LobsterAI-Client-Capabilities", "kimi-k3-agentic-v1")
	req.Header.Set("X-LobsterAI-Client-Version", version)
}

// authHeaders 设置 auth 请求头（exchange/refresh 不需要 Bearer token）。
func authHeaders(req *http.Request, version string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LobsterAI/"+version)
}

// keyfromBody 返回 exchange/refresh 请求体的 keyfrom 载荷。
func (a Account) keyfromBody() map[string]any {
	body := map[string]any{
		"firstKeyfrom":  a.FirstKeyfrom,
		"latestKeyfrom": a.LatestKeyfrom,
		"version":       defaultClientVersion,
	}
	if a.UUID != "" {
		body["uuid"] = a.UUID
	}
	if a.UserID != "" {
		body["userId"] = a.UserID
	}
	return body
}

// doJSON 发请求并解信封；HTTP 非 2xx 或业务 code != 0 时返回带 body 片段的 *upstreamError。
func doEnvelope(ctx context.Context, client *http.Client, req *http.Request) (json.RawMessage, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, &upstreamError{
			status:     resp.StatusCode,
			retryable:  isRetryableHTTP(resp.StatusCode),
			authBroken: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
			msg:        fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300)),
		}
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("解析上游响应失败: %w (body: %s)", err, truncate(strings.TrimSpace(string(raw)), 160))
	}
	if env.Code != 0 {
		return nil, classifyEnvelopeError(resp.StatusCode, env)
	}
	return env.Data, nil
}

// classifyEnvelopeError 把业务错误信封转成 *upstreamError。
// 业务码非 0 通常属请求侧问题（不可重试）；余额/限流类文案单独归为可重试并冷却。
func classifyEnvelopeError(status int, env apiEnvelope) error {
	msg := env.envMsg()
	kind := classifyMessage(msg)
	retryable := kind == "credit" || kind == "rate"
	authBroken := kind == "session"
	if status == 0 {
		status = http.StatusOK
	}
	return &upstreamError{
		status:     status,
		retryable:  retryable,
		authBroken: authBroken,
		msg:        fmt.Sprintf("code=%d %s", env.Code, truncate(msg, 200)),
	}
}

// hardCreditMarkers 是余额不足关键词（小写比较 + 中文原文双通道）。
var hardCreditMarkers = []string{
	"insufficient credit", "no credit", "credit exhausted", "out of credit",
	"quota exceeded", "quota exhaust", "payment required", "credit not enough",
	"not enough credit", "积分不足", "额度不足", "余额不足", "积分用完", "额度用尽", "没有积分", "积分耗尽",
}

// sessionDeadMarkers 是登录态终止的错误码/文案。
var sessionDeadMarkers = []string{"40100", "40101", "token rejected", "refresh token was rejected"}

// classifyMessage 按文案判定错误类别：credit / rate / session / other。
func classifyMessage(body string) string {
	lower := strings.ToLower(body)
	for _, m := range hardCreditMarkers {
		if strings.Contains(lower, strings.ToLower(m)) || strings.Contains(body, m) {
			return "credit"
		}
	}
	for _, m := range sessionDeadMarkers {
		if strings.Contains(body, m) {
			return "session"
		}
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(body, "频率限制") || strings.Contains(body, "限流") {
		return "rate"
	}
	return "other"
}

// -----------------------------------------------------------------------------
// 登录：授权 URL / 回调 / 换发 token
// -----------------------------------------------------------------------------

// loginState 是一次进行中的 OAuth 登录会话。
type loginState struct {
	state        string
	uuid         string
	firstKeyfrom string
	expires      time.Time

	// srv 是本地回调监听；登录完成/过期后经 shutdown 关闭，释放端口。
	srv          *http.Server
	shutdownOnce sync.Once

	mu     sync.Mutex
	code   string
	errMsg string
	done   bool
	// consumed 标记授权码是否已被取走用于换 token。
	consumed bool
	// terminalErr 记录换 token 失败后的最终错误：会话保留（不立即丢弃），
	// 让后续 poll 反复返回同一原因，便于用户看到真实失败信息。
	terminalErr string
}

// shutdown 关闭本地回调监听，幂等（可重复调用）。
func (l *loginState) shutdown() {
	l.shutdownOnce.Do(func() {
		if l.srv != nil {
			_ = l.srv.Close()
		}
	})
}

func (l *loginState) setResult(code, errMsg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return
	}
	l.code, l.errMsg, l.done = code, errMsg, true
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

// randomHex 生成 n 字节随机 hex。
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newUUID 生成随机 UUID4。
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// nowMillis 当前 Unix 毫秒时间戳字符串（keyfrom 格式）。
func nowMillis() string {
	return fmt.Sprintf("%d", time.Now().UnixMilli())
}

// jwtExpiry 解码 JWT payload 的 exp（Unix 秒）；失败返回 0。
func jwtExpiry(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return 0
	}
	return claims.Exp
}

// startLogin 生成 state/uuid，尽力启动本地回调监听，返回授权 URL。
//
// 本地监听失败（如部署在远程服务器）**不是致命错误**：此时自动回调不可用，
// 但用户仍可通过「粘贴回调地址」完成登录。因此这里降级为仅记录告警。
func (s *Service) startLogin() (state, authURL string, err error) {
	state, err = randomHex(16)
	if err != nil {
		return "", "", fmt.Errorf("生成 state 失败: %w", err)
	}
	uuid, err := newUUID()
	if err != nil {
		return "", "", fmt.Errorf("生成 uuid 失败: %w", err)
	}
	lst := &loginState{
		state:        state,
		uuid:         uuid,
		firstKeyfrom: nowMillis(),
		expires:      time.Now().Add(loginTTL),
	}

	s.loginMu.Lock()
	s.loginStates[state] = lst
	s.loginMu.Unlock()

	// 回调地址必须是 http://127.0.0.1:{随机端口}/auth/callback（登录页校验）。
	// 登录页对端口不做固定要求（官方客户端每次都是随机端口）。
	redirectURI, served := s.startCallbackListener(lst, state)
	if !served {
		// 本地监听不可用（常见于远程部署）：自动回调走不通，用户需把浏览器
		// 跳转后的地址栏整条链接粘回管理面，走「粘贴回调地址」路径完成登录。
		applog.Warn(context.Background(), "lobsterai", "oauth callback listener unavailable, use paste mode")
	}
	portal := loginPortalURL()
	authURL = fmt.Sprintf("%s/portal#/login?source=electron&redirect_uri=%s&state=%s",
		portal, url.QueryEscape(redirectURI), url.QueryEscape(state))
	return state, authURL, nil
}

// startCallbackListener 尝试绑定本地回环随机端口并挂回调处理，返回回调地址。
// 监听失败时返回一个不带端口的占位地址与 served=false，调用方据此提示改用粘贴模式。
func (s *Service) startCallbackListener(lst *loginState, state string) (redirectURI string, served bool) {
	ln, err := net.Listen("tcp", callbackHost+":0")
	if err != nil {
		return fmt.Sprintf("http://%s%s", callbackHost, callbackPath), false
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("state"); got != state {
			lst.setResult("", "state 不匹配")
			http.Error(w, "state 不匹配", http.StatusBadRequest)
			lst.shutdown()
			return
		}
		lst.setResult(q.Get("code"), "")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body><h2>登录成功，可以关闭此窗口了</h2></body></html>")
		lst.shutdown()
	})
	srv := &http.Server{Handler: mux}
	lst.srv = srv
	go func() { _ = srv.Serve(ln) }()
	time.AfterFunc(loginTTL, lst.shutdown)
	return fmt.Sprintf("http://%s:%d%s", callbackHost, port, callbackPath), true
}

// pollLogin 查询登录会话：未完成返回 done=false；完成后换取 token 并组装账号。
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
	if terminal := lst.getTerminalErr(); terminal != "" {
		return Account{}, false, fmt.Errorf("%s", terminal)
	}
	code, errMsg, done := lst.takeResult()
	if !done {
		return Account{}, false, nil
	}
	if errMsg != "" {
		lst.setTerminalErr("授权失败: " + errMsg)
		return Account{}, false, fmt.Errorf("授权失败: %s", errMsg)
	}
	acc, err := s.exchangeAccount(ctx, lst, code)
	if err != nil {
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
		if u, perr := url.Parse(raw); perr == nil {
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

// completeLoginByPaste 用用户粘贴的回调 URL/授权码完成登录。
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
	if !lst.claim() {
		return Account{}, fmt.Errorf("该登录会话的授权码已被使用，请重新发起登录")
	}
	acc, err := s.exchangeAccount(ctx, lst, code)
	if err != nil {
		lst.setTerminalErr(err.Error())
		return Account{}, err
	}
	s.forgetLoginState(st)
	return acc, nil
}

// exchangeAccount 用授权码调 /api/auth/exchange 换 token 并组装账号。
func (s *Service) exchangeAccount(ctx context.Context, lst *loginState, code string) (Account, error) {
	version := s.resolveClientVersion(ctx)
	body := map[string]any{
		"authCode":      code,
		"firstKeyfrom":  lst.firstKeyfrom,
		"latestKeyfrom": nowMillis(),
		"uuid":          lst.uuid,
		"version":       defaultClientVersion,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverBase()+"/api/auth/exchange", bytes.NewReader(raw))
	if err != nil {
		return Account{}, err
	}
	authHeaders(req, version)
	data, err := doEnvelope(ctx, s.httpClientFor(""), req)
	if err != nil {
		return Account{}, err
	}
	var ex struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		User         struct {
			ID       string `json:"id"`
			Yid      string `json:"yid"`
			UserId   string `json:"userId"`
			Nickname string `json:"nickname"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &ex); err != nil {
		return Account{}, fmt.Errorf("解析 exchange 响应失败: %w", err)
	}
	if strings.TrimSpace(ex.AccessToken) == "" {
		return Account{}, fmt.Errorf("exchange 响应缺少 accessToken")
	}
	uid := firstNonEmpty(ex.User.ID, ex.User.UserId, ex.User.Yid)
	if uid == "" {
		sum := sha256.Sum256([]byte(ex.AccessToken))
		uid = hex.EncodeToString(sum[:])[:16]
	}
	acc := Account{
		ID:            uid,
		Nickname:      ex.User.Nickname,
		UserID:        ex.User.UserId,
		UUID:          lst.uuid,
		FirstKeyfrom:  lst.firstKeyfrom,
		LatestKeyfrom: nowMillis(),
		AccessToken:   ex.AccessToken,
		RefreshToken:  ex.RefreshToken,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	acc.ExpiresAt = resolveExpires(ex.ExpiresIn, ex.AccessToken)
	return acc, nil
}

// resolveExpires 依据 expiresIn（秒）或 JWT exp 计算过期时刻（Unix 秒）；都拿不到返回 0。
func resolveExpires(expiresIn int64, token string) int64 {
	if expiresIn > 0 {
		return time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
	}
	return jwtExpiry(token)
}

// refreshAccessToken 用 refresh_token 换取新的 access token 并回写账号。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("账号缺少 refresh_token")
	}
	version := s.resolveClientVersion(ctx)
	body := acc.keyfromBody()
	body["refreshToken"] = acc.RefreshToken
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverBase()+"/api/auth/refresh", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	authHeaders(req, version)
	data, err := doEnvelope(ctx, s.httpClientFor(acc.ID), req)
	if err != nil {
		return err
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(data, &tok); err != nil || tok.AccessToken == "" {
		return fmt.Errorf("refresh 失败：响应无 accessToken（可能需要重新登录）")
	}
	acc.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	if exp := resolveExpires(tok.ExpiresIn, tok.AccessToken); exp > 0 {
		acc.ExpiresAt = exp
	}
	acc.LatestKeyfrom = nowMillis()
	return nil
}

// ensureToken 确保账号 access token 可用，过期则刷新。
func (s *Service) ensureToken(ctx context.Context, acc *Account) error {
	if acc.AccessToken != "" && time.Until(time.Unix(acc.ExpiresAt, 0)) > tokenExpiringWindow {
		return nil
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		if acc.AccessToken == "" {
			return fmt.Errorf("账号缺少可用凭据")
		}
		return nil
	}
	if err := s.refreshAccessToken(ctx, acc); err != nil {
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------
// 对话：强制流式上游 + SSE 聚合
// -----------------------------------------------------------------------------

// prepareChatBody 预处理请求体：强制 stream=true（上游只支持流式，实测
// stream:false 返回 500），并标准化 tool_choice。
func prepareChatBody(rawBody []byte) []byte {
	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return rawBody
	}
	body["stream"] = true
	if tc, ok := body["tool_choice"]; ok {
		switch v := tc.(type) {
		case string:
			if v == "" || v == "none" {
				delete(body, "tool_choice")
			}
		case nil:
			delete(body, "tool_choice")
		}
	}
	out, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return out
}

// ChatStream 发 chat 请求并返回原始 SSE body 流（调用方负责 Close）。
// 非 2xx 时 rc 为 nil、status 为上游状态码、err 为 nil（body 在 lastBody，
// 调用方用 classifyMessage 判定）；只有传输层失败才返回 err。
func (s *Service) ChatStream(ctx context.Context, acc Account, body []byte) (rc io.ReadCloser, status int, lastBody []byte, err error) {
	version := s.resolveClientVersion(ctx)
	url := serverBase() + "/api/proxy/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepareChatBody(body)))
	if err != nil {
		return nil, 0, nil, err
	}
	chatHeaders(req, acc, version)
	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		applog.Warn(ctx, "lobsterai", "chat stream transport error", "uid", acc.ID, "error", err.Error())
		return nil, 0, nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, resp.StatusCode, raw, nil
	}
	return resp.Body, resp.StatusCode, nil, nil
}

// aggregateSSE 读取完整 SSE 流，聚合 delta.content 为单个 OpenAI chat.completion 响应。
// tool_calls 按 index 合并（首片带 id/type/name，后续分片只带 arguments 片段）。
func aggregateSSE(r io.Reader) (map[string]any, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var (
		id, model     string
		created       float64
		content       strings.Builder
		reasoning     strings.Builder
		role          = "assistant"
		finishReason  = "stop"
		usage         map[string]any
		gotAnyContent bool
		toolCalls     = map[int]map[string]any{}
		toolOrder     []int
	)
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload != "" && payload != "[DONE]" {
				var chunk map[string]any
				if json.Unmarshal([]byte(payload), &chunk) == nil {
					if v, ok := chunk["id"].(string); ok && id == "" {
						id = v
					}
					if v, ok := chunk["model"].(string); ok && model == "" {
						model = v
					}
					if v, ok := chunk["created"].(float64); ok && created == 0 {
						created = v
					}
					if u, ok := chunk["usage"].(map[string]any); ok {
						usage = u
					}
					if ch, ok := chunk["choices"].([]any); ok {
						for _, ci := range ch {
							c, _ := ci.(map[string]any)
							if c == nil {
								continue
							}
							if fr, ok := c["finish_reason"].(string); ok && fr != "" {
								finishReason = fr
							}
							if delta, ok := c["delta"].(map[string]any); ok {
								if r2, ok := delta["role"].(string); ok && r2 != "" {
									role = r2
								}
								if txt, ok := delta["content"].(string); ok {
									content.WriteString(txt)
									gotAnyContent = true
								}
								if rc, ok := delta["reasoning_content"].(string); ok {
									reasoning.WriteString(rc)
								}
								if tcs, ok := delta["tool_calls"].([]any); ok {
									for _, tc := range tcs {
										call, ok := tc.(map[string]any)
										if !ok {
											continue
										}
										idx := 0
										if v, ok := call["index"].(float64); ok {
											idx = int(v)
										}
										merged, seen := toolCalls[idx]
										if !seen {
											merged = map[string]any{"index": idx}
											toolCalls[idx] = merged
											toolOrder = append(toolOrder, idx)
										}
										mergeToolCallDelta(merged, call)
									}
								}
							}
							if msg, ok := c["message"].(map[string]any); ok && !gotAnyContent {
								if txt, ok := msg["content"].(string); ok {
									content.WriteString(txt)
								}
							}
						}
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = float64(time.Now().Unix())
	}
	message := map[string]any{"role": role, "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolOrder) > 0 {
		sortInts(toolOrder)
		calls := make([]map[string]any, 0, len(toolOrder))
		for _, idx := range toolOrder {
			calls = append(calls, toolCalls[idx])
		}
		message["tool_calls"] = calls
	}
	resp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": int64(created),
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}},
	}
	if usage != nil {
		resp["usage"] = usage
	}
	return resp, nil
}

// mergeToolCallDelta 把流式 tool_call 片段合并到累计对象：
// id/type/function.name 直覆盖，function.arguments 拼接。
func mergeToolCallDelta(merged, delta map[string]any) {
	if v, ok := delta["id"].(string); ok && v != "" {
		merged["id"] = v
	}
	if v, ok := delta["type"].(string); ok && v != "" {
		merged["type"] = v
	}
	df, _ := delta["function"].(map[string]any)
	if df == nil {
		return
	}
	mf, _ := merged["function"].(map[string]any)
	if mf == nil {
		mf = map[string]any{}
		merged["function"] = mf
	}
	if v, ok := df["name"].(string); ok && v != "" {
		mf["name"] = v
	}
	if v, ok := df["arguments"].(string); ok && v != "" {
		if prev, _ := mf["arguments"].(string); prev != "" {
			mf["arguments"] = prev + v
		} else {
			mf["arguments"] = v
		}
	}
}

// sortInts 升序排序。
func sortInts(a []int) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// streamAndCapture 透传上游 SSE 到 w（每行 flush），保证至少写一个 [DONE]，
// 并对每个 data 分片回调 onChunk（用于提取 usage）。
func streamAndCapture(r io.Reader, w http.ResponseWriter, flusher http.Flusher, onChunk func(map[string]any)) error {
	if flusher == nil {
		flusher, _ = w.(http.Flusher)
	}
	br := bufio.NewReaderSize(r, 64*1024)
	sawDone := false
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if payload == "[DONE]" {
					sawDone = true
				} else if payload != "" && onChunk != nil {
					var chunk map[string]any
					if json.Unmarshal([]byte(payload), &chunk) == nil {
						onChunk(chunk)
					}
				}
			}
			if _, werr := io.WriteString(w, line); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	if !sawDone {
		if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// 模型目录
// -----------------------------------------------------------------------------

// FetchModels 调上游动态模型接口 GET /api/models/available（Bearer accessToken）。
func (s *Service) FetchModels(ctx context.Context, acc Account) ([]ModelInfo, error) {
	version := s.resolveClientVersion(ctx)
	u := serverBase() + "/api/models/available"
	parts := make([]string, 0, 4)
	for k, v := range acc.keyfromBody() {
		parts = append(parts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(fmt.Sprintf("%v", v))))
	}
	if len(parts) > 0 {
		u += "?" + strings.Join(parts, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LobsterAI/"+version)
	data, err := doEnvelope(ctx, s.httpClientFor(acc.ID), req)
	if err != nil {
		return nil, err
	}
	var items []struct {
		ModelID        string  `json:"modelId"`
		ModelName      string  `json:"modelName"`
		Provider       string  `json:"provider"`
		CostMultiplier float64 `json:"costMultiplier"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析模型目录失败: %w", err)
	}
	out := make([]ModelInfo, 0, len(items))
	for _, m := range items {
		if id := strings.TrimSpace(m.ModelID); id != "" {
			out = append(out, ModelInfo{ID: id, Name: m.ModelName, Provider: m.Provider, Cost: m.CostMultiplier})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("模型目录为空")
	}
	return out, nil
}

// fetchModelsFromAnyAccount 从池中任一可用账号拉模型目录。
func (s *Service) fetchModelsFromAnyAccount(ctx context.Context) ([]ModelInfo, error) {
	st := s.Settings()
	var lastErr error
	for _, a := range st.Accounts {
		if !accountAvailable(a) {
			continue
		}
		acc := a
		if err := s.ensureToken(ctx, &acc); err != nil {
			lastErr = err
			continue
		}
		models, err := s.FetchModels(ctx, acc)
		if err != nil {
			lastErr = err
			continue
		}
		return models, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用账号")
	}
	return nil, lastErr
}

// -----------------------------------------------------------------------------
// 额度与签到
// -----------------------------------------------------------------------------

// CreditItem 是一批积分的明细与到期时间。
// ExpiresAt 已归一化为带时区的 RFC3339（UTC），前端不再需要猜测时区。
type CreditItem struct {
	Type             string  `json:"type"`
	Label            string  `json:"label"`
	CreditsRemaining float64 `json:"creditsRemaining"`
	ExpiresAt        string  `json:"expiresAt"`
}

// upstreamTimeZone 是 LobsterAI 上游返回的裸时间所用时区。
// 上游的 expiresAt 形如 "2026-09-29T19:34:18"，不带时区后缀，实际是北京时间；
// 直接下发会让前端按浏览器本地时区解析，非东八区用户会看到偏移的时刻。
var upstreamTimeZone = time.FixedZone("CST", 8*3600)

// normalizeUpstreamTime 把上游不带时区的时间串归一化为 RFC3339（UTC）。
// 无法解析时原样返回，交由前端做兜底展示。
func normalizeUpstreamTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 已带时区信息（Z 或 ±HH:MM）则按原样解析，避免重复偏移。
	layout := "2006-01-02T15:04:05"
	if strings.HasSuffix(raw, "Z") || strings.ContainsAny(raw[len(raw)-6:], "+-") {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	if t, err := time.ParseInLocation(layout, raw, upstreamTimeZone); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return raw
}

// FetchCredits 查询账号剩余积分与批次明细（GET /api/user/profile-summary）。
// 注意：/api/user/quota 只显示 freeCreditsTotal，不含活动积分，故用 profile-summary。
func (s *Service) FetchCredits(ctx context.Context, acc Account) (remain float64, items []CreditItem, err error) {
	version := s.resolveClientVersion(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverBase()+"/api/user/profile-summary", nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LobsterAI/"+version)
	data, err := doEnvelope(ctx, s.httpClientFor(acc.ID), req)
	if err != nil {
		return 0, nil, err
	}
	var ps struct {
		TotalCreditsRemaining float64 `json:"totalCreditsRemaining"`
		Nickname              string  `json:"nickname"`
		CreditItems           []struct {
			Type             string  `json:"type"`
			Label            string  `json:"label"`
			CreditsRemaining float64 `json:"creditsRemaining"`
			ExpiresAt        string  `json:"expiresAt"`
		} `json:"creditItems"`
	}
	if err := json.Unmarshal(data, &ps); err != nil {
		return 0, nil, fmt.Errorf("解析额度响应失败: %w", err)
	}
	for _, it := range ps.CreditItems {
		items = append(items, CreditItem{
			Type: it.Type, Label: it.Label,
			CreditsRemaining: it.CreditsRemaining,
			ExpiresAt:        normalizeUpstreamTime(it.ExpiresAt),
		})
	}
	return ps.TotalCreditsRemaining, items, nil
}

// CheckinResult 是一次签到尝试的结果。
type CheckinResult struct {
	Status  string  `json:"status"` // success | already | unavailable
	Message string  `json:"message"`
	Gained  float64 `json:"gained,omitempty"`
}

// DailyCheckin 执行每日签到（GET slot → GET context → POST check_in），+100 积分/号/天。
// 已签到或无可用活动时返回对应状态而非错误。
func (s *Service) DailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	version := s.resolveClientVersion(ctx)
	client := s.httpClientFor(acc.ID)
	base := serverBase()

	// 1) 取可用活动槽位。
	slotURL := fmt.Sprintf("%s/api/client-activities/slot?placement=desktop_sidebar&clientVersion=%s&containerApiVersion=2&platform=win32",
		base, url.QueryEscape(version))
	slotReq, err := http.NewRequestWithContext(ctx, http.MethodGet, slotURL, nil)
	if err != nil {
		return CheckinResult{}, err
	}
	checkinHeaders(slotReq, acc, version)
	slotData, err := doEnvelope(ctx, client, slotReq)
	if err != nil {
		return CheckinResult{}, err
	}
	var slot struct {
		SlotState string `json:"slotState"`
		Activity  *struct {
			ActivityCode   string `json:"activityCode"`
			ConfigRevision any    `json:"configRevision"`
		} `json:"activity"`
	}
	if err := json.Unmarshal(slotData, &slot); err != nil {
		return CheckinResult{}, fmt.Errorf("解析活动槽位失败: %w", err)
	}
	if slot.Activity == nil || slot.SlotState != "available" {
		return CheckinResult{Status: "unavailable", Message: fmt.Sprintf("无可用活动（slotState=%s）", slot.SlotState)}, nil
	}
	code := strings.TrimSpace(slot.Activity.ActivityCode)
	if code == "" {
		return CheckinResult{Status: "unavailable", Message: "活动缺少 activityCode"}, nil
	}
	revision := encodeRevision(slot.Activity.ConfigRevision)

	// 2) 取活动上下文，判断今日是否已领。
	ctxURL := fmt.Sprintf("%s/api/client-activities/%s/context?configRevision=%s",
		base, url.PathEscape(code), url.QueryEscape(revision))
	ctxReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ctxURL, nil)
	if err != nil {
		return CheckinResult{}, err
	}
	checkinHeaders(ctxReq, acc, version)
	ctxData, err := doEnvelope(ctx, client, ctxReq)
	if err != nil {
		return CheckinResult{}, err
	}
	var activity struct {
		State struct {
			ClaimedToday bool `json:"claimedToday"`
		} `json:"state"`
		Actions []string `json:"actions"`
	}
	if err := json.Unmarshal(ctxData, &activity); err != nil {
		return CheckinResult{}, fmt.Errorf("解析活动上下文失败: %w", err)
	}
	if activity.State.ClaimedToday || !containsString(activity.Actions, "check_in") {
		return CheckinResult{Status: "already", Message: "今天已签到，跳过"}, nil
	}

	// 3) 执行签到。
	payload, _ := json.Marshal(map[string]any{
		"configRevision": rawRevision(slot.Activity.ConfigRevision),
		"idempotencyKey": randomUUIDString(),
		"payload":        map[string]any{},
	})
	postURL := fmt.Sprintf("%s/api/client-activities/%s/actions/check_in", base, url.PathEscape(code))
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(payload))
	if err != nil {
		return CheckinResult{}, err
	}
	checkinHeaders(postReq, acc, version)
	postData, err := doEnvelope(ctx, client, postReq)
	if err != nil {
		return CheckinResult{}, err
	}
	var res struct {
		Result map[string]any `json:"result"`
	}
	_ = json.Unmarshal(postData, &res)
	gained := firstCreditNumber(res.Result)
	return CheckinResult{Status: "success", Message: "签到成功", Gained: gained}, nil
}

// checkinHeaders 设置签到请求头。
func checkinHeaders(req *http.Request, acc Account, version string) {
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LobsterAI/"+version)
}

// encodeRevision 把 configRevision 归一化成查询串形式（数字原样，字符串原样）。
func encodeRevision(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		raw, _ := json.Marshal(t)
		return string(raw)
	}
}

// rawRevision 返回 configRevision 的原始 JSON 值（POST 体里保持类型）。
func rawRevision(v any) any {
	if v == nil {
		return 0
	}
	return v
}

// firstCreditNumber 从签到结果里尽力取本次获得的积分数。
func firstCreditNumber(result map[string]any) float64 {
	for _, key := range []string{"creditsGranted", "rewardCredits", "credits"} {
		if v, ok := result[key]; ok {
			switch n := v.(type) {
			case float64:
				return n
			case string:
				if f, err := parseFloat(n); err == nil {
					return f
				}
			}
		}
	}
	return 0
}

// containsString 判断字符串切片是否包含给定值。
func containsString(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

// randomUUIDString 生成随机 UUID4（失败时回退时间戳串）。
func randomUUIDString() string {
	if v, err := newUUID(); err == nil {
		return v
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// parseFloat 容错解析浮点数。
func parseFloat(s string) (float64, error) {
	var f float64
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &f); err != nil {
		return 0, err
	}
	return f, nil
}
