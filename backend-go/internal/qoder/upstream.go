package qoder

// 本文件是 Qoder 的「上游协议层」：设备登录（nonce + PKCE 轮询换取 token）、
// 加密 SSE 对话转发、以及官方 auth JSON 的解析与账号组装。
//
// 协议事实来自开源反代 qoder2api（jyao0708/qoder2api）：
//   - 设备登录：GET https://qoder.com/device/selectAccounts?nonce&challenge&... 引导
//     用户在浏览器完成授权；轮询 GET https://openapi.qoder.sh/api/v1/deviceToken/poll
//     拿 token（404 表示未授权，继续轮询）。
//   - 身份加密：把 name/aid/uid/... 用随机 tempKey（AES-CBC）加密得 info；tempKey 用
//     内置 RSA 公钥加密得 cosyKey；请求头 Authorization = "Bearer COSY."+payloadB64+"."+sig，
//     其中 payloadB64 是 {cosyVersion,info,...} 的 base64，sig = md5(payloadB64+cosyKey+date+body+path)。
//   - 对话：POST https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation，
//     body 经自定义字母表 base64 变换（legacyEncode），响应每行 data: {"body":"<openai chunk>"}。

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

const (
	challengeMethod = "S256"
	pollPath        = "/api/v1/deviceToken/poll"
	userInfoPath    = "/api/v1/userinfo"
	userStatusPath  = "/api/v3/user/status"
	userPlanPath    = "/api/v2/user/plan"

	// legacyCosyVersion 是对话请求头 cosy-version 的取值。
	legacyCosyVersion = "0.1.43"
	// legacyChatPath 是对话流式端点路径（不含 host；host 按区域派生）。
	legacyChatPath = "/algo/api/v2/service/pro/sse/agent_chat_generation"
	// legacyPathNoHost 是签名时使用的路径（不含 host）。
	legacyPathNoHost = "/api/v2/service/pro/sse/agent_chat_generation"
	// legacyServerPubKey 是包裹 tempKey 的内置 RSA 公钥。
	legacyServerPubKey = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc\n4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l\n6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17\nXcW+ML9FoCI6AOvOzwIDAQAB\n-----END PUBLIC KEY-----"

	// loginTTL 是一次设备登录会话的有效期。
	loginTTL = 10 * time.Minute
	// tokenExpiringWindow 是判定 access token「即将过期」的阈值。
	tokenExpiringWindow = 30 * time.Minute
)

// openAPIBaseOverride / loginBaseOverride / gatewayOverride 仅供测试把 openapi、
// 登录域与对话网关指向本地 mock；为空时按区域配置派生（生产路径不变）。
var (
	openAPIBaseOverride string
	loginBaseOverride   string
	gatewayOverride     string
)

// -----------------------------------------------------------------------------
// 数据模型
// -----------------------------------------------------------------------------

// Account 是一个 Qoder 设备登录凭据。
// 含 token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	// ID 是账号稳定标识，取上游 uid。
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	UserID   string `json:"userId,omitempty"`
	// Region 是账号所属区域：cn（国内版，默认）/ intl（国际版）。空值按国内版处理。
	Region string `json:"region,omitempty"`
	// Domain 是账号所属域名（如 www.qoder.com.cn），用于区域自证。
	Domain string `json:"domain,omitempty"`
	// AID / YXUID / OrgID / OrgName / UserType 是加密身份所需字段。
	AID     string `json:"aid,omitempty"`
	YXUID   string `json:"yxUid,omitempty"`
	OrgID   string `json:"orgId,omitempty"`
	OrgName string `json:"orgName,omitempty"`
	// UserType 是账号类型（默认 personal_standard）。
	UserType string `json:"userType,omitempty"`

	// SecurityOAuthToken 是设备登录换取的 securityOauthToken（Bearer COSY 用）。
	SecurityOAuthToken string `json:"securityOauthToken,omitempty"`
	// RefreshToken 用于（如上游支持）刷新 access token。
	RefreshToken string `json:"refreshToken,omitempty"`
	// AccessToken 是 securityOauthToken 的别名，供通用 ensureToken 逻辑判断有效期。
	AccessToken string `json:"accessToken,omitempty"`
	// ExpiresAt 是 access token 过期时刻（Unix 秒）。
	ExpiresAt int64 `json:"expiresAt,omitempty"`

	// Credits 是最近一次额度查询得到的剩余 Credits（签到/余额查询时回写）。
	Credits float64 `json:"credits,omitempty"`

	Disabled bool `json:"disabled,omitempty"`
	// CreatedAt / LastRefreshAt / LastCheckinAt 为 RFC3339（UTC）。
	CreatedAt     string `json:"createdAt,omitempty"`
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	// LastCheckinAt 是最近一次签到成功的时刻（RFC3339，UTC）。
	LastCheckinAt string `json:"lastCheckinAt,omitempty"`
	// LastError 记录最近一次上游失败原因，便于排障。
	LastError string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图：不含任何 token。
