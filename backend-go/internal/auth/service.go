package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

const (
	maxLoginAttempts   = 5
	lockDuration       = 15 * time.Minute
	sessionDuration    = 24 * time.Hour
	sessionLimit       = 10
	system2FAEnabled   = "system_2fa_enabled"
	system2FASecretKey = "system_2fa_secret"
	system2FAAppName   = "API-Monitor"
)

var bcryptHashPattern = regexp.MustCompile(`^\$2[aby]?\$\d{1,2}\$[./A-Za-z0-9]{53}$`)

type Service struct {
	cfg   config.Config
	store *database.Store
}

type loginRequest struct {
	Password  string `json:"password"`
	TOTPToken string `json:"totpToken"`
}

type setPasswordRequest struct {
	Password string `json:"password"`
}

type verifyPasswordRequest struct {
	Password string `json:"password"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type enable2FARequest struct {
	Secret string `json:"secret"`
	Token  string `json:"token"`
}

type disable2FARequest struct {
	Password string `json:"password"`
}

type loginAttemptStatus struct {
	Locked            bool
	RemainingAttempts int
	RemainingSeconds  int
	LockUntil         string
}

type sessionRecord struct {
	ID             string
	Password       string
	CreatedAt      string
	LastAccessedAt string
	ExpiresAt      string
	IsActive       int
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg, store: database.New(cfg)}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/auth/check-password":
		s.checkPassword(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/auth/session":
		s.session(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
		s.login(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/logout":
		s.logout(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/set-password":
		s.setPassword(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/verify-password":
		s.verifyPassword(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/change-password":
		s.changePassword(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/auth/2fa/status":
		s.status2FA(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/2fa/setup":
		s.setup2FA(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/2fa/enable":
		s.enable2FA(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/2fa/disable":
		s.disable2FA(w, r)
	default:
		response.Error(w, http.StatusNotFound, "auth route not implemented")
	}
}

func (s *Service) IsAuthenticated(ctx context.Context, r *http.Request) (bool, error) {
	db, err := s.openDB(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()

	_, ok, err := s.validateRequestSession(ctx, db, r)
	return ok, err
}

func (s *Service) checkPassword(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	password, err := s.loadAdminPassword(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"hasPassword": password != "",
		"isDemoMode":  s.isDemoMode(),
	})
}

func (s *Service) session(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	_, ok, err := s.validateRequestSession(r.Context(), db, r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"authenticated": ok})
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	clientIP := requestClientIP(r)
	if lockStatus, err := s.loginLockStatus(r.Context(), db, clientIP); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	} else if lockStatus.Locked {
		_ = s.logOperation(r.Context(), db, "LOGIN_BLOCKED", "auth", map[string]interface{}{
			"ip":     clientIP,
			"reason": "ip_locked",
		}, clientIP, r.UserAgent())
		remainingMinutes := (lockStatus.RemainingSeconds + 59) / 60
		if remainingMinutes < 1 {
			remainingMinutes = 1
		}
		response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"success":   false,
			"error":     fmt.Sprintf("登录尝试过多，请 %d 分钟后再试", remainingMinutes),
			"lockUntil": lockStatus.LockUntil,
		})
		return
	}

	savedPassword, err := s.loadAdminPassword(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.isDemoMode() {
		// Demo mode intentionally skips password verification, matching the Node backend.
	} else {
		if savedPassword == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "请先设置管理员密码",
			})
			return
		}
		if !verifyPassword(body.Password, savedPassword) {
			attempt, err := s.recordFailedAttempt(r.Context(), db, clientIP)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			_ = s.logOperation(r.Context(), db, "LOGIN_FAILED", "auth", map[string]interface{}{
				"ip":                clientIP,
				"remainingAttempts": attempt.RemainingAttempts,
			}, clientIP, r.UserAgent())
			if attempt.Locked {
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"success":   false,
					"error":     "登录尝试过多，账户已锁定 15 分钟",
					"lockUntil": attempt.LockUntil,
				})
				return
			}
			response.JSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("密码错误，还剩 %d 次尝试机会", attempt.RemainingAttempts),
			})
			return
		}
	}

	enabled, err := s.is2FAEnabled(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if enabled {
		if strings.TrimSpace(body.TOTPToken) == "" {
			response.JSON(w, http.StatusOK, map[string]interface{}{
				"success":    false,
				"require2FA": true,
				"error":      "请输入双因素验证码",
			})
			return
		}
		ok, err := s.verifyLogin2FA(r.Context(), db, body.TOTPToken)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			_ = s.logOperation(r.Context(), db, "LOGIN_2FA_FAILED", "auth", map[string]interface{}{
				"ip": clientIP,
			}, clientIP, r.UserAgent())
			response.JSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success":    false,
				"error":      "双因素验证码错误",
				"require2FA": true,
			})
			return
		}
	}

	if err := s.resetLoginAttempts(r.Context(), db, clientIP); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	sessionPassword := "authenticated"
	if s.isDemoMode() {
		sessionPassword = "demo"
	}
	sid, err := s.createSession(r.Context(), db, sessionPassword)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.logOperation(r.Context(), db, "LOGIN_SUCCESS", "auth", map[string]interface{}{
		"ip":      clientIP,
		"with2FA": enabled,
	}, clientIP, r.UserAgent())

	http.SetCookie(w, &http.Cookie{
		Name:     "sid",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": sid,
	})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	clientIP := requestClientIP(r)
	_, _ = s.destroySession(r.Context(), db, r)
	_ = s.logOperation(r.Context(), db, "LOGOUT", "auth", map[string]interface{}{
		"ip": clientIP,
	}, clientIP, r.UserAgent())

	http.SetCookie(w, &http.Cookie{
		Name:     "sid",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) setPassword(w http.ResponseWriter, r *http.Request) {
	var body setPasswordRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]string{"error": "演示模式禁止设置密码"})
		return
	}
	if strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")) != "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "密码已通过环境变量设置，无法修改"})
		return
	}
	if len(body.Password) < 6 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "密码长度至少6位"})
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	current, err := s.getConfig(r.Context(), db, "admin_password")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current != "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "密码已设置，无法重复设置"})
		return
	}
	if err := s.saveAdminPassword(r.Context(), db, body.Password); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "保存密码失败"})
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) verifyPassword(w http.ResponseWriter, r *http.Request) {
	var body verifyPasswordRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	savedPassword, err := s.loadAdminPassword(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if savedPassword == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请先设置密码"})
		return
	}
	if verifyPassword(body.Password, savedPassword) {
		response.JSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "密码错误"})
}

func (s *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	var body changePasswordRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	clientIP := requestClientIP(r)
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止修改密码"})
		return
	}
	if len(body.NewPassword) < 6 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "新密码长度至少6位"})
		return
	}

	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	savedPassword, err := s.loadAdminPassword(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if savedPassword == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请先设置密码"})
		return
	}
	if !verifyPassword(body.OldPassword, savedPassword) {
		_ = s.logOperation(r.Context(), db, "PASSWORD_CHANGE_FAILED", "auth", map[string]interface{}{
			"ip":     clientIP,
			"reason": "wrong_old_password",
		}, clientIP, r.UserAgent())
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "原密码错误"})
		return
	}
	if err := s.saveAdminPassword(r.Context(), db, body.NewPassword); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "保存密码失败"})
		return
	}
	_ = s.logOperation(r.Context(), db, "PASSWORD_CHANGED", "auth", map[string]interface{}{
		"ip": clientIP,
	}, clientIP, r.UserAgent())
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) status2FA(w http.ResponseWriter, r *http.Request) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	enabled, err := s.is2FAEnabled(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{
		"success": true,
		"enabled": enabled,
	})
}

func (s *Service) setup2FA(w http.ResponseWriter, r *http.Request) {
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止设置 2FA"})
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

	secret, err := generate2FASecret()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	qrCode, err := generate2FAQrCode(secret, "admin")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"secret":  secret,
		"qrCode":  qrCode,
	})
}

func (s *Service) enable2FA(w http.ResponseWriter, r *http.Request) {
	var body enable2FARequest
	if !decodeJSON(w, r, &body) {
		return
	}

	clientIP := requestClientIP(r)
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止设置 2FA"})
		return
	}
	if strings.TrimSpace(body.Secret) == "" || strings.TrimSpace(body.Token) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少必要参数"})
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

	secret := normalizeBase32Secret(body.Secret)
	if !verifyTOTP(secret, body.Token, time.Now().UTC()) {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "验证码错误，请重试"})
		return
	}
	encryptedSecret, err := encryptNodeGCM(secret)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.setConfig(r.Context(), db, system2FASecretKey, encryptedSecret, "系统 2FA 密钥(加密)"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.setConfig(r.Context(), db, system2FAEnabled, "true", "系统 2FA 启用状态"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.logOperation(r.Context(), db, "2FA_ENABLED", "auth", map[string]interface{}{
		"ip": clientIP,
	}, clientIP, r.UserAgent())

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "2FA 已启用"})
}

func (s *Service) disable2FA(w http.ResponseWriter, r *http.Request) {
	var body disable2FARequest
	if !decodeJSON(w, r, &body) {
		return
	}

	clientIP := requestClientIP(r)
	if s.isDemoMode() {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "演示模式禁止禁用 2FA"})
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

	savedPassword, err := s.loadAdminPassword(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !verifyPassword(body.Password, savedPassword) {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "密码错误"})
		return
	}
	if err := s.setConfig(r.Context(), db, system2FAEnabled, "false", "系统 2FA 启用状态"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.deleteConfig(r.Context(), db, system2FASecretKey); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.logOperation(r.Context(), db, "2FA_DISABLED", "auth", map[string]interface{}{
		"ip": clientIP,
	}, clientIP, r.UserAgent())

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "2FA 已禁用"})
}

func (s *Service) openDB(ctx context.Context) (*sql.DB, error) {
	return s.store.Open(ctx)
}

func (s *Service) loadAdminPassword(ctx context.Context, db *sql.DB) (string, error) {
	if password := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")); password != "" {
		return password, nil
	}
	return s.getConfig(ctx, db, "admin_password")
}

func (s *Service) getConfig(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get config %s: %w", key, err)
	}
	return value, nil
}

func (s *Service) setConfig(ctx context.Context, db *sql.DB, key, value, description string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO system_config (key, value, description, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			description = excluded.description,
			updated_at = CURRENT_TIMESTAMP
	`, key, value, description)
	if err != nil {
		return fmt.Errorf("set config %s: %w", key, err)
	}
	return nil
}

