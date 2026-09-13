package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	wa "github.com/go-webauthn/webauthn/webauthn"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	githubLoginEnabledKey       = "github_oauth_enabled"
	githubClientIDKey           = "github_oauth_client_id"
	githubClientSecretKey       = "github_oauth_client_secret"
	githubAllowedLoginsKey      = "github_oauth_allowed_logins"
	githubAllowedEmailsKey      = "github_oauth_allowed_emails"
	flowTypeGitHubOAuthState    = "github_oauth_state"
	flowTypeGitHub2FA           = "github_oauth_2fa"
	flowTypeWebAuthnRegister    = "webauthn_register"
	flowTypeWebAuthnLogin       = "webauthn_login"
	webAuthnFlowTTL             = 10 * time.Minute
	githubStateFlowTTL          = 10 * time.Minute
	githubPending2FAFlowTTL     = 5 * time.Minute
	adminWebAuthnUserName       = "admin"
	adminWebAuthnDisplayName    = "管理员"
	adminWebAuthnUserHandleText = "api-monitor-admin"

	// githubOAuthStateCookie 把 OAuth state 绑定到发起登录的浏览器：回调时必须
	// 携带同一 Cookie，防止攻击者用自己签发的 state 诱导受害者完成 OAuth 登录
	// （登录 CSRF）。Cookie 只存放随机 verifier，其 SHA-256 摘要写入 state flow。
	githubOAuthStateCookie = "gh_oauth_state"
)

type githubConfigResponse struct {
	Enabled           bool   `json:"enabled"`
	ClientID          string `json:"clientId"`
	HasClientSecret   bool   `json:"hasClientSecret"`
	AllowedLoginsText string `json:"allowedLoginsText"`
	AllowedEmailsText string `json:"allowedEmailsText"`
}

type githubConfigRequest struct {
	Enabled           bool   `json:"enabled"`
	ClientID          string `json:"clientId"`
	ClientSecret      string `json:"clientSecret"`
	AllowedLoginsText string `json:"allowedLoginsText"`
	AllowedEmailsText string `json:"allowedEmailsText"`
}

type githubOAuthConfig struct {
	Enabled       bool
	ClientID      string
	ClientSecret  string
	AllowedLogins []string
	AllowedEmails []string
}

type githubOAuthStateFlow struct {
	BaseURL       string `json:"baseUrl"`
	StateVerifier string `json:"stateVerifier,omitempty"`
}

type github2FAFlow struct {
	GitHubLogin string `json:"githubLogin"`
	BaseURL     string `json:"baseUrl"`
}

type webAuthnRegistrationRequest struct {
	Label string `json:"label"`
}

type webAuthnRegistrationFlow struct {
	Label   string         `json:"label"`
	Origin  string         `json:"origin"`
	Session wa.SessionData `json:"session"`
}

type webAuthnLoginFlow struct {
	Origin  string         `json:"origin"`
	Session wa.SessionData `json:"session"`
}

type webAuthnFinishRequest struct {
	FlowID     string          `json:"flowId"`
	Credential json.RawMessage `json:"credential"`
}

type github2FARequest struct {
	FlowID    string `json:"flowId"`
	TOTPToken string `json:"totpToken"`
}

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
}

type githubUserProfile struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
}

type githubUserEmail struct {
	Email   string `json:"email"`
	Primary bool   `json:"primary"`
}

type storedWebAuthnCredential struct {
	ID                string
	Label             string
	Credential        wa.Credential
	CreatedAt         string
	LastUsedAt        string
	LastUsedIP        string
	LastUsedUserAgent string
}

type adminWebAuthnUser struct {
	credentials []wa.Credential
}

func (u adminWebAuthnUser) WebAuthnID() []byte {
	return []byte(adminWebAuthnUserHandleText)
}

func (u adminWebAuthnUser) WebAuthnName() string {
	return adminWebAuthnUserName
}

func (u adminWebAuthnUser) WebAuthnDisplayName() string {
	return adminWebAuthnDisplayName
}

func (u adminWebAuthnUser) WebAuthnCredentials() []wa.Credential {
	return u.credentials
}

func (s *Service) loginOptions(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	githubConfig, err := s.loadGitHubOAuthConfig(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	credentialCount, err := s.countWebAuthnCredentials(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"github": map[string]interface{}{
			"enabled": githubConfig.complete(),
		},
		"webauthn": map[string]interface{}{
			"enabled": credentialCount > 0,
		},
	})
}

