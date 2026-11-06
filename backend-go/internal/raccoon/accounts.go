package raccoon

// 本文件实现账号管理：脱敏视图、token 状态（access_token ~2h + refresh_token ~30d）、
// 账号 CRUD/导入导出、选号与失败冷却，以及 access token 自动刷新（refresh 会轮换）。

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

// 上游基址常量（来自 xhx2api/raccoon2api 协议记录）。
const (
	baseOrigin = "https://xiaohuanxiong.com"
	llmBaseURI = baseOrigin + "/api/web/llm/v2"
	authBase   = baseOrigin + "/api/web/auth/v1"
	pointsBase = baseOrigin + "/api/web/points/v1"
)

// tokenExpiringWindow 是 access token 临期自动刷新的提前量（约 2h 有效，提前 5 分钟换新）。
const tokenExpiringWindow = 5 * time.Minute

// cooldownDuration 是账号瞬时故障（429/5xx/网络）后的冷却时长。
const cooldownDuration = 60 * time.Second

// autoRefreshInterval 是后台自动刷新的扫描周期。
const autoRefreshInterval = 5 * time.Minute

// maxBodyBytes 是对话/导入等请求体大小上限。
const maxBodyBytes = 64 << 20

// Account 是一个小浣熊账号凭据。
// 含 token 与 refresh token，属敏感数据：只在服务端内部流转，下发前端一律走 AccountView。
type Account struct {
	ID           string `json:"id"`
	Nickname     string `json:"nickname,omitempty"`
	UserID       string `json:"userId,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
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
// 无凭据、被停用或 access token 已过期都视为不可用；
// ExpiresAtMS 缺失（tokenState = unknown）仍视为可用，避免误伤缺少过期时刻的旧凭据。
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

// handleAddAccount 手动新增账号：body {nickname, access_token(必填), refresh_token, enabled}。
func (s *Service) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname     string `json:"nickname"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	token := strings.TrimSpace(body.AccessToken)
	if token == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 access_token"})
		return
	}
	acc := Account{
		AccessToken:  token,
		RefreshToken: strings.TrimSpace(body.RefreshToken),
		Nickname:     strings.TrimSpace(body.Nickname),
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
		Disabled bool `json:"disabled"`
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
	acc.Disabled = body.Disabled
	if !body.Disabled {
		acc.LastError = ""
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleTestAccount 对指定账号做一次连通性测试：用当前 token 拉一次模型目录作为探针。
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
	token := strings.TrimSpace(acc.AccessToken)
	if token == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "账号没有 access_token"})
		return
	}
	if err := s.ensureToken(ctx, &acc); err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	n, err := s.fetchCatalogProbe(ctx, acc.AccessToken)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": n})
}

// handleAccountBalance 查询单账号积分余额。
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
	// 强制刷新时跳过缓存（?refresh=1）。
	force := r.URL.Query().Get("refresh") == "1"
	if !force {
		if bal, ok := s.cachedPoints(acc.ID); ok {
			responseJSON(w, map[string]interface{}{"success": true, "balance": bal, "cached": true})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	bal, err := s.fetchBalance(ctx, acc.AccessToken)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.storePoints(acc.ID, bal)
	responseJSON(w, map[string]interface{}{"success": true, "balance": bal})
}

// cachedPoints 读取未过期的积分快照。
func (s *Service) cachedPoints(id string) (Balance, bool) {
	s.pointsMu.Lock()
	defer s.pointsMu.Unlock()
	snap, ok := s.pointsCache[id]
	if !ok || time.Since(snap.at) > pointsCacheTTL {
		return Balance{}, false
	}
	return snap.balance, true
}

// storePoints 写入积分快照。
func (s *Service) storePoints(id string, bal *Balance) {
	if bal == nil {
		return
	}
	s.pointsMu.Lock()
	if s.pointsCache == nil {
		s.pointsCache = map[string]pointsSnapshot{}
	}
	s.pointsCache[id] = pointsSnapshot{balance: *bal, at: time.Now()}
	s.pointsMu.Unlock()
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
		fmt.Sprintf("attachment; filename=raccoon-accounts-%s.json", time.Now().UTC().Format("20060102")))
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
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[a.ID] = true
	}
	added := 0
	for _, a := range body {
		if strings.TrimSpace(a.AccessToken) == "" {
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
//
// 策略：first（列表首个可用，其余主备）/ round-robin（依次轮询）。
// 都跳过：不可用（停用/过期/无 token）、本次已尝试、处于冷却期的账号。
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

// ensureToken 保证账号有可用的 access token：临期/过期时用 refresh_token 换新。
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
// 使用新值，否则会拿旧 refresh token 再刷一次（上游拒绝），失败路径还可能把
// 旧值写回、覆盖掉新值，导致账号永久续不上。
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
//
// force=false 时先重新读取库内副本：若已被并发请求刷新到可用状态，直接返回，
// 避免重复消费已经轮换的 refresh token。force=true 用于上游返回 401 后的强制重刷。
func (s *Service) refreshTokenLocked(ctx context.Context, acc *Account, force bool) (bool, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if !force {
		s.reloadAccountTokens(acc)
		if tokenState(*acc) == "valid" {
			return false, nil
		}
	}
	// 账号可能已被并发删除：不要用旧副本把它重新写回来。
	if id := strings.TrimSpace(acc.ID); id != "" {
		if _, ok := s.findAccount(id); !ok {
			return false, fmt.Errorf("账号 %s 不存在", id)
		}
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		if strings.TrimSpace(acc.AccessToken) == "" {
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
			// 落库失败不阻断本次请求（内存里已是新 token），但必须留痕：
			// refresh token 已轮换，丢失新值会让该账号后续无法续期。
			applog.Warn(ctx, "raccoon", "persist refreshed token failed", "account", acc.ID, "error", err.Error())
		}
	}
	return changed, nil
}

// markAccountError 只把 LastError 写回库内最新副本，避免用本次请求的旧 token
// 覆盖并发刷新得到的新 token。
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

// refreshAccessToken 用 refresh_token 换取新的 access/refresh token（refresh 会轮换）。
func (s *Service) refreshAccessToken(ctx context.Context, acc *Account) error {
	if acc == nil || strings.TrimSpace(acc.RefreshToken) == "" {
		return fmt.Errorf("缺少 refresh_token")
	}
	access, newRefresh, err := s.upstreamRefresh(ctx, acc.RefreshToken)
	if err != nil {
		return err
	}
	before := acc.AccessToken
	acc.AccessToken = access
	if newRefresh != "" {
		acc.RefreshToken = newRefresh
	}
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	if exp := jwtExpiryMS(access); exp > 0 {
		acc.ExpiresAtMS = exp
	}
	if strings.TrimSpace(acc.UserID) == "" {
		acc.UserID = jwtUserID(access)
	}
	if acc.AccessToken != before {
		acc.LastError = ""
	}
	return nil
}

// StartAutoRefresh 启动 access token 自动刷新：定期把临期/过期的账号提前换新。
// ctx 取消时退出。
func (s *Service) StartAutoRefresh(ctx context.Context) {
	s.autoRefreshOnce.Do(func() {
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
	})
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