func (s *Service) deleteConfig(ctx context.Context, db *sql.DB, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM system_config WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete config %s: %w", key, err)
	}
	return nil
}

func (s *Service) saveAdminPassword(ctx context.Context, db *sql.DB, password string) error {
	hashed := password
	if !isHashed(password) {
		generated, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		hashed = string(generated)
	}
	return s.setConfig(ctx, db, "admin_password", hashed, "管理员密码(哈希)")
}

func (s *Service) isDemoMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("DEMO_MODE")), "true")
}

func isHashed(password string) bool {
	return bcryptHashPattern.MatchString(password)
}

func verifyPassword(plaintext, stored string) bool {
	if plaintext == "" || stored == "" {
		return false
	}
	if !isHashed(stored) {
		return plaintext == stored
	}
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plaintext)) == nil
}

func (s *Service) createSession(ctx context.Context, db *sql.DB, password string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	sid := hex.EncodeToString(raw)
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (session_id, password, created_at, last_accessed_at, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, 1)
	`, sid, password, formatTime(now), formatTime(now), formatTime(now.Add(sessionDuration)))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	_, _ = db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE session_id NOT IN (
			SELECT session_id FROM sessions
			ORDER BY last_accessed_at DESC
			LIMIT ?
		)
	`, sessionLimit)
	return sid, nil
}

