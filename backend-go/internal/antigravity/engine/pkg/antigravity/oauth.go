package antigravity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/errors"
)

const (
	// Google OAuth 端点
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	UserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"

	// Antigravity OAuth 客户端凭证
	ClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"

	// AntigravityOAuthClientSecretEnv 是 Antigravity OAuth client_secret 的环境变量名。
	AntigravityOAuthClientSecretEnv = "ANTIGRAVITY_OAUTH_CLIENT_SECRET"

	// AntigravityUserAgentVersionEnv 是 Antigravity User-Agent 版本号的环境变量名。
	AntigravityUserAgentVersionEnv = "ANTIGRAVITY_USER_AGENT_VERSION"

	// DefaultUserAgentVersion 是未通过环境变量或后台设置覆盖时使用的兜底版本号。
	// 与 AntigravityHubFallbackVersion 同一取值：Cloud Code 会拒绝 2.23.0
	// 以下客户端访问新模型，所以这里不可下调。
	DefaultUserAgentVersion = AntigravityHubFallbackVersion

	// 固定的 redirect_uri（用户需手动复制 code）
	RedirectURI = "http://localhost:8085/callback"

	// OAuth scopes
	Scopes = "https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email " +
		"https://www.googleapis.com/auth/userinfo.profile " +
		"https://www.googleapis.com/auth/cclog " +
		"https://www.googleapis.com/auth/experimentsandconfigs"

	// Session 过期时间
	SessionTTL = 30 * time.Minute

	// URL 可用性 TTL（不可用 URL 的恢复时间）
	URLAvailabilityTTL = 5 * time.Minute

	// Antigravity API 端点
	antigravityProdBaseURL  = "https://cloudcode-pa.googleapis.com"
	antigravityDailyBaseURL = "https://daily-cloudcode-pa.googleapis.com"
)

var userAgentVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// UserAgentVersionResolver 提供运行时 User-Agent 版本号覆盖能力。
type UserAgentVersionResolver func(ctx context.Context) string

var (
	// envUserAgentVersion 是环境变量 ANTIGRAVITY_USER_AGENT_VERSION 的覆盖值；空表示未覆盖。
	envUserAgentVersion      string
	userAgentVersionMu       sync.RWMutex
	userAgentVersionResolver UserAgentVersionResolver
)

// defaultClientSecret 对应 Antigravity-Manager 开源项目的公共 Google OAuth
// 客户端凭据（client_id 亦为公开值）。该插件复用该开源项目的 OAuth 客户端，
// 因此此 secret 属公开项目凭据而非用户私有密钥；可通过环境变量
// ANTIGRAVITY_OAUTH_CLIENT_SECRET 覆盖（例如使用自有 OAuth 客户端时）。
var defaultClientSecret = "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"

func init() {
	// 从环境变量读取版本号覆盖；未设置时跟随 Hub 动态版本发现。
	if version := NormalizeUserAgentVersion(os.Getenv(AntigravityUserAgentVersionEnv)); version != "" {
		envUserAgentVersion = version
	}
	// 从环境变量读取 client_secret，未设置则使用开源项目默认值
	if secret := os.Getenv(AntigravityOAuthClientSecretEnv); secret != "" {
		defaultClientSecret = secret
	}
}

// NormalizeUserAgentVersion 校验并归一化 Antigravity User-Agent 版本号。
func NormalizeUserAgentVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || !userAgentVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

// GetDefaultUserAgentVersion 返回未考虑运行时解析器时的版本号。
// 优先级：环境变量覆盖 > Hub 动态发现 > 回退版本。
func GetDefaultUserAgentVersion() string {
	if envUserAgentVersion != "" {
		return envUserAgentVersion
	}
	return HubLatestVersion()
}

// SetUserAgentVersionResolver 设置运行时版本号解析器，通常由后台 settings 注入。
func SetUserAgentVersionResolver(resolver UserAgentVersionResolver) {
	userAgentVersionMu.Lock()
	defer userAgentVersionMu.Unlock()
	userAgentVersionResolver = resolver
}

// GetUserAgentVersionForContext 返回当前请求应使用的 Antigravity 版本号。
func GetUserAgentVersionForContext(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	userAgentVersionMu.RLock()
	resolver := userAgentVersionResolver
	userAgentVersionMu.RUnlock()
	if resolver != nil {
		if version := NormalizeUserAgentVersion(resolver(ctx)); version != "" {
			return version
		}
	}
	return GetDefaultUserAgentVersion()
}

// BuildUserAgent 按 Hub 家族格式构造 User-Agent；版本为空或非法时回退默认值。
//
// 格式对齐官方客户端（含 changelist）：
//
//	antigravity/hub/2.24.0 (aidev_client; os_type=darwin; arch=arm64; cl=1000335682)
func BuildUserAgent(version string) string {
	normalized := NormalizeUserAgentVersion(version)
	if normalized == "" {
		return HubUserAgent()
	}
	return hubUserAgentForVersion(normalized)
}

// hubUserAgentForVersion 用指定版本构造 Hub UA，changelist 仅对已知发版生效。
func hubUserAgentForVersion(version string) string {
	metadata := "aidev_client; " + antigravityHubPlatform
	if changelist := antigravityHubChangelists[version]; changelist != "" {
		metadata += "; cl=" + changelist
	}
	return fmt.Sprintf("antigravity/hub/%s (%s)", version, metadata)
}

