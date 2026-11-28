package cline

// 本文件实现账号管理：脱敏视图、token 状态、CRUD/导入导出、选号与失败冷却，
// 以及 access token 自动续期（refresh token 会轮换）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// 上游基址。
const (
	apiBaseURL    = "https://api.cline.bot/api/v1"
	workosBaseURL = "https://api.workos.com"
	// workosClientID 是 Cline 在 WorkOS 注册的应用标识（取自已登录 JWT 的 iss）。
	workosClientID = "client_01K3A541FN8TA3EPPHTD2325AR"
	// tokenPrefix 是 access token 的必需前缀（去掉它上游 401）。
	tokenPrefix = "workos:"
	// clientType 是免费池的硬门槛（缺失时免费池模型一律 403）。
	clientType = "cline-sdk"
)

// tokenExpiringWindow 是 access token 临期自动刷新的提前量（约 1 小时有效，提前 10 分钟换新）。
const tokenExpiringWindow = 10 * time.Minute

// cooldownDuration 是账号瞬时故障（429/5xx/网络）后的冷却时长。
const cooldownDuration = 60 * time.Second

// autoRefreshInterval 是后台自动刷新的扫描周期。
const autoRefreshInterval = 5 * time.Minute

// maxBodyBytes 是对话/导入等请求体大小上限。
const maxBodyBytes = 64 << 20

// Account 是一个 Cline 账号凭据。
// 含 token 与 refresh token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	ID string `json:"id"`
	// Nickname 是展示名（邮箱或自定义备注）。
	Nickname string `json:"nickname,omitempty"`
	// UserID 是 Cline 的 usr- 前缀用户 id。
	UserID string `json:"userId,omitempty"`
	// Email 是 WorkOS 用户邮箱（用于展示与去重）。
	Email string `json:"email,omitempty"`
	// AccessToken 含 `workos:` 前缀，原样发给上游。
	AccessToken string `json:"accessToken,omitempty"`
	// RefreshToken 用于续期（服务端一次性轮换语义）。
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAtMS 是 access token 过期时刻（毫秒 Unix 时间戳）；0 表示未知。
	ExpiresAtMS int64  `json:"expiresAtMS,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	// LastRefreshAt 为 RFC3339（UTC）。
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图：不含任何 token。
type AccountView struct {
	ID               string `json:"id"`
	Nickname         string `json:"nickname,omitempty"`
	UserID           string `json:"userId,omitempty"`
	Email            string `json:"email,omitempty"`
	TokenTail        string `json:"tokenTail,omitempty"`
	HasRefresh       bool   `json:"hasRefresh"`
	Disabled         bool   `json:"disabled"`
	TokenState       string `json:"tokenState"`
	ExpiresInSeconds int64  `json:"expiresInSeconds,omitempty"`
	Available        bool   `json:"available"`
	Cooling          bool   `json:"cooling"`
	CooldownSeconds  int64  `json:"cooldownSeconds,omitempty"`
	CallCount        int64  `json:"callCount"`
	LastRefreshAt    string `json:"lastRefreshAt,omitempty"`
	LastError        string `json:"lastError,omitempty"`
}

func (s *Service) toAccountView(a Account) AccountView {
	view := AccountView{
		ID:            a.ID,
		Nickname:      a.Nickname,
		UserID:        a.UserID,
		Email:         a.Email,
		TokenTail:     tokenTail(a.AccessToken),
		HasRefresh:    strings.TrimSpace(a.RefreshToken) != "",
		Disabled:      a.Disabled,
		TokenState:    tokenState(a),
		Available:     accountAvailable(a),
		Cooling:       s.inCooldown(a.ID, time.Now()),
		CallCount:     s.callDisplay(a.ID),
		LastRefreshAt: a.LastRefreshAt,
		LastError:     a.LastError,
	}
	if until, cooled := s.cooldownUntilOf(a.ID, time.Now()); cooled {
		if left := int64(time.Until(until).Seconds()); left > 0 {
			view.CooldownSeconds = left
		}
	}
	if a.ExpiresAtMS > 0 {
		if left := (a.ExpiresAtMS - time.Now().UnixMilli()) / 1000; left > 0 {
			view.ExpiresInSeconds = left
		}
	}
	return view
}

// tokenState 依据过期时刻给出 token 状态。
func tokenState(a Account) string {
	if a.AccessToken == "" || a.ExpiresAtMS <= 0 {
		return "unknown"
	}
	left := time.Until(time.UnixMilli(a.ExpiresAtMS))
	if left <= 0 {
		return "expired"
	}
	if left < tokenExpiringWindow {
		return "expiring"
	}
	return "valid"
}

// accountAvailable 表示账号当前是否可用于转发。
func accountAvailable(a Account) bool {
	if a.Disabled || a.ID == "" || a.AccessToken == "" {
		return false
	}
	return tokenState(a) != "expired"
}

// tokenTail 返回 token 最后 4 位（供前端显示，不泄露完整 token）。
func tokenTail(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	runes := []rune(token)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

// ensureTokenPrefix 给裸 JWT 补上 workos: 前缀（续期返回的令牌不带前缀）。
func ensureTokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, tokenPrefix) {
		return token
	}
	return tokenPrefix + token
}

// stripTokenPrefix 去掉 workos: 前缀（仅在需要裸 JWT 时使用）。
func stripTokenPrefix(token string) string {
	return strings.TrimPrefix(strings.TrimSpace(token), tokenPrefix)
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 管理账号列表：GET 列出，POST 手动新增（粘贴 token）。
func (s *Service) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		st := s.Settings()
		out := make([]AccountView, 0, len(st.Accounts))
		for _, a := range st.Accounts {
			out = append(out, s.toAccountView(a))
		}
		responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
	case http.MethodPost:
		s.handleAddAccount(w, r)
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// handleAddAccount 手动新增账号：body {nickname, email, access_token(必填), refresh_token}。
func (s *Service) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname     string `json:"nickname"`
		Email        string `json:"email"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	token := ensureTokenPrefix(body.AccessToken)
	if stripTokenPrefix(token) == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 access_token"})
		return
	}
	acc := Account{
		AccessToken:  token,
		RefreshToken: strings.TrimSpace(body.RefreshToken),
		Nickname:     strings.TrimSpace(body.Nickname),
		Email:        strings.TrimSpace(body.Email),
		ExpiresAtMS:  jwtExpiryMS(token),
		UserID:       jwtUserID(token),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if body.Enabled != nil {
		acc.Disabled = !*body.Enabled
	}
	acc.ID = deriveAccountID(acc)
	if _, exists := s.findAccount(acc.ID); exists {
		responseJSON(w, http.StatusConflict, map[string]interface{}{"success": false, "error": "账号已存在：" + acc.ID})
		return
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "account": s.toAccountView(acc)})
}

