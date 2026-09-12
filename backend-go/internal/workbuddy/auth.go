package workbuddy

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
		ID:           a.ID,
		Nickname:     a.Nickname,
		UID:          a.UID,
		EnterpriseID: a.EnterpriseID,
		Disabled:     a.Disabled,
		TokenState:   tokenState(a),
		ExpiresAt:    a.ExpiresAt,
		Available:    accountAvailable(a),
		CallCount:    s.callDisplay(a.ID),
		LastError:    a.LastError,
		// 模型级限流（ratelimit.go）：只是这些模型暂时不可用，账号本身仍可用。
		LimitedModels: s.accountModelLimits(a.ID),
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
// ExpiresAt 缺失（tokenState = unknown）仍视为可用，避免误伤未记录过期时刻的旧凭据。
func accountAvailable(a Account) bool {
	if a.Disabled || a.ID == "" || a.AccessToken == "" {
		return false
	}
	return tokenState(a) != "expired"
}

// -----------------------------------------------------------------------------
// 扫码登录
// -----------------------------------------------------------------------------

// handleLoginStart 发起扫码登录，返回二维码内容（authUrl）与 state。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s.cleanupLoginStates()
	state, authURL, err := s.startLogin(ctx)
	if err != nil {
		// 502（而非 501）：失败原因是上游不可用，不是本服务不支持该功能。
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
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
		acc.ID = strings.TrimSpace(firstNonEmpty(acc.UID, acc.Nickname))
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

// cleanupLoginStates 清理已过期的登录会话，避免 map 长期增长。
func (s *Service) cleanupLoginStates() {
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for k, v := range s.loginStates {
		if v == nil || now.After(v.expires) {
			delete(s.loginStates, k)
		}
	}
}

// forgetLoginState 丢弃已完成的登录会话。
func (s *Service) forgetLoginState(state string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginStates, state)
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 管理账号列表（登录产生的凭据，需扫码获取，故不提供手工新增）。
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

// handleTestAccount 对指定账号做一次连通性测试。
// 用 refresh 作为探针：它同时验证 refresh token 是否仍然有效（比拉目录更有意义，
// 目录本身与账号无关，登录前也能拿到）。
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
	// 必须落库：refreshAccessToken 会轮换 access token，并可能一并轮换 refresh token。
	// 探针若只验证不保存，轮换后的 refresh token 就丢了，账号会在下次刷新时失效。
	acc.LastError = ""
	if err := s.upsertAccount(ctx, acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "tokenSet": acc.AccessToken != ""})
}

// -----------------------------------------------------------------------------
// 导出 / 导入
// -----------------------------------------------------------------------------

// exportPayload 是账号导出文件的形状（含 token，属敏感数据）。
type exportPayload struct {
	Version  int       `json:"version"`
	Exported string    `json:"exportedAt"`
	Accounts []Account `json:"accounts"`
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
		fmt.Sprintf("attachment; filename=workbuddy-accounts-%s.json", time.Now().UTC().Format("20060102")))
	_ = json.NewEncoder(w).Encode(exportPayload{
		Version:  1,
		Exported: time.Now().UTC().Format(time.RFC3339),
		Accounts: st.Accounts,
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
		if a.ID == "" {
			a.ID = strings.TrimSpace(firstNonEmpty(a.UID, a.Nickname))
		}
		if a.ID == "" || seen[a.ID] {
			continue
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

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