func (s *Service) githubConfig(w http.ResponseWriter, r *http.Request) {
	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	config, err := s.loadGitHubOAuthConfig(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, s.githubConfigForResponse(config))
}

func (s *Service) saveGitHubConfig(w http.ResponseWriter, r *http.Request) {
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止修改 GitHub 登录配置"})
		return
	}

	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	var body githubConfigRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	current, err := s.loadGitHubOAuthConfig(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	next := githubOAuthConfig{
		Enabled:       body.Enabled,
		ClientID:      strings.TrimSpace(body.ClientID),
		ClientSecret:  strings.TrimSpace(body.ClientSecret),
		AllowedLogins: normalizeGitHubList(body.AllowedLoginsText),
		AllowedEmails: normalizeGitHubList(body.AllowedEmailsText),
	}
	if next.ClientSecret == "" {
		next.ClientSecret = current.ClientSecret
	}
	if next.Enabled {
		if next.ClientID == "" || next.ClientSecret == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "启用 GitHub 登录前请先填写 Client ID 和 Client Secret"})
			return
		}
		if len(next.AllowedLogins) == 0 && len(next.AllowedEmails) == 0 {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请至少配置一个允许登录的 GitHub 用户名或邮箱"})
			return
		}
	}

	if err := s.saveGitHubOAuthConfig(r.Context(), db, next); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.logOperation(r.Context(), db, "GITHUB_OAUTH_CONFIG_UPDATED", "auth", map[string]interface{}{
		"enabled":       next.Enabled,
		"allowedLogins": len(next.AllowedLogins),
		"allowedEmails": len(next.AllowedEmails),
	}, s.requestClientIP(r), r.UserAgent())

	response.OK(w, s.githubConfigForResponse(next))
}

func (s *Service) startGitHubLogin(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	config, err := s.loadGitHubOAuthConfig(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !config.complete() {
		response.Error(w, http.StatusBadRequest, "GitHub 登录尚未配置完成")
		return
	}

	baseURL, err := s.resolveBrowserBaseURL(r.Context(), db, r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	stateVerifierRaw := make([]byte, 24)
	if _, err := rand.Read(stateVerifierRaw); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	stateVerifier := hex.EncodeToString(stateVerifierRaw)
	stateID, err := s.createAuthFlow(r.Context(), db, flowTypeGitHubOAuthState, githubOAuthStateFlow{
		BaseURL:       baseURL,
		StateVerifier: hashStateVerifier(stateVerifier),
	}, githubStateFlowTTL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	redirectURL, err := s.githubAuthorizationURL(config, baseURL, stateID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    stateVerifier,
		Path:     "/api/auth/github",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(githubStateFlowTTL.Seconds()),
	})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *Service) finishGitHubLogin(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	stateID := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if stateID == "" {
		response.Error(w, http.StatusBadRequest, "GitHub 登录状态无效")
		return
	}

	var state githubOAuthStateFlow
	ok, err := s.consumeAuthFlow(r.Context(), db, flowTypeGitHubOAuthState, stateID, &state)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || state.BaseURL == "" {
		response.Error(w, http.StatusBadRequest, "GitHub 登录状态已失效，请重新发起登录")
		return
	}

	// state flow 里的 verifier 摘要必须与发起登录时下发的 HttpOnly Cookie 匹配，
	// 否则视为跨站发起的 OAuth（登录 CSRF），拒绝继续。
	s.clearGitHubStateCookie(w, r)
	if !stateVerifierMatches(r, state.StateVerifier) {
		_ = s.logOperation(r.Context(), db, "GITHUB_OAUTH_DENIED", "auth", map[string]interface{}{"reason": "state_verifier_mismatch"}, s.requestClientIP(r), r.UserAgent())
		response.Error(w, http.StatusBadRequest, "GitHub 登录状态已失效，请重新发起登录")
		return
	}

	if loginErr := strings.TrimSpace(r.URL.Query().Get("error")); loginErr != "" {
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "GitHub 授权被取消或失败"})
		return
	}
	if code == "" {
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "GitHub 未返回授权码"})
		return
	}

	config, err := s.loadGitHubOAuthConfig(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !config.complete() {
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "GitHub 登录配置不完整"})
		return
	}

	accessToken, err := s.exchangeGitHubAccessToken(r.Context(), config, state.BaseURL, code)
	if err != nil {
		_ = s.logOperation(r.Context(), db, "GITHUB_OAUTH_FAILED", "auth", map[string]interface{}{"reason": "token_exchange_failed"}, s.requestClientIP(r), r.UserAgent())
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "GitHub 授权交换失败"})
		return
	}

	profile, emails, err := s.loadGitHubIdentity(r.Context(), accessToken)
	if err != nil {
		_ = s.logOperation(r.Context(), db, "GITHUB_OAUTH_FAILED", "auth", map[string]interface{}{"reason": "identity_fetch_failed"}, s.requestClientIP(r), r.UserAgent())
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "无法读取 GitHub 账户信息"})
		return
	}

	if !config.allows(profile.Login, profile.Email, emails) {
		_ = s.logOperation(r.Context(), db, "GITHUB_OAUTH_DENIED", "auth", map[string]interface{}{
			"login": profile.Login,
			"email": strings.ToLower(strings.TrimSpace(profile.Email)),
		}, s.requestClientIP(r), r.UserAgent())
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{"authError": "当前 GitHub 账户不在允许列表中"})
		return
	}

	twoFAEnabled, err := s.is2FAEnabled(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if twoFAEnabled {
		flowID, err := s.createAuthFlow(r.Context(), db, flowTypeGitHub2FA, github2FAFlow{
			GitHubLogin: profile.Login,
			BaseURL:     state.BaseURL,
		}, githubPending2FAFlowTTL)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.redirectAuthPage(w, r, state.BaseURL, map[string]string{
			"githubFlow": flowID,
			"provider":   "github",
		})
		return
	}

	if err := s.issueAuthenticatedSession(w, r, db, "github", map[string]interface{}{
		"githubLogin": profile.Login,
	}); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.redirectAuthPage(w, r, state.BaseURL, nil)
}

