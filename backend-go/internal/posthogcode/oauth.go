package posthogcode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// PostHog Desktop（代号 "Array"/"Code"）的 OAuth 应用 client_id。PostHog 的
// llm_gateway:read 是 privileged scope，只有白名单内的 OAuth 应用能获得；
// 这些 client_id 是公开值（硬编码在 Desktop 源码中），非机密。
const (
	clientIDUS = "HCWoE0aRFMYxIxFNTTwkOORn5LBjOt2GVDzwSw5W"
	clientIDEU = "AIvijgMS0dxKEmr5z6odvRd8Pkh5vts3nPTzgzU9"
)

// 请求的 scope。
//   - llm_gateway:read 是访问模型网关的必需项（privileged scope，只能由 OAuth 白名单应用获得）。
//   - project:read 让网关在解析额度时能读取组织的 quota_limits（否则 fail open，用量字段为 null）。
//   - openid/profile/email 用于解析账号身份。
//
// Desktop 应用的 ceiling 为 @default（含以上全部），故这些都在可授予范围内。
const oauthScope = "openid profile email llm_gateway:read project:read"

// oauthTTL 是待完成授权会话的有效期。
const oauthTTL = 30 * time.Minute

// defaultRedirectURI 是 PostHog OAuth 应用注册过的回调地址。
// PostHog Desktop 应用注册的是 http://localhost/callback（不带端口）；按
// RFC 8252 §7.3，localhost 允许任意端口，因此这里保留不带端口的写法。
//
// 关键：redirect_uri 是 OAuth 应用的注册属性，与面板部署在哪个域名无关，
// 必须由服务端固定。若按前端所在源（window.location.origin）推导，任何非
// localhost 的部署都会得到 "Mismatching redirect URI"。
const defaultRedirectURI = "http://localhost/callback"

// envRedirectURI 允许部署方自带 OAuth 应用时覆盖回调地址。
const envRedirectURI = "POSTHOGCODE_REDIRECT_URI"

// callbackRedirectURI 返回授权与换码统一使用的 redirect_uri。
// 两处必须完全一致，否则 token 端点会以 invalid_grant 拒绝。
func callbackRedirectURI() string {
	if v := strings.TrimSpace(os.Getenv(envRedirectURI)); v != "" {
		return v
	}
	return defaultRedirectURI
}

// tokenRequestTimeout 是 token 端点请求超时。
const tokenRequestTimeout = 30 * time.Second

// oauthState 是暂存待完成授权的 PKCE 会话。
type oauthState struct {
	verifier    string
	region      string
	redirectURI string
	createdAt   time.Time
}

// cloudBaseURL 返回指定区域的 PostHog 主站地址。
func cloudBaseURL(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "eu") {
		return "https://eu.posthog.com"
	}
	return "https://us.posthog.com"
}

// gatewayBaseURL 返回指定区域的 LLM 网关地址。
func gatewayBaseURL(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "eu") {
		return "https://gateway.eu.posthog.com"
	}
	return "https://gateway.us.posthog.com"
}

// clientIDForRegion 返回指定区域的 OAuth client_id。
func clientIDForRegion(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "eu") {
		return clientIDEU
	}
	return clientIDUS
}

// productSlug 归一化产品路径段，仅允许 posthog_code。
// 其他产品（llm_gateway / ci / django 等）要么不对 OAuth 应用开放，
// 要么不接受 OAuth 鉴权，故此处收敛到唯一可用值。
func productSlug(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "posthog_code") {
		return "posthog_code"
	}
	return "posthog_code"
}

// b64url 做无填充的 base64url 编码。
func b64url(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

// randomVerifier 生成 PKCE code_verifier（32 字节随机）。
func randomVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b64url(buf), nil
}

// randomState 生成 OAuth state（16 字节随机）。
func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b64url(buf), nil
}

// codeChallenge 由 verifier 计算 S256 challenge。
func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return b64url(sum[:])
}

// cleanupOAuthStates 清理过期授权会话。
func (s *Service) cleanupOAuthStates() {
	now := time.Now()
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	for k, v := range s.oauthStates {
		if v == nil || now.Sub(v.createdAt) > oauthTTL {
			delete(s.oauthStates, k)
		}
	}
}

