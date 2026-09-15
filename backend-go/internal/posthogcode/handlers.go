package posthogcode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ServeHTTP 是 PostHog Code 插件总入口：
//   - /api/posthogcode/*：插件管理接口（会话鉴权）
//   - /api/posthogcode/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/posthogcode/v1"):
		s.serveRelay(w, r, strings.TrimPrefix(path, "/api/posthogcode"))
	case path == "/api/posthogcode/settings":
		s.handleSettings(w, r)
	case path == "/api/posthogcode/status":
		s.handleStatus(w, r)
	case path == "/api/posthogcode/test":
		s.handleTest(w, r)
	case path == "/api/posthogcode/models":
		s.handleModels(w, r)
	case path == "/api/posthogcode/usage":
		s.handleUsage(w, r)
	case path == "/api/posthogcode/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/posthogcode/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/posthogcode/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/posthogcode/models/toggle/"))
	case path == "/api/posthogcode/link":
		s.handleLink(w, r)
	case path == "/api/posthogcode/oauth/auth-url":
		s.handleAuthURL(w, r)
	case path == "/api/posthogcode/oauth/exchange":
		s.handleExchange(w, r)
	case path == "/api/posthogcode/accounts":
		s.handleAccounts(w, r)
	case path == "/api/posthogcode/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/posthogcode/accounts/import":
		s.handleImportAccounts(w, r)
	case strings.HasPrefix(path, "/api/posthogcode/accounts/"):
		s.handleAccountAction(w, r, strings.TrimPrefix(path, "/api/posthogcode/accounts/"))
	default:
		response.Error(w, http.StatusNotFound, "posthogcode route not found")
	}
}

// handleSettings 读写插件设置。管理面不接受前端回传的 Accounts 与 DisabledModels，
// 避免整对象 PUT 把凭据或刚做的模型启停覆盖掉。
func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.publicSettings()})
	case http.MethodPut, http.MethodPost:
		var body Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
			return
		}
		// 读当前值回填 + 整份写回必须原子：与并发的刷新/导入/登录同处 accountsMu，
		// 否则会把临界区外的账号改动覆盖掉。
		s.accountsMu.Lock()
		current := s.Settings()
		// 这些字段由服务端拥有，前端表单不编辑：整对象 PUT 时必须回填库内当前值，
		// 否则会被零值覆盖（账号丢失、模型启停被重置、首次初始化标记被清空）。
		body.Accounts = current.Accounts
		body.DisabledModels = current.DisabledModels
		body.ModelsInitialized = current.ModelsInitialized
		if err := s.SaveSettings(r.Context(), body); err != nil {
			s.accountsMu.Unlock()
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		s.accountsMu.Unlock()
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.publicSettings()})
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// publicSettings 返回可下发前端的设置（账号已脱敏）。
func (s *Service) publicSettings() map[string]interface{} {
	st := s.Settings()
	views := make([]AccountView, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		views = append(views, s.toAccountView(a))
	}
	return map[string]interface{}{
		"enabled":         st.Enabled,
		"region":          st.Region,
		"product":         productSlug(st.Product),
		"modelPrefix":     st.ModelPrefix,
		"disabledModels":  st.DisabledModels,
		"freeTierOnly":    st.FreeTierOnly,
		"accountStrategy": normalizeStrategy(st.AccountStrategy),
		"accounts":        views,
		"freeTierModels":  freeTierModels,
	}
}

// handleStatus 返回插件运行状态。
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	available := 0
	for _, a := range st.Accounts {
		if accountAvailable(a) {
			available++
		}
	}
	responseJSON(w, map[string]interface{}{
		"enabled":        st.Enabled,
		"region":         st.Region,
		"accountCount":   len(st.Accounts),
		"availableCount": available,
		"modelCount":     len(s.catalog(r.Context())),
		"linkBaseUrl":    s.linkBaseURL(),
		"strategy":       normalizeStrategy(st.AccountStrategy),
	})
}

// handleTest 探测上游可达性（拉一次模型列表）。
func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.pickAccount("", nil)
	if !ok {
		responseJSON(w, map[string]interface{}{"success": false, "error": "没有可用账号，请先完成授权"})
		return
	}
	token, err := s.ensureFreshToken(r.Context(), &acc)
	if err != nil {
		responseJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	models, err := s.fetchModels(r.Context(), acc, token)
	if err != nil {
		responseJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(models)})
}

