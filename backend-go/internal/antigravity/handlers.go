package antigravity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// pendingOAuth 暂存待完成授权的 PKCE verifier（按 state 索引，30 分钟过期）。
// 由服务端持有，前端只回传回调地址即可，避免 verifier 随前端状态丢失。
type pendingOAuthEntry struct {
	verifier  string
	createdAt time.Time
}

var (
	pendingOAuthMu sync.Mutex
	pendingOAuth   = map[string]pendingOAuthEntry{}
)

const pendingOAuthTTL = 30 * time.Minute

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
const linkedEndpointID = "antigravity-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "Antigravity"

// internalKey 是网关转发时端点使用的占位 API Key：网关要求端点 api_key 非空，
// 但 antigravity 中继实际使用账号 token 鉴权，此 key 仅用于通过网关 key 选择。
const internalKey = "sk-antigravity-internal"

// ServeHTTP 是 Antigravity 插件的总入口：
//   - /v1/messages：Anthropic 兼容中继（需已授权并启用）
//   - /v1/chat/completions：OpenAI 兼容中继（网关转发走这条路）
//   - /api/antigravity/*：管理接口
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/v1/messages"):
		s.handleMessages(w, r)
	case strings.HasSuffix(path, "/v1/chat/completions"):
		s.handleOpenAIChat(w, r)
	case strings.HasSuffix(path, "/v1/models"):
		s.handleOpenAIModels(w, r)
	case path == "/api/antigravity/settings":
		s.handleSettings(w, r)
	case path == "/api/antigravity/status":
		s.handleStatus(w, r)
	case path == "/api/antigravity/accounts":
		s.handleAccounts(w, r)
	case path == "/api/antigravity/accounts/export":
		s.exportAccounts(w, r)
	case strings.HasPrefix(path, "/api/antigravity/accounts/"):
		s.handleAccountAction(w, r, strings.TrimPrefix(path, "/api/antigravity/accounts/"))
	case path == "/api/antigravity/oauth/auth-url":
		s.handleAuthURL(w, r)
	case path == "/api/antigravity/oauth/exchange":
		s.handleExchange(w, r)
	case path == "/api/antigravity/models":
		s.handleModels(w, r)
	case path == "/api/antigravity/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/antigravity/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/antigravity/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/antigravity/models/toggle/"))
	case path == "/api/antigravity/link":
		s.handleLink(w, r)
	case path == "/api/antigravity/test":
		s.handleTest(w, r)
	case path == "/api/antigravity/quota":
		s.handleQuota(w, r)
	default:
		response.Error(w, http.StatusNotFound, "antigravity route not found")
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	enabled := 0
	for _, a := range st.Accounts {
		if !a.Disabled && a.AccessToken != "" && a.ProjectID != "" {
			enabled++
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"enabled":      st.Enabled,
		"authorized":   enabled > 0,
		"accountCount": len(st.Accounts),
		"enabledCount": enabled,
		"proxyPoolId":  st.ProxyPoolID,
	})
}

func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": s.Settings()})
	case http.MethodPut, http.MethodPost:
		var body Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			response.Error(w, http.StatusBadRequest, "请求体解析失败")
			return
		}
		// 保留已有账号列表：设置表单不编辑账号。
		cur := s.Settings()
		body.Accounts = cur.Accounts
		if err := s.SaveSettings(r.Context(), body); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": s.Settings()})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// accountView 是返回给前端的账号展示结构（脱敏 token）。
type accountView struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	ProjectID string `json:"projectId"`
	PlanType  string `json:"planType"`
	TokenSet  bool   `json:"tokenSet"`
	Disabled  bool   `json:"disabled"`
	ExpiresAt int64  `json:"expiresAt"`
}