func (s *Service) validateRequestSession(ctx context.Context, db *sql.DB, r *http.Request) (sessionRecord, bool, error) {
	cookie, err := r.Cookie("sid")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return sessionRecord{}, false, nil
	}
	return s.validateSession(ctx, db, cookie.Value)
}

func (s *Service) validateSession(ctx context.Context, db *sql.DB, sid string) (sessionRecord, bool, error) {
	var session sessionRecord
	err := db.QueryRowContext(ctx, `
		SELECT session_id, password, created_at, last_accessed_at, expires_at, is_active
		FROM sessions
		WHERE session_id = ?
	`, sid).Scan(
		&session.ID,
		&session.Password,
		&session.CreatedAt,
		&session.LastAccessedAt,
		&session.ExpiresAt,
		&session.IsActive,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return sessionRecord{}, false, nil
	}
	if err != nil {
		return sessionRecord{}, false, fmt.Errorf("get session: %w", err)
	}
	if session.IsActive == 0 {
		return session, false, nil
	}
	expiresAt, err := parseDBTime(session.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		_, _ = db.ExecContext(ctx, `UPDATE sessions SET is_active = 0 WHERE session_id = ?`, sid)
		return session, false, nil
	}
	_, err = db.ExecContext(ctx, `UPDATE sessions SET last_accessed_at = ? WHERE session_id = ?`, formatTime(time.Now().UTC()), sid)
	if err != nil {
		return sessionRecord{}, false, fmt.Errorf("touch session: %w", err)
	}
	return session, true, nil
}