// buildAuthorizeURL 构造授权链接，并把 PKCE verifier 存入待完成会话。
// redirectURI 由调用方给定（前端所在源的回调地址）。
func (s *Service) buildAuthorizeURL(region, redirectURI string) (authURL, state string, err error) {
	verifier, err := randomVerifier()
	if err != nil {
		return "", "", err
	}
	st, err := randomState()
	if err != nil {
		return "", "", err
	}
	u, err := url.Parse(cloudBaseURL(region) + "/oauth/authorize/")
	if err != nil {
		return "", "", err
	}
	q := u.Query()
	q.Set("client_id", clientIDForRegion(region))
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("code_challenge", codeChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("scope", oauthScope)
	q.Set("state", st)
	q.Set("required_access_level", "project")
	u.RawQuery = q.Encode()

	s.cleanupOAuthStates()
	s.oauthMu.Lock()
	s.oauthStates[st] = &oauthState{verifier: verifier, region: region, redirectURI: redirectURI, createdAt: time.Now()}
	s.oauthMu.Unlock()

	return u.String(), st, nil
}

// takeOAuthState 取出并删除待完成会话。
func (s *Service) takeOAuthState(state string) *oauthState {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	v := s.oauthStates[state]
	delete(s.oauthStates, state)
	return v
}

// tokenResponse 是 /oauth/token 的响应。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// postToken 向 PostHog token 端点发起一次表单请求。
func postToken(ctx context.Context, region string, form url.Values) (*tokenResponse, error) {
	body := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cloudBaseURL(region)+"/oauth/token", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: tokenRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 token 端点失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("token 端点返回 %d: %s", resp.StatusCode, msg)
	}
	var out tokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("token 响应缺少 access_token")
	}
	return &out, nil
}

// exchangeCode 用授权码换取 token。
func exchangeCode(ctx context.Context, region, code, verifier, redirectURI string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientIDForRegion(region))
	form.Set("code_verifier", verifier)
	return postToken(ctx, region, form)
}

// refreshToken 用 refresh token 换新 token（PostHog 会轮换 refresh token）。
func refreshToken(ctx context.Context, region, token string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", token)
	form.Set("client_id", clientIDForRegion(region))
	return postToken(ctx, region, form)
}

// userInfo 是 /oauth/userinfo/ 的响应子集。
type userInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// fetchUserInfo 用 access token 解析账号身份。
func fetchUserInfo(ctx context.Context, region, accessToken string) (*userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cloudBaseURL(region)+"/oauth/userinfo/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: tokenRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 userinfo 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo 返回 %d", resp.StatusCode)
	}
	var out userInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 userinfo 失败: %w", err)
	}
	return &out, nil
}

// accountIDFor 为账号生成稳定标识：优先用户 uuid，退回邮箱。
func accountIDFor(info *userInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(info.Sub, info.Email))
}

// fetchDefaultProjectID 查询账号可访问的第一个项目 ID。
// 额度端点按项目组织，故授权后需要记录一个项目 ID；失败时返回空串，
// 由前端或后续调用按需重试，不阻断授权。
func fetchDefaultProjectID(ctx context.Context, region, accessToken string) string {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		cloudBaseURL(region)+"/api/projects/", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var parsed struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Results) == 0 {
		return ""
	}
	return strconv.Itoa(parsed.Results[0].ID)
}

// ensureFreshToken 确保账号的 access token 可用：未过期直接返回；
// 已过期/临近过期时用 refresh token 换新并回写（含轮换后的 refresh token）。
// 落库失败不阻断本次请求，但轮换后的 token 必须成功写回，否则下次刷新会失效。
func (s *Service) ensureFreshToken(ctx context.Context, acc *Account) (string, error) {
	if strings.TrimSpace(acc.AccessToken) != "" && acc.ExpiresAt > time.Now().Unix()+int64(tokenExpiringWindow.Seconds()) {
		return acc.AccessToken, nil
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return "", fmt.Errorf("账号 %s token 已过期且无 refresh token", acc.Email)
	}
	// 刷新必须走账号自己的区域主站：跨区 token 在另一端不认。
	region := s.accountRegion(*acc)
	tok, err := refreshToken(ctx, region, acc.RefreshToken)
	if err != nil {
		return "", err
	}
	expiresIn := tok.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 604800
	}
	acc.AccessToken = tok.AccessToken
	if strings.TrimSpace(tok.RefreshToken) != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	acc.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
	acc.LastError = ""
	if err := s.upsertAccount(ctx, *acc); err != nil {
		// 落库失败仍返回可用 token，但记录原因供排障。
		acc.LastError = "token 已刷新但落库失败: " + err.Error()
	}
	return acc.AccessToken, nil
}

