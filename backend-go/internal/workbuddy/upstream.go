package workbuddy

// 本文件是 WorkBuddy 的「上游协议层」：CodeBuddy（copilot.tencent.com）的协议事实、
// 数据形状、HTTP 管线，以及真正访问上游的五个调用。
//
// 协议事实来自 https://github.com/LiuJiaCheng11/workbuddy-cliproxy 的 README 与 main.go。
// 需要注意的边界行为都写在各函数注释里，改动前先读。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// upstreamImplemented 标记上游协议层是否已补全（管理面据此展示状态）。
const upstreamImplemented = true

// -----------------------------------------------------------------------------
// 协议常量（照抄参考实现，勿改）
// -----------------------------------------------------------------------------

const (
	providerName = "workbuddy"

	// clientUA 是 CodeBuddy CLI 的客户端标识，上游按此识别终端类型。
	clientUA = "CLI/2.63.2 CodeBuddy/2.63.2"
	// originReferer 必须随请求下发：缺失时上游网关对 /v3/config 直接返回 400。
	originReferer = "https://www.codebuddy.cn"

	// loginTTL 是扫码登录会话的有效期。
	loginTTL = 5 * time.Minute

	// modelCacheTTL 是模型目录缓存时长；上游目录变动很慢，无需每次发现都回源。
	modelCacheTTL = 10 * time.Minute

	// configProbeUID 用于拉取模型目录：/v3/config 只校验 X-User-Id 是否存在
	// （值不被校验），存在即从匿名形态（models:null）切到完整目录，且目录与账号无关，
	// 因此登录前就能拿到。这里用一个固定 UUID。
	configProbeUID = "00000000-0000-0000-0000-000000000000"

	// upstreamCodeLoginPending 是 /v2/plugin/auth/token 在登录未完成时返回的业务码。
	upstreamCodeLoginPending = 11217
)

// upstreamBase 是 CodeBuddy 上游基址。声明为变量而非常量，是为了让端到端测试能把它
// 指向本地 mock 服务，从而验证「上游返回 usage → 本插件中继 → 下游字节」的完整链路。
var upstreamBase = "https://copilot.tencent.com"

// 端点 URL 一律经这些函数派生，避免在别处硬编码域名（测试改 upstreamBase 即刻生效）。
func endpointAuthState() string { return upstreamBase + "/v2/plugin/auth/state?platform=CLI" }
func endpointLoginAccount(state string) string {
	return upstreamBase + "/v2/plugin/login/account?state=" + url.QueryEscape(state)
}
func endpointAuthToken(state string) string {
	return upstreamBase + "/v2/plugin/auth/token?state=" + url.QueryEscape(state)
}
func endpointTokenRefresh() string { return upstreamBase + "/v2/plugin/auth/token/refresh" }
func endpointChat() string         { return upstreamBase + "/v2/chat/completions" }
func endpointConfig() string       { return upstreamBase + "/v3/config" }

// -----------------------------------------------------------------------------
// 数据形状
// -----------------------------------------------------------------------------

// ModelInfo 是模型目录里的一项（映射自上游 /v3/config 的模型条目）。
type ModelInfo struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	ContextLength     int64  `json:"contextLength,omitempty"`
	MaxOutputTokens   int64  `json:"maxOutputTokens,omitempty"`
	SupportsImages    bool   `json:"supportsImages,omitempty"`
	SupportsReasoning bool   `json:"supportsReasoning,omitempty"`
	SupportsToolCall  bool   `json:"supportsToolCall,omitempty"`
	OnlyReasoning     bool   `json:"onlyReasoning,omitempty"`
	Vendor            string `json:"vendor,omitempty"`
	MaxAllowedSize    int64  `json:"maxAllowedSize,omitempty"`
	Description       string `json:"description,omitempty"`

	// CreditsMultiplier 是上游的积分倍率，来自 /v3/config 的 credits 字段
	// （原始串形如 "x0.79 credits"）。它是 CodeBuddy 计费体系内的相对倍数，
	// 不含货币单价，因此只能用于横向比较，不能直接换算成金额。
	CreditsMultiplier float64 `json:"creditsMultiplier,omitempty"`
	// CreditsLabel 是倍率原始串，解析失败时供前端原样展示。
	CreditsLabel string `json:"creditsLabel,omitempty"`
	// CreditsParsed 表示倍率是否解析成功（false 时前端显示「—」而不是 0）。
	CreditsParsed bool `json:"creditsParsed,omitempty"`
}

// Account 是一个 CodeBuddy 扫码登录凭据。
// 含 token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	// ID 是账号稳定标识，取上游 uid；uid 缺失时回退为 nickname。
	ID           string `json:"id"`
	UID          string `json:"uid,omitempty"`
	EnterpriseID string `json:"enterpriseId,omitempty"`
	Nickname     string `json:"nickname,omitempty"`
	Domain       string `json:"domain,omitempty"`

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
	ID           string `json:"id"`
	Nickname     string `json:"nickname,omitempty"`
	UID          string `json:"uid,omitempty"`
	EnterpriseID string `json:"enterpriseId,omitempty"`
	Disabled     bool   `json:"disabled"`
	// TokenState：valid / expiring（<30 分钟）/ expired / unknown。
	TokenState       string `json:"tokenState"`
	ExpiresAt        int64  `json:"expiresAt,omitempty"`
	ExpiresInSeconds int64  `json:"expiresInSeconds,omitempty"`
	Available        bool   `json:"available"`
	CallCount        int64  `json:"callCount"`
	LastError        string `json:"lastError,omitempty"`
	// LimitedModels 是该账号当前被上游**模型级限流**的模型（含恢复时刻）。
	// 与 Available 独立：限流的只是这些模型，账号本身仍可用（其它模型照常转发）。
	LimitedModels []ModelLimitView `json:"limitedModels,omitempty"`
}