func (s *Service) destroySession(ctx context.Context, db *sql.DB, r *http.Request) (bool, error) {
	cookie, err := r.Cookie("sid")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false, nil
	}
	result, err := db.ExecContext(ctx, `UPDATE sessions SET is_active = 0 WHERE session_id = ?`, cookie.Value)
	if err != nil {
		return false, fmt.Errorf("destroy session: %w", err)
	}
	changes, _ := result.RowsAffected()
	return changes > 0, nil
}

func (s *Service) openSessionDB(w http.ResponseWriter, r *http.Request) (*sql.DB, bool) {
	db, err := s.openDB(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return db, true
}

func (s *Service) requireSession(w http.ResponseWriter, ctx context.Context, db *sql.DB, r *http.Request) bool {
	_, ok, err := s.validateRequestSession(ctx, db, r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "请先登录"})
		return false
	}
	return true
}

func (s *Service) loginLockStatus(ctx context.Context, db *sql.DB, ip string) (loginAttemptStatus, error) {
	var lockedUntil sql.NullString
	err := db.QueryRowContext(ctx, `SELECT locked_until FROM login_attempts WHERE ip_address = ?`, ip).Scan(&lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return loginAttemptStatus{Locked: false}, nil
	}
	if err != nil {
		return loginAttemptStatus{}, fmt.Errorf("get login lock: %w", err)
	}
	if !lockedUntil.Valid || lockedUntil.String == "" {
		return loginAttemptStatus{Locked: false}, nil
	}
	until, err := parseDBTime(lockedUntil.String)
	if err != nil || !until.After(time.Now().UTC()) {
		return loginAttemptStatus{Locked: false}, nil
	}
	return loginAttemptStatus{
		Locked:           true,
		RemainingSeconds: int(time.Until(until).Seconds()),
		LockUntil:        lockedUntil.String,
	}, nil
}