// accountCandidate 是一个通过硬性过滤、可供选号策略挑选的账号。
// idx 是它在完整账号列表中的下标，轮询用它锚定推进位置。
type accountCandidate struct {
	idx int
	acc Account
}

// quotaSnapshotTTL 是额度快照的有效期。超期视为失效（least-used 按 0 处理），
// 避免基于几天前的陈旧数据选号。
const quotaSnapshotTTL = 2 * time.Hour

// pickAccount 按当前策略选取本次转发的账号。
//
// 三种策略（Settings.AccountStrategy）：
//   - first：列表首个可用账号，其余作主备。行为最可预期。
//   - round-robin：从上次位置继续向后轮询，请求均匀分摊。
//   - least-used：选剩余额度最多的账号，按实际额度拉平消耗；
//     额度快照来自用量查询与转发后的刷新，缺失时按 0 处理（退化为列表序）。
//
// 三种策略都跳过：不可用（停用/过期）、本次已尝试、处于冷却期的账号。
// 若可用账号全在冷却中，退回「冷却最早结束」的那个继续尝试 ——
// 冷却只是软偏好，不该把一次瞬时抖动放大成整池不可用。
func (s *Service) pickAccount(model string, tried map[string]bool) (Account, bool) {
	accounts := s.Settings().Accounts
	strategy := normalizeStrategy(s.Settings().AccountStrategy)

	// 先收集所有符合硬条件的候选（保持列表序，并记下原始下标）。
	usable := make([]accountCandidate, 0, len(accounts))
	for i, a := range accounts {
		if !accountAvailable(a) {
			continue
		}
		if tried != nil && tried[a.ID] {
			continue
		}
		if s.inCooldown(a.ID) {
			continue
		}
		usable = append(usable, accountCandidate{idx: i, acc: a})
	}

	if len(usable) > 0 {
		switch strategy {
		case strategyRoundRobin:
			return s.pickRoundRobin(usable), true

		case strategyLeastUsed:
			return s.pickLeastUsed(usable), true

		default: // strategyFirst
			return usable[0].acc, true
		}
	}

	// 全部在冷却中：退回最早恢复的账号。
	bestIdx := -1
	var bestUntil time.Time
	for i, a := range accounts {
		if !accountAvailable(a) {
			continue
		}
		if tried != nil && tried[a.ID] {
			continue
		}
		until, cooled := s.cooldownUntilOf(a.ID)
		if !cooled {
			continue
		}
		if bestIdx < 0 || until.Before(bestUntil) {
			bestIdx, bestUntil = i, until
		}
	}
	if bestIdx >= 0 {
		return accounts[bestIdx], true
	}
	return Account{}, false
}

// recordQuotaSnapshot 更新某账号的剩余额度快照，供 least-used 策略选号。
// 剩余额度 = 上限 - 已用；任一侧缺失时记 0（该账号在 least-used 下劣后，
// 但不会被排除，额度数据恢复后自然回到排序中）。
func (s *Service) recordQuotaSnapshot(accountID string, used, limit *float64) {
	if accountID == "" {
		return
	}
	remain := 0.0
	if used != nil && limit != nil {
		if r := *limit - *used; r > 0 {
			remain = r
		}
	}
	s.quotaMu.Lock()
	if s.quotaSnap == nil {
		s.quotaSnap = map[string]float64{}
	}
	s.quotaSnap[accountID] = remain
	s.quotaAt = time.Now()
	s.quotaMu.Unlock()
}

// quotaSnapshotAt 返回最近一次额度快照的时间，供前端提示数据新鲜度。
func (s *Service) quotaSnapshotAt() time.Time {
	s.quotaMu.RLock()
	defer s.quotaMu.RUnlock()
	return s.quotaAt
}

