package raccoon

// 本文件实现「网页登录」：走小浣熊官方授权页，登录后通过 **loopback 回调**
// 自动把授权码交回本服务，无需手动粘贴。
//
// 协议事实来自上游授权页前端代码（/authorize 的 authorize/page-*.js）实测：
//   - 授权页：GET {origin}/authorize（/code/authorize 会 307 到 /authorize）
//     参数：login_source=desktop、appname、state
//   - 回调（desktop，无 desktop_callback）：office-raccoon://auth/callback?code&state
//     —— 自定义协议，只有装了官方桌面客户端的浏览器能接收；普通浏览器静默失败，
//        页面停在授权页（这就是「回调没有自动跳转」的根因）。
//   - 回调（loopback）：desktop_callback=loopback
//       redirect=http://127.0.0.1:<端口>/auth/callback
//     校验规则：state 必须 ^[a-f0-9]{64}$，redirect 必须严格匹配
//       ^http://127\.0\.0\.1:[1-9][0-9]{3,4}/auth/callback$ 且端口 ∈ [1024,65535]。
//     这条走浏览器普通跳转，能真正自动回跳，故本服务优先用它。
//   - 换码：POST {auth_base}/login_with_authorization_code  body {"authorization_code"}
//
// 安全语义：state 由本服务生成（64 位十六进制）、内存登记带过期，回调里的 state
// 必须逐字相等且未过期，且一次性消费；校验失败/过期/缺失一律拒绝，且不调用上游。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 网页登录协议常量（按上游授权页实测，固定不做配置项）。
const (
	loginSource = "desktop"

	// loginAppName 是授权页上报的客户端名（上游对这三个名字有特殊展示）。
	loginAppName = "办公小浣熊客户端"

	// loginStateTTL 是 state 有效期。
	loginStateTTL = 10 * time.Minute

	maxLoginCodeLength  = 8192
	maxLoginStateLength = 512

	// loopbackPort 是本地回调监听端口，必须落在上游允许的 [1024,65535]。
	loopbackPort = 55060
	// loopbackPath 是上游 loopback 回调要求的固定路径。
	loopbackPath = "/auth/callback"
)

// codeExpired 是「授权码已失效」的上游业务码。
const codeExpired = 200035

// errInvalidLoginCallback 表示回调 URL 的形态不对。
var errInvalidLoginCallback = errors.New("登录回调地址无效")

// loginPending 是一次进行中的网页登录。
type loginPending struct {
	state     string
	expiresAt time.Time
	// result 由 loopback 回调写入；nil 表示尚未回调。
	result chan loginCallback
	// serverMu 保护 server：sweep 回收与登录完成可能并发关闭同一个监听。
	serverMu sync.Mutex
	// server 是本轮临时起的 loopback 监听（可为 nil，表示未启用）。
	server *http.Server
}

// setLoopback 登记本轮的 loopback 监听。
func (p *loginPending) setLoopback(srv *http.Server) {
	p.serverMu.Lock()
	p.server = srv
	p.serverMu.Unlock()
}

// loginManager 维护网页登录的 pending state（内存、带过期、一次性）。
type loginManager struct {
	mu      sync.Mutex
	pending map[string]*loginPending
	now     func() time.Time
}

func newLoginManager() *loginManager {
	return &loginManager{pending: map[string]*loginPending{}, now: time.Now}
}

// newState 生成 64 位十六进制 state（上游 loopback 校验要求 ^[a-f0-9]{64}$）。
func newState() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (m *loginManager) pruneLocked(now time.Time) {
	for state, p := range m.pending {
		if !p.expiresAt.After(now) {
			delete(m.pending, state)
			// 过期登录要回收其 loopback 监听，避免端口长期被占。
			go p.stopLoopback()
		}
	}
}

// sweep 定期清理过期登录（含回收监听端口）；ctx 取消时退出。
func (m *loginManager) sweep(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			m.pruneLocked(m.now())
			m.mu.Unlock()
		case <-ctx.Done():
			m.mu.Lock()
			all := make([]*loginPending, 0, len(m.pending))
			for state, p := range m.pending {
				all = append(all, p)
				delete(m.pending, state)
			}
			m.mu.Unlock()
			for _, p := range all {
				p.stopLoopback()
			}
			return
		}
	}
}

