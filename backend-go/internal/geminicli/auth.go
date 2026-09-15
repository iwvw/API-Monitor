package geminicli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// tokenExpiringWindow 是判定 access token「即将过期」的阈值。
const tokenExpiringWindow = 30 * time.Minute

// toAccountView 把账号脱敏成可下发前端的视图（不下发任何 token）。
func (s *Service) toAccountView(a Account) AccountView {
	view := AccountView{
		ID:         a.ID,
		Nickname:   a.Nickname,
		Email:      a.Email,
		ProjectID:  a.ProjectID,
		Disabled:   a.Disabled,
		TokenState: tokenState(a),
		ExpiresAt:  a.ExpiresAt,
		Available:  accountAvailable(a),
		CallCount:  s.callDisplay(a.ID),
		LastError:  a.LastError,
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
	if a.AccessToken == "" {
		return "unknown"
	}
	if a.ExpiresAt <= 0 {
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
// OAuth 登录
// -----------------------------------------------------------------------------

// handleLoginStart 发起 OAuth 登录，返回授权 URL 与 state。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	// 可选：用户预设的 GCP 项目 ID（账号无自动分配项目时兜底）。
	var body struct {
		ProjectID string `json:"projectId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.cleanupLoginStates()
	state, authURL, err := s.startLogin(body.ProjectID)
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
	if acc.ID == "" {
		acc.ID = strings.TrimSpace(firstNonEmpty(acc.Email, acc.Nickname))
	}
	if acc.ID == "" {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "登录返回缺少账号标识"})
		return
	}
	if acc.CreatedAt == "" {
		acc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.forgetLoginState(state)
	responseJSON(w, map[string]interface{}{
		"success": true,
		"status":  "success",
		"account": s.toAccountView(acc),
	})
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
	if acc.ID == "" {
		acc.ID = strings.TrimSpace(firstNonEmpty(acc.Email, acc.Nickname, acc.ProjectID))
	}
	if acc.ID == "" {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "登录返回缺少账号标识"})
		return
	}
	if acc.CreatedAt == "" {
		acc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
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
		fmt.Sprintf("attachment; filename=geminicli-accounts-%s.json", time.Now().UTC().Format("20060102")))
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
	// 导入是「读整份设置 → 追加账号 → 写回」，与并发登录/刷新互斥，否则会丢更新。
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[a.ID] = true
	}
	added := 0
	for _, a := range raw {
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" {
			a.ID = strings.TrimSpace(firstNonEmpty(a.Email, a.Nickname, a.ProjectID))
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

// handleTestAccount 对指定账号做一次连通性测试（用 refresh 作为探针）。
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
	if err := s.refreshAccessToken(ctx, &acc); err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "tokenSet": acc.AccessToken != ""})
}

// -----------------------------------------------------------------------------
// 选号
// -----------------------------------------------------------------------------

// pickAccount 选择用于转发的账号：优先未停用、token 未过期、不在冷却期的账号；
// 在候选里取累计调用次数最少者（消耗均衡），全部冷却时回退到可用账号。
func (s *Service) pickAccount(exclude map[string]bool) (Account, bool) {
	st := s.Settings()
	now := time.Now()
	type cand struct {
		acc   Account
		calls int64
	}
	var pool []cand
	var fallback []cand
	for _, a := range st.Accounts {
		if exclude[a.ID] {
			continue
		}
		if !accountAvailable(a) {
			continue
		}
		if s.inCooldown(a.ID, now) {
			continue
		}
		pool = append(pool, cand{acc: a, calls: s.callDisplay(a.ID)})
	}
	if len(pool) == 0 {
		for _, a := range st.Accounts {
			if exclude[a.ID] || !accountAvailable(a) {
				continue
			}
			fallback = append(fallback, cand{acc: a, calls: s.callDisplay(a.ID)})
		}
		pool = fallback
	}
	if len(pool) == 0 {
		return Account{}, false
	}
	best := pool[0]
	for _, c := range pool[1:] {
		if c.calls < best.calls {
			best = c
		}
	}
	return best.acc, true
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
