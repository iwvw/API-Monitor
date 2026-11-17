package tokenharbor

// 本文件实现账号管理:脱敏视图、token 状态、CRUD/导入导出、选号与失败冷却,
// 以及 Supabase 会话的自动续期(refresh token 会轮换)。

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

// tokenExpiringWindow 是 access token 临期自动刷新的提前量(Supabase 会话约 1 小时,提前 10 分钟换新)。
const tokenExpiringWindow = 10 * time.Minute

// cooldownDuration 是账号瞬时故障(429/5xx/网络)后的冷却时长。
const cooldownDuration = 60 * time.Second

// autoRefreshInterval 是后台自动刷新的扫描周期。
const autoRefreshInterval = 5 * time.Minute

// maxBodyBytes 是对话/导入等请求体大小上限。
const maxBodyBytes = 64 << 20

// Account 是一个 TokenHarbor 账号凭据。
// Cookie 属敏感数据:只在服务端内部流转,下发前端一律走 AccountView。
type Account struct {
	ID string `json:"id"`
	// Nickname 是展示名(邮箱或自定义备注)。
	Nickname string `json:"nickname,omitempty"`
	// Email 是登录邮箱(用于展示与去重)。
	Email string `json:"email,omitempty"`
	// UserID 是 Supabase 用户 id(来自 access_token 的 sub),用于去重。
	UserID string `json:"userId,omitempty"`
	// Cookie 是完整 Cookie 头(含 sb-auth-auth-token.0 / th_sid 等),原样发给上游。
	Cookie string `json:"cookie,omitempty"`
	// ExpiresAtMS 是 access token 过期时刻(毫秒 Unix 时间戳);0 表示未知。
	ExpiresAtMS int64  `json:"expiresAtMS,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	// LastRefreshAt 为 RFC3339(UTC)。
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图:不含任何凭据。
type AccountView struct {
	ID               string `json:"id"`
	Nickname         string `json:"nickname,omitempty"`
	Email            string `json:"email,omitempty"`
	UserID           string `json:"userId,omitempty"`
	TokenTail        string `json:"tokenTail,omitempty"`
	HasCookie        bool   `json:"hasCookie"`
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
		Email:         a.Email,
		UserID:        a.UserID,
		TokenTail:     tokenTail(a.Cookie),
		HasCookie:     strings.TrimSpace(a.Cookie) != "",
		HasRefresh:    strings.TrimSpace(refreshTokenOf(a.Cookie)) != "",
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
	if strings.TrimSpace(a.Cookie) == "" || a.ExpiresAtMS <= 0 {
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
	if a.Disabled || a.ID == "" || strings.TrimSpace(a.Cookie) == "" {
		return false
	}
	if tokenState(a) == "expired" {
		// 已过期但持 refresh token 时仍可选中并即时续期。
		return strings.TrimSpace(refreshTokenOf(a.Cookie)) != ""
	}
	return true
}

// tokenTail 返回凭据最后 4 位(供前端显示,不泄露完整凭据)。
func tokenTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

// refreshTokenOf 从 Cookie 里取 refresh_token(RefreshToken 字段优先,便于续期后使用新值)。
func refreshTokenOf(cookie string) string {
	if rt := refreshTokenFromCookie(cookie); rt != "" {
		return rt
	}
	return ""
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 管理账号列表:GET 列出,POST 手动新增(粘贴 Cookie)。
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

// handleAddAccount 手动新增账号:body {nickname, email, cookie(必填)}。
func (s *Service) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
		Cookie   string `json:"cookie"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	cookie := normalizeCookie(body.Cookie)
	if cookie == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "未识别到 sb-auth-auth-token.0,请粘贴完整 Cookie"})
		return
	}
	acc, err := s.buildAccount(cookie, strings.TrimSpace(body.Nickname), strings.TrimSpace(body.Email))
	if err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if body.Enabled != nil {
		acc.Disabled = !*body.Enabled
	}
	if _, exists := s.findAccount(acc.ID); exists {
		responseJSON(w, http.StatusConflict, map[string]interface{}{"success": false, "error": "账号已存在:" + acc.ID})
		return
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "account": s.toAccountView(acc)})
}