type AccountView struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	UserID   string `json:"userId,omitempty"`
	// Region 是账号所属区域：cn / intl（前端据此显示区域徽标）。
	Region           string  `json:"region,omitempty"`
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
// 否则用共享客户端。
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
// 设备登录
// -----------------------------------------------------------------------------

// loginState 是一次进行中的设备登录会话。
type loginState struct {
	loginID         string
	region          string
	nonce           string
	verifier        string
	challengeMethod string
	machineToken    string
	machineType     string
	expires         time.Time

	mu     sync.Mutex
	done   bool
	acc    Account
	errMsg string
}

func (l *loginState) setResult(acc Account, errMsg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return
	}
	l.acc, l.errMsg, l.done = acc, errMsg, true
}

func (l *loginState) takeResult() (Account, string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.done {
		return Account{}, "", false
	}
	return l.acc, l.errMsg, true
}

// loginResponse 是 login/start 的返回。
type loginResponse struct {
	LoginID         string `json:"loginId"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int64  `json:"expiresIn"`
}

// deviceTokenResult 是 deviceToken/poll 返回的 token 数据。
type deviceTokenResult struct {
	Token                 string `json:"token"`
	UserID                string `json:"user_id"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresAt             string `json:"expires_at"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at"`
}

// startLogin 创建设备登录会话，返回验证 URL 与 loginID。region 决定走国内版还是国际版。
func (s *Service) startLogin(region string) (loginResponse, error) {
	region = normalizeRegion(region)
	cfg := regionConfigFor(region)
	// nonce：国内版带横线 UUID，国际版 32 位 hex。
	nonce, err := randomNonce(cfg.nonceDashed)
	if err != nil {
		return loginResponse{}, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	verifier, err := generatePKCEVerifier()
	if err != nil {
		return loginResponse{}, fmt.Errorf("生成 PKCE verifier 失败: %w", err)
	}
	challenge := generatePKCEChallenge(verifier)
	loginID, err := randomSimpleUUID()
	if err != nil {
		return loginResponse{}, fmt.Errorf("生成 login_id 失败: %w", err)
	}

	u, _ := url.Parse(s.loginBaseFor(region))
	q := u.Query()
	q.Set("nonce", nonce)
	q.Set("challenge", challenge)
	q.Set("challenge_method", challengeMethod)
	q.Set("client_id", cfg.clientID)
	q.Set("machine_id", randomUUID())
	if cfg.sendRedirect && cfg.redirectURI != "" {
		q.Set("redirect_uri", cfg.redirectURI)
	}
	u.RawQuery = q.Encode()

	lst := &loginState{
		loginID:         loginID,
		region:          region,
		nonce:           nonce,
		verifier:        verifier,
		challengeMethod: challengeMethod,
		expires:         time.Now().Add(loginTTL),
	}
	s.loginMu.Lock()
	s.loginStates[loginID] = lst
	s.loginMu.Unlock()

	return loginResponse{LoginID: loginID, VerificationURI: u.String(), ExpiresIn: int64(loginTTL.Seconds())}, nil
}

// pollLogin 查询一次登录会话；未完成返回 done=false，完成后组装账号。
func (s *Service) pollLogin(ctx context.Context, loginID string) (Account, bool, error) {
	s.loginMu.Lock()
	lst := s.loginStates[loginID]
	s.loginMu.Unlock()
	if lst == nil {
		return Account{}, false, fmt.Errorf("登录会话不存在或已过期")
	}
	if time.Now().After(lst.expires) {
		s.forgetLoginState(loginID)
		return Account{}, false, fmt.Errorf("登录会话已过期，请重新发起")
	}
	if acc, errMsg, done := lst.takeResult(); done {
		s.forgetLoginState(loginID)
		if errMsg != "" {
			return Account{}, false, fmt.Errorf("%s", errMsg)
		}
		return acc, true, nil
	}

	// 单次轮询：404 表示未授权，继续等待。
	tok, err := pollDeviceToken(ctx, s.httpClientFor(""), s.openAPIBaseFor(lst.region), lst.nonce, lst.verifier, lst.challengeMethod)
	if err != nil {
		return Account{}, false, err
	}
	if tok == nil {
		return Account{}, false, nil
	}
	acc, err := s.buildAccountFromDeviceToken(ctx, lst.region, tok)
	if err != nil {
		lst.setResult(Account{}, err.Error())
		return Account{}, false, err
	}
	lst.setResult(acc, "")
	s.forgetLoginState(loginID)
	return acc, true, nil
}

// forgetLoginState 丢弃已完成的登录会话。
func (s *Service) forgetLoginState(loginID string) {
	s.loginMu.Lock()
	delete(s.loginStates, loginID)
	s.loginMu.Unlock()
}

// pollDeviceToken 轮询设备授权；未授权返回 (nil, nil)。
func pollDeviceToken(ctx context.Context, client *http.Client, openAPIBase, nonce, verifier, challengeMethod string) (*deviceTokenResult, error) {
	u, _ := url.Parse(openAPIBase + pollPath)
	q := u.Query()
	q.Set("nonce", nonce)
	q.Set("verifier", verifier)
	q.Set("challenge_method", challengeMethod)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("轮询 device token 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("轮询 device token 失败: status=%d, body=%s", resp.StatusCode, truncate(string(body), 200))
	}
	var result deviceTokenResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 device token 响应失败: %w", err)
	}
	if strings.TrimSpace(result.Token) == "" {
		return nil, nil
	}
	return &result, nil
}

