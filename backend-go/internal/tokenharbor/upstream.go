package tokenharbor

// 本文件封装对 TokenHarbor / Supabase 的 HTTP 调用:共享客户端、Cookie 解析、
// 会话创建、对话流、额度查询,以及登录与自动续期。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// thBaseURL 是 TokenHarbor 站点基址(direct-chat 接口挂在它下面)。
	thBaseURL = "https://tokenharbor.ai"
	// supabaseAuthURL 是站点使用的 Supabase 认证端点。
	supabaseAuthURL = "https://auth.tokenharbor.ai"
	// supabaseAnonKey 是浏览器公开的 anon key(非机密)。
	supabaseAnonKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImlzYm56bXdqbXRpdWlwZXNnbW1nIiwicm9sZSI6ImFub24iLCJpYXQiOjE3NzY3NjU1MzYsImV4cCI6MjA5MjM0MTUzNn0.CodUcchio6jNW_k68vaAb--LshBQXK51tZ6VTxNSz_A"
	// desktopUA 与站点浏览器一致的 UA,上游对 UA 有基本校验。
	desktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

	maxCatalogBytes = 4 << 20
	maxErrorBytes   = 64 << 10
)

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 是无总超时的共享客户端(长流式回答不能被总超时截断)。
func sharedHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		sharedClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConnsPerHost: 5,
			},
		}
	})
	return sharedClient
}

// httpClientFor 返回本次出网要用的客户端:配置了代理池时经 ProxyPoolSelector 选出口。
func (s *Service) httpClientFor(sessionKey string) *http.Client {
	poolID := strings.TrimSpace(s.Settings().ProxyPoolID)
	if poolID == "" || s.externalPool == nil {
		return sharedHTTPClient()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := s.externalPool.SelectProxy(ctx, poolID, sessionKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return sharedHTTPClient()
	}
	u, err := httpProxyURL(strings.TrimSpace(raw))
	if err != nil {
		return sharedHTTPClient()
	}
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(u),
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 5,
		},
	}
}

// httpProxyURL 解析代理字符串为 *url.URL;非法返回 error。
func httpProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("代理地址非法")
	}
	return u, nil
}

// doJSON 发一次请求并读取响应体(受 limit 限制)。timeout > 0 时套请求级总超时。
func (s *Service) doJSON(ctx context.Context, client *http.Client, method, target string, body []byte, headers map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return resp.StatusCode, data, err
	}
	return resp.StatusCode, data, nil
}

// siteHeaders 构造对 TokenHarbor 站点的请求头(origin / referer 必须与站点一致)。
func siteHeaders(cookie string) map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/event-stream",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Content-Type":    "application/json",
		"Origin":          thBaseURL,
		"Referer":         thBaseURL + "/chat",
		"User-Agent":      desktopUA,
		"Cookie":          cookie,
	}
}

// jsonMessage 从上游错误体里尽量取出人类可读文案。
func jsonMessage(data []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return truncate(strings.TrimSpace(string(data)), 300)
	}
	for _, key := range []string{"message", "error_description", "error", "msg", "detail"} {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil && text != "" {
			return truncate(text, 300)
		}
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(raw, &inner); err == nil {
			var msg string
			if err := json.Unmarshal(inner["message"], &msg); err == nil && msg != "" {
				return truncate(msg, 300)
			}
		}
	}
	return truncate(strings.TrimSpace(string(data)), 300)
}

// -----------------------------------------------------------------------------
// Cookie 解析
// -----------------------------------------------------------------------------

// authSession 是从 Cookie 里解析出的会话信息。
type authSession struct {
	AccessToken  string
	RefreshToken string
	ExpiresAtSec int64
	Email        string
	UserID       string
}

// normalizeCookie 把用户粘贴的多种形态(裸 Cookie 头 / curl -b '-H Cookie:')
// 归一化成标准 Cookie 头;识别不到 sb-auth-auth-token.0 时返回空串。
func normalizeCookie(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	idx := strings.Index(raw, "sb-auth-auth-token.0=")
	if idx < 0 {
		return ""
	}
	raw = raw[idx:]
	if i := strings.IndexAny(raw, "\r\n"); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"'")
		p = strings.ReplaceAll(p, "\\", "")
		if p == "" || !strings.Contains(p, "=") {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "; ")
}

// parseAuthSession 解码 sb-auth-auth-token.0 得到会话信息。
func parseAuthSession(cookie string) (authSession, bool) {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "sb-auth-auth-token.0=") {
			continue
		}
		val := strings.TrimPrefix(part, "sb-auth-auth-token.0=")
		val = strings.Trim(val, "\"")
		val = strings.TrimPrefix(val, "base64-")
		text, ok := decodeAuth0Text(val)
		if !ok {
			continue
		}
		return authSession{
			AccessToken:  extractJSONString(text, "access_token"),
			RefreshToken: extractJSONString(text, "refresh_token"),
			ExpiresAtSec: extractJSONInt(text, "expires_at"),
			Email:        extractJSONString(text, "email"),
			UserID:       extractJSONString(text, "id"),
		}, true
	}
	return authSession{}, false
}