func (s *Service) completeGitHub2FA(w http.ResponseWriter, r *http.Request) {
	var body github2FARequest
	if !decodeJSON(w, r, &body) {
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var flow github2FAFlow
	ok, err := s.consumeAuthFlow(r.Context(), db, flowTypeGitHub2FA, body.FlowID, &flow)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "GitHub 登录验证已失效，请重新发起登录"})
		return
	}

	twoFAEnabled, err := s.is2FAEnabled(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if twoFAEnabled {
		if strings.TrimSpace(body.TOTPToken) == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请输入 6 位动态验证码"})
			return
		}
		valid, err := s.verifyLogin2FA(r.Context(), db, body.TOTPToken)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !valid {
			response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "双因素验证码错误"})
			return
		}
	}

	if err := s.issueAuthenticatedSession(w, r, db, "github", map[string]interface{}{
		"githubLogin": flow.GitHubLogin,
		"with2FA":     twoFAEnabled,
	}); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) listWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	credentials, err := s.loadStoredWebAuthnCredentials(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, 0, len(credentials))
	for _, item := range credentials {
		items = append(items, map[string]interface{}{
			"id":         item.ID,
			"label":      item.Label,
			"createdAt":  item.CreatedAt,
			"lastUsedAt": item.LastUsedAt,
			"attachment": string(item.Credential.Authenticator.Attachment),
			"backedUp":   item.Credential.Flags.BackupEligible,
		})
	}
	response.OK(w, map[string]interface{}{
		"credentials": items,
	})
}

func (s *Service) beginWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止添加通行密钥"})
		return
	}

	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	var body webAuthnRegistrationRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	label := strings.TrimSpace(body.Label)
	if label == "" {
		label = "默认通行密钥"
	}
	if len([]rune(label)) > 48 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "通行密钥名称不能超过 48 个字符"})
		return
	}

	instance, origin, err := s.webauthnForRequest(r.Context(), db, r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	credentials, err := s.loadWebAuthnCredentials(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	user := adminWebAuthnUser{credentials: credentials}
	options, sessionData, err := instance.BeginRegistration(
		user,
		wa.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		wa.WithExclusions(wa.Credentials(user.WebAuthnCredentials()).CredentialDescriptors()),
		wa.WithPublicKeyCredentialHints([]protocol.PublicKeyCredentialHints{
			protocol.PublicKeyCredentialHintClientDevice,
			protocol.PublicKeyCredentialHintSecurityKey,
		}),
		wa.WithExtensions(protocol.AuthenticationExtensions{"credProps": true}),
	)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "创建通行密钥挑战失败"})
		return
	}

	flowID, err := s.createAuthFlow(r.Context(), db, flowTypeWebAuthnRegister, webAuthnRegistrationFlow{
		Label:   label,
		Origin:  origin,
		Session: *sessionData,
	}, webAuthnFlowTTL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"flowId":  flowID,
		"options": options,
	})
}