// deriveAccountID 由账号信息生成稳定 ID：优先 user-<userId>，否则 acct-<随机 hex>。
func deriveAccountID(acc Account) string {
	if uid := strings.TrimSpace(acc.UserID); uid != "" {
		return "user-" + uid
	}
	if id := strings.TrimSpace(acc.ID); id != "" {
		return id
	}
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return "acct-" + hex.EncodeToString(buf)
}

func (s *Service) handleDeleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	id = strings.TrimSpace(id)
	if _, ok := s.findAccount(id); !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if err := s.removeAccount(r.Context(), id); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

func (s *Service) handleUpdateAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPut {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Nickname *string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if body.Nickname != nil {
		acc.Nickname = strings.TrimSpace(*body.Nickname)
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "account": s.toAccountView(acc)})
}

func (s *Service) handleToggleAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Disabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 disabled"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	acc.Disabled = *body.Disabled
	if !acc.Disabled {
		acc.LastError = ""
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleTestAccount 对指定账号做一次连通性测试：拉一次模型目录作为探针。
func (s *Service) handleTestAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	n, err := s.fetchCatalogProbe(ctx, acc.AccessToken)
	if err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.markAccountError(ctx, acc.ID, "")
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": n})
}

// handleAccountBalance 查询单账号余额（免费池贴 0 / 欠费是常态）。
func (s *Service) handleAccountBalance(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	bal, err := s.fetchBalance(ctx, acc.AccessToken)
	if err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "balance": bal})
}

// -----------------------------------------------------------------------------
// 账号导入导出
// -----------------------------------------------------------------------------

type exportPayload struct {
	Version    int       `json:"version"`
	ExportedAt string    `json:"exportedAt"`
	Accounts   []Account `json:"accounts"`
}

func (s *Service) handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=cline-accounts-%s.json", time.Now().UTC().Format("20060102")))
	_ = json.NewEncoder(w).Encode(exportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:   st.Accounts,
	})
}

