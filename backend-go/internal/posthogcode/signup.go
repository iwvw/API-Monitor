package posthogcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/emailcode"
)

// 注册流程状态。
const (
	signupQueued        = "queued"
	signupRegistering   = "registering"
	signupAwaitManual   = "awaiting_manual_signup"
	signupAwaitVerify   = "awaiting_email_verify"
	signupLoggingIn     = "logging_in"
	signupAwaitLogin    = "awaiting_login_code"
	signupDone          = "done"
	signupFailed        = "failed"
	signupThrottled     = "throttled"
	signupChallenge     = "challenge_required"
	signupEmailUnmulti  = "no_inbox_domain"
	signupCodeTimeout   = 5 * time.Minute
	signupFlowTTL       = 30 * time.Minute
	signupDefaultPrefix = "probe"
	// 手动注册向导（人机分工）保留更长时间：用户可能中途离开。
	signupManualTTL = 2 * time.Hour
)

// signupState 是一次一键注册的中间状态（内存态，重启清空）。
type signupState struct {
	id        string
	region    string
	prefix    string
	domain    string
	email     string
	password  string
	accountID string
	userUUID  string
	manual    bool
	// proxy 是该注册流程选定的出口代理（空 = 直连），全程复用。
	proxy string
	// proxyPool 是本次注册请求指定的代理池 id（可覆盖设置值）。
	proxyPool string
	status    string
	steps     []string
	lastError string
	account   *AccountView
	createdAt time.Time
	mu        sync.Mutex
}

type signupStartReq struct {
	Region    string `json:"region"`
	Prefix    string `json:"prefix"`
	Domain    string `json:"domain"`
	Password  string `json:"password"`
	AccountID string `json:"accountId"`
	// Manual 为 true 时进入「人机分工」：面板只备好邮箱与密码，
	// 由用户在真实浏览器（建议无痕）完成含 Turnstile 的注册，面板随后自动验证邮箱并授权。
	Manual bool `json:"manual"`
	// Email/UserUUID 用于手动注册完成后接管：用户已在 PostHog 注册的邮箱。
	Email    string `json:"email"`
	UserUUID string `json:"userUuid"`
	// ProxyPoolID 按次覆盖插件设置里的出口代理池；空表示沿用设置值。
	ProxyPoolID string `json:"proxyPoolId"`
}

func signupID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// signupEnabled 返回是否具备一键注册前提（已配置可用收件域名）。
func (s *Service) signupDomains(ctx context.Context) ([]string, error) {
	if s.inbox == nil {
		return nil, errors.New("邮件收件箱未启用")
	}
	return s.inbox.AvailableDomains(ctx)
}

func (s *Service) handleSignupStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body signupStartReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	region := strings.ToLower(strings.TrimSpace(body.Region))
	if region != "eu" {
		region = "us"
	}
	domains, err := s.signupDomains(r.Context())
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if len(domains) == 0 {
		responseJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  signupEmailUnmulti,
			"error":   "没有可用于收信的域名，请先在「邮件」Tab 部署收件箱 Worker",
		})
		return
	}
	domain := strings.TrimSpace(body.Domain)
	if domain == "" {
		domain = domains[0]
	} else if !containsString(domains, domain) {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "所选域名未部署收件箱：" + domain})
		return
	}
	prefix := sanitizePrefix(body.Prefix)
	password := strings.TrimSpace(body.Password)
	if password == "" {
		password = defaultSignupPassword()
	}

	id, err := signupID()
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	st := &signupState{
		id:        id,
		region:    region,
		prefix:    prefix,
		domain:    domain,
		email:     prefix + "-" + randomSuffix() + "@" + domain,
		password:  password,
		accountID: strings.TrimSpace(body.AccountID),
		manual:    body.Manual,
		proxyPool: strings.TrimSpace(body.ProxyPoolID),
		status:    signupQueued,
		createdAt: time.Now(),
	}
	// 手动模式允许用户指定自己在浏览器里注册用的邮箱。
	if email := strings.ToLower(strings.TrimSpace(body.Email)); email != "" {
		st.email = email
	}
	s.saveSignup(st)
	if st.manual {
		go s.runManualSignup(st)
		responseJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"status":    signupAwaitManual,
			"sessionId": id,
			"email":     st.email,
			"password":  st.password,
			"region":    region,
			"signupUrl": cloudBaseURL(region) + "/signup",
		})
		return
	}
	go s.runSignup(st)

	responseJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"status":    signupQueued,
		"sessionId": id,
		"email":     st.email,
	})
}