func (s *Service) finishWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止添加通行密钥"})
		return
	}

	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	payload, err := decodeWebAuthnFinishRequest(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	var flow webAuthnRegistrationFlow
	ok, err = s.consumeAuthFlow(r.Context(), db, flowTypeWebAuthnRegister, payload.FlowID, &flow)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "通行密钥挑战已失效，请重新开始"})
		return
	}

	instance, err := s.webauthnForOrigin(flow.Origin)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	credentials, err := s.loadWebAuthnCredentials(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	user := adminWebAuthnUser{credentials: credentials}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload.Credential)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "浏览器返回的通行密钥数据无效"})
		return
	}
	credential, err := instance.CreateCredential(user, flow.Session, parsed)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "通行密钥校验失败"})
		return
	}

	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	if err := s.saveWebAuthnCredential(r.Context(), db, credentialID, flow.Label, *credential); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			response.JSON(w, http.StatusConflict, map[string]interface{}{"success": false, "error": "该通行密钥已经添加过了"})
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.logOperation(r.Context(), db, "WEBAUTHN_CREDENTIAL_ADDED", "auth", map[string]interface{}{
		"credentialId": credentialID,
		"label":        flow.Label,
	}, s.requestClientIP(r), r.UserAgent())

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) beginWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	instance, origin, err := s.webauthnForRequest(r.Context(), db, r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	credentials, err := s.loadWebAuthnCredentials(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(credentials) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "当前没有可用的通行密钥"})
		return
	}

	options, sessionData, err := instance.BeginDiscoverableLogin(
		wa.WithUserVerification(protocol.VerificationRequired),
		wa.WithAssertionPublicKeyCredentialHints([]protocol.PublicKeyCredentialHints{
			protocol.PublicKeyCredentialHintClientDevice,
			protocol.PublicKeyCredentialHintSecurityKey,
		}),
	)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "创建通行密钥登录挑战失败"})
		return
	}

	flowID, err := s.createAuthFlow(r.Context(), db, flowTypeWebAuthnLogin, webAuthnLoginFlow{
		Origin:  origin,
		Session: *sessionData,
	}, webAuthnFlowTTL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"flowId":  flowID,
		"options": options,
	})
}