func (s *Service) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	body, err := decodeImportBody(r)
	if err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if len(body) == 0 {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的账号"})
		return
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[a.ID] = true
	}
	added := 0
	for _, a := range body {
		a.AccessToken = ensureTokenPrefix(a.AccessToken)
		if stripTokenPrefix(a.AccessToken) == "" {
			continue
		}
		if strings.TrimSpace(a.UserID) == "" {
			a.UserID = jwtUserID(a.AccessToken)
		}
		if a.ExpiresAtMS <= 0 {
			a.ExpiresAtMS = jwtExpiryMS(a.AccessToken)
		}
		a.ID = deriveAccountID(a)
		if a.ID == "" || seen[a.ID] {
			continue
		}
		if a.CreatedAt == "" {
			a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		seen[a.ID] = true
		st.Accounts = append(st.Accounts, a)
		added++
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "added": added})
}

func decodeImportBody(r *http.Request) ([]Account, error) {
	var probe json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		return nil, fmt.Errorf("请求体解析失败")
	}
	trimmed := strings.TrimSpace(string(probe))
	if trimmed == "" {
		return nil, fmt.Errorf("请求体为空")
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []Account
		if err := json.Unmarshal(probe, &arr); err != nil {
			return nil, fmt.Errorf("账号解析失败: %w", err)
		}
		return arr, nil
	}
	var wrapper exportPayload
	if err := json.Unmarshal(probe, &wrapper); err != nil {
		return nil, fmt.Errorf("配置解析失败: %w", err)
	}
	return wrapper.Accounts, nil
}

// -----------------------------------------------------------------------------
// 选号与冷却
// -----------------------------------------------------------------------------

// pickAccount 按当前策略选取本次转发的账号。
func (s *Service) pickAccount(exclude map[string]bool) (Account, bool) {
	st := s.Settings()
	now := time.Now()
	strategy := normalizeStrategy(st.AccountStrategy)

	cands := make([]accountpick.Candidate, 0, len(st.Accounts))
	byID := make(map[string]Account, len(st.Accounts))
	for i, a := range st.Accounts {
		byID[a.ID] = a
		if exclude[a.ID] || !accountAvailable(a) || s.inCooldown(a.ID, now) {
			continue
		}
		cands = append(cands, accountpick.Candidate{ID: a.ID, Index: i})
	}
	if len(cands) > 0 {
		if strategy == accountpick.RoundRobin {
			if c, ok := s.rr.Pick(cands); ok {
				return byID[c.ID], true
			}
		} else {
			return byID[cands[0].ID], true
		}
	}
	return Account{}, false
}

func (s *Service) inCooldown(id string, now time.Time) bool {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	return ok && now.Before(until)
}

func (s *Service) setCooldown(id string, d time.Duration) {
	if id == "" || d <= 0 {
		return
	}
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	s.cooldownUntil[id] = time.Now().Add(d)
}

func (s *Service) clearCooldown(id string) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	delete(s.cooldownUntil, id)
}

func (s *Service) cooldownUntilOf(id string, now time.Time) (time.Time, bool) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	if !ok || !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// -----------------------------------------------------------------------------
// access token 自动刷新
// -----------------------------------------------------------------------------

// ensureToken 保证账号有可用的 access token：临期/过期时用 refresh token 换新。
func (s *Service) ensureToken(ctx context.Context, acc *Account) error {
	if tokenState(*acc) == "valid" {
		return nil
	}
	_, err := s.refreshTokenLocked(ctx, acc, false)
	return err
}

// reloadAccountTokens 用库里的最新副本覆盖本地 token 字段。
//
// 并发刷新时，先拿到锁的请求已经把轮换后的 refresh token 落库；后来者必须
// 使用新值，否则会拿旧 refresh token 再刷一次（上游拒绝）。
func (s *Service) reloadAccountTokens(acc *Account) {
	if acc == nil || strings.TrimSpace(acc.ID) == "" {
		return
	}
	latest, ok := s.findAccount(acc.ID)
	if !ok {
		return
	}
	acc.AccessToken = latest.AccessToken
	acc.RefreshToken = latest.RefreshToken
	acc.ExpiresAtMS = latest.ExpiresAtMS
	acc.LastRefreshAt = latest.LastRefreshAt
	acc.LastError = latest.LastError
	if strings.TrimSpace(acc.UserID) == "" {
		acc.UserID = latest.UserID
	}
}