func (s *Service) recordFailedAttempt(ctx context.Context, db *sql.DB, ip string) (loginAttemptStatus, error) {
	now := time.Now().UTC()
	var failedCount int
	var lockedUntil sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT failed_count, locked_until FROM login_attempts WHERE ip_address = ?
	`, ip).Scan(&failedCount, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = db.ExecContext(ctx, `
			INSERT INTO login_attempts (ip_address, failed_count, last_attempt)
			VALUES (?, 1, ?)
		`, ip, formatTime(now))
		if err != nil {
			return loginAttemptStatus{}, fmt.Errorf("insert login attempt: %w", err)
		}
		return loginAttemptStatus{RemainingAttempts: maxLoginAttempts - 1}, nil
	}
	if err != nil {
		return loginAttemptStatus{}, fmt.Errorf("get login attempt: %w", err)
	}
	if lockedUntil.Valid && lockedUntil.String != "" {
		if until, err := parseDBTime(lockedUntil.String); err == nil && until.After(now) {
			return loginAttemptStatus{
				Locked:            true,
				RemainingAttempts: 0,
				RemainingSeconds:  int(time.Until(until).Seconds()),
				LockUntil:         lockedUntil.String,
			}, nil
		}
		failedCount = 0
	}

	failedCount++
	if failedCount >= maxLoginAttempts {
		lockUntil := now.Add(lockDuration)
		lockUntilStr := formatTime(lockUntil)
		_, err = db.ExecContext(ctx, `
			UPDATE login_attempts
			SET failed_count = ?, locked_until = ?, last_attempt = ?
			WHERE ip_address = ?
		`, failedCount, lockUntilStr, formatTime(now), ip)
		if err != nil {
			return loginAttemptStatus{}, fmt.Errorf("lock login attempt: %w", err)
		}
		return loginAttemptStatus{
			Locked:            true,
			RemainingAttempts: 0,
			RemainingSeconds:  int(lockDuration.Seconds()),
			LockUntil:         lockUntilStr,
		}, nil
	}

	_, err = db.ExecContext(ctx, `
		UPDATE login_attempts
		SET failed_count = ?, locked_until = NULL, last_attempt = ?
		WHERE ip_address = ?
	`, failedCount, formatTime(now), ip)
	if err != nil {
		return loginAttemptStatus{}, fmt.Errorf("update login attempt: %w", err)
	}
	return loginAttemptStatus{RemainingAttempts: maxLoginAttempts - failedCount}, nil
}

func (s *Service) resetLoginAttempts(ctx context.Context, db *sql.DB, ip string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM login_attempts WHERE ip_address = ?`, ip)
	if err != nil {
		return fmt.Errorf("reset login attempts: %w", err)
	}
	return nil
}

func (s *Service) logOperation(ctx context.Context, db *sql.DB, operationType, tableName string, details map[string]interface{}, ipAddress, userAgent string) error {
	detailsJSON, _ := json.Marshal(details)
	_, err := db.ExecContext(ctx, `
		INSERT INTO operation_logs (operation_type, table_name, details, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?)
	`, operationType, tableName, string(detailsJSON), ipAddress, userAgent)
	return err
}

func (s *Service) is2FAEnabled(ctx context.Context, db *sql.DB) (bool, error) {
	value, err := s.getConfig(ctx, db, system2FAEnabled)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(value, "true"), nil
}

func (s *Service) verifyLogin2FA(ctx context.Context, db *sql.DB, token string) (bool, error) {
	encryptedSecret, err := s.getConfig(ctx, db, system2FASecretKey)
	if err != nil {
		return false, err
	}
	if encryptedSecret == "" {
		return false, nil
	}
	secret, err := decryptNodeGCM(encryptedSecret)
	if err != nil {
		return false, err
	}
	return verifyTOTP(secret, token, time.Now().UTC()), nil
}

func generate2FASecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 2FA 密钥失败")
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func generate2FAQrCode(secret, label string) (string, error) {
	png, err := qrcode.Encode(otpAuthURL(secret, label), qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("生成 2FA 密钥失败")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func otpAuthURL(secret, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "admin"
	}
	return fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s",
		queryEscape(system2FAAppName),
		queryEscape(label),
		normalizeBase32Secret(secret),
		queryEscape(system2FAAppName),
	)
}

func queryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func decryptNodeGCM(encrypted string) (string, error) {
	return secure.DecryptNodeGCM(encrypted)
}

func encryptNodeGCM(plain string) (string, error) {
	return secure.EncryptNodeGCM(plain)
}

func verifyTOTP(secret, token string, now time.Time) bool {
	token = strings.ReplaceAll(strings.TrimSpace(token), " ", "")
	if len(token) != 6 {
		return false
	}
	secret = normalizeBase32Secret(secret)
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return false
	}
	counter := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		if hotp(key, uint64(counter+offset)) == token {
			return true
		}
	}
	return false
}

func normalizeBase32Secret(secret string) string {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	secret = strings.ReplaceAll(secret, " ", "")
	secret = strings.TrimRight(secret, "=")
	return secret
}

func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", code%1000000)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	if strings.TrimSpace(buf.String()) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	if err := json.Unmarshal(buf.Bytes(), target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	return true
}

func requestClientIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000Z",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %s", value)
}