func (s *Service) finishWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	payload, err := decodeWebAuthnFinishRequest(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var flow webAuthnLoginFlow
	ok, err := s.consumeAuthFlow(r.Context(), db, flowTypeWebAuthnLogin, payload.FlowID, &flow)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "通行密钥登录挑战已失效，请重新开始"})
		return
	}

	instance, err := s.webauthnForOrigin(flow.Origin)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload.Credential)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "浏览器返回的通行密钥数据无效"})
		return
	}
	user, credential, err := instance.ValidatePasskeyLogin(func(rawID, userHandle []byte) (wa.User, error) {
		if !bytes.Equal(userHandle, []byte(adminWebAuthnUserHandleText)) {
			return nil, fmt.Errorf("unknown user handle")
		}
		credentials, loadErr := s.loadWebAuthnCredentials(r.Context(), db)
		if loadErr != nil {
			return nil, loadErr
		}
		if len(credentials) == 0 {
			return nil, fmt.Errorf("no credentials")
		}
		return adminWebAuthnUser{credentials: credentials}, nil
	}, flow.Session, parsed)
	if err != nil {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "通行密钥验证失败"})
		return
	}

	adminUser, ok := user.(adminWebAuthnUser)
	if !ok {
		response.Error(w, http.StatusInternalServerError, "通行密钥用户上下文异常")
		return
	}
	if err := s.updateWebAuthnCredentialUsage(r.Context(), db, base64.RawURLEncoding.EncodeToString(credential.ID), *credential, s.requestClientIP(r), r.UserAgent()); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(adminUser.credentials) == 0 {
		response.Error(w, http.StatusInternalServerError, "通行密钥账户不可用")
		return
	}

	if err := s.issueAuthenticatedSession(w, r, db, "webauthn", map[string]interface{}{
		"credentialId": base64.RawURLEncoding.EncodeToString(credential.ID),
	}); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止删除通行密钥"})
		return
	}

	db, ok := s.openSessionDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	if !s.requireSession(w, r.Context(), db, r) {
		return
	}

	credentialID := strings.TrimPrefix(r.URL.Path, "/api/auth/webauthn/credentials/")
	credentialID = strings.TrimSuffix(credentialID, "/delete")
	credentialID = strings.TrimSpace(strings.Trim(credentialID, "/"))
	if credentialID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "通行密钥标识无效"})
		return
	}

	result, err := db.ExecContext(r.Context(), `DELETE FROM webauthn_credentials WHERE credential_id = ?`, credentialID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "通行密钥不存在"})
		return
	}
	_ = s.logOperation(r.Context(), db, "WEBAUTHN_CREDENTIAL_DELETED", "auth", map[string]interface{}{
		"credentialId": credentialID,
	}, s.requestClientIP(r), r.UserAgent())
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) issueAuthenticatedSession(w http.ResponseWriter, r *http.Request, db *sql.DB, method string, details map[string]interface{}) error {
	clientIP := s.requestClientIP(r)
	if err := s.resetLoginAttempts(r.Context(), db, clientIP); err != nil {
		return err
	}
	sessionPassword := "authenticated"
	if s.isDemoMode() {
		sessionPassword = "demo"
	}
	sid, err := s.createSession(r.Context(), db, sessionPassword)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(r.Context(), `UPDATE sessions SET ip_address=?, user_agent=? WHERE session_id=?`, clientIP, r.UserAgent(), sid); err != nil {
		return fmt.Errorf("update session metadata: %w", err)
	}
	payload := map[string]interface{}{
		"ip":     clientIP,
		"method": method,
	}
	for key, value := range details {
		payload[key] = value
	}
	if err := s.logOperation(r.Context(), db, strings.ToUpper(method)+"_LOGIN_SUCCESS", "auth", payload, clientIP, r.UserAgent()); err != nil {
		return err
	}

	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{
		Name:     "sid",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
	return nil
}

// hashStateVerifier 返回 state verifier 的 SHA-256 十六进制摘要，用于在 state
// flow 中保存「Cookie 是否匹配」的判据，而不落库明文。
func hashStateVerifier(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return hex.EncodeToString(sum[:])
}

// stateVerifierMatches 以恒定时间比较回调请求携带的 Cookie 与 state flow 中记录的摘要。
func stateVerifierMatches(r *http.Request, expectedDigest string) bool {
	if strings.TrimSpace(expectedDigest) == "" {
		return false
	}
	cookie, err := r.Cookie(githubOAuthStateCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	actual := hashStateVerifier(cookie.Value)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedDigest)) == 1
}

// clearGitHubStateCookie 在回调处理时立即失效一次性 state Cookie。
func (s *Service) clearGitHubStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    "",
		Path:     "/api/auth/github",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Service) githubAuthorizationURL(config githubOAuthConfig, baseURL, state string) (string, error) {
	callbackURL, err := joinURL(baseURL, "/api/auth/github/callback")
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("client_id", config.ClientID)
	values.Set("redirect_uri", callbackURL)
	values.Set("scope", "read:user user:email")
	values.Set("state", state)
	return "https://github.com/login/oauth/authorize?" + values.Encode(), nil
}

func (s *Service) exchangeGitHubAccessToken(ctx context.Context, config githubOAuthConfig, baseURL, code string) (string, error) {
	callbackURL, err := joinURL(baseURL, "/api/auth/github/callback")
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("client_id", config.ClientID)
	form.Set("client_secret", config.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", callbackURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "API-Monitor")

	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload githubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 || payload.AccessToken == "" || payload.Error != "" {
		if payload.Error != "" {
			return "", errors.New(payload.Error)
		}
		return "", fmt.Errorf("github oauth returned status %d", resp.StatusCode)
	}
	return payload.AccessToken, nil
}

func (s *Service) loadGitHubIdentity(ctx context.Context, accessToken string) (githubUserProfile, []githubUserEmail, error) {
	profileReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return githubUserProfile{}, nil, err
	}
	profileReq.Header.Set("Accept", "application/vnd.github+json")
	profileReq.Header.Set("Authorization", "Bearer "+accessToken)
	profileReq.Header.Set("User-Agent", "API-Monitor")

	client := &http.Client{Timeout: 12 * time.Second}
	profileResp, err := client.Do(profileReq)
	if err != nil {
		return githubUserProfile{}, nil, err
	}
	defer profileResp.Body.Close()
	if profileResp.StatusCode >= 400 {
		return githubUserProfile{}, nil, fmt.Errorf("github user api returned status %d", profileResp.StatusCode)
	}
	var profile githubUserProfile
	if err := json.NewDecoder(profileResp.Body).Decode(&profile); err != nil {
		return githubUserProfile{}, nil, err
	}

	emailsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return profile, nil, err
	}
	emailsReq.Header.Set("Accept", "application/vnd.github+json")
	emailsReq.Header.Set("Authorization", "Bearer "+accessToken)
	emailsReq.Header.Set("User-Agent", "API-Monitor")
	emailsResp, err := client.Do(emailsReq)
	if err != nil {
		return profile, nil, err
	}
	defer emailsResp.Body.Close()
	if emailsResp.StatusCode >= 400 {
		return profile, nil, fmt.Errorf("github emails api returned status %d", emailsResp.StatusCode)
	}
	var emails []githubUserEmail
	if err := json.NewDecoder(emailsResp.Body).Decode(&emails); err != nil {
		return profile, nil, err
	}
	return profile, emails, nil
}