// handleAccounts 列出账号或导入授权文件。
func (s *Service) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		st := s.Settings()
		out := make([]accountView, 0, len(st.Accounts))
		for _, a := range st.Accounts {
			out = append(out, accountView{
				Name:      a.Name,
				Email:     a.Email,
				ProjectID: a.ProjectID,
				PlanType:  a.PlanType,
				TokenSet:  strings.TrimSpace(a.AccessToken) != "",
				Disabled:  a.Disabled,
				ExpiresAt: a.ExpiresAt,
			})
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": out})
	case http.MethodPost:
		s.importAccounts(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAccountAction 处理单账号动作：DELETE 删除，POST .../toggle 启停，PUT 编辑。
func (s *Service) handleAccountAction(w http.ResponseWriter, r *http.Request, action string) {
	action = strings.TrimSuffix(action, "/")
	switch {
	case r.Method == http.MethodDelete:
		s.deleteAccount(w, r, action)
	case r.Method == http.MethodPost && strings.HasSuffix(action, "/toggle"):
		s.toggleAccount(w, r, strings.TrimSuffix(action, "/toggle"))
	case r.Method == http.MethodPut:
		s.updateAccount(w, r, action)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// updateAccount 编辑账号可编辑字段（显示名、套餐）。
func (s *Service) updateAccount(w http.ResponseWriter, r *http.Request, email string) {
	var body struct {
		Name     string `json:"name"`
		PlanType string `json:"planType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	st := s.Settings()
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, email) {
			st.Accounts[i].Name = strings.TrimSpace(body.Name)
			if body.PlanType != "" {
				st.Accounts[i].PlanType = strings.TrimSpace(body.PlanType)
			}
			if err := s.SaveSettings(r.Context(), st); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
			return
		}
	}
	response.Error(w, http.StatusNotFound, "账号不存在")
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, email string) {
	ctx := r.Context()
	st := s.Settings()
	out := st.Accounts[:0]
	for _, a := range st.Accounts {
		if !strings.EqualFold(a.Email, email) {
			out = append(out, a)
		}
	}
	if len(out) == len(st.Accounts) {
		response.Error(w, http.StatusNotFound, "账号不存在")
		return
	}
	st.Accounts = out
	if err := s.SaveSettings(ctx, st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) toggleAccount(w http.ResponseWriter, r *http.Request, email string) {
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Disabled == nil {
		response.Error(w, http.StatusBadRequest, "缺少 disabled")
		return
	}
	st := s.Settings()
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, email) {
			st.Accounts[i].Disabled = *body.Disabled
			if err := s.SaveSettings(r.Context(), st); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
			return
		}
	}
	response.Error(w, http.StatusNotFound, "账号不存在")
}

// importAccounts 导入授权文件（账号凭证数组 JSON）。
func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if len(body.Accounts) == 0 {
		response.Error(w, http.StatusBadRequest, "没有可导入的账号")
		return
	}
	st := s.Settings()
	seen := map[string]bool{}
	for _, a := range st.Accounts {
		seen[strings.ToLower(a.Email)] = true
	}
	added := 0
	for _, a := range body.Accounts {
		if strings.TrimSpace(a.Email) == "" || strings.TrimSpace(a.AccessToken) == "" {
			continue
		}
		if seen[strings.ToLower(a.Email)] {
			continue
		}
		seen[strings.ToLower(a.Email)] = true
		st.Accounts = append(st.Accounts, a)
		added++
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "added": added})
}

// exportAccounts 导出授权文件（账号凭证 JSON，含 token）。
func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=antigravity-accounts-%s.json", time.Now().Format("20060102")))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"accounts": st.Accounts})
}

// handleAuthURL 生成 OAuth 授权 URL，verifier 暂存服务端（按 state）。
func (s *Service) handleAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	state, err := engineag.GenerateState()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	verifier, err := engineag.GenerateCodeVerifier()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	challenge := engineag.GenerateCodeChallenge(verifier)
	authURL := engineag.BuildAuthorizationURL(state, challenge)

	now := time.Now()
	pendingOAuthMu.Lock()
	for st, e := range pendingOAuth {
		if now.Sub(e.createdAt) > pendingOAuthTTL {
			delete(pendingOAuth, st)
		}
	}
	pendingOAuth[state] = pendingOAuthEntry{verifier: verifier, createdAt: now}
	pendingOAuthMu.Unlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"authUrl": authURL,
		"state":   state,
		// verifier 一并返回给前端持有：服务重启/多实例/TTL 过期时
		// exchange 仍可用请求体里的 verifier 完成授权，不依赖服务端内存。
		"verifier": verifier,
	})
}

// handleExchange 用授权回调地址换取 token，追加为账号。
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Code        string `json:"code"`
		State       string `json:"state"`
		Verifier    string `json:"verifier"`
		CallbackURL string `json:"callbackUrl"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}

	if strings.TrimSpace(body.CallbackURL) != "" {
		u, err := url.Parse(strings.TrimSpace(body.CallbackURL))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "回调地址解析失败")
			return
		}
		body.Code = u.Query().Get("code")
		body.State = u.Query().Get("state")
	}

	body.Code = strings.TrimSpace(body.Code)
	body.State = strings.TrimSpace(body.State)
	if body.Code == "" {
		response.Error(w, http.StatusBadRequest, "缺少授权 code，请粘贴完整回调地址")
		return
	}

	verifier := strings.TrimSpace(body.Verifier)
	if state := body.State; state != "" {
		pendingOAuthMu.Lock()
		if e, ok := pendingOAuth[state]; ok {
			verifier = e.verifier
			delete(pendingOAuth, state)
		}
		pendingOAuthMu.Unlock()
	}
	if verifier == "" {
		response.Error(w, http.StatusBadRequest, "缺少 code verifier，请重新发起授权")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	client, err := engineag.NewClient("")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	tok, err := client.ExchangeCode(ctx, body.Code, verifier)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "token 交换失败: "+err.Error())
		return
	}

	user, err := client.GetUserInfo(ctx, tok.AccessToken)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "获取用户信息失败: "+err.Error())
		return
	}

	projResp, _, err := client.LoadCodeAssist(ctx, tok.AccessToken)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "加载 Code Assist 失败: "+err.Error())
		return
	}
	projectID := projResp.CloudAICompanionProject
	tierID := projResp.GetTier()
	planType := engineag.TierIDToPlanType(tierID)

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	st := s.Settings()
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, user.Email) {
			// 覆盖已有账号。
			st.Accounts[i].AccessToken = tok.AccessToken
			st.Accounts[i].RefreshToken = tok.RefreshToken
			st.Accounts[i].ExpiresAt = expiresAt
			st.Accounts[i].ProjectID = projectID
			st.Accounts[i].PlanType = planType
			if body.Name != "" {
				st.Accounts[i].Name = body.Name
			}
			st.Accounts[i].Disabled = false
			if err := s.SaveSettings(ctx, st); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			response.JSON(w, http.StatusOK, map[string]interface{}{
				"success": true, "email": user.Email, "projectId": projectID, "planType": planType, "updated": true,
			})
			return
		}
	}
	st.Accounts = append(st.Accounts, Account{
		Name:         body.Name,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiresAt,
		Email:        user.Email,
		ProjectID:    projectID,
		PlanType:     planType,
	})
	if err := s.SaveSettings(ctx, st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "email": user.Email, "projectId": projectID, "planType": planType, "updated": false,
	})
}