// buildAccountFromDeviceToken 用 device token 拉取用户信息并组装账号。
func (s *Service) buildAccountFromDeviceToken(ctx context.Context, region string, tok *deviceTokenResult) (Account, error) {
	region = normalizeRegion(region)
	base := s.openAPIBaseFor(region)
	client := s.httpClientFor("")
	userInfo, _ := fetchOpenAPIJSON(ctx, client, base, userInfoPath, map[string]string{
		"Authorization": "Bearer " + tok.Token,
	})
	userStatus, err := fetchOpenAPIJSON(ctx, client, base, userStatusPath, map[string]string{
		"Authorization":   "Bearer " + tok.Token,
		"Cosy-ClientType": "0",
	})
	if err != nil {
		// 国际版没有 user/status（返回 404）；降级为只依赖 userinfo。
		if region == regionIntl {
			userStatus = map[string]any{}
		} else {
			return Account{}, fmt.Errorf("获取用户状态失败: %w", err)
		}
	}
	if err := checkWhitelistStatus(userStatus); err != nil {
		return Account{}, err
	}
	userPlan, _ := fetchOpenAPIJSON(ctx, client, base, userPlanPath, map[string]string{
		"Authorization": "Bearer " + tok.Token,
	})

	uid := firstNonEmpty(strValue(userStatus["id"]), tok.UserID, strValue(userInfo["id"]))
	name := firstNonEmpty(strValue(userStatus["name"]), strValue(userInfo["name"]), strValue(userInfo["email"]))
	acc := Account{
		ID:                 uid,
		Region:             region,
		Domain:             regionConfigFor(region).website,
		Nickname:           name,
		UserID:             uid,
		AID:                firstNonEmpty(strValue(userInfo["aid"]), uid),
		YXUID:              strValue(userStatus["yxUid"]),
		OrgID:              firstNonEmpty(strValue(userStatus["orgId"]), strValue(userInfo["orgId"])),
		OrgName:            firstNonEmpty(strValue(userStatus["orgName"]), strValue(userInfo["orgName"])),
		UserType:           firstNonEmpty(strValue(userStatus["userType"]), "personal_standard"),
		SecurityOAuthToken: tok.Token,
		AccessToken:        tok.Token,
		RefreshToken:       tok.RefreshToken,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	_ = userPlan
	acc.ExpiresAt = parseExpireTime(tok.ExpiresAt)
	return acc, nil
}

// refreshAccessToken 用 refresh_token 换取新的 device token 并回写账号。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("账号缺少 refresh_token，需重新登录")
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": acc.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.openAPIBaseFor(acc.Region)+"/api/v1/deviceToken/refresh", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("refresh_token 已失效，需重新登录")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("刷新 token 失败: HTTP %d", resp.StatusCode)
	}
	var tok struct {
		Token        string `json:"token"`
		DeviceToken  string `json:"device_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return fmt.Errorf("解析刷新响应失败: %w", err)
	}
	token := firstNonEmpty(tok.Token, tok.DeviceToken)
	if token == "" {
		return fmt.Errorf("刷新响应缺少 token")
	}
	acc.AccessToken = token
	acc.SecurityOAuthToken = token
	if tok.RefreshToken != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresAt != "" {
		acc.ExpiresAt = parseExpireTime(tok.ExpiresAt)
	} else if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	acc.LastError = ""
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
	return s.refreshAccessToken(ctx, acc)
}

// fetchOpenAPIJSON 发 GET 并解析 JSON 对象。base 是区域 openapi 基址。
func fetchOpenAPIJSON(ctx context.Context, client *http.Client, base, path string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("status=%d, body=%s", resp.StatusCode, truncate(string(body), 200))
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// checkWhitelistStatus 校验用户状态里的白名单/禁用标记。
// 空 map（国际版无 user/status）直接放行。
func checkWhitelistStatus(userStatus map[string]any) error {
	if len(userStatus) == 0 {
		return nil
	}
	if strValue(userStatus["id"]) == "" {
		return fmt.Errorf("用户状态缺少 id，无法确认登录身份")
	}
	switch strValue(userStatus["whitelistStatus"]) {
	case "NoIpPermission":
		return fmt.Errorf("企业设置了 IP 白名单，当前 IP 无法登录")
	case "AppDisable":
		return fmt.Errorf("Qoder 应用已被停用，无法登录")
	case "LoginExpire":
		return fmt.Errorf("Qoder 登录已失效，请重试")
	case "NotAllow", "NOT_ALLOW":
		return fmt.Errorf("当前账号暂无 Qoder 使用权限")
	}
	return nil
}

// -----------------------------------------------------------------------------
// 身份加密与签名
// -----------------------------------------------------------------------------

// legacySession 是一次对话会话所需的加密身份。
type legacySession struct {
	Identity     Account
	TempKey      []byte
	CosyKey      string
	Info         string
	MachineID    string
	MachineToken string
	MachineType  string
}

// newLegacySession 为账号生成一次会话加密身份。
func newLegacySession(acc Account) (legacySession, error) {
	tempKey := []byte(randomHex(16))
	cosyKey, err := legacyRSAEncrypt(tempKey)
	if err != nil {
		return legacySession{}, err
	}
	info, err := legacyEncryptInfo(acc, tempKey)
	if err != nil {
		return legacySession{}, err
	}
	return legacySession{
		Identity:     acc,
		TempKey:      tempKey,
		CosyKey:      cosyKey,
		Info:         info,
		MachineID:    randomUUID(),
		MachineToken: randomMachineToken(),
		MachineType:  randomHex(18),
	}, nil
}

// legacyEncryptInfo 把身份字段 AES-CBC 加密并 base64。
func legacyEncryptInfo(acc Account, key []byte) (string, error) {
	payload := map[string]string{
		"name":                 firstNonEmpty(acc.Nickname, acc.ID),
		"aid":                  firstNonEmpty(acc.AID, acc.ID),
		"uid":                  acc.ID,
		"yx_uid":               acc.YXUID,
		"organization_id":      acc.OrgID,
		"organization_name":    acc.OrgName,
		"user_type":            firstNonEmpty(acc.UserType, "personal_standard"),
		"security_oauth_token": firstNonEmpty(acc.SecurityOAuthToken, acc.AccessToken),
		"refresh_token":        acc.RefreshToken,
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain = pkcs5Pad(plain, block.BlockSize())
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key).CryptBlocks(out, plain)
	return base64.StdEncoding.EncodeToString(out), nil
}

// legacyRSAEncrypt 用内置公钥加密 tempKey。
func legacyRSAEncrypt(plain []byte) (string, error) {
	block, _ := pem.Decode([]byte(legacyServerPubKey))
	if block == nil {
		return "", fmt.Errorf("invalid public key")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("unexpected public key type")
	}
	out, err := rsa.EncryptPKCS1v15(rand.Reader, pub, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// legacyBuildPayloadB64 构造请求头里的 payload（base64）。
func legacyBuildPayloadB64(info string) (string, error) {
	payload := struct {
		CosyVersion string `json:"cosyVersion"`
		IdeVersion  string `json:"ideVersion"`
		Info        string `json:"info"`
		RequestID   string `json:"requestId"`
		Version     string `json:"version"`
	}{
		CosyVersion: legacyCosyVersion,
		Info:        info,
		RequestID:   randomUUID(),
		Version:     "v1",
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// legacySignRequest 计算签名 md5(payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + path)。
func legacySignRequest(payloadB64, cosyKey, cosyDate, body, path string) string {
	sum := md5.Sum([]byte(payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + path))
	return fmt.Sprintf("%x", sum[:])
}

// legacyEncode 用自定义字母表对 body 做 base64 变换（与官方客户端一致）。
func legacyEncode(plaintext []byte) string {
	const stdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	const customAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
	const customPad = '$'

	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3
	rearranged := std[n-a:] + std[a:n-a] + std[:a]

	lookup := make(map[byte]byte, 65)
	for i := 0; i < 64; i++ {
		lookup[stdAlphabet[i]] = customAlphabet[i]
	}
	lookup['='] = customPad

	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = lookup[rearranged[i]]
	}
	return string(out)
}

func pkcs5Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, 0, len(data)+padding)
	out = append(out, data...)
	for i := 0; i < padding; i++ {
		out = append(out, byte(padding))
	}
	return out
}

// -----------------------------------------------------------------------------
// 对话：加密请求 + SSE 解析
// -----------------------------------------------------------------------------

// buildLegacyRequest 把 OpenAI 请求体映射成 Qoder 上游请求体。
func buildLegacyRequest(acc Account, rawBody []byte) (map[string]any, error) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return nil, fmt.Errorf("请求体解析失败: %w", err)
	}
	body := newLegacyTemplate()
	requestID := randomUUID()
	body["request_id"] = requestID
	body["chat_record_id"] = requestID
	body["request_set_id"] = randomUUID()
	body["session_id"] = randomUUID()
	body["stream"] = true
	body["aliyun_user_type"] = firstNonEmpty(acc.UserType, "personal_standard")

	modelKey := normalizeModelKey(req.Model)
	if mc, ok := body["model_config"].(map[string]any); ok {
		mc["key"] = modelKey
	}
	prompt := latestPrompt(req.Messages)
	if biz, ok := body["business"].(map[string]any); ok {
		biz["id"] = randomUUID()
		biz["begin_at"] = time.Now().UnixMilli()
		if len(prompt) > 30 {
			biz["name"] = prompt[:30]
		} else {
			biz["name"] = prompt
		}
	}
	if ctx, ok := body["chat_context"].(map[string]any); ok {
		if text, ok := ctx["text"].(map[string]any); ok {
			text["text"] = prompt
		}
		if extra, ok := ctx["extra"].(map[string]any); ok {
			if original, ok := extra["originalContent"].(map[string]any); ok {
				original["text"] = prompt
			}
			if modelConfig, ok := extra["modelConfig"].(map[string]any); ok {
				modelConfig["key"] = modelKey
			}
		}
	}
	body["messages"] = legacyMessages(req.Messages)
	if len(bytes.TrimSpace(req.Tools)) > 0 && !rawJSONIsEmpty(req.Tools) {
		var tools any
		if err := json.Unmarshal(req.Tools, &tools); err == nil {
			body["tools"] = tools
		}
	}
	return body, nil
}

// legacyMessageInput 是 buildLegacyRequest 里解析出的消息形状。
type legacyMessageInput struct {
	Role       string
	Content    json.RawMessage
	ToolCallID string
	ToolCalls  json.RawMessage
}

// latestPrompt 取最后一条 user 消息的文本。
func latestPrompt(messages []struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			continue
		}
		if text := normalizeContent(messages[i].Content); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// legacyMessages 把消息转成 Qoder 的 messages 形状。
func legacyMessages(messages []struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		role := firstNonEmpty(m.Role, "user")
		switch role {
		case "user":
			contents := legacyUserContents(m.Content)
			if len(contents) == 0 {
				continue
			}
			out = append(out, map[string]any{
				"role":     "user",
				"content":  "",
				"contents": contents,
			})
		case "assistant":
			entry := map[string]any{"role": "assistant", "content": normalizeContent(m.Content)}
			if !rawJSONIsEmpty(m.ToolCalls) {
				var v any
				if json.Unmarshal(m.ToolCalls, &v) == nil {
					entry["tool_calls"] = v
				}
			}
			out = append(out, entry)
		case "tool":
			text := normalizeContent(m.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": text})
		default:
			text := normalizeContent(m.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, map[string]any{"role": role, "content": text})
		}
	}
	return out
}

// legacyUserContents 把 user content 归一化为 Qoder 的 contents 数组。
func legacyUserContents(content json.RawMessage) []map[string]any {
	var s string
	if json.Unmarshal(content, &s) == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": s}}
	}
	var arr []map[string]any
	if json.Unmarshal(content, &arr) == nil {
		out := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			switch strValue(item["type"]) {
			case "text":
				text := strValue(item["text"])
				if strings.TrimSpace(text) == "" {
					continue
				}
				out = append(out, map[string]any{"type": "text", "text": text})
			case "image_url":
				imageURL := firstNonEmpty(strValue(item["image_url"]), nestedString(item["image_url"], "url"))
				if strings.TrimSpace(imageURL) == "" {
					continue
				}
				out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}})
			case "file":
				fileURL := firstNonEmpty(strValue(item["file_url"]), strValue(item["data"]))
				if strings.TrimSpace(fileURL) == "" {
					continue
				}
				out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": fileURL}})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	text := normalizeContent(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []map[string]any{{"type": "text", "text": text}}
}

// normalizeContent 把 content（string 或 parts 数组）压平成文本。
func normalizeContent(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var arr []map[string]any
	if json.Unmarshal(content, &arr) == nil {
		var parts []string
		for _, item := range arr {
			switch strValue(item["type"]) {
			case "text":
				parts = append(parts, strValue(item["text"]))
			case "image_url":
				if u := firstNonEmpty(strValue(item["image_url"]), nestedString(item["image_url"], "url")); u != "" {
					parts = append(parts, "[image] "+u)
				}
			case "file":
				parts = append(parts, renderFilePart(item))
			}
		}
		return strings.Join(parts, "\n")
	}
	return strings.TrimSpace(string(content))
}

// renderFilePart 把 file part 渲染成一行描述。
func renderFilePart(part map[string]any) string {
	name := firstNonEmpty(strValue(part["filename"]), strValue(part["file_name"]), strValue(part["file_id"]), strValue(part["file_url"]))
	mime := firstNonEmpty(strValue(part["mime_type"]), "application/octet-stream")
	fields := []string{"[file]"}
	if name != "" {
		fields = append(fields, "name="+name)
	}
	if mime != "" {
		fields = append(fields, "mime="+mime)
	}
	if u := strValue(part["file_url"]); u != "" {
		fields = append(fields, "url="+u)
	}
	return strings.Join(fields, " ")
}

// normalizeModelKey 归一化模型 key。
func normalizeModelKey(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "auto", "default":
		return "auto"
	case "lite":
		return "lite"
	case "claude-sonnet-4.5", "claude_sonnet_4_5", "sonnet", "claude":
		return "auto"
	default:
		return strings.TrimSpace(model)
	}
}

// newLegacyTemplate 返回 Qoder 请求模板（字段照抄官方客户端）。
func newLegacyTemplate() map[string]any {
	return map[string]any{
		"stream":    true,
		"chat_task": "FREE_INPUT",
		"chat_context": map[string]any{
			"chatPrompt": "",
			"extra": map[string]any{
				"context": []any{},
				"modelConfig": map[string]any{
					"is_reasoning": false,
					"key":          "auto",
				},
				"originalContent": map[string]any{"type": "text", "text": ""},
			},
			"features":  []any{},
			"imageUrls": nil,
			"text":      map[string]any{"type": "text", "text": ""},
		},
		"image_urls":       nil,
		"is_reply":         true,
		"is_retry":         false,
		"code_language":    "",
		"source":           1,
		"version":          "3",
		"chat_prompt":      "",
		"parameters":       map[string]any{"max_tokens": 32768},
		"aliyun_user_type": "personal_standard",
		"session_type":     "qodercli",
		"agent_id":         "agent_common",
		"task_id":          "common",
		"model_config": map[string]any{
			"key":              "auto",
			"display_name":     "Auto",
			"model":            "",
			"format":           "openai",
			"is_vl":            false,
			"is_reasoning":     false,
			"api_key":          "",
			"url":              "",
			"source":           "system",
			"max_input_tokens": 180000,
		},
		"business": map[string]any{
			"id":          "",
			"begin_at":    time.Now().UnixMilli(),
			"scene":       "chat",
			"type":        "chat",
			"sub_type":    "free_input",
			"name":        "",
			"language":    "",
			"parent_type": "",
		},
		"messages": []any{},
		"tools":    defaultLegacyTools(),
	}
}

func defaultLegacyTools() []any {
	return []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Read",
				"description": "Read a local file or image attachment referenced in the conversation.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"file_path": map[string]any{"type": "string", "description": "Path of the file to read."}},
					"required":   []any{"file_path"},
				},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Bash",
				"description": "Execute a shell command when the client allows command execution.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"command": map[string]any{"type": "string", "description": "Command to execute."}},
					"required":   []any{"command"},
				},
			},
		},
	}
}

// ChatStream 发对话请求并返回原始 SSE body 流（调用方负责 Close）。
// 非 2xx 时 rc 为 nil、status 为上游状态码；只有传输层失败才返回 err。
func (s *Service) ChatStream(ctx context.Context, acc Account, body []byte) (rc io.ReadCloser, status int, lastBody []byte, err error) {
	reqBody, err := buildLegacyRequest(acc, body)
	if err != nil {
		return nil, 0, nil, err
	}
	plainPayload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, nil, err
	}
	payload := []byte(legacyEncode(plainPayload))

	session, err := newLegacySession(acc)
	if err != nil {
		return nil, 0, nil, err
	}
	cosyDate := fmt.Sprintf("%d", time.Now().Unix())
	payloadB64, err := legacyBuildPayloadB64(session.Info)
	if err != nil {
		return nil, 0, nil, err
	}
	sig := legacySignRequest(payloadB64, session.CosyKey, cosyDate, string(payload), legacyPathNoHost)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.chatStreamURLFor(acc.Region), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("cosy-data-policy", "AGREE")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("authorization", "Bearer COSY."+payloadB64+"."+sig)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("cosy-clientip", "169.254.198.161")
	req.Header.Set("cosy-date", cosyDate)
	req.Header.Set("cosy-key", session.CosyKey)
	req.Header.Set("cosy-machineid", session.MachineID)
	req.Header.Set("cosy-machinetoken", session.MachineToken)
	req.Header.Set("cosy-machinetype", session.MachineType)
	req.Header.Set("cosy-user", acc.ID)
	req.Header.Set("cosy-version", legacyCosyVersion)
	req.Header.Set("login-version", "v2")
	req.Header.Set("user-agent", "Go-http-client/2.0")
	req.Header.Set("x-model-key", normalizeModelKey(modelOf(body)))
	req.Header.Set("x-model-source", "system")

	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		applog.Warn(ctx, "qoder", "chat stream transport error", "uid", acc.ID, "error", err.Error())
		return nil, 0, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, resp.StatusCode, raw, nil
	}
	return resp.Body, resp.StatusCode, nil, nil
}

// modelOf 从 OpenAI 请求体读 model。
func modelOf(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return m.Model
}

// aggregateSSE 读取完整 Qoder SSE 流，聚合为单个 OpenAI chat.completion 响应。
// 每行形如 data: {"body":"<openai chunk>"}；body 里是标准 OpenAI chunk。
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
				if chunk := decodeLegacyChunk(payload); chunk != nil {
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
					consumeChoices(chunk, &content, &reasoning, &role, &finishReason, &gotAnyContent, toolCalls, &toolOrder)
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

// decodeLegacyChunk 解析一行 data 载荷：外层 {body:"..."}，内层是 OpenAI chunk。
func decodeLegacyChunk(payload string) map[string]any {
	var envelope struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil || envelope.Body == "" {
		// 兼容直接是 OpenAI chunk 的情况。
		var direct map[string]any
		if json.Unmarshal([]byte(payload), &direct) == nil {
			if _, ok := direct["choices"]; ok {
				return direct
			}
		}
		return nil
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(envelope.Body), &chunk); err != nil {
		return nil
	}
	return chunk
}

// consumeChoices 累积一个 chunk 的 choices。
func consumeChoices(chunk map[string]any, content, reasoning *strings.Builder, role *string, finishReason *string, gotAnyContent *bool, toolCalls map[int]map[string]any, toolOrder *[]int) {
	ch, ok := chunk["choices"].([]any)
	if !ok {
		return
	}
	for _, ci := range ch {
		c, _ := ci.(map[string]any)
		if c == nil {
			continue
		}
		if fr, ok := c["finish_reason"].(string); ok && fr != "" {
			*finishReason = fr
		}
		if delta, ok := c["delta"].(map[string]any); ok {
			if r2, ok := delta["role"].(string); ok && r2 != "" {
				*role = r2
			}
			if txt, ok := delta["content"].(string); ok {
				content.WriteString(txt)
				*gotAnyContent = true
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
						*toolOrder = append(*toolOrder, idx)
					}
					mergeToolCallDelta(merged, call)
				}
			}
		}
		if msg, ok := c["message"].(map[string]any); ok && !*gotAnyContent {
			if txt, ok := msg["content"].(string); ok {
				content.WriteString(txt)
			}
		}
	}
}

// mergeToolCallDelta 合并流式 tool_call 片段。
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

// streamAndCapture 把上游 Qoder SSE 解包成标准 OpenAI chunk 写往 w（每行 flush），
// 末尾保证至少一个 data: [DONE]。上游每行形如
//
//	data: {"headers":...,"body":"<openai chunk>","statusCodeValue":200}
//
// 需要取出 body（内层 OpenAI chunk）重新封装，网关才认。
func streamAndCapture(r io.Reader, w http.ResponseWriter, flusher http.Flusher, onChunk func(map[string]any)) error {
	if flusher == nil {
		flusher, _ = w.(http.Flusher)
	}
	br := bufio.NewReaderSize(r, 64*1024)
	sawDone := false
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload == "[DONE]" {
				sawDone = true
				if _, werr := io.WriteString(w, "data: [DONE]\n\n"); werr != nil {
					return werr
				}
				if flusher != nil {
					flusher.Flush()
				}
			} else if payload != "" {
				if chunk := decodeLegacyChunk(payload); chunk != nil {
					if onChunk != nil {
						onChunk(chunk)
					}
					out, merr := json.Marshal(chunk)
					if merr == nil {
						if _, werr := fmt.Fprintf(w, "data: %s\n\n", out); werr != nil {
							return werr
						}
						if flusher != nil {
							flusher.Flush()
						}
					}
				}
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
// 小工具
// -----------------------------------------------------------------------------

func rawJSONIsEmpty(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null" || trimmed == "[]"
}

func strValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func nestedString(v any, key string) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return strValue(obj[key])
}

func sortInts(a []int) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// parseExpireTime 把上游过期时间（RFC3339 或毫秒时间戳字符串）转成 Unix 秒；失败返回 0。
func parseExpireTime(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.Unix()
	}
	var num int64
	if _, err := fmt.Sscanf(raw, "%d", &num); err == nil && num > 0 {
		if num < 1_000_000_000_000 {
			return num
		}
		return num / 1000
	}
	return 0
}

func generatePKCEVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generatePKCEChallenge(verifier string) string {
	// 与 qoder2api 一致：challenge = base64url(sha256(verifier))。
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytesToUint32(b[0:4]), bytesToUint16(b[4:6]), bytesToUint16(b[6:8]), bytesToUint16(b[8:10]), bytesToUint48(b[10:16]))
}

// randomNonce 生成设备登录 nonce：dashed=true 时返回带横线的 UUID（国内版），
// 否则返回 32 位小写 hex（国际版，与官方客户端 Uuid::new_v4().simple() 一致）。
func randomNonce(dashed bool) (string, error) {
	if dashed {
		return randomUUID(), nil
	}
	return randomSimpleUUID()
}

// randomSimpleUUID 生成 32 位小写 hex（与官方客户端 Uuid::new_v4().simple() 一致）。
func randomSimpleUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x%04x%04x%04x%012x",
		bytesToUint32(b[0:4]), bytesToUint16(b[4:6]), bytesToUint16(b[6:8]), bytesToUint16(b[8:10]), bytesToUint48(b[10:16])), nil
}

func randomHex(length int) string {
	if length <= 0 {
		return ""
	}
	max := new(big.Int).Lsh(big.NewInt(1), uint(length*4))
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return fmt.Sprintf("%0*x", length, 0)
	}
	return fmt.Sprintf("%0*x", length, n)
}

func randomMachineToken() string {
	raw := randomUUID() + randomUUID()
	if len(raw) > 50 {
		raw = raw[:50]
	}
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func bytesToUint16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func bytesToUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func bytesToUint48(b []byte) uint64 {
	var out uint64
	for _, x := range b {
		out = (out << 8) | uint64(x)
	}
	return out
}