// quotaRefreshMinInterval 是单账号额度快照的最小刷新间隔。
// 高频转发时用它做节流，避免每个请求都去打上游的额度接口。
const quotaRefreshMinInterval = 2 * time.Minute

// maybeRefreshQuotaSnapshot 在需要时异步刷新单个账号的额度快照。
// 仅在 least-used 策略下生效；同一账号在 quotaRefreshMinInterval 内只刷新一次。
func (s *Service) maybeRefreshQuotaSnapshot(acc Account) {
	if normalizeStrategy(s.Settings().AccountStrategy) != strategyLeastUsed {
		return
	}
	// 节流：该账号最近刚刷新过就跳过。
	s.quotaMu.RLock()
	last := s.quotaRefreshedAt[acc.ID]
	s.quotaMu.RUnlock()
	if time.Since(last) < quotaRefreshMinInterval {
		return
	}
	// 先占位，避免并发请求同时触发同一账号的刷新。
	s.quotaMu.Lock()
	if s.quotaRefreshedAt == nil {
		s.quotaRefreshedAt = map[string]time.Time{}
	}
	s.quotaRefreshedAt[acc.ID] = time.Now()
	s.quotaMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		s.collectOneAccountUsage(ctx, acc)
	}()
}

// pickRoundRobin 轮询选取。
//
// 游标锚定**完整账号列表的下标**而非候选集下标：候选集长度会随冷却、停用、
// 本次已尝试而变化，若用候选集下标取模，集合缩短时游标会回绕到前面，
// 导致部分账号被反复选中、另一些被跳过。
// 用原始下标则始终「从上次位置继续向后找下一个可用者」，与候选集变化无关。
func (s *Service) pickRoundRobin(usable []accountCandidate) Account {
	s.rrMu.Lock()
	defer s.rrMu.Unlock()

	chosen := usable[0]
	if s.rrInit {
		// 找列表序中「上次位置之后」的第一个候选；找不到则回绕到最前的候选。
		for _, c := range usable {
			if c.idx > s.rrLastIdx {
				chosen = c
				break
			}
		}
	}
	s.rrLastIdx = chosen.idx
	s.rrInit = true
	return chosen.acc
}

// pickLeastUsed 选剩余额度最多的账号。
//
// 额度快照由用量查询、后台刷新与转发成功后的扣减共同维护。快照超过
// quotaSnapshotTTL 未更新则视为失效（记 0），避免基于陈旧数据决策。
func (s *Service) pickLeastUsed(usable []accountCandidate) Account {
	s.quotaMu.RLock()
	defer s.quotaMu.RUnlock()

	stale := time.Since(s.quotaAt) > quotaSnapshotTTL
	remainOf := func(id string) float64 {
		if stale {
			return 0
		}
		return s.quotaSnap[id]
	}

	best := usable[0]
	bestRemain := remainOf(best.acc.ID)
	for _, c := range usable[1:] {
		if remain := remainOf(c.acc.ID); remain > bestRemain {
			best, bestRemain = c, remain
		}
	}
	return best.acc
}
func (s *Service) refreshStaleAccounts(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if a.Disabled || a.RefreshToken == "" || tokenState(a) == "valid" {
			continue
		}
		acc := a
		if _, err := s.ensureFreshToken(ctx, &acc); err != nil {
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
		}
	}
}

// StartAutoRefresh 启动 access token 自动刷新，并顺带刷新额度快照。
// 额度快照供 least-used 策略选号，故与 token 刷新同周期维护。
func (s *Service) StartAutoRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshStaleAccounts(context.Background())
				s.refreshQuotaSnapshots(context.Background())
			case <-ctx.Done():
				return
			}
		}
	}()
}

// refreshQuotaSnapshots 刷新所有账号的额度快照。
// 仅在 least-used 策略下有意义，其余策略直接跳过以免无谓打上游。
func (s *Service) refreshQuotaSnapshots(ctx context.Context) {
	if normalizeStrategy(s.Settings().AccountStrategy) != strategyLeastUsed {
		return
	}
	accounts := s.Settings().Accounts
	if len(accounts) == 0 {
		return
	}
	s.collectAccountUsage(ctx, accounts)
}