// handleOpenAIChat OpenAI 兼容 chat.completions 入口（网关转发路径）。
func (s *Service) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	st := s.Settings()
	if !st.Enabled {
		oaiCompatError(w, http.StatusNotFound, "invalid_request_error", "Antigravity 插件未启用")
		return
	}
	if !s.hasAuthorizedAccounts() {
		oaiCompatError(w, http.StatusBadGateway, "authentication_error", "尚无可用账号，请先完成 Google 账号授权")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024))
	if err != nil {
		oaiCompatError(w, http.StatusBadRequest, "invalid_request_error", "读取请求体失败")
		return
	}
	var oaiReq openAIChatRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		oaiCompatError(w, http.StatusBadRequest, "invalid_request_error", "请求体解析失败")
		return
	}
	if strings.TrimSpace(oaiReq.Model) == "" {
		oaiCompatError(w, http.StatusBadRequest, "invalid_request_error", "缺少 model")
		return
	}
	if len(oaiReq.Messages) == 0 {
		oaiCompatError(w, http.StatusBadRequest, "invalid_request_error", "缺少 messages")
		return
	}
	if err := s.forwardOpenAIChat(r.Context(), w, &oaiReq); err != nil {
		oaiCompatError(w, http.StatusBadGateway, "upstream_error", err.Error())
	}
}

// oaiCompatError 输出 OpenAI 兼容错误。
func oaiCompatError(w http.ResponseWriter, status int, errType, msg string) {
	response.JSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{"message": msg, "type": errType, "code": status},
	})
}

// handleMessages Anthropic Messages 兼容端点。
func (s *Service) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	st := s.Settings()
	if !st.Enabled {
		claudeError(w, http.StatusNotFound, "invalid_request_error", "Antigravity 插件未启用")
		return
	}
	if !s.hasAuthorizedAccounts() {
		claudeError(w, http.StatusBadGateway, "authentication_error", "尚无可用账号，请先完成 Google 账号授权")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024))
	if err != nil {
		claudeError(w, http.StatusBadRequest, "invalid_request_error", "读取请求体失败")
		return
	}
	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)
	if err := s.ForwardClaude(r.Context(), w, body, probe.Stream); err != nil {
		claudeError(w, http.StatusBadGateway, "api_error", err.Error())
	}
}

