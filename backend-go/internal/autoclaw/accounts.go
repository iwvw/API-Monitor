package autoclaw

// 本文件实现账号管理：脱敏视图、CRUD/导入导出、选号与失败冷却。

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

// cooldownDuration 是账号瞬时故障（429/5xx/网络）后的冷却时长。
const cooldownDuration = 60 * time.Second

// Account 是一个 AutoClaw 账号凭据。
//
// AutoClaw 的 refresh token 会轮换，而桌面端的 auth.json 是 safeStorage 加密的，
// 回写有写坏用户登录态的风险 —— 因此本插件**只保存自己的一份副本**，
// 不读写客户端文件。
type Account struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	// Region 是账号所属区域：cn（国内版）/ intl（国际版）。空值按 cn 处理。
	Region string `json:"region,omitempty"`
	// UserID 是上游 user_id。
	UserID string `json:"userId,omitempty"`
	// DeviceID 是客户端设备标识（签名链路会带，跨请求稳定）。
	DeviceID string `json:"deviceId,omitempty"`
	// AccessToken 是访问令牌。
	AccessToken string `json:"accessToken,omitempty"`
	// RefreshToken 用于续期（服务端会轮换）。
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAtMS 是 access token 过期时刻（毫秒 Unix 时间戳）；0 表示未知。
	ExpiresAtMS int64  `json:"expiresAtMS,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	// LastRefreshAt 为 RFC3339（UTC）。
	LastRefreshAt string `json:"lastRefreshAt,omitempty"`
	// LastCheckinAt 是最近一次签到成功的时刻（RFC3339，UTC）。
	LastCheckinAt string `json:"lastCheckinAt,omitempty"`
	// CheckinDisabled 是签到停用：与转发停用相互独立。
	CheckinDisabled bool   `json:"checkinDisabled,omitempty"`
	LastError       string `json:"lastError,omitempty"`
}

// AccountView 是下发前端的账号视图：不含任何 token。
type AccountView struct {
	ID              string `json:"id"`
	Nickname        string `json:"nickname,omitempty"`
	Region          string `json:"region,omitempty"`
	UserID          string `json:"userId,omitempty"`
	TokenTail       string `json:"tokenTail,omitempty"`
	HasRefresh      bool   `json:"hasRefresh"`
	Disabled        bool   `json:"disabled"`
	CheckinDisabled bool   `json:"checkinDisabled"`
	TokenState      string `json:"tokenState"`
	Available       bool   `json:"available"`
	Cooling         bool   `json:"cooling"`
	CooldownSeconds int64  `json:"cooldownSeconds,omitempty"`
	CallCount       int64  `json:"callCount"`
	LastRefreshAt   string `json:"lastRefreshAt,omitempty"`
	LastCheckinAt   string `json:"lastCheckinAt,omitempty"`
	LastError       string `json:"lastError,omitempty"`
}

// tokenExpiringWindow 是 access token 临期自动刷新的提前量。
const tokenExpiringWindow = 10 * time.Minute

func (s *Service) toAccountView(a Account) AccountView {
	view := AccountView{
		ID:              a.ID,
		Nickname:        a.Nickname,
		Region:          normalizeRegion(a.Region),
		UserID:          a.UserID,
		TokenTail:       tokenTail(a.AccessToken),
		HasRefresh:      strings.TrimSpace(a.RefreshToken) != "",
		Disabled:        a.Disabled,
		CheckinDisabled: a.CheckinDisabled,
		TokenState:      tokenState(a),
		Available:       accountAvailable(a),
		Cooling:         s.inCooldown(a.ID, time.Now()),
		CallCount:       s.callDisplay(a.ID),
		LastRefreshAt:   a.LastRefreshAt,
		LastCheckinAt:   a.LastCheckinAt,
		LastError:       a.LastError,
	}
	if until, cooled := s.cooldownUntilOf(a.ID, time.Now()); cooled {
		if left := int64(time.Until(until).Seconds()); left > 0 {
			view.CooldownSeconds = left
		}
	}
	return view
}

// tokenState 依据过期时刻给出 token 状态。
func tokenState(a Account) string {
	if strings.TrimSpace(a.AccessToken) == "" || a.ExpiresAtMS <= 0 {
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
	if a.Disabled || a.ID == "" || strings.TrimSpace(a.AccessToken) == "" {
		return false
	}
	return tokenState(a) != "expired"
}

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

// newDeviceID 生成一个 UUID 形态的设备标识（签名链路要求跨请求稳定）。
func newDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

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

// handleAddAccount 手动新增账号：body {nickname, region, access_token(必填), refresh_token}。
func (s *Service) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname     string `json:"nickname"`
		Region       string `json:"region"`
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
		Region:       normalizeRegion(body.Region),
		DeviceID:     newDeviceID(),
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

// deriveAccountID 由账号信息生成稳定 ID（区域前缀避免两地撞 id）。
func deriveAccountID(acc Account) string {
	prefix := "autoclaw-user-"
	if normalizeRegion(acc.Region) == regionIntl {
		prefix = "autoclaw-intl-user-"
	}
	if uid := strings.TrimSpace(acc.UserID); uid != "" {
		return prefix + uid
	}
	if id := strings.TrimSpace(acc.ID); id != "" {
		return id
	}
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return prefix + hex.EncodeToString(buf)
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
		Region   *string `json:"region"`
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
	if body.Region != nil {
		acc.Region = normalizeRegion(*body.Region)
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
		Disabled        *bool `json:"disabled"`
		CheckinDisabled *bool `json:"checkinDisabled"`
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
	if body.Disabled == nil && body.CheckinDisabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 disabled 或 checkinDisabled"})
		return
	}
	if body.Disabled != nil {
		acc.Disabled = *body.Disabled
		if !*body.Disabled {
			acc.LastError = ""
		}
	}
	if body.CheckinDisabled != nil {
		acc.CheckinDisabled = *body.CheckinDisabled
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleTestAccount 连通性测试：拉一次模型目录作为探针。
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
	models, err := s.fetchCatalog(ctx, acc)
	if err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.markAccountError(ctx, acc.ID, "")
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(models)})
}

// markAccountError 只把 LastError 写回库内最新副本。
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
	if err := s.upsertAccount(ctx, acc); err != nil {
		applog.Warn(ctx, "autoclaw", "persist account error failed", "account", id, "error", err.Error())
	}
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
		fmt.Sprintf("attachment; filename=autoclaw-accounts-%s.json", time.Now().UTC().Format("20060102")))
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
		a.Region = normalizeRegion(a.Region)
		if a.DeviceID == "" {
			a.DeviceID = newDeviceID()
		}
		if a.ExpiresAtMS <= 0 {
			a.ExpiresAtMS = jwtExpiryMS(a.AccessToken)
		}
		if strings.TrimSpace(a.UserID) == "" {
			a.UserID = jwtUserID(a.AccessToken)
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
