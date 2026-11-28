package cline

// 本文件实现 Cline 的设备授权登录（WorkOS RFC 8628）与网关端点接入。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// loginPollInterval 是设备授权的轮询间隔（上游返回 interval，缺省 5 秒）。
const loginPollInterval = 5 * time.Second

// loginStateTTL 是一次登录的有效期（上游 expires_in 是 300 秒）。
const loginStateTTL = 5 * time.Minute

// loginPending 是一次进行中的设备授权登录。
type loginPending struct {
	DeviceCode string
	UserCode   string
	ExpiresAt  time.Time
}

// loginManager 维护进行中的设备授权登录（内存、带过期）。
type loginManager struct {
	mu      sync.Mutex
	pending map[string]*loginPending
}

func newLoginManager() *loginManager {
	return &loginManager{pending: map[string]*loginPending{}}
}

func (m *loginManager) put(state string, p *loginPending) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[state] = p
}

func (m *loginManager) get(state string) (*loginPending, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[state]
	if !ok || !p.ExpiresAt.After(time.Now()) {
		delete(m.pending, state)
		return nil, false
	}
	return p, true
}

func (m *loginManager) take(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, state)
}

// sweep 定期清理过期登录；ctx 取消时退出。
func (m *loginManager) sweep(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			for state, p := range m.pending {
				if !p.ExpiresAt.After(now) {
					delete(m.pending, state)
				}
			}
			m.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

// StartLoginSweep 启动登录 pending 的定期清理。
func (s *Service) StartLoginSweep(ctx context.Context) {
	go s.login.sweep(ctx)
}

// handleLoginStart 发起设备授权登录。
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	status, payload, err := workosFormPost(ctx,
		workosBaseURL+"/user_management/authorize/device",
		url.Values{"client_id": {workosClientID}})
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "发起设备授权失败：" + err.Error()})
		return
	}
	if status < 200 || status >= 300 {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("发起设备授权失败：HTTP %d %s", status, jsonMessage(payload)),
		})
		return
	}
	var resp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil || resp.DeviceCode == "" {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": "设备授权响应缺少 device_code"})
		return
	}
	verifyURL := resp.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = resp.VerificationURI
	}
	if verifyURL == "" {
		verifyURL = "https://authkit.cline.bot/device"
	}
	ttl := loginStateTTL
	if resp.ExpiresIn > 0 && time.Duration(resp.ExpiresIn)*time.Second < ttl {
		ttl = time.Duration(resp.ExpiresIn) * time.Second
	}
	// state 用 device_code（一次性、不可猜测），前端只拿它来轮询。
	s.login.put(resp.DeviceCode, &loginPending{
		DeviceCode: resp.DeviceCode,
		UserCode:   resp.UserCode,
		ExpiresAt:  time.Now().Add(ttl),
	})
	interval := loginPollInterval
	if resp.Interval > 0 {
		interval = time.Duration(resp.Interval) * time.Second
	}
	responseJSON(w, map[string]interface{}{
		"success":    true,
		"state":      resp.DeviceCode,
		"url":        verifyURL,
		"userCode":   resp.UserCode,
		"pollMs":     interval.Milliseconds(),
		"ttlSeconds": int(ttl.Seconds()),
		"instructions": fmt.Sprintf("在打开的页面里输入授权码 %s 并确认（%d 秒内有效）；确认后会自动取回凭证",
			resp.UserCode, int(ttl.Seconds())),
	})
}