// issue 生成新 state 并登记，返回 pending 与过期毫秒时间戳。
func (m *loginManager) issue(ttl time.Duration) (*loginPending, int64, error) {
	state, err := newState()
	if err != nil {
		return nil, 0, err
	}
	now := m.now()
	m.mu.Lock()
	m.pruneLocked(now)
	p := &loginPending{
		state:     state,
		expiresAt: now.Add(ttl),
		result:    make(chan loginCallback, 1),
	}
	m.pending[state] = p
	m.mu.Unlock()
	return p, p.expiresAt.UnixMilli(), nil
}

// consume 校验并一次性消费 state（用于「粘贴回调地址」的同步路径）。
// ok=false 时用 expired 区分「已过期」与「缺失/不匹配/已用过」。
func (m *loginManager) consume(state string) (ok bool, expired bool) {
	now := m.now()
	m.mu.Lock()
	p, found := m.pending[state]
	if !found {
		m.mu.Unlock()
		return false, false
	}
	delete(m.pending, state)
	m.mu.Unlock()
	if !p.expiresAt.After(now) {
		return false, true
	}
	return true, false
}

// peek 读取（不消费）某 state 的 pending，供 loopback 回调校验用。
func (m *loginManager) peek(state string) (*loginPending, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[state]
	return p, ok
}

// take 按 state 取出并删除 pending（loopback 回调成功后消费）。
func (m *loginManager) take(state string) (*loginPending, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[state]
	if !ok {
		return nil, false
	}
	if !p.expiresAt.After(m.now()) {
		delete(m.pending, state)
		return nil, false
	}
	delete(m.pending, state)
	return p, true
}

// loginCallback 是从回调里解析出的授权码与 state。
type loginCallback struct {
	Code  string
	State string
}

// parseLoginCallback 解析登录回调 URL，兼容两种形态：
//   - 自定义协议：office-raccoon://auth/callback?code&state（官方客户端）
//   - loopback：  http://127.0.0.1:<port>/auth/callback?code&state（浏览器跳转）
//
// 逐项比对 scheme/host/path，不做前缀匹配（避免 callback@evil.com 绕过）。
func parseLoginCallback(raw string) (loginCallback, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return loginCallback{}, errInvalidLoginCallback
	}
	switch {
	case parsed.Scheme == "office-raccoon" &&
		strings.EqualFold(parsed.Host, "auth") &&
		parsed.Path == "/callback":
		// 官方客户端自定义协议回调。
	case (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Path == loopbackPath &&
		isLoopbackHost(parsed.Hostname()):
		// 浏览器 loopback 回调（本机或经 SSH 端口转发）。
	default:
		return loginCallback{}, errInvalidLoginCallback
	}
	query := parsed.Query()
	return loginCallback{
		Code:  strings.TrimSpace(query.Get("code")),
		State: strings.TrimSpace(query.Get("state")),
	}, nil
}

// isLoopbackHost 判定主机名是否为回环地址。
func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