func (s *Service) resolveBrowserBaseURL(ctx context.Context, db *sql.DB, r *http.Request) (string, error) {
	if configured, err := s.loadPublicAPIURL(ctx, db); err == nil && configured != "" {
		return configured, nil
	}

	requestHost := requestHostname(r)
	for _, candidate := range []string{
		strings.TrimSpace(r.Header.Get("Origin")),
		refererOrigin(r),
		requestSchemeAndHost(r),
	} {
		if candidate == "" {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if requestHost != "" && !strings.EqualFold(parsed.Hostname(), requestHost) {
			continue
		}
		parsed.Path = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	return "", fmt.Errorf("无法确定当前浏览器访问地址，请先在系统设置里配置公网 API 地址")
}

func (s *Service) webauthnForRequest(ctx context.Context, db *sql.DB, r *http.Request) (*wa.WebAuthn, string, error) {
	origin, err := s.resolveBrowserBaseURL(ctx, db, r)
	if err != nil {
		return nil, "", err
	}
	instance, err := s.webauthnForOrigin(origin)
	if err != nil {
		return nil, "", err
	}
	return instance, origin, nil
}

func (s *Service) webauthnForOrigin(origin string) (*wa.WebAuthn, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("通行密钥来源地址无效")
	}
	return wa.New(&wa.Config{
		RPDisplayName: "API Monitor",
		RPID:          parsed.Hostname(),
		RPOrigins:     []string{strings.TrimRight(origin, "/")},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		},
	})
}

func (s *Service) countWebAuthnCredentials(ctx context.Context, db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webauthn_credentials`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count webauthn credentials: %w", err)
	}
	return count, nil
}

func (s *Service) loadWebAuthnCredentials(ctx context.Context, db *sql.DB) ([]wa.Credential, error) {
	stored, err := s.loadStoredWebAuthnCredentials(ctx, db)
	if err != nil {
		return nil, err
	}
	credentials := make([]wa.Credential, 0, len(stored))
	for _, item := range stored {
		credentials = append(credentials, item.Credential)
	}
	return credentials, nil
}

func (s *Service) loadStoredWebAuthnCredentials(ctx context.Context, db *sql.DB) ([]storedWebAuthnCredential, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT credential_id, label, credential_json, created_at, COALESCE(last_used_at, ''), COALESCE(last_used_ip, ''), COALESCE(last_used_user_agent, '')
		FROM webauthn_credentials
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("load webauthn credentials: %w", err)
	}
	defer rows.Close()

	items := make([]storedWebAuthnCredential, 0)
	for rows.Next() {
		var item storedWebAuthnCredential
		var raw string
		if err := rows.Scan(&item.ID, &item.Label, &raw, &item.CreatedAt, &item.LastUsedAt, &item.LastUsedIP, &item.LastUsedUserAgent); err != nil {
			return nil, fmt.Errorf("scan webauthn credential: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &item.Credential); err != nil {
			return nil, fmt.Errorf("decode webauthn credential %s: %w", item.ID, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webauthn credentials: %w", err)
	}
	return items, nil
}

func (s *Service) saveWebAuthnCredential(ctx context.Context, db *sql.DB, credentialID, label string, credential wa.Credential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode webauthn credential: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO webauthn_credentials (credential_id, label, credential_json)
		VALUES (?, ?, ?)
	`, credentialID, label, string(raw))
	if err != nil {
		return fmt.Errorf("insert webauthn credential: %w", err)
	}
	return nil
}