// buildAccount 由 Cookie 组装账号:Cookie 非法时报错;ID 由用户 id 派生。
func (s *Service) buildAccount(cookie, nickname, email string) (Account, error) {
	if refreshTokenFromCookie(cookie) == "" && accessTokenFromCookie(cookie) == "" {
		return Account{}, fmt.Errorf("Cookie 不含有效会话(缺少 access_token / refresh_token)")
	}
	acc := Account{
		Cookie:    cookie,
		Nickname:  nickname,
		Email:     email,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.fillAccountDerived(&acc)
	acc.ID = deriveAccountID(acc)
	return acc, nil
}

// fillAccountDerived 从 Cookie 回填 userID / 过期时刻 / 邮箱。
func (s *Service) fillAccountDerived(acc *Account) {
	if acc.UserID == "" {
		acc.UserID = jwtSub(accessTokenFromCookie(acc.Cookie))
	}
	if acc.ExpiresAtMS <= 0 {
		acc.ExpiresAtMS = expiresAtMS(acc.Cookie)
	}
	if acc.Email == "" {
		acc.Email = emailFromCookie(acc.Cookie)
	}
}

// deriveAccountID 由账号信息生成稳定 ID:优先 user-<userId>,否则 acct-<随机 hex>。
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

// handleTestAccount 对指定账号做一次连通性测试:拉一次免费额度作为探针。
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
	if _, err := s.fetchFreeTier(ctx, acc.Cookie); err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.markAccountError(ctx, acc.ID, "")
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(s.catalog())})
}

// handleAccountBalance 查询单账号额度(免费层用量)。
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
	bal, err := s.fetchFreeTier(ctx, acc.Cookie)
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
		fmt.Sprintf("attachment; filename=tokenharbor-accounts-%s.json", time.Now().UTC().Format("20060102")))
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
		a.Cookie = normalizeCookie(a.Cookie)
		if a.Cookie == "" {
			continue
		}
		s.fillAccountDerived(&a)
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
// Supabase 会话自动刷新
// -----------------------------------------------------------------------------

// ensureToken 保证账号有可用的 access token:临期/过期时用 refresh token 换新。
func (s *Service) ensureToken(ctx context.Context, acc *Account) error {
	if tokenState(*acc) == "valid" {
		return nil
	}
	_, err := s.refreshTokenLocked(ctx, acc, false)
	return err
}

// reloadAccountTokens 用库里的最新副本覆盖本地 Cookie 字段。
func (s *Service) reloadAccountTokens(acc *Account) {
	if acc == nil || strings.TrimSpace(acc.ID) == "" {
		return
	}
	latest, ok := s.findAccount(acc.ID)
	if !ok {
		return
	}
	acc.Cookie = latest.Cookie
	acc.ExpiresAtMS = latest.ExpiresAtMS
	acc.LastRefreshAt = latest.LastRefreshAt
	acc.LastError = latest.LastError
	if strings.TrimSpace(acc.UserID) == "" {
		acc.UserID = latest.UserID
	}
}

// refreshTokenLocked 串行化账号续期,成功后立即落库。
func (s *Service) refreshTokenLocked(ctx context.Context, acc *Account, force bool) (bool, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if !force {
		s.reloadAccountTokens(acc)
		if tokenState(*acc) == "valid" {
			return false, nil
		}
	}
	rt := refreshTokenOf(acc.Cookie)
	if strings.TrimSpace(rt) == "" {
		if accessTokenFromCookie(acc.Cookie) == "" {
			return false, fmt.Errorf("账号 %s 没有可用的会话", acc.ID)
		}
		return false, fmt.Errorf("账号 %s 的 access token 已过期且没有 refresh_token", acc.ID)
	}
	beforeCookie := acc.Cookie
	if err := s.refreshSession(ctx, acc, rt); err != nil {
		return false, err
	}
	changed := acc.Cookie != beforeCookie
	if changed {
		if err := s.upsertAccount(ctx, *acc); err != nil {
			applog.Warn(ctx, "tokenharbor", "persist refreshed cookie failed", "account", acc.ID, "error", err.Error())
		}
	}
	return changed, nil
}

// markAccountError 只把 LastError 写回库内最新副本,避免覆盖并发刷新得到的新凭据。
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

// StartAutoRefresh 启动 access token 自动刷新;ctx 取消时退出。
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
		if a.Disabled || strings.TrimSpace(refreshTokenOf(a.Cookie)) == "" {
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