// buildAuthorizeURL 构造授权页地址。
//
// loopback != "" 时走 desktop_callback=loopback 分支（浏览器普通跳转，可自动回跳）；
// 否则走 office-raccoon:// 自定义协议分支（需官方客户端）。
func buildAuthorizeURL(origin, appName, state, loopback string) (string, error) {
	base, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	base.Path = "/authorize"
	query := base.Query()
	query.Set("lang", "zh")
	query.Set("login_source", loginSource)
	query.Set("appname", appName)
	query.Set("state", state)
	if loopback != "" {
		query.Set("desktop_callback", "loopback")
		query.Set("redirect", loopback)
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func loopbackRedirectURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", loopbackPort, loopbackPath)
}

// loopbackPortAvailable 报告本机 loopback 端口是否可用（多实例并存时回退到手动粘贴）。
func loopbackPortAvailable() bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", loopbackPort))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// startLoopbackListener 为一次登录起本地回调监听，收到合法回调后把结果写入
// pending.result 并关闭监听。listener 起不来时返回 false，调用方回退到手动粘贴。
func (s *Service) startLoopbackListener(p *loginPending) bool {
	mux := http.NewServeMux()
	mux.HandleFunc(loopbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		cb := loginCallback{
			Code:  strings.TrimSpace(q.Get("code")),
			State: strings.TrimSpace(q.Get("state")),
		}
		// 只接受与本次登录 state 完全一致且未过期的回调。
		if cb.State != p.state && p.state != "" {
			http.Error(w, "state 不匹配，请回到面板重新发起登录", http.StatusBadRequest)
			return
		}
		select {
		case p.result <- cb:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><meta charset=utf-8><title>登录成功</title>" +
			"<div style=\"font:16px/1.8 system-ui;padding:48px;text-align:center\">" +
			"<h2>登录成功</h2><p>可以关闭此页面，返回 API Monitor。</p></div>"))
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", loopbackPort))
	if err != nil {
		return false
	}
	p.setLoopback(srv)
	go func() { _ = srv.Serve(ln) }()
	return true
}

// StartLoginSweep 启动网页登录 pending 的定期清理（回收过期监听端口）。
func (s *Service) StartLoginSweep(ctx context.Context) {
	go s.login.sweep(ctx)
}

// stopLoopback 关闭该次登录的监听（结果已取到或超时）。
func (p *loginPending) stopLoopback() {
	if p == nil {
		return
	}
	p.serverMu.Lock()
	srv := p.server
	p.server = nil
	p.serverMu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// -----------------------------------------------------------------------------
// HTTP 处理
// -----------------------------------------------------------------------------

// handleLoginStart 发起网页登录。
//
// 请求体可选 {"local": bool}：表示浏览器是否与本服务同机（前端用
// window.location.hostname 判定）。
//
// 无论是否同机，只要本地端口可用就生成 loopback 回调地址——因为浏览器跳转到
// http://127.0.0.1:<port>/auth/callback?code=... 时，即使连不上也能从地址栏拿到
// 完整 URL 供手动粘贴（远程访问场景）；同机时则会自动送达后端。
// auto 仅表示「本次是否可能有自动回调」，用于前端决定是否轮询。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Local *bool `json:"local"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	localBrowser := body.Local == nil || *body.Local

	p, expiresAt, err := s.login.issue(loginStateTTL)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "生成登录 state 失败：" + err.Error()})
		return
	}
	// 起 loopback 监听（端口占用则退回自定义协议分支）。
	listening := s.startLoopbackListener(p)
	loopback := ""
	if listening {
		loopback = loopbackRedirectURL()
	}
	// 只有「同机 + 监听成功」才可能出现自动回调。
	auto := listening && localBrowser
	authorizeURL, err := buildAuthorizeURL(baseOrigin, loginAppName, p.state, loopback)
	if err != nil {
		p.stopLoopback()
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "构造登录地址失败：" + err.Error()})
		return
	}
	writeJSONUnescaped(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"url":        authorizeURL,
		"state":      p.state,
		"expiresAt":  expiresAt,
		"ttlSeconds": int(loginStateTTL.Seconds()),
		"auto":       auto,
		"local":      localBrowser,
	})
}

// handleLoginWait 轮询本次登录是否已通过 loopback 自动拿到授权码并完成换码。
// 前端在「网页登录」弹窗打开期间轮询它；成功即自动加入账号，无需粘贴。
func (s *Service) handleLoginWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	state := strings.TrimSpace(body.State)
	if state == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 state"})
		return
	}
	p, ok := s.login.peek(state)
	if !ok {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": true, "status": "expired"})
		return
	}
	select {
	case cb := <-p.result:
		// 收到回调：取出 pending（一次性）并换码。
		s.login.take(state)
		p.stopLoopback()
		if strings.TrimSpace(cb.Code) == "" {
			responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "status": "error", "error": "登录回调里没有授权码"})
			return
		}
		acc, updated, err := s.finishLogin(r.Context(), cb.Code)
		if err != nil {
			status, msg := loginExchangeError(err)
			responseJSON(w, status, map[string]interface{}{"success": false, "status": "error", "error": msg})
			return
		}
		responseJSON(w, map[string]interface{}{
			"success": true,
			"status":  "success",
			"id":      acc.ID,
			"updated": updated,
			"account": s.toAccountView(acc),
		})
	default:
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": true, "status": "pending"})
	}
}

