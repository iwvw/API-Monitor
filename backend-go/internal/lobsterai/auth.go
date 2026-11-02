package lobsterai

// 本文件实现账号管理：脱敏视图、token 状态、OAuth 登录入口、账号 CRUD、
// 选号与失败冷却，以及额度/签到相关的账号级操作。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
)

// toAccountView 把账号脱敏成可下发前端的视图（不下发任何 token）。
func (s *Service) toAccountView(a Account) AccountView {
	view := AccountView{
		ID:            a.ID,
		Nickname:      a.Nickname,
		UserID:        a.UserID,
		Credits:       a.Credits,
		Disabled:      a.Disabled,
		TokenState:    tokenState(a),
		ExpiresAt:     a.ExpiresAt,
		Available:     accountAvailable(a),
		Cooling:       s.inCooldown(a.ID, time.Now()),
		CallCount:     s.callDisplay(a.ID),
		LastCheckinAt: a.LastCheckinAt,
		LastError:     a.LastError,
	}
	if a.ExpiresAt > 0 {
		if left := a.ExpiresAt - time.Now().Unix(); left > 0 {
			view.ExpiresInSeconds = left
		}
	}
	return view
}

// tokenState 依据过期时刻给出 token 状态。
func tokenState(a Account) string {
	if a.AccessToken == "" || a.ExpiresAt <= 0 {
		return "unknown"
	}
	left := time.Until(time.Unix(a.ExpiresAt, 0))
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
// ExpiresAt 缺失（tokenState = unknown）仍视为可用，避免误伤缺少过期时刻的旧凭据。
func accountAvailable(a Account) bool {
	if a.Disabled || a.ID == "" || a.AccessToken == "" {
		return false
	}
	return tokenState(a) != "expired"
}

// -----------------------------------------------------------------------------
// OAuth 登录入口
// -----------------------------------------------------------------------------

// handleLoginStart 发起 OAuth 登录，返回登录 URL 与 state。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	s.cleanupLoginStates()
	state, authURL, err := s.startLogin()
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success":   true,
		"state":     state,
		"url":       authURL,
		"expiresAt": time.Now().Add(loginTTL).UTC().Format(time.RFC3339),
	})
}

// handleLoginPoll 单次轮询登录状态；轮询节奏由前端驱动。
func (s *Service) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
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
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	acc, done, err := s.pollLogin(ctx, state)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if !done {
		responseJSON(w, map[string]interface{}{"success": true, "status": "pending"})
		return
	}
	s.finishLogin(ctx, w, state, acc)
}