// loginState 是一次进行中的扫码登录会话。
// CodeBuddy 把浏览器登录与 auth/state 签发的 state 绑定，因此同一个 state 的
// state 请求与后续轮询必须复用同一个 cookie jar —— 每个 state 一个独立 client。
type loginState struct {
	client  *http.Client
	expires time.Time
}

// apiEnvelope 是 CodeBuddy 各接口统一的 {code,msg,data} 包装。
type apiEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// authStateData 是 auth/state 的返回内容，authUrl 即前端要渲染的二维码内容。
type authStateData struct {
	State   string `json:"state"`
	AuthURL string `json:"authUrl"`
}

// tokenData 是登录/刷新接口返回的 token 包。
type tokenData struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresIn        int64  `json:"expiresIn"`
	RefreshExpiresIn int64  `json:"refreshExpiresIn"`
	Domain           string `json:"domain"`
}

// accountData 是登录完成后补充的账号信息。
type accountData struct {
	UID          string `json:"uid"`
	EnterpriseID string `json:"enterpriseId"`
	Nickname     string `json:"nickname"`
}

// -----------------------------------------------------------------------------
// HTTP 管线
// -----------------------------------------------------------------------------

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 是共享的出网客户端（带 cookie jar）。
func sharedHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		jar, _ := cookiejar.New(nil)
		sharedClient = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConnsPerHost: 5,
			},
			Jar: jar,
		}
	})
	return sharedClient
}

// newLoginClient 构造带独立 cookie jar 的登录客户端，
// 保证某次浏览器登录的 cookie 绝不串到另一次登录上。
func newLoginClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: sharedHTTPClient().Transport,
		Jar:       jar,
	}
}

// httpClientFor 返回本次出网要用的客户端：配置了代理池时经 ProxyPoolSelector 选出口，
// 否则用共享客户端。选路失败时静默回退直连，避免代理池故障直接打断转发。
// 注意：带代理的客户端按请求新建（连接不复用），换来的是出口可随请求轮换。
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
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(u),
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 5,
		},
	}
}

// commonHeaders 施加 CodeBuddy 的通用请求头。
// Origin / Referer 是必需的：缺失会让上游网关对 /v3/config 返回 400。
func commonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", originReferer)
	req.Header.Set("Referer", originReferer+"/")
	req.Header.Set("User-Agent", clientUA)
}

// backendHeaders 施加凭据派生头。空字段按上游的 X-No-* 约定标注，
// 不能简单省略（"显式为空"与"不发送"在上游语义不同）。
func backendHeaders(req *http.Request, acc *Account) {
	commonHeaders(req)
	if acc.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	} else {
		req.Header.Set("X-No-Authorization", "1")
	}
	if acc.UID != "" {
		req.Header.Set("X-User-Id", acc.UID)
	} else {
		req.Header.Set("X-No-User-Id", "1")
	}
	if acc.EnterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
	} else {
		req.Header.Set("X-No-Enterprise-Id", "1")
	}
	if acc.RefreshToken != "" {
		req.Header.Set("X-Refresh-Token", acc.RefreshToken)
	}
	if acc.Domain != "" {
		req.Header.Set("X-Domain", acc.Domain)
	} else {
		req.Header.Set("X-No-Department-Info", "1")
	}
	req.Header.Set("X-Product", "SaaS")
}

// doEnvelope 发送请求并解析 {code,msg,data} 包装，但**不**因业务码非 0 返回错误：
// 登录轮询必须自己区分"仍待扫码"（业务码 11217）与真正的失败。
// status 为上游 HTTP 状态码；status == 0 表示传输层失败。
func doEnvelope(ctx context.Context, client *http.Client, method, fullURL string, headers func(*http.Request), body io.Reader) (apiEnvelope, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return apiEnvelope{}, 0, err
	}
	if headers != nil {
		headers(req)
	} else {
		commonHeaders(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return apiEnvelope{}, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	// 非 2xx 时上游未必回 JSON 信封（例如 openresty 的 401 页面），
	// 此时不把解析失败当错误，交给调用方按 status 分支。
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return apiEnvelope{}, resp.StatusCode, nil
	}
	return env, resp.StatusCode, nil
}

// doJSON 在 doEnvelope 之上做严格判定：HTTP 4xx/5xx 或业务码非 0 都算失败。
func doJSON(ctx context.Context, client *http.Client, method, fullURL string, headers func(*http.Request), body io.Reader) (json.RawMessage, int, error) {
	env, status, err := doEnvelope(ctx, client, method, fullURL, headers, body)
	if err != nil {
		return nil, 0, err
	}
	if status >= 400 {
		return nil, status, fmt.Errorf("上游 HTTP %d", status)
	}
	if env.Code != 0 {
		return nil, status, fmt.Errorf("上游业务码 %d: %s", env.Code, env.Msg)
	}
	return env.Data, status, nil
}

// -----------------------------------------------------------------------------
// 登录 / 刷新
// -----------------------------------------------------------------------------

// startLogin 发起扫码登录：向 auth/state 要一个 state 与授权链接，
// 并把本次的独立 cookie jar 与 state 绑定保存，供后续轮询复用。
func (s *Service) startLogin(ctx context.Context) (string, string, error) {
	client := newLoginClient()
	data, _, err := doJSON(ctx, client, http.MethodPost, endpointAuthState(), nil, bytesReader([]byte("{}")))
	if err != nil {
		return "", "", fmt.Errorf("发起登录失败: %w", err)
	}
	var st authStateData
	if err := json.Unmarshal(data, &st); err != nil {
		return "", "", fmt.Errorf("登录响应解析失败: %w", err)
	}
	if st.State == "" || st.AuthURL == "" {
		return "", "", fmt.Errorf("登录接口未返回 state 或 authUrl")
	}
	s.loginMu.Lock()
	s.loginStates[st.State] = &loginState{client: client, expires: time.Now().Add(loginTTL)}
	s.loginMu.Unlock()
	return st.State, st.AuthURL, nil
}