// handleModels 返回模型目录（带对外前缀）与逐项启停状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	disabled := s.disabledSet()
	models := s.catalog(r.Context())
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		entry := map[string]interface{}{
			"id":                id,
			"rawId":             m.ID,
			"displayName":       firstNonEmpty(m.DisplayName, m.ID),
			"ownedBy":           m.OwnedBy,
			"contextWindow":     m.ContextWindow,
			"enabled":           !disabled[id] && !disabled[m.ID],
			"allowed":           m.Allowed,
			"restrictionReason": m.RestrictionReason,
			"freeTier":          isFreeTierModel(m.ID),
		}
		if price := priceInfoFor(m.Pricing); price != nil {
			entry["price"] = price
		}
		out = append(out, entry)
	}
	responseJSON(w, map[string]interface{}{
		"success":      true,
		"models":       out,
		"modelPrefix":  st.ModelPrefix,
		"freeTierOnly": st.FreeTierOnly,
	})
}

// handleToggleModel 切换单个模型的启停。
func (s *Service) handleToggleModel(w http.ResponseWriter, r *http.Request, rawID string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := strings.TrimSpace(firstNonEmpty(rawID, body.ID))
	if id == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少模型 id"})
		return
	}
	// 入参可能是带前缀的对外名，统一归一化到未加前缀的上游模型名存储。
	raw := s.stripModelPrefix(id)
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if err := s.setModelEnabled(r.Context(), raw, enabled); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "id": id, "enabled": enabled})
}

// handleBatchToggleModels 批量切换模型启停。
func (s *Service) handleBatchToggleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Enabled bool     `json:"enabled"`
		IDs     []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	// 读改写加 accountsMu：与并发的单模型启停 / 首次初始化互斥，避免丢改动。
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	next := make([]string, 0, len(st.DisabledModels))
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	for _, id := range body.IDs {
		raw := s.stripModelPrefix(strings.TrimSpace(id))
		if raw == "" {
			continue
		}
		if body.Enabled {
			delete(disabled, raw)
		} else {
			disabled[raw] = true
		}
	}
	for k := range disabled {
		next = append(next, k)
	}
	st.DisabledModels = next
	if err := s.SaveSettings(r.Context(), st); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// setModelEnabled 设置单个模型的启停状态。读改写加 accountsMu，避免与并发的
// 批量启停 / 首次初始化互相覆盖 DisabledModels。
func (s *Service) setModelEnabled(ctx context.Context, rawID string, enabled bool) error {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	if enabled {
		delete(disabled, rawID)
	} else {
		disabled[rawID] = true
	}
	next := make([]string, 0, len(disabled))
	for k := range disabled {
		next = append(next, k)
	}
	st.DisabledModels = next
	return s.SaveSettings(ctx, st)
}

// -----------------------------------------------------------------------------
// 账号管理
// -----------------------------------------------------------------------------

// handleAccounts 列出账号（脱敏）。
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

// exportPayload 是账号导出文件的形状。
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
		fmt.Sprintf("attachment; filename=posthogcode-accounts-%s.json", time.Now().UTC().Format("20060102")))
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
			a.ID = strings.TrimSpace(firstNonEmpty(a.Email, a.ProjectID))
		}
		// ID 与 refresh token 都必须有：只有 access token 的账号过期后无法自动续期。
		if a.ID == "" || strings.TrimSpace(a.AccessToken) == "" || strings.TrimSpace(a.RefreshToken) == "" || seen[a.ID] {
			continue
		}
		if a.Region == "" {
			a.Region = normalizeRegion(st.Region)
		}
		if a.CreatedAt == "" {
			a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		// 过期时间缺失时按已过期处理，首次转发会触发 refresh 续期。
		if a.ExpiresAt < 0 {
			a.ExpiresAt = 0
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

// handleAccountAction 处理 /accounts/{id} 与 /accounts/{id}/{action}。
func (s *Service) handleAccountAction(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "缺少账号 id"})
		return
	}
	// 账号 id 可能含 @ 与 . ，做一次解码。
	id := rest
	action := ""
	if idx := strings.LastIndex(rest, "/"); idx >= 0 {
		id = rest[:idx]
		action = rest[idx+1:]
	}
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}

	switch action {
	case "toggle":
		s.handleToggleAccount(w, r, id)
	case "test":
		s.handleTestAccount(w, r, id)
	case "":
		if r.Method == http.MethodDelete {
			s.handleDeleteAccount(w, r, id)
			return
		}
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	default:
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "未知操作"})
	}
}

// handleDeleteAccount 删除账号。
func (s *Service) handleDeleteAccount(w http.ResponseWriter, r *http.Request, id string) {
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

// handleToggleAccount 启用/停用账号。
func (s *Service) handleToggleAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if body.Disabled != nil {
		acc.Disabled = *body.Disabled
	} else {
		acc.Disabled = !acc.Disabled
	}
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "disabled": acc.Disabled})
}