// handleLoginCallback 用用户粘贴的回调 URL/授权码完成登录（远程部署时自动回调不可用）。
func (s *Service) handleLoginCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		State string `json:"state"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	acc, err := s.completeLoginByPaste(ctx, body.State, body.URL)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.finishLogin(ctx, w, "", acc)
}

// finishLogin 落库登录得到的账号并返回脱敏视图。
func (s *Service) finishLogin(ctx context.Context, w http.ResponseWriter, state string, acc Account) {
	if acc.ID == "" {
		acc.ID = strings.TrimSpace(firstNonEmpty(acc.Nickname, acc.UserID))
	}
	if acc.ID == "" {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "登录返回缺少账号标识"})
		return
	}
	if acc.CreatedAt == "" {
		acc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	// 登录后立即拉一次额度，让前端马上看到余额。
	if remain, _, err := s.FetchCredits(ctx, acc); err == nil {
		acc.Credits = remain
		s.recordQuotaSnapshot(acc.ID, remain)
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if state != "" {
		s.forgetLoginState(state)
	}
	responseJSON(w, map[string]interface{}{
		"success": true,
		"status":  "success",
		"account": s.toAccountView(acc),
	})
}

// cleanupLoginStates 清理已过期的登录会话，避免 map 长期增长。
func (s *Service) cleanupLoginStates() {
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for k, v := range s.loginStates {
		if v == nil || now.After(v.expires) {
			if v != nil {
				v.shutdown()
			}
			delete(s.loginStates, k)
		}
	}
}

// forgetLoginState 丢弃已完成的登录会话，并释放其本地回调监听。
func (s *Service) forgetLoginState(state string) {
	s.loginMu.Lock()
	lst := s.loginStates[state]
	delete(s.loginStates, state)
	s.loginMu.Unlock()
	if lst != nil {
		lst.shutdown()
	}
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 管理账号列表（登录产生的凭据，故不提供手工新增）。
func (s *Service) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	out := make([]AccountView, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		out = append(out, s.toAccountView(a))
	}
	responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
}

// handleDeleteAccount 删除账号。
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

// handleUpdateAccount 修改账号备注名（昵称）。
func (s *Service) handleUpdateAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPut {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	id = strings.TrimSpace(id)
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	acc.Nickname = strings.TrimSpace(body.Nickname)
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleToggleAccount 启用/停用账号（停用即摘出转发与自动刷新）。
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
	id = strings.TrimSpace(id)
	acc, ok := s.findAccount(id)
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

// handleRefreshAccount 立即刷新指定账号的 access token。
func (s *Service) handleRefreshAccount(w http.ResponseWriter, r *http.Request, id string) {
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
	if err := s.refreshAccessToken(ctx, &acc); err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "account": s.toAccountView(acc)})
}

// handleTestAccount 对指定账号做一次连通性测试（用额度查询作为探针）。
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
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	remain, _, err := s.FetchCredits(ctx, acc)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	acc.Credits = remain
	s.recordQuotaSnapshot(acc.ID, remain)
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "credits": remain})
}

// handleAccountCredits 返回单账号额度与批次到期明细。
func (s *Service) handleAccountCredits(w http.ResponseWriter, r *http.Request, id string) {
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
	remain, items, err := s.FetchCredits(ctx, acc)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.Credits = remain
	s.recordQuotaSnapshot(acc.ID, remain)
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{
		"success": true,
		"account": s.toAccountView(acc),
		"credits": remain,
		"items":   items,
	})
}

// handleCheckinAccount 对单账号立即执行一次签到。
func (s *Service) handleCheckinAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := s.runCheckin(ctx, acc)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "result": result})
}

// handleCheckinAll 立即对全部可用账号执行一次签到。
func (s *Service) handleCheckinAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	results := s.CheckinAll(ctx)
	responseJSON(w, map[string]interface{}{"success": true, "results": results})
}

// runCheckin 执行签到并回写账号状态（余额 + 最近签到时间 + 错误）。
func (s *Service) runCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	if err := s.ensureToken(ctx, &acc); err != nil {
		s.persistAccountError(ctx, acc, err)
		return CheckinResult{}, err
	}
	result, err := s.DailyCheckin(ctx, acc)
	if err != nil {
		s.persistAccountError(ctx, acc, err)
		return CheckinResult{}, err
	}
	if remain, _, cerr := s.FetchCredits(ctx, acc); cerr == nil {
		acc.Credits = remain
		s.recordQuotaSnapshot(acc.ID, remain)
	}
	acc.LastError = ""
	acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.upsertAccount(ctx, acc)
	return result, nil
}

// persistAccountError 把上游失败原因写回账号。
func (s *Service) persistAccountError(ctx context.Context, acc Account, err error) {
	acc.LastError = err.Error()
	_ = s.upsertAccount(ctx, acc)
}

// -----------------------------------------------------------------------------
// 账号导入导出
// -----------------------------------------------------------------------------

// exportPayload 是账号导出文件的形状（含 token，属敏感数据）。
type exportPayload struct {
	Version    int       `json:"version"`
	ExportedAt string    `json:"exportedAt"`
	Accounts   []Account `json:"accounts"`
}

// handleExportAccounts 导出全部账号（含 token，仅本机会话可下载，用于迁移/备份）。
func (s *Service) handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=lobsterai-accounts-%s.json", time.Now().UTC().Format("20060102")))
	_ = json.NewEncoder(w).Encode(exportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:   st.Accounts,
	})
}

// handleImportAccounts 导入账号，按 ID 去重合并（已存在则保留本机版本，不覆盖）。
// 兼容两种形状：{"accounts":[...]} 或裸数组。
func (s *Service) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	raw, err := decodeImportBody(r)
	if err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if len(raw) == 0 {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的账号"})
		return
	}
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[a.ID] = true
	}
	added := 0
	for _, a := range raw {
		a.ID = strings.TrimSpace(a.ID)
		// 无 ID 时用昵称/用户 ID 兜底；仍为空则跳过（无法作为稳定标识）。
		if a.ID == "" {
			a.ID = strings.TrimSpace(firstNonEmpty(a.Nickname, a.UserID))
		}
		if a.ID == "" || strings.TrimSpace(a.AccessToken) == "" || seen[a.ID] {
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

// decodeImportBody 解析导入请求体，同时支持 {"accounts":[...]} 与裸数组。
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
// 选号
// -----------------------------------------------------------------------------

// pickAccount 按当前策略选取本次转发的账号。
//
// 三种策略（Settings.AccountStrategy）：
//   - first：列表首个可用账号，其余作主备。
//   - round-robin：从上次位置继续向后轮询，请求均匀分摊。
//   - least-used：选剩余积分最多的账号（额度快照缺失时退化为列表序）。
//
// 三种策略都跳过：不可用（停用/过期/无 token）、本次已尝试、处于冷却期的账号。
// 若可用账号全在冷却中，退回「冷却最早结束」的那个继续尝试 —— 冷却只是软偏好，
// 不该把一次瞬时抖动放大成整池不可用。
func (s *Service) pickAccount(exclude map[string]bool) (Account, bool) {
	st := s.Settings()
	now := time.Now()
	strategy := accountpick.Normalize(st.AccountStrategy)

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
		switch strategy {
		case accountpick.RoundRobin:
			if c, ok := s.rr.Pick(cands); ok {
				return byID[c.ID], true
			}
		case accountpick.LeastUsed:
			if c, ok := accountpick.Best(cands, s.quotaWeight); ok {
				return byID[c.ID], true
			}
		default: // first
			return byID[cands[0].ID], true
		}
	}

	// 全部在冷却中：退回最早恢复的账号。
	bestIdx := -1
	var bestUntil time.Time
	for i, a := range st.Accounts {
		if exclude[a.ID] || !accountAvailable(a) {
			continue
		}
		until, cooled := s.cooldownUntilOf(a.ID, now)
		if !cooled {
			continue
		}
		if bestIdx < 0 || until.Before(bestUntil) {
			bestIdx, bestUntil = i, until
		}
	}
	if bestIdx >= 0 {
		return st.Accounts[bestIdx], true
	}
	return Account{}, false
}

// quotaWeight 返回账号的选号权重（剩余积分）；快照缺失或失效时返回 0。
func (s *Service) quotaWeight(id string) float64 {
	return s.quotaSnap.Get(id, quotaSnapshotTTL)
}

// inCooldown 判断账号是否处于失败冷却期。
func (s *Service) inCooldown(id string, now time.Time) bool {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	return ok && now.Before(until)
}

// setCooldown 给账号设置失败冷却。
func (s *Service) setCooldown(id string, d time.Duration) {
	if id == "" || d <= 0 {
		return
	}
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	s.cooldownUntil[id] = time.Now().Add(d)
}

// clearCooldown 清除账号冷却（转发成功时调用）。
func (s *Service) clearCooldown(id string) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	delete(s.cooldownUntil, id)
}

// cooldownUntilOf 读取账号当前冷却的截止时刻；未冷却或已过冷却期返回 ok=false。
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
// 选号额度快照
// -----------------------------------------------------------------------------

// quotaSnapshotTTL 是选号额度快照的有效期。超期视为失效（least-used 按 0 处理），
// 避免基于陈旧数据选号。
const quotaSnapshotTTL = 2 * time.Hour

// quotaRefreshMinInterval 是单账号额度快照的最小刷新间隔（转发后刷新节流）。
const quotaRefreshMinInterval = 2 * time.Minute

// recordQuotaSnapshot 把账号剩余积分写入选号快照。
func (s *Service) recordQuotaSnapshot(accountID string, credits float64) {
	s.quotaSnap.Set(accountID, credits)
}

// maybeRefreshQuotaSnapshot 在需要时异步刷新单个账号的选号额度快照。
// 仅在 least-used 策略下生效；同一账号在 quotaRefreshMinInterval 内只刷新一次。
func (s *Service) maybeRefreshQuotaSnapshot(acc Account) {
	if accountpick.Normalize(s.Settings().AccountStrategy) != accountpick.LeastUsed {
		return
	}
	if !s.quotaSnap.ClaimRefresh(acc.ID, quotaRefreshMinInterval) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a := acc
		if err := s.ensureToken(ctx, &a); err != nil {
			return
		}
		remain, _, err := s.FetchCredits(ctx, a)
		if err != nil {
			return
		}
		s.recordQuotaSnapshot(acc.ID, remain)
	}()
}

// refreshQuotaSnapshots 刷新所有可用账号的选号额度快照。
// 仅在 least-used 策略下有意义，其余策略直接跳过以免无谓打上游。
func (s *Service) refreshQuotaSnapshots(ctx context.Context) {
	if accountpick.Normalize(s.Settings().AccountStrategy) != accountpick.LeastUsed {
		return
	}
	for _, a := range s.Settings().Accounts {
		if !accountAvailable(a) {
			continue
		}
		acc := a
		if err := s.ensureToken(ctx, &acc); err != nil {
			continue
		}
		if remain, _, err := s.FetchCredits(ctx, acc); err == nil {
			s.recordQuotaSnapshot(acc.ID, remain)
		}
	}
}
