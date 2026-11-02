package posthogcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/emailcode"
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
//              └─ 需验证码/2FA：返回待验证状态 + 会话 id，
//                 验证码类会启动后台消费者从通用邮件收件箱等码并自动完成
//   verify → 手动提交验证码/TOTP → 完成登录 → OAuth authorize → 落账号
//   status → 查询会话进度（自动消费可能已在后台完成）
//   cancel → 丢弃会话
// ---------------------------------------------------------------------------

// autoLoginCodeWait 是后台等待登录验证码邮件的最长时间。
const autoLoginCodeWait = 5 * time.Minute

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
	client, jar, _, err := s.newAutologinClient(r.Context())
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
	// 同一邮箱同一时刻只保留一个等待会话：否则多个会话会互相抢同一封验证码邮件，
	// 被抢走那个必然报「No pending verification」。旧会话标记为被取代。
	s.supersedeAutoLogins(body.Email, id)

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if err := s.posthogLoginStep1(ctx, st); err != nil {
		var le *loginError
		if errors.As(err, &le) {
			// 需要验证码 / TOTP：进入待输入状态。
			if le.code == "2fa_required" {
				st.status = autoLoginAwaitingTOTP
			} else if le.code == "verify_email_pending" {
				// 邮箱未验证，先要求完成邮箱验证，无法走登录码流程。
				responseJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"status":  "email_unverified",
					"detail":  "邮箱尚未验证，请先在 PostHog 完成邮箱验证",
					"userId":  le.detail,
				})
				return
			} else {
				st.status = autoLoginAwaitingCode
			}
			s.saveAutoLogin(st)
			if st.status == autoLoginAwaitingCode {
				s.consumeLoginCodeAsync(st)
			}
			responseJSON(w, http.StatusOK, map[string]interface{}{
				"success":   true,
				"status":    st.status,
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
	acc, err := s.finishAutoLogin(ctx, st)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  autoLoginLoggedIn,
		"account": acc,
	})
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

	st := s.getAutoLogin(body.SessionID)
	if st == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "自动登录会话不存在或已过期，请重新发起"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.posthogLoginVerify(ctx, st, body.Code); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc, err := s.finishAutoLogin(ctx, st)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  autoLoginLoggedIn,
		"account": acc,
	})
}

// handleAutoLoginStatus 查询自动登录会话进度。
// 后台消费验证码可能已自动完成登录，前端轮询此接口获取最终结果。
func (s *Service) handleAutoLoginStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	if id == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 sessionId"})
		return
	}
	payload := s.autoLoginStatusSnapshot(id)
	if payload == nil {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "自动登录会话不存在或已过期"})
		return
	}
	responseJSON(w, http.StatusOK, payload)
}

// autoLoginStatusSnapshot 在锁内构建会话状态的稳定快照。
// 后台消费者（consumeLoginCodeAsync）与 supersede/complete/fail 会并发写
// status/result/lastError，锁外读会构成 data race。
func (s *Service) autoLoginStatusSnapshot(id string) map[string]interface{} {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin == nil {
		return nil
	}
	st, ok := s.autoLogin.sessions[id]
	if !ok {
		return nil
	}
	if time.Since(st.createdAt) > autoLoginTTL {
		delete(s.autoLogin.sessions, id)
		return nil
	}
	payload := map[string]interface{}{
		"success":   true,
		"status":    st.status,
		"sessionId": st.id,
		"email":     st.email,
	}
	if st.result != nil {
		payload["account"] = st.result
	}
	if st.lastError != "" {
		payload["error"] = st.lastError
	}
	return payload
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

// consumeLoginCodeAsync 在后台从通用邮件收件箱等待该邮箱的登录验证码，
// 取到后自动提交并完成授权落库。用发件人域名与主题过滤，避免同邮箱多站点串码。
//
// 时间下界刻意放宽：PostHog 在短时间内的重复登录会复用同一封验证邮件，不会重发。
// 若只认「会话开始之后」到达的邮件，用户先点登录、稍后再发起面板自动登录时就会错过。
// 精确性由 mailbox + fromDomain + subject 三重收敛保证，时间只作粗过滤（15 分钟容差）。
func (s *Service) consumeLoginCodeAsync(st *autoLoginState) {
	if s.inbox == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), autoLoginCodeWait+10*time.Second)
		defer cancel()
		msg, err := s.inbox.Wait(ctx, emailcode.Selector{
			Mailbox:         st.email,
			FromDomain:      "posthog.com",
			SubjectContains: "login",
			Since:           st.createdAt.Add(-15 * time.Minute),
			RequireCode:     true,
		}, "autologin:"+st.id, autoLoginCodeWait)
		if err != nil {
			s.failAutoLogin(st, "等待验证码失败："+err.Error())
			return
		}
		if err := s.posthogLoginVerify(ctx, st, msg.Code); err != nil {
			s.failAutoLogin(st, "提交验证码失败："+err.Error())
			return
		}
		acc, err := s.finishAutoLogin(ctx, st)
		if err != nil {
			s.failAutoLogin(st, err.Error())
			return
		}
		s.completeAutoLogin(st, acc)
	}()
}