// GetUserAgentForContext 返回当前请求应使用的 User-Agent。
func GetUserAgentForContext(ctx context.Context) string {
	return BuildUserAgent(GetUserAgentVersionForContext(ctx))
}

// GetUserAgent 返回当前配置的 User-Agent。
func GetUserAgent() string {
	return GetUserAgentForContext(context.Background())
}

func getClientSecret() (string, error) {
	if v := strings.TrimSpace(defaultClientSecret); v != "" {
		return v, nil
	}
	return "", infraerrors.Newf(http.StatusBadRequest, "ANTIGRAVITY_OAUTH_CLIENT_SECRET_MISSING", "missing antigravity oauth client_secret; set %s", AntigravityOAuthClientSecretEnv)
}

// BaseURLs 定义 Antigravity API 端点。
//
// 顺序与上游一致：daily 优先，prod 仅作兜底。Cloud Code 的消费级凭据默认
// 落在 daily 上，新模型/新配额接口（retrieveUserQuotaSummary 等）也只在 daily 可用。
var BaseURLs = []string{
	antigravityDailyBaseURL, // daily（主）
	antigravityProdBaseURL,  // prod（兜底）
}

// BaseURL 默认 URL
var BaseURL = BaseURLs[0]

// ForwardBaseURLs 返回 API 转发用的 URL 顺序（daily 优先）。
//
// 保留该函数是为了兼容既有调用点；BaseURLs 现在已按 daily 优先排序，
// 因此这里只做一次去重后的复制。
func ForwardBaseURLs() []string {
	if len(BaseURLs) == 0 {
		return nil
	}
	return append([]string(nil), BaseURLs...)
}

// URLAvailability 管理 URL 可用性状态（带 TTL 自动恢复和动态优先级）
type URLAvailability struct {
	mu          sync.RWMutex
	unavailable map[string]time.Time // URL -> 恢复时间
	ttl         time.Duration
	lastSuccess string // 最近成功请求的 URL，优先使用
}

// DefaultURLAvailability 全局 URL 可用性管理器
var DefaultURLAvailability = NewURLAvailability(URLAvailabilityTTL)

// NewURLAvailability 创建 URL 可用性管理器
func NewURLAvailability(ttl time.Duration) *URLAvailability {
	return &URLAvailability{
		unavailable: make(map[string]time.Time),
		ttl:         ttl,
	}
}

// MarkUnavailable 标记 URL 临时不可用
func (u *URLAvailability) MarkUnavailable(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.unavailable[url] = time.Now().Add(u.ttl)
}

// MarkSuccess 标记 URL 请求成功，将其设为优先使用
func (u *URLAvailability) MarkSuccess(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastSuccess = url
	// 成功后清除该 URL 的不可用标记
	delete(u.unavailable, url)
}

// IsAvailable 检查 URL 是否可用
func (u *URLAvailability) IsAvailable(url string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	expiry, exists := u.unavailable[url]
	if !exists {
		return true
	}
	return time.Now().After(expiry)
}

// GetAvailableURLs 返回可用的 URL 列表
// 最近成功的 URL 优先，其他按默认顺序
func (u *URLAvailability) GetAvailableURLs() []string {
	return u.GetAvailableURLsWithBase(BaseURLs)
}

// GetAvailableURLsWithBase 返回可用的 URL 列表（使用自定义顺序）
// 最近成功的 URL 优先，其他按传入顺序
func (u *URLAvailability) GetAvailableURLsWithBase(baseURLs []string) []string {
	u.mu.RLock()
	defer u.mu.RUnlock()

	now := time.Now()
	result := make([]string, 0, len(baseURLs))

	// 如果有最近成功的 URL 且可用，放在最前面
	if u.lastSuccess != "" {
		found := false
		for _, url := range baseURLs {
			if url == u.lastSuccess {
				found = true
				break
			}
		}
		if found {
			expiry, exists := u.unavailable[u.lastSuccess]
			if !exists || now.After(expiry) {
				result = append(result, u.lastSuccess)
			}
		}
	}

	// 添加其他可用的 URL（按传入顺序）
	for _, url := range baseURLs {
		// 跳过已添加的 lastSuccess
		if url == u.lastSuccess {
			continue
		}
		expiry, exists := u.unavailable[url]
		if !exists || now.After(expiry) {
			result = append(result, url)
		}
	}
	return result
}

// OAuthSession 保存 OAuth 授权流程的临时状态
type OAuthSession struct {
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	ProxyURL     string    `json:"proxy_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// SessionStore OAuth session 存储
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*OAuthSession
	stopCh   chan struct{}
}

func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*OAuthSession),
		stopCh:   make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *SessionStore) Set(sessionID string, session *OAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
}

func (s *SessionStore) Get(sessionID string) (*OAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}
	if time.Since(session.CreatedAt) > SessionTTL {
		return nil, false
	}
	return session, true
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) Stop() {
	select {
	case <-s.stopCh:
		return
	default:
		close(s.stopCh)
	}
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			for id, session := range s.sessions {
				if time.Since(session.CreatedAt) > SessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func GenerateState() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

func GenerateSessionID() (string, error) {
	bytes, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GenerateCodeVerifier() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// BuildAuthorizationURL 构建 Google OAuth 授权 URL
func BuildAuthorizationURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("redirect_uri", RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", Scopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")

	return fmt.Sprintf("%s?%s", AuthorizeURL, params.Encode())
}