func (s *Service) updateWebAuthnCredentialUsage(ctx context.Context, db *sql.DB, credentialID string, credential wa.Credential, ipAddress, userAgent string) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode webauthn credential: %w", err)
	}
	result, err := db.ExecContext(ctx, `
		UPDATE webauthn_credentials
		SET credential_json = ?, last_used_at = ?, last_used_ip = ?, last_used_user_agent = ?
		WHERE credential_id = ?
	`, string(raw), formatTime(time.Now().UTC()), ipAddress, userAgent, credentialID)
	if err != nil {
		return fmt.Errorf("update webauthn credential: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("update webauthn credential: credential not found")
	}
	return nil
}

func (s *Service) loadGitHubOAuthConfig(ctx context.Context, db *sql.DB) (githubOAuthConfig, error) {
	clientSecret, err := s.getConfig(ctx, db, githubClientSecretKey)
	if err != nil {
		return githubOAuthConfig{}, err
	}
	if clientSecret != "" {
		clientSecret, err = decryptNodeGCM(clientSecret)
		if err != nil {
			return githubOAuthConfig{}, fmt.Errorf("decrypt github oauth secret: %w", err)
		}
	}

	config := githubOAuthConfig{
		ClientSecret: clientSecret,
	}
	if value, err := s.getConfig(ctx, db, githubLoginEnabledKey); err != nil {
		return githubOAuthConfig{}, err
	} else {
		config.Enabled = strings.EqualFold(value, "true")
	}
	if value, err := s.getConfig(ctx, db, githubClientIDKey); err != nil {
		return githubOAuthConfig{}, err
	} else {
		config.ClientID = strings.TrimSpace(value)
	}
	if value, err := s.getConfig(ctx, db, githubAllowedLoginsKey); err != nil {
		return githubOAuthConfig{}, err
	} else {
		config.AllowedLogins = decodeStoredGitHubList(value)
	}
	if value, err := s.getConfig(ctx, db, githubAllowedEmailsKey); err != nil {
		return githubOAuthConfig{}, err
	} else {
		config.AllowedEmails = decodeStoredGitHubList(value)
	}
	return config, nil
}

func (s *Service) saveGitHubOAuthConfig(ctx context.Context, db *sql.DB, config githubOAuthConfig) error {
	encryptedSecret := ""
	if strings.TrimSpace(config.ClientSecret) != "" {
		secret, err := encryptNodeGCM(strings.TrimSpace(config.ClientSecret))
		if err != nil {
			return fmt.Errorf("encrypt github oauth secret: %w", err)
		}
		encryptedSecret = secret
	}
	if err := s.setConfig(ctx, db, githubLoginEnabledKey, boolString(config.Enabled), "GitHub OAuth 登录启用状态"); err != nil {
		return err
	}
	if err := s.setConfig(ctx, db, githubClientIDKey, strings.TrimSpace(config.ClientID), "GitHub OAuth Client ID"); err != nil {
		return err
	}
	if err := s.setConfig(ctx, db, githubClientSecretKey, encryptedSecret, "GitHub OAuth Client Secret(加密)"); err != nil {
		return err
	}
	if err := s.setConfig(ctx, db, githubAllowedLoginsKey, encodeGitHubList(config.AllowedLogins), "允许 GitHub OAuth 登录的用户名列表"); err != nil {
		return err
	}
	if err := s.setConfig(ctx, db, githubAllowedEmailsKey, encodeGitHubList(config.AllowedEmails), "允许 GitHub OAuth 登录的邮箱列表"); err != nil {
		return err
	}
	return nil
}

func (s *Service) githubConfigForResponse(config githubOAuthConfig) githubConfigResponse {
	return githubConfigResponse{
		Enabled:           config.Enabled,
		ClientID:          config.ClientID,
		HasClientSecret:   strings.TrimSpace(config.ClientSecret) != "",
		AllowedLoginsText: strings.Join(config.AllowedLogins, "\n"),
		AllowedEmailsText: strings.Join(config.AllowedEmails, "\n"),
	}
}

func (c githubOAuthConfig) complete() bool {
	return c.Enabled && strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != "" && (len(c.AllowedLogins) > 0 || len(c.AllowedEmails) > 0)
}