type loginExchangeRequest struct {
	CallbackURL string `json:"callbackUrl"`
	Code        string `json:"code"`
	State       string `json:"state"`
}

// handleLoginExchange 用回调 URL（或 code+state）换凭证并落账号（手动粘贴路径）。
func (s *Service) handleLoginExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var req loginExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体不是合法 JSON"})
		return
	}
	code, state := strings.TrimSpace(req.Code), strings.TrimSpace(req.State)
	if strings.TrimSpace(req.CallbackURL) != "" {
		parsed, err := parseLoginCallback(req.CallbackURL)
		if err != nil {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": errInvalidLoginCallback.Error()})
			return
		}
		code, state = parsed.Code, parsed.State
	}
	if code == "" || len([]rune(code)) > maxLoginCodeLength {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "登录回调里没有授权码"})
		return
	}
	// state 缺失/过长/不匹配/已用过/已过期：一律 400，且绝不调用上游
	// （否则等于拿别人的 code 换凭证写进自己的账号库）。
	if state == "" || len([]rune(state)) > maxLoginStateLength {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "登录回调 state 校验失败，请重新发起登录"})
		return
	}
	if ok, expired := s.login.consume(state); !ok {
		msg := "登录回调 state 校验失败，请重新发起登录"
		if expired {
			msg = "本次登录已超时，请重新发起网页登录"
		}
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": msg})
		return
	}
	acc, updated, err := s.finishLogin(r.Context(), code)
	if err != nil {
		status, msg := loginExchangeError(err)
		responseJSON(w, status, map[string]interface{}{"success": false, "error": msg})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success": true,
		"id":      acc.ID,
		"updated": updated,
		"account": s.toAccountView(acc),
	})
}

// finishLogin 用授权码换 token 并落账号；同账号重复登录保留用户设置。
func (s *Service) finishLogin(ctx context.Context, code string) (Account, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tokens, err := s.exchangeAuthorizationCode(ctx, code)
	if err != nil {
		return Account{}, false, err
	}
	acc := Account{
		UserID:       jwtUserID(tokens.AccessToken),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAtMS:  jwtExpiryMS(tokens.AccessToken),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	acc.ID = deriveAccountID(acc)
	updated := false
	if existing, ok := s.findAccount(acc.ID); ok {
		acc.Nickname = existing.Nickname
		acc.Disabled = existing.Disabled
		acc.CreatedAt = firstNonEmpty(existing.CreatedAt, acc.CreatedAt)
		updated = true
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		return Account{}, false, fmt.Errorf("保存账号失败: %w", err)
	}
	return acc, updated, nil
}

// loginExchangeError 把换凭证失败映射成状态码与中文文案。
func loginExchangeError(err error) (int, string) {
	var codeErr *authCodeError
	if errors.As(err, &codeErr) {
		if codeErr.hasCode && codeErr.code == codeExpired {
			return http.StatusBadRequest, "授权码已失效，请重新发起网页登录"
		}
		msg := codeErr.message
		if msg == "" || msg == "internal_server_error" {
			msg = "登录服务异常，请稍后重试"
		}
		if codeErr.httpStatus >= 400 && codeErr.httpStatus < 500 {
			return codeErr.httpStatus, msg
		}
		return http.StatusBadGateway, "网页登录换取凭证失败：" + msg
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "网页登录换取凭证失败：请求超时"
	}
	return http.StatusBadGateway, "网页登录换取凭证失败：" + err.Error()
}

// writeJSONUnescaped 写 2xx JSON 但不转义 HTML 字符（授权 URL 里的 & 需原样保留）。
func writeJSONUnescaped(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