// handleTestAccount 强制刷新该账号 token 以验证凭据有效性。
// 必须落库：refresh 会轮换 refresh token，只验证不保存会让账号下次刷新失效。
func (s *Service) handleTestAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	// 强制过期以触发刷新路径。
	acc.ExpiresAt = 0
	token, err := s.ensureFreshToken(r.Context(), &acc)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(r.Context(), acc)
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	if err := s.upsertAccount(r.Context(), acc); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "tokenSet": token != ""})
}

// -----------------------------------------------------------------------------
// OAuth 授权
// -----------------------------------------------------------------------------

// handleAuthURL 生成授权链接。
// 请求体 {"region":"us|eu","redirectUri":"<回调地址>"}。
func (s *Service) handleAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Region      string `json:"region"`
		RedirectURI string `json:"redirectUri"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	region := strings.ToLower(strings.TrimSpace(body.Region))
	if region != "eu" {
		region = "us"
	}
	redirectURI := strings.TrimSpace(body.RedirectURI)
	if redirectURI == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "缺少 redirectUri：PostHog 只接受其注册过的回调地址（Desktop 应用为 http://localhost/callback，端口任意）",
		})
		return
	}

	authURL, state, err := s.buildAuthorizeURL(region, redirectURI)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success":     true,
		"url":         authURL,
		"state":       state,
		"region":      region,
		"redirectUri": redirectURI,
		"expiresAt":   time.Now().Add(oauthTTL).UTC().Format(time.RFC3339),
	})
}

// handleExchange 用回调地址（或 code+state）换取 token 并落库。
// 请求体 {"callbackUrl":"<完整回调地址>"} 或 {"code":"...","state":"..."}。
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		CallbackURL string `json:"callbackUrl"`
		Code        string `json:"code"`
		State       string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}

	code := strings.TrimSpace(body.Code)
	state := strings.TrimSpace(body.State)
	redirectURI := ""

	if cb := strings.TrimSpace(body.CallbackURL); cb != "" {
		u, err := url.Parse(cb)
		if err != nil {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "回调地址无法解析"})
			return
		}
		q := u.Query()
		if e := q.Get("error"); e != "" {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "授权被拒绝: " + e + " " + q.Get("error_description"),
			})
			return
		}
		if code == "" {
			code = strings.TrimSpace(q.Get("code"))
		}
		if state == "" {
			state = strings.TrimSpace(q.Get("state"))
		}
		// 回调地址去掉 query/fragment 即为注册的 redirect_uri。
		u.RawQuery = ""
		u.Fragment = ""
		redirectURI = u.String()
	}

	if code == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少授权码"})
		return
	}

	sess := s.takeOAuthState(state)
	if sess == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "授权会话不存在或已过期，请重新发起授权",
		})
		return
	}
	if redirectURI == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少回调地址"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	tok, err := exchangeCode(ctx, sess.region, code, sess.verifier, redirectURI)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	info, infoErr := fetchUserInfo(ctx, sess.region, tok.AccessToken)
	id := accountIDFor(info)
	if id == "" {
		// userinfo 不可用时退回邮箱缺失的占位标识，保证凭据仍能保存。
		id = "posthog-" + time.Now().UTC().Format("20060102T150405")
	}
	email := ""
	if info != nil {
		email = info.Email
	}

	expiresIn := tok.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 604800
	}
	projectID := fetchDefaultProjectID(ctx, sess.region, tok.AccessToken)
	acc := Account{
		ID:           id,
		Email:        email,
		Region:       sess.region,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second).Unix(),
		ProjectID:    projectID,
		Scope:        tok.Scope,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if infoErr != nil {
		acc.LastError = "userinfo 解析失败: " + infoErr.Error()
	}

	// 账号级区域已记在 Account 上；全局 Region 仅作旧数据回落与前端默认值。
	// 与 upsertAccount 同处一个 accountsMu 临界区，避免写全局设置时把并发
	// 刷新/导入的账号改动覆盖掉。
	s.accountsMu.Lock()
	st := s.Settings()
	st.Region = sess.region
	st.Product = "posthog_code"
	if err := s.SaveSettings(ctx, st); err != nil {
		s.accountsMu.Unlock()
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if err := s.upsertAccountLocked(ctx, acc); err != nil {
		s.accountsMu.Unlock()
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	s.accountsMu.Unlock()

	responseJSON(w, map[string]interface{}{
		"success": true,
		"account": s.toAccountView(acc),
		"scopes":  tok.Scope,
	})
}

// ensureOpenAIEndpointsTable 幂等确保 openai_endpoints 表存在（列定义与 openai 模块一致）。
func ensureOpenAIEndpointsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS openai_endpoints (
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