// pollLogin 单次探测登录状态（节奏由前端驱动）。
//
// 判定顺序（来自参考实现的实测结论，勿调整）：
//  1. auth/token 是登录状态的权威接口：未完成时返回业务码 11217（"login ing"）；
//  2. login/account 位于 openresty 之后，登录完成前会被挡成 401，
//     所以必须**先拿到 bearer 再取账号信息**；
//  3. HTTP 4xx 视为"尚未登录"（不是故障），传输层失败才是真错误。
//
// 返回 (account, done, err)：done=false 且 err=nil 表示仍在等待扫码。
func (s *Service) pollLogin(ctx context.Context, state string) (Account, bool, error) {
	s.loginMu.Lock()
	lc := s.loginStates[state]
	s.loginMu.Unlock()
	if lc == nil {
		return Account{}, false, fmt.Errorf("登录会话不存在或已过期，请重新获取二维码")
	}
	if time.Now().After(lc.expires) {
		s.forgetLoginState(state)
		return Account{}, false, fmt.Errorf("登录会话已过期，请重新获取二维码")
	}

	env, status, err := doEnvelope(ctx, lc.client, http.MethodGet, endpointAuthToken(state), nil, nil)
	if err != nil {
		return Account{}, false, fmt.Errorf("查询登录状态失败: %w", err)
	}
	if status >= 400 {
		// 尚未登录时该路径可能被前置网关拒绝。
		return Account{}, false, nil
	}
	if env.Code == upstreamCodeLoginPending {
		return Account{}, false, nil
	}
	if env.Code != 0 {
		return Account{}, false, fmt.Errorf("上游业务码 %d: %s", env.Code, env.Msg)
	}
	var tok tokenData
	if err := json.Unmarshal(env.Data, &tok); err != nil || tok.AccessToken == "" {
		// token 尚未就绪，按仍待登录处理。
		return Account{}, false, nil
	}

	// 已持 bearer，补账号信息（失败不阻断登录，只少了昵称/企业信息）。
	var acct accountData
	acctHeaders := func(r *http.Request) {
		commonHeaders(r)
		r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	if raw, _, err := doJSON(ctx, lc.client, http.MethodGet, endpointLoginAccount(state), acctHeaders, nil); err == nil {
		_ = json.Unmarshal(raw, &acct)
	}

	acc := Account{
		ID:           firstNonEmpty(acct.UID, acct.Nickname),
		UID:          acct.UID,
		EnterpriseID: acct.EnterpriseID,
		Nickname:     acct.Nickname,
		Domain:       tok.Domain,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	return acc, true, nil
}

// refreshAccessToken 用 refresh token 换新的 access token，并原地更新凭据。
// HTTP 4xx 表示 refresh token 已失效（需重新扫码登录），会作为错误返回。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("账号缺少 refresh token，需重新扫码登录")
	}
	headers := func(r *http.Request) {
		commonHeaders(r)
		r.Header.Set("X-Refresh-Token", acc.RefreshToken)
		if acc.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
		}
		r.Header.Set("X-Auth-Refresh-Source", providerName)
	}
	env, status, err := doEnvelope(ctx, s.httpClientFor(acc.ID), http.MethodPost, endpointTokenRefresh(), headers, nil)
	if err != nil {
		return fmt.Errorf("刷新 token 失败: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("刷新被上游拒绝（HTTP %d），refresh token 可能已失效", status)
	}
	if env.Code != 0 {
		return fmt.Errorf("上游业务码 %d: %s", env.Code, env.Msg)
	}
	var tok tokenData
	if err := json.Unmarshal(env.Data, &tok); err != nil || tok.AccessToken == "" {
		return fmt.Errorf("刷新响应缺少 accessToken")
	}
	acc.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	if tok.Domain != "" {
		acc.Domain = tok.Domain
	}
	if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	acc.LastError = ""
	return nil
}

// -----------------------------------------------------------------------------
// 模型目录
// -----------------------------------------------------------------------------