// finishAutoLogin 在登录成功后走 OAuth 授权并落库账号凭据。
func (s *Service) finishAutoLogin(ctx context.Context, st *autoLoginState) (*AccountView, error) {
	redirectURI := callbackRedirectURI()
	// 用登录 session 拉授权码（first-party 自动授权，或已登录态下的 auto 批准）。
	code, err := s.posthogOAuthAuthorize(ctx, st, redirectURI)
	if err != nil {
		return nil, err
	}

	// 换取 token（不依赖登录 cookie，标准 PKCE 换码）。
	codeVerifier := st.verifier
	if codeVerifier == "" {
		// 兜底：会话未记录 verifier 时无法换码，明确报错而不是用空值。
		return nil, fmt.Errorf("授权会话缺少 PKCE verifier，请重新发起自动登录")
	}
	tok, err := exchangeCode(ctx, st.region, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, err
	}

	acc, err := s.buildAccountFromToken(ctx, st.region, tok)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	st.oauthToken = tok
	view := s.toAccountView(acc)
	return &view, nil
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

// peekAutoLogin 取回会话但不删除（供状态查询与并发消费）。
func (s *Service) peekAutoLogin(id string) *autoLoginState {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin == nil {
		return nil
	}
	st, ok := s.autoLogin.sessions[id]
	if !ok {
		return nil
	}
	if time.Since(st.createdAt) > autoLoginTTL {
		delete(s.autoLogin.sessions, id)
		return nil
	}
	return st
}

// getAutoLogin 与 peekAutoLogin 等价，语义上用于「即将终结该会话」的读取。
func (s *Service) getAutoLogin(id string) *autoLoginState {
	return s.peekAutoLogin(id)
}

// supersedeAutoLogins 终结同一邮箱下仍在等待验证码的其它会话。
// 同一邮箱的并发等待会互相抢同一封验证码邮件，被抢走的一方必然提交失败；
// 保留最新会话符合用户直觉（后发起的意图覆盖先前的）。
func (s *Service) supersedeAutoLogins(email, keepID string) {
	email = strings.ToLower(strings.TrimSpace(email))
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin == nil {
		return
	}
	for id, st := range s.autoLogin.sessions {
		if id == keepID || st.status != autoLoginAwaitingCode {
			continue
		}
		if strings.ToLower(st.email) != email {
			continue
		}
		st.status = autoLoginFailed
		st.lastError = "已被同一邮箱的新登录请求取代"
	}
}

func (s *Service) dropAutoLogin(id string) {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.autoLogin != nil {
		delete(s.autoLogin.sessions, id)
	}
}

// completeAutoLogin 标记会话成功并保存账号视图。
// 已完成会话不立即删除，保留至 TTL 过期，供前端轮询状态拿到最终结果。
func (s *Service) completeAutoLogin(st *autoLoginState, acc *AccountView) {
	autologinMu.Lock()
	st.status = autoLoginLoggedIn
	st.result = acc
	autologinMu.Unlock()
}

// failAutoLogin 标记会话失败并记录原因，同样保留至 TTL 过期。
func (s *Service) failAutoLogin(st *autoLoginState, reason string) {
	autologinMu.Lock()
	st.status = autoLoginFailed
	st.lastError = reason
	autologinMu.Unlock()
}