// handleModels 拉取指定账号（或当前轮询账号）的模型列表。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email := r.URL.Query().Get("email")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	models, err := s.FetchModels(ctx, email)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	disabled := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		disabled[d] = true
	}
	names := make([]string, 0, len(models))
	for k := range models {
		names = append(names, k)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	out := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		mi := models[n]
		out = append(out, map[string]interface{}{
			"id":               n,
			"alias":            s.aliasForUpstream(n),
			"displayName":      mi.DisplayName,
			"maxTokens":        mi.MaxTokens,
			"maxOutputTokens":  mi.MaxOutputTokens,
			"supportsThinking": mi.SupportsThinking,
			"enabled":          !disabled[n],
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "models": out})
}

// handleToggleModel 切换单个模型的启用/停用。
func (s *Service) handleToggleModel(w http.ResponseWriter, r *http.Request, modelID string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		response.Error(w, http.StatusBadRequest, "缺少 enabled")
		return
	}
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	if *body.Enabled {
		delete(disabled, modelID)
	} else {
		disabled[modelID] = true
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleBatchToggleModels 批量启用/停用模型。
func (s *Service) handleBatchToggleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Enabled *bool    `json:"enabled"`
		Models  []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		response.Error(w, http.StatusBadRequest, "缺少 enabled")
		return
	}
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	for _, m := range body.Models {
		if m == "" {
			continue
		}
		if *body.Enabled {
			delete(disabled, m)
		} else {
			disabled[m] = true
		}
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleTest 测试上游连通性。
func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !s.hasAuthorizedAccounts() {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "尚无可用账号"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	acc := s.findAccount(body.Email)
	if acc == nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	client, err := engineag.NewClient("")
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_, err = client.FetchUserInfo(ctx, acc.AccessToken, acc.ProjectID)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleQuota 拉取账号的配额摘要（credits 余额 + bucket 窗口）。
func (s *Service) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email := r.URL.Query().Get("email")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	view, err := s.FetchQuota(ctx, email)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "quota": view})
}
func (s *Service) handleLink(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.linkStatus(w, r)
	case http.MethodPost:
		s.linkCreate(w, r)
	case http.MethodDelete:
		s.linkDelete(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// linkBaseURL 构造 loopback 基址。
func (s *Service) linkBaseURL() string {
	port := s.cfg.Port
	if port <= 0 {
		port = 3000
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/antigravity/v1"
}

type linkedEndpointInfo struct {
	id      string
	name    string
	baseURL string
	enabled int
	models  []string
}

func (s *Service) readLinkedEndpoint(ctx context.Context, db *sql.DB) *linkedEndpointInfo {
	var id, name, baseURL string
	var enabled int
	var modelsRaw sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, name, base_url, enabled, COALESCE(models,'')
		FROM openai_endpoints WHERE id = ?`, linkedEndpointID).Scan(&id, &name, &baseURL, &enabled, &modelsRaw)
	if err != nil {
		return nil
	}
	info := &linkedEndpointInfo{id: id, name: name, baseURL: baseURL, enabled: enabled}
	if modelsRaw.Valid && modelsRaw.String != "" {
		_ = json.Unmarshal([]byte(modelsRaw.String), &info.models)
	}
	return info
}

func (s *Service) linkStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	status := s.readLinkedEndpoint(ctx, db)
	if status == nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"linked":     false,
			"baseUrl":    s.linkBaseURL(),
			"endpointId": linkedEndpointID,
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"linked":     status.enabled == 1,
		"baseUrl":    status.baseURL,
		"endpointId": status.id,
		"name":       status.name,
		"models":     status.models,
	})
}

func (s *Service) linkCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	models := s.fetchModelNames(ctx)
	modelsJSON, _ := json.Marshal(models)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, ?, ?, ?, '[]', '[]', '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, ?, ?, 100, 'antigravity')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			models = excluded.models,
			enabled = 1,
			plugin_id = excluded.plugin_id`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey, string(modelsJSON), now, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
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
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `DELETE FROM openai_endpoints WHERE id = ?`, linkedEndpointID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "linked": false})
}

// fetchModelNames 尝试从上游拉取模型名列表；失败时用内置默认模型名。
func (s *Service) fetchModelNames(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := s.FetchModels(ctx, "")
	if err != nil {
		return defaultModelNames()
	}
	out := make([]string, 0, len(models))
	for k := range models {
		out = append(out, k)
	}
	return out
}

// defaultModelNames 上游不可达时的兜底模型列表。
func defaultModelNames() []string {
	return []string{
		"claude-sonnet-4-6",
		"claude-sonnet-4-6-20250805",
		"claude-opus-4-6",
		"claude-opus-4-6-thinking",
		"claude-3-5-haiku",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
	}
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

// claudeError 输出 Anthropic 兼容错误格式。
func claudeError(w http.ResponseWriter, status int, errType, msg string) {
	response.JSON(w, status, map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": msg,
		},
	})
}
