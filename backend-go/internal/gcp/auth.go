package gcp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	scopeFull             = "https://www.googleapis.com/auth/cloud-platform"
	jwtTTLSeconds         = 3600
	tokenRefreshInSeconds = 300
)

type serviceAccountJSON struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
	TokenURI     string `json:"token_uri"`
}

func parseServiceAccount(raw string) (serviceAccountJSON, error) {
	var sa serviceAccountJSON
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return sa, fmt.Errorf("SA JSON 格式错误: %w", err)
	}
	if strings.TrimSpace(sa.ClientEmail) == "" {
		return sa, errors.New("SA JSON 缺少 client_email")
	}
	if strings.TrimSpace(sa.PrivateKey) == "" {
		return sa, errors.New("SA JSON 缺少 private_key")
	}
	if strings.TrimSpace(sa.TokenURI) == "" {
		sa.TokenURI = tokenBaseURL()
	}
	return sa, nil
}

func tokenBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("GCP_TOKEN_BASE_URL")); value != "" {
		return value
	}
	return defaultTokenURL
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// tokenCache 进程内存缓存（accountID + scope 维度），不落库。
type tokenCache struct {
	mu     sync.Mutex
	values map[string]cachedToken
}

func newTokenCache() *tokenCache {
	return &tokenCache{values: map[string]cachedToken{}}
}

func (c *tokenCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.values[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		delete(c.values, key)
		return "", false
	}
	return entry.Token, true
}

func (c *tokenCache) set(key, token string, expiresInSeconds int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := time.Duration(expiresInSeconds-tokenRefreshInSeconds) * time.Second
	if ttl < time.Minute {
		ttl = time.Minute
	}
	c.values[key] = cachedToken{Token: token, ExpiresAt: time.Now().Add(ttl)}
}

// invalidateAccount 使某账号的全部 scope 缓存失效（SA 轮换后调用，
// 避免旧凭证在剩余 TTL 内继续被使用）。
func (c *tokenCache) invalidateAccount(accountID int64) {
	prefix := strconv.FormatInt(accountID, 10) + ":"
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.values {
		if strings.HasPrefix(key, prefix) {
			delete(c.values, key)
		}
	}
}

// tokenProvider 为 REST client 提供 Bearer access token（SA JSON → JWT → 交换）。
type tokenProvider struct {
	httpClient *http.Client
	cache      *tokenCache
	sa         serviceAccountJSON
	scope      string
	key        string
}

func (p *tokenProvider) AccessToken(ctx context.Context) (string, error) {
	if token, ok := p.cache.get(p.key); ok {
		return token, nil
	}
	assertion, err := buildJWTAssertion(p.sa, p.scope)
	if err != nil {
		return "", err
	}
	token, expiresIn, err := p.exchange(ctx, assertion)
	if err != nil {
		return "", err
	}
	p.cache.set(p.key, token, expiresIn)
	return token, nil
}

func (p *tokenProvider) exchange(ctx context.Context, assertion string) (string, int64, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token 交换失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return "", 0, fmt.Errorf("token 交换被拒绝（%d %s）: %s", resp.StatusCode, apiErr.Error, apiErr.ErrorDescription)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, err
	}
	if payload.AccessToken == "" {
		return "", 0, errors.New("token 交换返回空 access_token")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = jwtTTLSeconds
	}
	return payload.AccessToken, payload.ExpiresIn, nil
}

func buildJWTAssertion(sa serviceAccountJSON, scope string) (string, error) {
	header := map[string]interface{}{
		"alg": "RS256",
		"typ": "JWT",
	}
	now := time.Now().Unix()
	claims := map[string]interface{}{
		"iss":   sa.ClientEmail,
		"scope": scope,
		"aud":   sa.TokenURI,
		"iat":   now,
		"exp":   now + jwtTTLSeconds,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	signature, err := signRS256(sa.PrivateKey, signingInput)
	if err != nil {
		return "", err
	}
	return signingInput + "." + signature, nil
}

func signRS256(privateKeyPEM, signingInput string) (string, error) {
	privateKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("JWT 签名失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

func parsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("private_key 不是有效的 PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("private_key 不是 RSA 密钥")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("无法解析 private_key（仅支持 RSA PKCS8/PKCS1）")
}
