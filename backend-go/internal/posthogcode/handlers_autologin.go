package posthogcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// autoLoginSess 由 Service 持有，存进行中的自动登录会话（内存态，重启清空）。
// key = 会话 id；value 含独立 cookie jar，实现账号登录环境隔离。
type autoLoginSess struct {
	sessions map[string]*autoLoginState
}

// ---------------------------------------------------------------------------
// 自动登录状态机：
//   start  → 建独立 cookie jar 会话，POST /api/login（邮箱+密码）
//              ├─ 成功：直接走 OAuth authorize → code → 换 token → 落账号
//              └─ 需验证码/2FA：返回待验证状态 + 会话 id
//   verify → 提交验证码/TOTP → 完成登录 → OAuth authorize → 落账号
//   cancel → 丢弃会话
// ---------------------------------------------------------------------------

type autoLoginStartReq struct {
	Region   string `json:"region"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// AccountID 可选：指定后，授权落库会复用该既有账号（含其 ID/邮箱），
	// 并把密码并入该账号。为空则按 userinfo 生成的 id 新增/覆盖。
	AccountID string `json:"accountId"`
}

type autoLoginVerifyReq struct {
	SessionID string `json:"sessionId"`
	Code      string `json:"code"`
}

// handleAutoLoginStart 发起账号密码自动登录。
// 响应：{"success", "status": "logged_in"|"awaiting_code"|"awaiting_totp", "sessionId"?, "email"?}
func (s *Service) handleAutoLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body autoLoginStartReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	body.Password = strings.TrimSpace(body.Password)
	region := strings.ToLower(strings.TrimSpace(body.Region))
	if region != "eu" {
		region = "us"
	}
	if body.Email == "" || body.Password == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "邮箱与密码不能为空"})
		return
	}

	id, err := autoLoginID()
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	client, jar, err := newAutoLoginClient()
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	st := &autoLoginState{
		id:        id,
		region:    region,
		email:     body.Email,
		password:  body.Password,
		accountID: strings.TrimSpace(body.AccountID),
		jar:       jar,
		client:    client,
		createdAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if err := s.posthogLoginStep1(ctx, st); err != nil {
		var le *loginError
		if errors.As(err, &le) {
			// 需要验证码 / TOTP：进入待输入状态。
			status := "awaiting_code"
			if le.code == "2fa_required" {
				status = "awaiting_totp"
			}
			s.saveAutoLogin(st)
			responseJSON(w, http.StatusOK, map[string]interface{}{
				"success":   true,
				"status":    status,
				"sessionId": id,
				"email":     body.Email,
				"detail":    le.detail,
			})
			return
		}
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// 已登录：直接完成授权。
	if err := s.finishAutoLogin(ctx, st, w); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
}

// handleAutoLoginVerify 提交邮箱验证码或 TOTP，完成登录并授权落库。
func (s *Service) handleAutoLoginVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body autoLoginVerifyReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	body.SessionID = strings.TrimSpace(body.SessionID)
	body.Code = strings.TrimSpace(body.Code)
	if body.SessionID == "" || body.Code == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "会话 id 与验证码不能为空"})
		return
	}

	st := s.takeAutoLogin(body.SessionID)
	if st == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "自动登录会话不存在或已过期，请重新发起"})
		return
	}
	defer s.dropAutoLogin(st.id)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.posthogLoginVerify(ctx, st, body.Code); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if err := s.finishAutoLogin(ctx, st, w); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
}

// handleAutoLoginCancel 主动放弃一次自动登录。
func (s *Service) handleAutoLoginCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.dropAutoLogin(strings.TrimSpace(body.SessionID))
	responseJSON(w, map[string]interface{}{"success": true})
}

// finishAutoLogin 在登录成功后走 OAuth 授权并落库账号凭据。
func (s *Service) finishAutoLogin(ctx context.Context, st *autoLoginState, w http.ResponseWriter) error {
	redirectURI := callbackRedirectURI()
	// 用登录 session 拉授权码（first-party 自动授权，或已登录态下的 auto 批准）。
	code, err := s.posthogOAuthAuthorize(ctx, st, redirectURI)
	if err != nil {
		return err
	}

	// 换取 token（不依赖登录 cookie，标准 PKCE 换码）。
	codeVerifier := st.verifier
	if codeVerifier == "" {
		// 兜底：会话未记录 verifier 时无法换码，明确报错而不是用空值。
		return fmt.Errorf("授权会话缺少 PKCE verifier，请重新发起自动登录")
	}
	tok, err := exchangeCode(ctx, st.region, code, codeVerifier, redirectURI)
	if err != nil {
		return err
	}
	_ = tok

	acc, err := s.buildAccountFromToken(ctx, st.region, tok)
	if err != nil {
		return err
	}
	// 自动登录会把 PostHog 密码并入账号，供吊销后一键重新授权。
	acc.Password = st.password
	// 指定账号时复用其身份（ID/邮箱/停用态），避免 token 与账号脱钩。
	if st.accountID != "" {
		if existing, ok := s.findAccount(st.accountID); ok {
			acc.ID = existing.ID
			if existing.Email != "" {
				acc.Email = existing.Email
			}
		}
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		return err
	}
	st.oauthToken = tok

	responseJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  "logged_in",
		"account": s.toAccountView(acc),
	})
	return nil
}

// buildAccountFromToken 用换取到的 token 解析账号身份并构造 Account。
// 与 handleExchange 的落库逻辑保持一致；userinfo 缺失时兜底邮箱。
func (s *Service) buildAccountFromToken(ctx context.Context, region string, tok *tokenResponse) (Account, error) {
	info, infoErr := fetchUserInfo(ctx, region, tok.AccessToken)
	id := accountIDFor(info)
	if id == "" {
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
	projectID := fetchDefaultProjectID(ctx, region, tok.AccessToken)
	acc := Account{
		ID:           id,
		Email:        email,
		Region:       region,
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
	return acc, nil
}

// --- 会话容器 ---

func (s *Service) saveAutoLogin(st *autoLoginState) {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin == nil {
		s.autoLogin = &autoLoginSess{sessions: map[string]*autoLoginState{}}
	}
	s.autoLogin.sessions[st.id] = st
}

func (s *Service) takeAutoLogin(id string) *autoLoginState {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin == nil {
		return nil
	}
	st, ok := s.autoLogin.sessions[id]
	if !ok || time.Since(st.createdAt) > autoLoginTTL {
		delete(s.autoLogin.sessions, id)
		return nil
	}
	return st
}

func (s *Service) dropAutoLogin(id string) {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin != nil {
		delete(s.autoLogin.sessions, id)
	}
}