// runManualSignup 是「人机分工」：面板不碰 Turnstile，只在用户于真实浏览器完成注册后
// 接管后续全部步骤——从收件箱自动获取验证码、完成邮箱验证、登录并授权。
// 之所以可行：注册验证邮件收在我们自己的域名下，验证码由面板自动提取提交。
func (s *Service) runManualSignup(st *signupState) {
	ctx, cancel := context.WithTimeout(context.Background(), signupManualTTL)
	defer cancel()
	s.setSignupStatus(st, signupAwaitManual, "已备好邮箱与密码，请在浏览器（建议无痕）完成注册")

	client, _, _, err := s.newAutologinClient(ctx)
	if err != nil {
		s.failSignup(st, err.Error())
		return
	}
	authState := &autoLoginState{region: st.region, email: st.email, password: st.password, client: client, createdAt: st.createdAt}

	// 登录未验证账号会返回 verify_email_pending，其 detail 正是 user uuid，
	// 无需注册响应即可拿到验证所需 uuid。
	uuid, err := s.waitForPendingUser(ctx, authState)
	if err != nil && !errors.Is(err, errAlreadyVerified) {
		s.failSignup(st, err.Error())
		return
	}
	if errors.Is(err, errAlreadyVerified) {
		// 邮箱早已验证：跳过验证，直接收尾。
		s.setSignupStatus(st, signupLoggingIn, "账号已可登录，正在完成授权")
		s.finishSignupLogin(ctx, st, authState)
		return
	}
	st.userUUID = uuid
	s.setSignupStatus(st, signupAwaitVerify, "检测到已注册账号，等待验证邮件并自动验证邮箱")
	if err := s.completeEmailVerify(ctx, st); err != nil {
		s.failSignup(st, err.Error())
		return
	}
	s.setSignupStatus(st, signupLoggingIn, "邮箱已验证，正在登录")
	// 邮箱验证完成后走与自动注册相同的登录 + 授权收尾。
	s.finishSignupLogin(ctx, st, authState)
}