func (c githubOAuthConfig) allows(login, email string, emails []githubUserEmail) bool {
	login = strings.ToLower(strings.TrimSpace(login))
	email = strings.ToLower(strings.TrimSpace(email))
	if containsString(c.AllowedLogins, login) {
		return true
	}
	if email != "" && containsString(c.AllowedEmails, email) {
		return true
	}
	for _, item := range emails {
		if containsString(c.AllowedEmails, strings.ToLower(strings.TrimSpace(item.Email))) {
			return true
		}
	}
	return false
}

func (s *Service) createAuthFlow(ctx context.Context, db *sql.DB, flowType string, payload interface{}, ttl time.Duration) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate auth flow id: %w", err)
	}
	id := hex.EncodeToString(raw)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode auth flow: %w", err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_flows (id, flow_type, payload, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, flowType, string(encoded), formatTime(now), formatTime(now.Add(ttl))); err != nil {
		return "", fmt.Errorf("insert auth flow: %w", err)
	}
	_, _ = db.ExecContext(ctx, `DELETE FROM auth_flows WHERE expires_at <= ? OR consumed_at IS NOT NULL`, formatTime(now))
	return id, nil
}

func (s *Service) consumeAuthFlow(ctx context.Context, db *sql.DB, flowType, id string, target interface{}) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin auth flow transaction: %w", err)
	}
	defer tx.Rollback()

	var payload string
	var expiresAt string
	var consumedAt sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT payload, expires_at, consumed_at
		FROM auth_flows
		WHERE id = ? AND flow_type = ?
	`, strings.TrimSpace(id), flowType).Scan(&payload, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load auth flow: %w", err)
	}
	if consumedAt.Valid && consumedAt.String != "" {
		return false, nil
	}
	expires, err := parseDBTime(expiresAt)
	if err != nil || !expires.After(time.Now().UTC()) {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_flows SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, formatTime(time.Now().UTC()), id); err != nil {
		return false, fmt.Errorf("consume auth flow: %w", err)
	}
	if target != nil {
		if err := json.Unmarshal([]byte(payload), target); err != nil {
			return false, fmt.Errorf("decode auth flow: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit auth flow: %w", err)
	}
	return true, nil
}

func (s *Service) loadPublicAPIURL(ctx context.Context, db *sql.DB) (string, error) {
	var configured sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT public_api_url FROM user_settings WHERE id = 1`).Scan(&configured); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("load public api url: %w", err)
	}
	if !configured.Valid {
		return "", nil
	}
	value := strings.TrimRight(strings.TrimSpace(configured.String), "/")
	if value == "" {
		return "", nil
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return value, nil
	}
	return "", nil
}

func decodeWebAuthnFinishRequest(r *http.Request) (webAuthnFinishRequest, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return webAuthnFinishRequest{}, fmt.Errorf("读取请求失败")
	}
	var payload webAuthnFinishRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return webAuthnFinishRequest{}, fmt.Errorf("请求参数验证失败")
	}
	if strings.TrimSpace(payload.FlowID) == "" || len(payload.Credential) == 0 {
		return webAuthnFinishRequest{}, fmt.Errorf("缺少必要参数")
	}
	return payload, nil
}

func normalizeGitHubList(value string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	items := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		item := strings.ToLower(strings.TrimSpace(field))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func encodeGitHubList(items []string) string {
	raw, _ := json.Marshal(items)
	return string(raw)
}

func decodeStoredGitHubList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return normalizeGitHubList(strings.Join(items, "\n"))
	}
	return normalizeGitHubList(value)
}

func containsString(items []string, candidate string) bool {
	for _, item := range items {
		if item == candidate {
			return true
		}
	}
	return false
}

func requestHostname(r *http.Request) string {
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if parsed := strings.TrimSpace(host); parsed != "" {
		if value, err := url.Parse("http://" + parsed); err == nil {
			return value.Hostname()
		}
	}
	return ""
}

func refererOrigin(r *http.Request) string {
	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func requestSchemeAndHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func joinURL(baseURL, routePath string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	parsed.Path = path.Join(parsed.Path, routePath)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (s *Service) redirectAuthPage(w http.ResponseWriter, r *http.Request, baseURL string, params map[string]string) {
	target, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "登录回跳地址无效")
		return
	}
	if len(params) > 0 {
		query := target.Query()
		for key, value := range params {
			if strings.TrimSpace(value) != "" {
				query.Set(key, value)
			}
		}
		target.RawQuery = query.Encode()
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