// handleLoginPoll 轮询一次设备授权结果。
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
	p, ok := s.login.get(state)
	if !ok {
		responseJSON(w, map[string]interface{}{"success": true, "status": "expired"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	status, payload, err := workosFormPost(ctx,
		workosBaseURL+"/user_management/authenticate",
		url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {p.DeviceCode},
			"client_id":   {workosClientID},
		})
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "status": "error", "error": err.Error()})
		return
	}
	if status >= 200 && status < 300 {
		var tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(payload, &tokens); err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
			responseJSON(w, http.StatusBadGateway, map[string]interface{}{
				"success": false, "status": "error", "error": "登录响应缺少令牌",
			})
			return
		}
		// 第四步：WorkOS 令牌不能直接打 LLM，必须经 /auth/register 换成本家会话令牌。
		acc, err := s.registerSession(ctx, tokens.AccessToken, tokens.RefreshToken)
		if err != nil {
			responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "status": "error", "error": err.Error()})
			return
		}
		s.login.take(state)
		updated := false
		if existing, found := s.findAccount(acc.ID); found {
			acc.Nickname = existing.Nickname
			acc.Disabled = existing.Disabled
			acc.CreatedAt = firstNonEmpty(existing.CreatedAt, acc.CreatedAt)
			updated = true
		}
		if err := s.upsertAccount(ctx, acc); err != nil {
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "status": "error", "error": "保存账号失败：" + err.Error()})
			return
		}
		responseJSON(w, map[string]interface{}{
			"success": true, "status": "success", "id": acc.ID, "updated": updated,
			"account": s.toAccountView(acc),
		})
		return
	}
	// 非 2xx：按 RFC 8628 的 error 码决定继续还是终止。
	var errResp struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(payload, &errResp)
	switch strings.ToLower(strings.TrimSpace(errResp.Error)) {
	case "authorization_pending", "slow_down":
		responseJSON(w, map[string]interface{}{"success": true, "status": "pending"})
	case "expired_token":
		s.login.take(state)
		responseJSON(w, map[string]interface{}{"success": true, "status": "expired", "error": "授权码已过期，请重新发起登录"})
	case "access_denied":
		s.login.take(state)
		responseJSON(w, map[string]interface{}{"success": true, "status": "error", "error": "登录被拒绝（在授权页取消了）"})
	default:
		msg := firstNonEmpty(errResp.Description, errResp.Error, jsonMessage(payload))
		responseJSON(w, map[string]interface{}{
			"success": false, "status": "error",
			"error": fmt.Sprintf("登录失败（HTTP %d）：%s", status, msg),
		})
	}
}

// registerSession 把 WorkOS 令牌换成 Cline 会话令牌（第四步，不能省）。
func (s *Service) registerSession(ctx context.Context, workosAccess, workosRefresh string) (Account, error) {
	body, _ := json.Marshal(map[string]string{
		"accessToken":  workosAccess,
		"refreshToken": workosRefresh,
	})
	status, data, err := s.doJSON(ctx, s.httpClientFor("login"), http.MethodPost,
		apiBaseURL+"/auth/register", body, "",
		map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "application/json",
			"X-CLIENT-TYPE": clientType,
		}, 30*time.Second, maxErrorBytes)
	if err != nil {
		return Account{}, fmt.Errorf("令牌登记失败：%w", err)
	}
	if status < 200 || status >= 300 {
		return Account{}, fmt.Errorf("令牌登记失败（HTTP %d）：%s", status, jsonMessage(data))
	}
	var resp struct {
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return Account{}, fmt.Errorf("令牌登记响应解析失败：%w", err)
	}
	access := strings.TrimSpace(resp.Data.AccessToken)
	if access == "" {
		return Account{}, fmt.Errorf("令牌登记响应缺少 accessToken")
	}
	refresh := strings.TrimSpace(resp.Data.RefreshToken)
	if refresh == "" {
		refresh = workosRefresh
	}
	acc := Account{
		AccessToken:  ensureTokenPrefix(access),
		RefreshToken: refresh,
		ExpiresAtMS:  jwtExpiryMS(ensureTokenPrefix(access)),
		UserID:       jwtUserID(ensureTokenPrefix(access)),
		Email:        jwtEmail(ensureTokenPrefix(access)),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	acc.Nickname = firstNonEmpty(acc.Email, acc.UserID)
	acc.ID = deriveAccountID(acc)
	return acc, nil
}

// workosFormPost 发一次表单 POST（WorkOS 两个端点都只认 form 编码）。
func workosFormPost(ctx context.Context, target string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := sharedHTTPClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	return resp.StatusCode, data, nil
}

// -----------------------------------------------------------------------------
// 网关端点接入
// -----------------------------------------------------------------------------

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
const linkedEndpointID = "cline-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "Cline"