// refreshTokenLocked 串行化账号 token 刷新，成功后立即落库。
func (s *Service) refreshTokenLocked(ctx context.Context, acc *Account, force bool) (bool, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if !force {
		s.reloadAccountTokens(acc)
		if tokenState(*acc) == "valid" {
			return false, nil
		}
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		if stripTokenPrefix(acc.AccessToken) == "" {
			return false, fmt.Errorf("账号 %s 没有 access_token", acc.ID)
		}
		return false, fmt.Errorf("账号 %s 的 access token 已过期且没有 refresh_token", acc.ID)
	}
	beforeAccess, beforeRefresh := acc.AccessToken, acc.RefreshToken
	if err := s.refreshAccessToken(ctx, acc); err != nil {
		return false, err
	}
	changed := acc.AccessToken != beforeAccess || acc.RefreshToken != beforeRefresh
	if changed {
		if err := s.upsertAccount(ctx, *acc); err != nil {
			applog.Warn(ctx, "cline", "persist refreshed token failed", "account", acc.ID, "error", err.Error())
		}
	}
	return changed, nil
}

// refreshAccessToken 用 refresh token 换新 access token。
//
// 协议要点（实测）：`grantType` 是 camelCase（不是 OAuth 标准的 grant_type）；
// 返回的 accessToken **不带** workos: 前缀，写入前必须补上；
// `expiresAt` 是 ISO 8601 字符串（不是毫秒数）。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if acc == nil || strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("缺少 refresh_token")
	}
	body, _ := json.Marshal(map[string]string{
		"refreshToken": acc.RefreshToken,
		"grantType":    "refresh_token",
	})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, data, err := s.doJSON(ctx, s.httpClientFor("refresh"), http.MethodPost,
		apiBaseURL+"/auth/refresh", body, "",
		map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, 0, maxErrorBytes)
	if err != nil {
		return fmt.Errorf("刷新 token 请求失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("刷新 token 失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Data struct {
			AccessToken  string          `json:"accessToken"`
			RefreshToken string          `json:"refreshToken"`
			ExpiresAt    json.RawMessage `json:"expiresAt"`
		} `json:"data"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("刷新 token 响应解析失败: %w", err)
	}
	access := strings.TrimSpace(resp.Data.AccessToken)
	if access == "" {
		return fmt.Errorf("刷新 token 失败: 上游未返回 accessToken")
	}
	before := acc.AccessToken
	acc.AccessToken = ensureTokenPrefix(access)
	if rt := strings.TrimSpace(resp.Data.RefreshToken); rt != "" {
		acc.RefreshToken = rt
	}
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	if exp := parseExpiresAt(resp.Data.ExpiresAt); exp > 0 {
		acc.ExpiresAtMS = exp
	} else if exp := jwtExpiryMS(acc.AccessToken); exp > 0 {
		acc.ExpiresAtMS = exp
	}
	if strings.TrimSpace(acc.UserID) == "" {
		acc.UserID = jwtUserID(acc.AccessToken)
	}
	if acc.AccessToken != before {
		acc.LastError = ""
	}
	return nil
}

// parseExpiresAt 解析续期响应里的 expiresAt（ISO 8601 字符串或毫秒数字）。
func parseExpiresAt(raw json.RawMessage) int64 {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0
	}
	// 数字形态：毫秒时间戳。
	if n, err := strconvParseInt(strings.Trim(text, `"`)); err == nil && n > 0 {
		if n < 1e12 {
			n *= 1000
		}
		return n
	}
	// 字符串形态：RFC3339。
	if t, err := time.Parse(time.RFC3339, strings.Trim(text, `"`)); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// markAccountError 只把 LastError 写回库内最新副本，避免覆盖并发刷新得到的新 token。
func (s *Service) markAccountError(ctx context.Context, id, message string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	acc, ok := s.findAccount(id)
	if !ok {
		return
	}
	acc.LastError = message
	_ = s.upsertAccount(ctx, acc)
}

// StartAutoRefresh 启动 access token 自动刷新；ctx 取消时退出。
func (s *Service) StartAutoRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(autoRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshStaleAccounts(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// refreshStaleAccounts 刷新所有「未停用、有 refresh token、临期或过期」的账号。
func (s *Service) refreshStaleAccounts(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if a.Disabled || strings.TrimSpace(a.RefreshToken) == "" {
			continue
		}
		if tokenState(a) == "valid" {
			continue
		}
		acc := a
		if _, err := s.refreshTokenLocked(ctx, &acc, false); err != nil {
			s.markAccountError(ctx, acc.ID, err.Error())
		}
	}
}