// decodeAuth0Text 把 base64 值解成 JSON 文本;偶发截断时尝试去掉尾部 1-3 个字符。
func decodeAuth0Text(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return "", false
	}
	for cut := 0; cut <= 3 && cut < len(val); cut++ {
		candidate := val[:len(val)-cut]
		if data, err := decodeBase64Flexible(candidate); err == nil {
			return string(data), true
		}
	}
	return "", false
}

// decodeBase64Flexible 依次尝试 URL-safe 无填充 / URL-safe / 标准 base64。
func decodeBase64Flexible(s string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	if data, err := base64.URLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func extractJSONString(text, key string) string {
	needle := `"` + key + `":"`
	i := strings.Index(text, needle)
	if i < 0 {
		return ""
	}
	rest := text[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func extractJSONInt(text, key string) int64 {
	needle := `"` + key + `":`
	i := strings.Index(text, needle)
	if i < 0 {
		return 0
	}
	rest := text[i+len(needle):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(rest[:end], 10, 64)
	return n
}

func accessTokenFromCookie(cookie string) string {
	if sess, ok := parseAuthSession(cookie); ok {
		return sess.AccessToken
	}
	return ""
}

func refreshTokenFromCookie(cookie string) string {
	if sess, ok := parseAuthSession(cookie); ok {
		return sess.RefreshToken
	}
	return ""
}

func emailFromCookie(cookie string) string {
	if sess, ok := parseAuthSession(cookie); ok {
		return sess.Email
	}
	return ""
}

// expiresAtMS 返回 access token 的过期时刻(毫秒);优先用会话 expires_at,回退 JWT exp。
func expiresAtMS(cookie string) int64 {
	sess, ok := parseAuthSession(cookie)
	if ok {
		if sess.ExpiresAtSec > 0 {
			return sess.ExpiresAtSec * 1000
		}
		if ms := jwtExpiryMS(sess.AccessToken); ms > 0 {
			return ms
		}
	}
	return 0
}

// jwtExpiryMS 解码 JWT payload 的 exp(秒),返回毫秒;解不出返回 0(不验签)。
func jwtExpiryMS(token string) int64 {
	claims, ok := jwtClaims(token)
	if !ok {
		return 0
	}
	raw, ok := claims["exp"]
	if !ok {
		return 0
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(num.String(), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
}

// jwtSub 解码 JWT payload 里的用户 id(sub,不验签)。
func jwtSub(token string) string {
	claims, ok := jwtClaims(token)
	if !ok {
		return ""
	}
	for _, key := range []string{"sub", "user_id", "userId"} {
		raw, ok := claims[key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil && text != "" {
			return text
		}
	}
	return ""
}

// jwtClaims 解出 JWT payload(不验签)。
func jwtClaims(token string) (map[string]json.RawMessage, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, false
		}
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

// -----------------------------------------------------------------------------
// Supabase 登录 / 续期
// -----------------------------------------------------------------------------

// supabaseSession 是 Supabase 登录/续期响应的会话字段。
type supabaseSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type"`
	User         struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

// supabaseToken 调 Supabase 的 token 端点进行登录或续期。
func (s *Service) supabaseToken(ctx context.Context, sessionKey, grantType string, payload map[string]string) (supabaseSession, error) {
	body, _ := json.Marshal(payload)
	target := supabaseAuthURL + "/auth/v1/token?grant_type=" + grantType
	headers := map[string]string{
		"apikey":        supabaseAnonKey,
		"Authorization": "Bearer " + supabaseAnonKey,
		"Content-Type":  "application/json",
		"Accept":        "application/json",
	}
	status, data, err := s.doJSON(ctx, s.httpClientFor(sessionKey), http.MethodPost, target, body, headers, 30*time.Second, maxErrorBytes)
	if err != nil {
		return supabaseSession{}, fmt.Errorf("请求失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return supabaseSession{}, fmt.Errorf("HTTP %d: %s", status, jsonMessage(data))
	}
	var sess supabaseSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return supabaseSession{}, fmt.Errorf("响应解析失败: %w", err)
	}
	if sess.AccessToken == "" || sess.RefreshToken == "" {
		return supabaseSession{}, fmt.Errorf("响应缺少 token")
	}
	if sess.ExpiresAt <= 0 {
		sess.ExpiresAt = time.Now().Unix() + maxInt64(sess.ExpiresIn, 3600)
	}
	return sess, nil
}

// loginEmail 用邮箱密码登录 Supabase,返回组装好的 Cookie。
func (s *Service) loginEmail(ctx context.Context, email, password string) (string, string, int64, error) {
	sess, err := s.supabaseToken(ctx, "login", "password", map[string]string{
		"email": email, "password": password,
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("登录失败: %w", err)
	}
	cookie := buildCookieFromSession(sess, true)
	userID := sess.User.ID
	if userID == "" {
		userID = jwtSub(sess.AccessToken)
	}
	return cookie, userID, sess.ExpiresAt * 1000, nil
}

// refreshSession 用 refresh_token 换新会话,并把新 Cookie 写回 acc。
func (s *Service) refreshSession(ctx context.Context, acc *Account, refreshToken string) error {
	sess, err := s.supabaseToken(ctx, "refresh", "refresh_token", map[string]string{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return fmt.Errorf("刷新会话失败: %w", err)
	}
	before := acc.Cookie
	acc.Cookie = rebuildCookie(acc.Cookie, sess)
	acc.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	acc.ExpiresAtMS = sess.ExpiresAt * 1000
	if acc.UserID == "" {
		acc.UserID = firstNonEmpty(sess.User.ID, jwtSub(sess.AccessToken))
	}
	if acc.Email == "" {
		acc.Email = sess.User.Email
	}
	if acc.Cookie != before {
		acc.LastError = ""
	}
	return nil
}

// buildCookieFromSession 用会话组装 Cookie(登录时同时写 tok0 + tok1 + th_sid)。
func buildCookieFromSession(sess supabaseSession, includeTok1 bool) string {
	obj := map[string]interface{}{
		"access_token":  sess.AccessToken,
		"token_type":    firstNonEmpty(sess.TokenType, "bearer"),
		"expires_in":    maxInt64(sess.ExpiresIn, 3600),
		"expires_at":    sess.ExpiresAt,
		"refresh_token": sess.RefreshToken,
		"user":          map[string]interface{}{"id": sess.User.ID, "email": sess.User.Email},
	}
	tok0 := "base64-" + encodeAuth0JSON(obj)
	parts := []string{"sb-auth-auth-token.0=" + tok0}
	if includeTok1 {
		parts = append(parts, "sb-auth-auth-token.1="+tok0)
	}
	parts = append(parts, "th_sid="+randomHex(16))
	return strings.Join(parts, "; ")
}

// rebuildCookie 保留原 Cookie 的 th_* 字段,替换 sb-auth-auth-token.0(移除 tok1)。
//
// 上游实测:带 tok1 的续期 Cookie 会 401,只保留 tok0 + th_* 才通过。
func rebuildCookie(oldCookie string, sess supabaseSession) string {
	obj := map[string]interface{}{
		"access_token":  sess.AccessToken,
		"token_type":    firstNonEmpty(sess.TokenType, "bearer"),
		"expires_in":    maxInt64(sess.ExpiresIn, 3600),
		"expires_at":    sess.ExpiresAt,
		"refresh_token": sess.RefreshToken,
		"user":          map[string]interface{}{"id": sess.User.ID, "email": sess.User.Email},
	}
	tok0 := "base64-" + encodeAuth0JSON(obj)
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(oldCookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "sb-auth-auth-token.0=") || strings.HasPrefix(part, "sb-auth-auth-token.1=") {
			continue
		}
		parts = append(parts, part)
	}
	parts = append(parts, "sb-auth-auth-token.0="+tok0)
	return strings.Join(parts, "; ")
}

func encodeAuth0JSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(data)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// -----------------------------------------------------------------------------
// direct-chat 上游
// -----------------------------------------------------------------------------

// createSession 创建一个上游会话,返回 session id。
func (s *Service) createSession(ctx context.Context, cookie, model string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{"model": model, "temporary": true})
	status, data, err := s.doJSON(ctx, s.httpClientFor("session"), http.MethodPost,
		thBaseURL+"/api/direct-chat/sessions", body, siteHeaders(cookie), 30*time.Second, maxCatalogBytes)
	if err != nil {
		return "", fmt.Errorf("创建上游会话失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("创建上游会话失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("解析会话响应失败: %w", err)
	}
	if strings.TrimSpace(resp.Session.ID) == "" {
		return "", fmt.Errorf("上游未返回 session id")
	}
	return resp.Session.ID, nil
}

// streamChat 发起一次对话流,成功时返回未消费的 SSE 响应体。
func (s *Service) streamChat(ctx context.Context, cookie string, req map[string]interface{}) (*http.Response, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, thBaseURL+"/api/direct-chat/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range siteHeaders(cookie) {
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := s.httpClientFor("stream").Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("上游对话流请求失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		_ = resp.Body.Close()
		return nil, &upstreamError{status: resp.StatusCode, message: jsonMessage(data)}
	}
	return resp, nil
}

// upstreamError 携带上游 HTTP 状态码,便于按 429/401 分类处理。
type upstreamError struct {
	status  int
	message string
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("上游返回 %d: %s", e.status, e.message)
}

// fetchFreeTier 拉取免费额度(账号连通性探针)。
func (s *Service) fetchFreeTier(ctx context.Context, cookie string) (json.RawMessage, error) {
	status, data, err := s.doJSON(ctx, s.httpClientFor("free-tier"), http.MethodGet,
		thBaseURL+"/api/me/free-tier", nil, siteHeaders(cookie), 20*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("查询免费额度失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询免费额度失败: HTTP %d: %s", status, jsonMessage(data))
	}
	return json.RawMessage(data), nil
}

// upstreamStatus 从任意 error 里抽出上游 HTTP 状态码;抽不到返回 0。
func upstreamStatus(err error) int {
	var ue *upstreamError
	if errors.As(err, &ue) {
		return ue.status
	}
	return 0
}