// handleLink 处理插件与模型网关端点的接入/断开/状态。
func (s *Service) handleLink(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.linkStatus(w, r)
	case http.MethodPost:
		s.linkCreate(w, r)
	case http.MethodDelete:
		s.linkDelete(w, r)
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

func (s *Service) linkStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()

	var id, name, baseURL string
	var enabled int
	var modelsRaw string
	err = db.QueryRowContext(ctx, `
		SELECT id, name, base_url, enabled, COALESCE(models,'')
		FROM modelhub_endpoints WHERE id = ?`, linkedEndpointID).Scan(&id, &name, &baseURL, &enabled, &modelsRaw)
	if err != nil {
		responseJSON(w, map[string]interface{}{
			"linked":     false,
			"baseUrl":    s.linkBaseURL(),
			"endpointId": linkedEndpointID,
		})
		return
	}
	var models []string
	if modelsRaw != "" {
		_ = json.Unmarshal([]byte(modelsRaw), &models)
	}
	responseJSON(w, map[string]interface{}{
		"linked":     enabled == 1,
		"baseUrl":    baseURL,
		"endpointId": id,
		"name":       name,
		"models":     models,
	})
}

func (s *Service) linkCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()
	if err := ensureModelHubEndpointsTable(ctx, db); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	models := s.enabledModelIDs()
	modelsJSON, _ := json.Marshal(models)
	disabled := s.Settings().DisabledModels
	if disabled == nil {
		disabled = []string{}
	}
	disabledJSON, _ := json.Marshal(disabled)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO modelhub_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, model_names, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, ?, ?, ?, '[]', ?, '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, '{}', ?, ?, 100, 'cline')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			models = excluded.models,
			disabled_models = excluded.disabled_models,
			model_names = excluded.model_names,
			enabled = 1,
			plugin_id = excluded.plugin_id`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey, string(disabledJSON), string(modelsJSON), now, now)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success":    true,
		"linked":     true,
		"endpointId": linkedEndpointID,
		"baseUrl":    s.linkBaseURL(),
		"models":     models,
	})
}

func (s *Service) linkDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM modelhub_endpoints WHERE id = ?`, linkedEndpointID); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "linked": false})
}

// unlinkIfDisabled 供保存设置联动：插件被关闭时移除已接入的网关端点行。
func (s *Service) unlinkIfDisabled(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `DELETE FROM modelhub_endpoints WHERE id = ?`, linkedEndpointID)
}

// syncLinkedEndpointModels 把插件当前启用模型名单同步到已接入网关端点的 models 列。
func (s *Service) syncLinkedEndpointModels(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	disabled := s.Settings().DisabledModels
	if disabled == nil {
		disabled = []string{}
	}
	disabledPayload, _ := json.Marshal(disabled)
	modelsPayload, _ := json.Marshal(s.enabledModelIDs())
	_, _ = db.ExecContext(ctx, `
		UPDATE modelhub_endpoints SET models = ?, disabled_models = ?, model_names = '{}' WHERE id = ?`,
		string(modelsPayload), string(disabledPayload), linkedEndpointID)
}

// ReconcileLinkedEndpoint 在启动时按当前启用名单对账已接入端点的 models 列。
func (s *Service) ReconcileLinkedEndpoint(ctx context.Context) {
	s.syncLinkedEndpointModels(ctx)
}

// ensureModelHubEndpointsTable 幂等确保 modelhub_endpoints 表存在。
func ensureModelHubEndpointsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS modelhub_endpoints (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			headers TEXT,
			disabled_models TEXT,
			proxy_pool TEXT,
			proxy_batches TEXT,
			auto_switch INTEGER DEFAULT 0,
			proxy_enabled INTEGER DEFAULT 0,
			force_proxy INTEGER DEFAULT 0,
			rate_limit_retry_enabled INTEGER DEFAULT 1,
			rate_limit_retry_wait_seconds INTEGER DEFAULT 10,
			status TEXT DEFAULT 'unknown',
			enabled INTEGER DEFAULT 1,
			models TEXT,
			model_names TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			last_checked DATETIME,
			sort_order INTEGER DEFAULT 0,
			priority INTEGER DEFAULT 0,
			weight INTEGER DEFAULT 100,
			models_url TEXT,
			pricing TEXT,
			proxy_pool_id TEXT,
			plugin_id TEXT
		)`)
	return err
}