// waitForPendingUser 轮询登录，直到账号已注册（返回 verify_email_pending 的 uuid）。
// 用户可能在浏览器里花几分钟才完成注册，因此在 TTL 内持续重试。
func (s *Service) waitForPendingUser(ctx context.Context, st *autoLoginState) (string, error) {
	deadline := time.Now().Add(signupManualTTL - 5*time.Minute)
	for {
		err := s.posthogLoginStep1(ctx, st)
		if err == nil {
			// 已能直接登录：说明邮箱早已验证过，无 uuid 可用，
			// 但既然能登录，直接进入收尾即可。
			return "", errAlreadyVerified
		}
		var le *loginError
		if errors.As(err, &le) {
			switch le.code {
			case "verify_email_pending":
				if strings.TrimSpace(le.detail) != "" {
					return strings.TrimSpace(le.detail), nil
				}
			case "invalid_credentials":
				// 用户还没在浏览器完成注册，继续等。
			default:
				// code_based_verification_required 等：账号已存在且邮箱已验证。
				return "", errAlreadyVerified
			}
		}
		if time.Now().After(deadline) {
			return "", errors.New("等待在浏览器完成注册超时，请重新发起")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// errAlreadyVerified 表示账号可登录（邮箱已验证），无需再做邮箱验证。
var errAlreadyVerified = errors.New("account already verified")

// finishSignupLogin 是注册与手动两条流程共用的收尾：登录（必要时取登录码）→ OAuth 授权落库。
func (s *Service) finishSignupLogin(ctx context.Context, st *signupState, authState *autoLoginState) {
	if err := s.posthogLoginStep1(ctx, authState); err != nil {
		var le *loginError
		switch {
		case errors.As(err, &le) && le.code == "code_based_verification_required":
			s.setSignupStatus(st, signupAwaitLogin, "等待登录验证码")
			code, cerr := s.waitEmailCode(ctx, st, "posthog.com", "login")
			if cerr != nil {
				s.failSignup(st, "等待登录验证码失败："+cerr.Error())
				return
			}
			if verr := s.posthogLoginVerify(ctx, authState, code); verr != nil {
				s.failSignup(st, "提交登录验证码失败："+verr.Error())
				return
			}
		case errors.As(err, &le) && le.code == "2fa_required":
			s.failSignup(st, "该账号启用了两步验证，无法自动完成，请改用密码授权并手动输入 TOTP")
			return
		default:
			s.failSignup(st, "登录失败："+err.Error())
			return
		}
	}
	acc, err := s.finishAutoLogin(ctx, authState)
	if err != nil {
		s.failSignup(st, err.Error())
		return
	}
	s.completeSignup(st, acc)
}

// runSignup 在后台串行执行注册 → 验证邮箱 → 登录 → 授权，全程写回状态供前端轮询。
func (s *Service) runSignup(st *signupState) {
	ctx, cancel := context.WithTimeout(context.Background(), signupFlowTTL)
	defer cancel()
	s.setSignupStatus(st, signupRegistering, "正在创建 PostHog 账号")
	client, jar, proxyURL, err := s.newAutologinClientWith(ctx, st.proxyPool)
	if err != nil {
		s.failSignup(st, err.Error())
		return
	}
	_ = jar
	if proxyURL != "" {
		s.setSignupStatus(st, signupRegistering, "正在通过代理出口 "+redactProxy(proxyURL)+" 创建账号")
	}
	authState := &autoLoginState{region: st.region, email: st.email, password: st.password, client: client, createdAt: time.Now()}
	st.proxy = proxyURL

	uuid, err := s.posthogSignup(ctx, authState, st)
	if err != nil {
		var le *loginError
		switch {
		case errors.As(err, &le) && le.code == "throttled":
			s.setSignupError(st, signupThrottled, "注册被限流："+le.detail)
		case errors.As(err, &le) && le.code == "challenge_required":
			s.setSignupError(st, signupChallenge, "需要人机验证（Turnstile），当前出口 IP 无法自动通过")
		case errors.As(err, &le) && le.code == "password_too_weak":
			s.setSignupError(st, signupFailed, "密码强度不足："+le.detail)
		default:
			s.failSignup(st, err.Error())
		}
		return
	}
	st.userUUID = uuid

	// 等注册验证码并完成邮箱验证。
	s.setSignupStatus(st, signupAwaitVerify, "等待注册验证邮件")
	if err := s.completeEmailVerify(ctx, st); err != nil {
		s.failSignup(st, err.Error())
		return
	}

	// 登录：可能直接成功，也可能再要一次登录验证码。
	s.setSignupStatus(st, signupLoggingIn, "邮箱已验证，正在登录")
	s.finishSignupLogin(ctx, st, authState)
}

func (s *Service) completeEmailVerify(ctx context.Context, st *signupState) error {
	code, err := s.waitEmailCode(ctx, st, "posthog.com", "verify your email")
	if err != nil {
		return errors.New("等待注册验证码失败：" + err.Error())
	}
	if err := s.posthogVerifyEmail(ctx, st, code); err != nil {
		return errors.New("邮箱验证失败：" + err.Error())
	}
	return nil
}

// waitEmailCode 从通用收件箱按发件人域名与主题过滤等待验证码。
func (s *Service) waitEmailCode(ctx context.Context, st *signupState, fromDomain, subject string) (string, error) {
	msg, err := s.inbox.Wait(ctx, emailcode.Selector{
		Mailbox:         st.email,
		FromDomain:      fromDomain,
		SubjectContains: subject,
		Since:           st.createdAt,
		RequireCode:     true,
	}, "signup:"+st.id, signupCodeTimeout)
	if err != nil {
		return "", err
	}
	return msg.Code, nil
}

// posthogSignup 调用 PostHog 注册接口，返回用户 uuid。
func (s *Service) posthogSignup(ctx context.Context, st *autoLoginState, signup *signupState) (string, error) {
	base := cloudBaseURL(st.region)
	csrf, err := fetchCSRF(ctx, st.client, base)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{
		"first_name":        "Probe",
		"email":             signup.email,
		"password":          signup.password,
		"organization_name": "API Monitor",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/signup/", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", base+"/signup")
	req.Header.Set("X-CSRFToken", csrf)

	resp, err := st.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", parseLoginError(raw)
	}
	var out struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.UUID == "" {
		return "", fmt.Errorf("注册响应缺少 uuid: %s", strings.TrimSpace(string(raw)))
	}
	return out.UUID, nil
}

// posthogVerifyEmail 用验证码完成邮箱验证。
func (s *Service) posthogVerifyEmail(ctx context.Context, st *signupState, code string) error {
	base := cloudBaseURL(st.region)
	// 复用注册会话选定的出口，避免验证请求换 IP 触发风控。
	client, err := proxyClient(st.proxy)
	if err != nil {
		return err
	}
	csrf, err := fetchCSRF(ctx, client, base)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"uuid": st.userUUID, "code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/users/verify_email/", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", base+"/login")
	req.Header.Set("X-CSRFToken", csrf)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return parseLoginError(raw)
	}
	return nil
}

// --- 会话容器与状态 ---

func (s *Service) saveSignup(st *signupState) {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.signups == nil {
		s.signups = map[string]*signupState{}
	}
	s.signups[st.id] = st
}

func (s *Service) peekSignup(id string) *signupState {
	autologinMu.Lock()
	defer autologinMu.Unlock()
	if s.signups == nil {
		return nil
	}
	return s.signups[id]
}

func (s *Service) setSignupStatus(st *signupState, status, step string) {
	st.mu.Lock()
	st.status = status
	if step != "" {
		st.steps = append(st.steps, step)
	}
	st.mu.Unlock()
}

func (s *Service) setSignupError(st *signupState, status, msg string) {
	st.mu.Lock()
	st.status = status
	st.lastError = msg
	st.steps = append(st.steps, msg)
	st.mu.Unlock()
}

func (s *Service) failSignup(st *signupState, msg string) {
	s.setSignupError(st, signupFailed, msg)
}

func (s *Service) completeSignup(st *signupState, acc *AccountView) {
	st.mu.Lock()
	st.status = signupDone
	st.account = acc
	st.steps = append(st.steps, "授权完成，账号已保存")
	st.mu.Unlock()
}

func (s *Service) handleSignupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	st := s.peekSignup(id)
	if st == nil {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "注册会话不存在或已过期"})
		return
	}
	st.mu.Lock()
	payload := map[string]interface{}{
		"success":   true,
		"status":    st.status,
		"sessionId": st.id,
		"email":     st.email,
		"steps":     append([]string{}, st.steps...),
	}
	// 手动模式下密码由用户带到浏览器使用，需回显；自动模式不需要暴露。
	if st.manual {
		payload["password"] = st.password
		payload["region"] = st.region
		payload["signupUrl"] = cloudBaseURL(st.region) + "/signup"
	}
	if st.proxy != "" {
		payload["proxy"] = redactProxy(st.proxy)
	}
	if st.lastError != "" {
		payload["error"] = st.lastError
	}
	if st.account != nil {
		payload["account"] = st.account
	}
	st.mu.Unlock()
	responseJSON(w, http.StatusOK, payload)
}

// redactProxy 隐去代理地址中的凭据，便于在状态里展示出口而不泄露密码。
func redactProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "已配置"
	}
	if u.User != nil {
		u.User = url.UserPassword("***", "***")
	}
	return u.String()
}

func sanitizePrefix(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	var b strings.Builder
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		out = signupDefaultPrefix
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// defaultSignupPassword 生成随机账号密码，不硬编码任何已知凭据前缀。
func defaultSignupPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return randomSuffix() + randomSuffix()
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = charset[int(v)%len(charset)]
	}
	return string(out)
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