// fetchCatalog 拉取上游模型目录。
//
// 三个容易"改错"的点：
//   - 只需要 X-User-Id **存在**（值不被校验），不需要登录；目录与账号无关；
//   - commonHeaders 不可省：丢了 Origin/Referer 上游直接 400；
//   - maxInputTokens <= 0 的条目不能服务对话（当前上游只有 hunyuan-image-v3.0-art
//     这类图像模型），必须跳过。
//
// 上游条目字段比参考实现用到的多，这里额外解析了 credits（倍率）、vendor、
// supportsToolCall、onlyReasoning、maxAllowedSize 与中文描述，仅供展示。
func (s *Service) fetchCatalog(ctx context.Context) ([]ModelInfo, error) {
	headers := func(r *http.Request) {
		commonHeaders(r)
		r.Header.Set("X-User-Id", configProbeUID)
		r.Header.Set("X-Product", "SaaS")
	}
	data, _, err := doJSON(ctx, s.httpClientFor(""), http.MethodGet, endpointConfig(), headers, nil)
	if err != nil {
		return nil, fmt.Errorf("拉取模型目录失败: %w", err)
	}
	var cfg struct {
		Models []struct {
			ID                string `json:"id"`
			Name              string `json:"name"`
			MaxInputTokens    int64  `json:"maxInputTokens"`
			MaxOutputTokens   int64  `json:"maxOutputTokens"`
			MaxAllowedSize    int64  `json:"maxAllowedSize"`
			SupportsImages    bool   `json:"supportsImages"`
			SupportsReasoning bool   `json:"supportsReasoning"`
			SupportsToolCall  bool   `json:"supportsToolCall"`
			OnlyReasoning     bool   `json:"onlyReasoning"`
			Vendor            string `json:"vendor"`
			DescriptionZh     string `json:"descriptionZh"`
			Credits           string `json:"credits"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("模型目录解析失败: %w", err)
	}
	out := make([]ModelInfo, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		if m.ID == "" || m.MaxInputTokens <= 0 {
			continue
		}
		multiplier, parsed := parseCredits(m.Credits)
		out = append(out, ModelInfo{
			ID:                m.ID,
			DisplayName:       firstNonEmpty(m.Name, m.ID),
			ContextLength:     m.MaxInputTokens,
			MaxOutputTokens:   m.MaxOutputTokens,
			SupportsImages:    m.SupportsImages,
			SupportsReasoning: m.SupportsReasoning,
			SupportsToolCall:  m.SupportsToolCall,
			OnlyReasoning:     m.OnlyReasoning,
			Vendor:            m.Vendor,
			MaxAllowedSize:    m.MaxAllowedSize,
			Description:       m.DescriptionZh,
			CreditsMultiplier: multiplier,
			CreditsLabel:      strings.TrimSpace(m.Credits),
			CreditsParsed:     parsed,
		})
	}
	return out, nil
}

// parseCredits 解析上游的积分倍率串，形如 "x0.79 credits" / "X5.00 credits"。
// 解析失败（字段缺失或换了格式）时返回 false，调用方按「未知」展示，
// 不要退化成 0 —— 0 会被误读成「免费」。
func parseCredits(raw string) (float64, bool) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return 0, false
	}
	t = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "x"), "X"))
	t = strings.TrimSpace(strings.TrimSuffix(t, "credits"))
	v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// -----------------------------------------------------------------------------
// 对话转发
// -----------------------------------------------------------------------------

// chatRequest 是一次对话转发的入参（已按上游要求整理完毕）。
type chatRequest struct {
	// Account 是本次转发使用的账号（决定鉴权头）。
	Account Account
	// Body 是发给上游的请求体（已强制 stream:true、已做模板改写与思考档位覆盖）。
	Body []byte
	// Model 是客户端原始模型名（已剥本插件前缀，用于非流式聚合时回填 model 字段）。
	Model string
	// Stream 是客户端是否要求流式；上游一律按流式发，非流式在本层聚合。
	Stream bool
}

// upstreamError 表示一次上游失败的分类结果。
// retryable 为 true 时，relay 层会把该账号标记冷却并换号重试。
// 约定：chatCompletions 返回的 error（无论可重试与否）都保证发生在
// **写出任何下游字节之前**，因此换号重试是安全的。
type upstreamError struct {
	msg       string
	retryable bool
	// rateLimit 为 true 表示这是**模型级频率限制**（而非普通瞬时故障）：
	// relay 层据此只封「该账号 × 该模型」，而不是把整个账号打进冷却。见 ratelimit.go。
	rateLimit bool
	// rateLimitUntil 是上游给出的恢复时刻（rateLimit 为 true 时有意义）。
	rateLimitUntil time.Time
}

func (e *upstreamError) Error() string { return e.msg }

// isRetryableHTTP 判断上游 HTTP 状态码是否值得换号重试。
// 429（配额/限流）与 5xx（网关/上游过载）换号常常能成功；4xx 是请求侧问题，重试无意义。
func isRetryableHTTP(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// classifyUpstreamPayload 判断上游一段正文是不是「错误」，返回分类后的 *upstreamError。
//
// 为什么不能只看 HTTP 状态码：上游在部分失败场景用 **HTTP 200 + {code,msg} 信封**下发，
// 若不解出来，中继会把「空回答」当成功透给下游（下游收到一个没有内容的 completion，
// 既不是错误也无从分辨）。三条判据依次收紧，宁可漏判也不要误伤正常分片：
//   - 错误信封（envelopeError）：msg 带限流措辞即判**模型级限流**——那里是错误说明
//     而非模型正文，故措辞本身足够；没有重置时刻时回落到 modelLimitDefault。
//   - 自由文本：必须「限流措辞 + 可解析时刻」同时命中，且**不含 choices**
//     （正常 chunk 一定带 choices），避免把「用户问 rate limit 是什么」的正常回答误判。
func (s *Service) classifyUpstreamPayload(text string, loc *time.Location) *upstreamError {
	now := time.Now()
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if code, msg, ok := envelopeError(text); ok {
		if until, hit := rateLimitFromError(msg, loc, now); hit {
			s.noteRateLimit(msg)
			return &upstreamError{
				msg:            fmt.Sprintf("上游业务错误 code=%d: %s", code, msg),
				retryable:      true,
				rateLimit:      true,
				rateLimitUntil: until,
			}
		}
		return &upstreamError{msg: fmt.Sprintf("上游业务错误 code=%d: %s", code, msg)}
	}
	if strings.Contains(text, `"choices"`) {
		return nil
	}
	if until, hit := rateLimitFromText(text, loc, now); hit {
		s.noteRateLimit(text)
		return &upstreamError{msg: truncate(text, 200), retryable: true, rateLimit: true, rateLimitUntil: until}
	}
	return nil
}

// streamPeekLines 是流式路径在写出响应头**之前**最多窥探的行数。
// 只为跨过 SSE 的空行/心跳，尽快看到第一个真正的负载行。
const streamPeekLines = 8

// chatCompletions 向上游发起一次对话并把结果写给下游。
//
// 上游对非流式请求直接回业务码 11101，所以无论客户端要什么，发给上游的 body
// 都必须带 "stream":true（由 relay 层用 forceStreamBody 保证）：
//   - 客户端要流式 → 边读上游 SSE 边 Flush 给下游（真流式，思考过程也实时流出）；
//   - 客户端要非流式 → 把 SSE 聚合回一个完整 chat.completion 再回。
//
// 返回的 error 一定发生在**写出任何响应之前**，调用方可以安全地改写成 OpenAI 错误体。
// 失败会被分类成 *upstreamError：HTTP 429/5xx、网络错误、流中途断流都标记为
// 可重试（换号常能成功），4xx 标记为不可重试。
func (s *Service) chatCompletions(ctx context.Context, w http.ResponseWriter, req *chatRequest) error {
	if strings.TrimSpace(req.Account.AccessToken) == "" {
		return fmt.Errorf("账号 %s 缺少 access token", req.Account.ID)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointChat(), bytesReader(req.Body))
	if err != nil {
		return err
	}
	backendHeaders(httpReq, &req.Account)
	resp, err := s.httpClientFor(req.Account.ID).Do(httpReq)
	if err != nil {
		return &upstreamError{msg: fmt.Sprintf("上游请求失败: %v", err), retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		text := strings.TrimSpace(string(payload))
		// 限流文案优先于状态码判定：上游可能用 4xx 回「频率限制」，
		// 而 4xx 默认是不可重试的 —— 但换个账号/等重置就很值得重试。
		if until, ok := rateLimitFromText(text, s.siteLocation(ctx), time.Now()); ok {
			s.noteRateLimit(text)
			return &upstreamError{
				msg:            fmt.Sprintf("上游 HTTP %d: %s", resp.StatusCode, truncate(text, 200)),
				retryable:      true,
				rateLimit:      true,
				rateLimitUntil: until,
			}
		}
		return &upstreamError{
			msg:       fmt.Sprintf("上游 HTTP %d: %s", resp.StatusCode, truncate(text, 200)),
			retryable: isRetryableHTTP(resp.StatusCode),
		}
	}

	s.recordCall(req.Account.ID)
	if req.Stream {
		return s.streamToClient(ctx, w, resp.Body, req.Account.ID, req.Model)
	}
	return s.writeAggregated(ctx, w, resp.Body, req.Model, req.Account.ID)
}

// relayEmitsSSEFrame 报告中继面是否给 chunk 加 "data: " 分帧。
// 参考实现（CPA ABI 插件）要按入口协议区分：原生 chat-completions 由宿主加分帧，
// 跨协议翻译器只认已分帧 payload。本项目的中继面是**真实 HTTP 端点**、只说 OpenAI 协议，
// 下游（模型网关）按标准 OpenAI SSE 消费（见 internal/openai/relay.go 的
// `data: ` 前缀判定），因此恒为真，并在流末补 [DONE] 终止符。
func relayEmitsSSEFrame() bool { return true }

// streamToClient 把上游 SSE 逐块清洗后实时写给下游。
// 写出过程中出错（客户端断开）直接收尾返回，不再向上报错——此时响应头已发出。
func (s *Service) streamToClient(ctx context.Context, w http.ResponseWriter, body io.Reader, accountID, model string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// 先窥探开头再写响应头：上游失败时可能用 200 + 错误信封下发，而**响应头一旦写出**
	// 就再也无法换号重试（会退化成「200 + 空回答」）。开头是唯一的拦截窗口，
	// 代价只是第一个分片晚到一个读周期。窥探到的行稍后原样补写，不丢内容。
	loc := s.siteLocation(ctx)
	var peeked []string
	for len(peeked) < streamPeekLines && scanner.Scan() {
		line := scanner.Text()
		peeked = append(peeked, line)
		content := stripDataPrefix(line)
		if content == "" || content == "[DONE]" {
			continue // 空行/心跳：接着往后看
		}
		if ue := s.classifyUpstreamPayload(content, loc); ue != nil {
			// 尚未写出任何字节 → 调用方可安全换号或改写成 OpenAI 错误体。
			return ue
		}
		break // 第一个真负载不是错误 → 正常流，收手
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	// consume 处理一行原始 SSE 行；返回 error 表示**客户端写失败**（断开），
	// 此时响应头已发出，收尾返回即可，不再向上报错。
	consume := func(line string) error {
		content := stripDataPrefix(line)
		if content == "" || content == "[DONE]" {
			return nil
		}
		// 观察原始 chunk（清洗前），才能看出上游真实上报了哪些 usage 字段。
		s.observeUsage(content)
		cleaned := cleanChunkJSON(content)
		if cleaned == "" {
			return nil
		}
		// 记账用**清洗归一后**的 chunk，才能拿到标准的 prompt_tokens_details.cached_tokens。
		s.recordUsageFromChunk(ctx, accountID, model, cleaned)
		// 留痕归一化后真正写往下游的 usage —— 排障时用来确认网关的正则能否命中。
		s.captureEmittedUsage(cleaned)
		if _, err := io.WriteString(w, "data: "+cleaned+"\n\n"); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	for _, line := range peeked {
		if err := consume(line); err != nil {
			return nil
		}
	}
	for scanner.Scan() {
		if err := consume(scanner.Text()); err != nil {
			return nil
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

// writeAggregated 把上游 SSE 折叠成一个非流式 chat.completion 后写出。
func (s *Service) writeAggregated(ctx context.Context, w http.ResponseWriter, body io.Reader, model, accountID string) error {
	loc := s.siteLocation(ctx)
	// 边聚合边看有没有错误信封：上游用 200 + 信封下发时，聚合结果会是一个
	// 「空回答」，必须先于写出把它拦下来（此刻尚未写任何字节，换号重试是安全的）。
	var payloadErr *upstreamError
	completion, err := aggregateCompletion(body, model, func(chunk string) {
		s.observeUsage(chunk)
		if payloadErr == nil {
			payloadErr = s.classifyUpstreamPayload(chunk, loc)
		}
	})
	if err != nil {
		// 上游流中途断掉：换号重发常能成功，标为可重试。
		return &upstreamError{msg: err.Error(), retryable: true}
	}
	if payloadErr != nil {
		return payloadErr
	}
	// 聚合结果的 usage 已归一化，直接取出来记账。
	var probe struct {
		Usage map[string]any `json:"usage"`
	}
	if json.Unmarshal(completion, &probe) == nil {
		s.recordUsage(ctx, accountID, model, probe.Usage)
	}
	s.captureEmittedUsage(string(completion))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(completion)
	return nil
}

// aggregateCompletion 把上游 SSE 流折叠成单个 chat.completion 对象。
// 内容 / 思考内容 / tool_calls 分别累加，usage 取最后一处出现的值。
// onChunk 可选，用于在解析前观察每个原始 chunk（诊断上游 usage 字段用）。
func aggregateCompletion(r io.Reader, model string, onChunk func(string)) ([]byte, error) {
	var content, reasoning, role, respModel, respID, finish string
	var created int64
	var usage map[string]any
	var toolCalls []map[string]any

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		data := stripDataPrefix(scanner.Text())
		if data == "" || data == "[DONE]" {
			continue
		}
		if onChunk != nil {
			onChunk(data)
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if v, ok := chunk["id"].(string); ok && v != "" {
			respID = v
		}
		if v, ok := chunk["model"].(string); ok && v != "" {
			respModel = v
		}
		if v, ok := chunk["created"].(float64); ok {
			created = int64(v)
		}
		if v, ok := chunk["usage"].(map[string]any); ok {
			usage = v
		}
		choices, _ := chunk["choices"].([]any)
		for _, c := range choices {
			choice, _ := c.(map[string]any)
			if delta, ok := choice["delta"].(map[string]any); ok {
				if v, ok := delta["role"].(string); ok && v != "" {
					role = v
				}
				if v, ok := delta["content"].(string); ok {
					content += v
				}
				if v, ok := delta["reasoning_content"].(string); ok {
					reasoning += v
				}
				if tcs, ok := delta["tool_calls"].([]any); ok {
					for _, tc := range tcs {
						if call, ok := tc.(map[string]any); ok {
							toolCalls = append(toolCalls, call)
						}
					}
				}
			}
			if v, ok := choice["finish_reason"].(string); ok && v != "" {
				finish = v
			}
		}
	}
	// 上游中途断流时 scanner 会静默停止：不检查会把**截断的正文**当成完整
	// 回答返回（finish_reason 还会补成 stop），下游无从分辨。这里按失败上报，
	// 调用方在写出任何响应之前就能改写成 OpenAI 错误体。
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取上游流失败: %w", err)
	}

	message := map[string]any{"role": firstNonEmpty(role, "assistant"), "content": content}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	result := map[string]any{
		"id":      firstNonEmpty(respID, "chatcmpl-workbuddy"),
		"object":  "chat.completion",
		"created": created,
		"model":   firstNonEmpty(respModel, model),
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": firstNonEmpty(finish, "stop"),
		}},
	}
	if usage != nil {
		normalizeUsageCache(usage)
		result["usage"] = usage
	}
	out, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// usageTypesFrom 返回 usage 各字段的 JSON 类型（如 prompt_cache_hit_tokens=string）。
// 归一化必须输出裸数字，而网关的正则不认带引号的数 —— 有这个一眼就能看出类型问题。
func usageTypesFrom(usage map[string]any) map[string]string {
	out := make(map[string]string, len(usage))
	for k, v := range usage {
		switch v.(type) {
		case string:
			out[k] = "string"
		case float64, float32, int, int64, json.Number:
			out[k] = "number"
		case bool:
			out[k] = "bool"
		case nil:
			out[k] = "null"
		case []any:
			out[k] = "array"
		case map[string]any:
			out[k] = "object"
		default:
			out[k] = "unknown"
		}
	}
	return out
}

// usageKeysFrom 返回 usage 对象的字段名（排序），用于诊断上游上报了哪些字段。
func usageKeysFrom(usage map[string]any) []string {
	out := make([]string, 0, len(usage))
	for k := range usage {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// usageReportsCache 判断一个原始 usage 是否真的带了缓存命中字段（含各种别名）。
func usageReportsCache(usage map[string]any) bool {
	if usage == nil {
		return false
	}
	has := func(m map[string]any) bool {
		for _, k := range cacheHitKeys {
			if v, ok := m[k]; ok && !isEmptyValue(v) {
				return true
			}
		}
		return false
	}
	if has(usage) {
		return true
	}
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		return has(details)
	}
	return false
}

// observeUsage 记录最近一次上游 usage 的字段快照（传**未经清洗**的原始 chunk）。
// 目的：回答"上游到底有没有上报缓存命中"这类问题 —— 不需要额外探测请求，
// 一次正常调用即可从 /api/workbuddy/status 的 upstreamUsage 看到真相。
func (s *Service) observeUsage(rawChunk string) {
	if !strings.Contains(rawChunk, `"usage"`) {
		return
	}
	var obj map[string]any
	if json.Unmarshal([]byte(rawChunk), &obj) != nil {
		return
	}
	usage, ok := obj["usage"].(map[string]any)
	if !ok || len(usage) == 0 {
		return
	}
	raw, _ := json.Marshal(usage)
	s.usageMu.Lock()
	s.usageKeys = usageKeysFrom(usage)
	s.usageSample = truncate(string(raw), 512)
	s.usageReportsCache = usageReportsCache(usage)
	s.usageTypes = usageTypesFrom(usage)
	s.usageAt = time.Now().UTC().Format(time.RFC3339)
	s.usageMu.Unlock()
}

// upstreamUsageInfo 返回最近一次上游 usage 快照；尚无调用时返回 nil。
func (s *Service) upstreamUsageInfo() map[string]interface{} {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	if s.usageAt == "" {
		return nil
	}
	return map[string]interface{}{
		"keys":          s.usageKeys,
		"sample":        s.usageSample,
		"types":         s.usageTypes,
		"cacheReported": s.usageReportsCache,
		"at":            s.usageAt,
	}
}

// -----------------------------------------------------------------------------
// 纯函数适配器
// -----------------------------------------------------------------------------

// rewriteBlockedTemplates 对上游逐字拉黑的两句 Claude Code 固定 system 模板做最小改写。
// 上游是**精确匹配**（非语义审核），改一个词即可绕过且保持语义不变。
// 属于 cat-and-mouse：腾讯新增模板句时需在此扩展。
func rewriteBlockedTemplates(s string) string {
	s = strings.ReplaceAll(s,
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Claude Code, Anthropic's official CLI tool for Claude.")
	s = strings.ReplaceAll(s,
		"Main branch (you will usually use this for PRs)",
		"Default branch (you will usually use this for PRs)")
	return s
}

// forceMaxThinking 对混元系列（hy3 / hy4 前缀）强制 reasoning_effort=high。
// 上游只对 high 真正开启深度思考，medium / max / xhigh 等档位被直接忽略，
// 因此这里覆盖客户端传入的任何设置。返回是否发生了改动。
func forceMaxThinking(model string, payload map[string]any) bool {
	if !strings.HasPrefix(model, "hy3") && !strings.HasPrefix(model, "hy4") {
		return false
	}
	if eff, _ := payload["reasoning_effort"].(string); eff == "high" {
		return false
	}
	payload["reasoning_effort"] = "high"
	return true
}

// rewriteForUpstream 在转发前改写请求体：清理 messages 里被拉黑的模板句
// （字符串与多模态数组两种形态），并按模型覆盖思考档位。
// 任一步骤失败或无需改动时返回原 body，绝不因改写失败而阻断转发。
func rewriteForUpstream(body []byte, model string) []byte {
	if len(body) == 0 {
		return body
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	changed := false
	if messages, ok := payload["messages"].([]any); ok {
		for _, m := range messages {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			switch c := msg["content"].(type) {
			case string:
				if r := rewriteBlockedTemplates(c); r != c {
					msg["content"] = r
					changed = true
				}
			case []any:
				for _, p := range c {
					part, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if t, ok := part["text"].(string); ok {
						if r := rewriteBlockedTemplates(t); r != t {
							part["text"] = r
							changed = true
						}
					}
				}
			}
		}
	}
	if model == "" {
		model, _ = payload["model"].(string)
	} else if cur, _ := payload["model"].(string); cur != model {
		// 调用方传的是**剥离插件前缀后**的真实模型名，必须写回 body ——
		// 否则上游会收到带插件前缀的名字（如 wb-hy3）而报未知模型。
		// 注意 hy3/hy4 的思考档位判定依赖这个 model，写回与判定用同一个值。
		payload["model"] = model
		changed = true
	}
	if forceMaxThinking(model, payload) {
		changed = true
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// cacheHitKeys 是各家上游对「缓存命中 token」的常见命名。网关只认标准形状
// usage.prompt_tokens_details.cached_tokens（见 internal/openai/service.go 的
// cachedTokensRegex 与 relay.go 对 prompt_tokens_details 的解析），
// 因此这里把别名归一到标准字段，避免"上游明明报了、统计却是 0"。
//
// CodeBuddy 用的是 DeepSeek 风格命名：`prompt_cache_hit_tokens`（命中）/
// `prompt_cache_miss_tokens`（未命中），另带 `credit`（本次扣费）；
// 腾讯的 OpenAI 兼容协议文档还列了 `cache_read_tokens` / `cache_write_tokens`。
// 只归一「命中」语义的字段 —— `*_write_*` 是写入缓存、不是命中，不能算进命中率。
var cacheHitKeys = []string{
	"cached_tokens",           // OpenAI 标准（details 内层）
	"prompt_cache_hit_tokens", // DeepSeek / CodeBuddy 命名
	"cache_read_tokens",       // 腾讯 OpenAI 兼容协议文档命名
	"cache_read_input_tokens", // Anthropic 原生命名
	"cache_hit_tokens",
	"total_cached_tokens", // Gemini 原生命名
}

// normalizeUsageCache 把 usage 里的缓存命中字段归一并**同时写回顶层与 details**。
//
// 两个必须遵守的约束，都是被实测数据逼出来的：
//
//  1. **网关的正则取第一个匹配**。它用 `"cached_tokens"\s*:\s*(\d+)` +
//     FindStringSubmatch 扫描整段字节流，只认最先出现的那个。而 Go 序列化 map 是**按字母序**
//     排 key，`cached_tokens` 排在 `completion_tokens_details` / `prompt_tokens_details` 之前 ——
//     所以**顶层那个同名值就是最终被采信的值**，写到 details 里没用。
//
//  2. **上游会在顶层放一个恒为 0 的同名占位字段**。实测 CodeBuddy 的 usage 形如：
//     `{"cached_tokens":0, "prompt_cache_hit_tokens":131712,
//     "prompt_tokens_details":{"cached_tokens":129664}, "cache_read_input_tokens":0, ...}`
//     —— 顶层 `cached_tokens` 是占位 0，真值在别处。若不覆盖它，网关永远读到 0
//     （而插件账本读的是 details，于是出现"插件有值、网关为 0"的诡异不一致）。
//
// 取值规则（两段式，别退化成"一味取最大"）：
//  1. **优先 `prompt_tokens_details.cached_tokens`** —— 它与目标列同名、是 OpenAI 标准形状，
//     也是网关非流式解析路径读的字段。实测它与 DeepSeek 风格的 `prompt_cache_hit_tokens`
//     口径略有差异（131712 vs 129664，差 2048），不能混为一谈，所以标准字段优先。
//  2. 仅当它缺失或为 0 时，才在其余「命中语义」候选里取**最大值** ——
//     这样既跳过上游的占位 0，又能在标准字段确实没值时兜住真值。
//
// 最后把权威值**同时写入顶层与 details**：网关正则只认第一个匹配，而顶层键在字母序上更靠前，
// 不覆盖顶层就等于没改。
func normalizeUsageCache(usage map[string]any) {
	if usage == nil {
		return
	}
	details, hasDetails := usage["prompt_tokens_details"].(map[string]any)

	// 第一段：标准字段。
	var best any
	if hasDetails {
		if v, has := details["cached_tokens"]; has {
			if n, ok := numericValue(v); ok && !isEmptyValue(v) {
				best = n
			}
		}
	}

	// 第二段：其余候选取最大（占位 0 自然被跳过）。
	if best == nil {
		candidates := make([]any, 0, 8)
		collect := func(m map[string]any) {
			if m == nil {
				return
			}
			for _, k := range cacheHitKeys {
				if v, has := m[k]; has {
					if n, ok := numericValue(v); ok {
						candidates = append(candidates, n)
					}
				}
			}
		}
		collect(details)
		collect(usage)
		if len(candidates) == 0 {
			return
		}
		best = maxNumeric(candidates)
	}

	if !hasDetails {
		details = map[string]any{}
		usage["prompt_tokens_details"] = details
	}
	details["cached_tokens"] = best
	// 顶层同样写：它才是网关正则最先匹配到的那个。
	usage["cached_tokens"] = best
}

// maxNumeric 取候选里的最大数值（同为 int64 时按整数比较，混有小数时按 float64 比较）。
func maxNumeric(vals []any) any {
	bestInt := int64(0)
	bestFloat := 0.0
	for _, v := range vals {
		switch n := v.(type) {
		case int64:
			if n > bestInt {
				bestInt = n
			}
			if float64(n) > bestFloat {
				bestFloat = float64(n)
			}
		case float64:
			if n > bestFloat {
				bestFloat = n
			}
			if int64(n) > bestInt {
				bestInt = int64(n)
			}
		}
	}
	if bestFloat == float64(bestInt) {
		return bestInt
	}
	return bestFloat
}

// numericValue 把 JSON 值规范成「Go 会序列化成裸数字」的类型。
// 整数一律给 int64（避免 930560 被写成 9.3056e+05 这类形态），小数给 float64。
// 非数字（含非数字字符串、对象、数组）返回 false。
func numericValue(v any) (any, bool) {
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
		return n, true
	case float32:
		return numericValue(float64(n))
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		if f, err := n.Float64(); err == nil {
			return f, true
		}
		return nil, false
	case string:
		t := strings.TrimSpace(n)
		if t == "" {
			return nil, false
		}
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f, true
		}
		return nil, false
	}
	return nil, false
}

// cleanChunkJSON 清理 choice delta 里的空值字段（null / "" / [] / {}），
// 避免严格客户端被 {"function_call":null,"tool_calls":[]} 这类形状绊倒；
// 同时把 usage 里的缓存命中字段归一到标准形状。
func cleanChunkJSON(s string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(s), &obj) != nil {
		return s
	}
	if choices, ok := obj["choices"].([]any); ok {
		for _, c := range choices {
			choice, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if delta, ok := choice["delta"].(map[string]any); ok {
				for k, v := range delta {
					if isEmptyValue(v) {
						delete(delta, k)
					}
				}
			}
		}
	}
	if usage, ok := obj["usage"].(map[string]any); ok {
		normalizeUsageCache(usage)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return s
	}
	return string(out)
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// forceStreamBody 把请求体的 stream 置为 true（上游拒绝非流式对话）。
// forceStreamBody 把请求体改成上游能接受的形状：
//   - `stream: true`：上游对非流式对话直接回业务码 11101；
//   - `stream_options.include_usage: true`：让上游在流末回完整的 usage
//     （prompt/completion/total + `prompt_cache_hit_tokens` + `credit`）。
//     网关不会自己带这个参数（internal/openai 里搜不到 stream_options），
//     而缺少 usage 会让缓存命中与用量统计全部为 0，所以由本层补上。
//     已有的 stream_options 会被保留合并，不整体覆盖。
func forceStreamBody(body []byte) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	payload["stream"] = true
	if opts, ok := payload["stream_options"].(map[string]any); ok {
		opts["include_usage"] = true
	} else {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// stripDataPrefix 剥掉 SSE 行可能带的多层 "data:" 前缀。
func stripDataPrefix(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "data:") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "data:"))
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// decodeMessages 便捷构造：把 messages 数组包成最小请求体（供单测与调试）。
func decodeMessages(messages []any, model string) ([]byte, error) {
	return json.Marshal(map[string]any{"model": model, "messages": messages})
}

// bytesReader 便于在上游实现里复用。
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
