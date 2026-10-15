package posthogcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PostHog 已把 `posthog_code` 产品从 Python 网关（gateway.{region}.posthog.com）整条
// 下线：任何带 phe_/pha_ 的对话请求都返回 403 product_retired。当前 Desktop 的新架构是
// 「服务端铸造短时 phe_ token + Go AI 网关（ai-gateway.{region}.posthog.com）根路径」：
//
//   - 用账号的 pha_ OAuth 凭据调 POST {cloud}/api/projects/{id}/desktop/gateway_token/，
//     拿到 {token, expires_at, gateway_url, plan, allowed_models, ...}。
//   - 对话直接打 {gateway_url}/v1/chat/completions（无产品路径段，产品钉在 token 里）。
//
// phe_ 默认 TTL 300s，且铸造端点有每用户每小时限流（默认 120/hour），故按账号缓存、
// 到期前重铸。

// goGatewayBaseURL 返回指定区域的 Go AI 网关基址。仅用于模型目录（公开、无需鉴权）；
// 对话目标地址以铸造返回的 gateway_url 为准，不在本地硬编码。
func goGatewayBaseURL(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "eu") {
		return "https://ai-gateway.eu.posthog.com"
	}
	return "https://ai-gateway.us.posthog.com"
}

// gatewayToken 是 Django 为账号铸造的 Go 网关短时凭据（phe_）。
type gatewayToken struct {
	Token         string
	GatewayURL    string
	ExpiresAt     time.Time
	Plan          string
	AllowedModels []string
	CapUSD        string
}

// mintRequestTimeout 是铸造请求的超时。
const mintRequestTimeout = 15 * time.Second

// mintRefreshSkew 是提前重铸的窗口：剩余寿命小于该值时视为需要续铸。
const mintRefreshSkew = 60 * time.Second

// mintError 是铸造被拒绝或失败。
type mintError struct {
	status int
	reason string
	detail string
}

func (e *mintError) Error() string {
	msg := strings.TrimSpace(firstNonEmpty(e.detail, e.reason))
	if msg == "" {
		msg = fmt.Sprintf("铸造网关 token 失败（HTTP %d）", e.status)
	}
	return msg
}

// gatewayTokenMintURL 构造指定账号项目的铸造地址（Django 主站，非网关）。
func (s *Service) gatewayTokenMintURL(acc Account, projectID string) string {
	return cloudBaseURL(s.accountRegion(acc)) + "/api/projects/" + projectID + "/desktop/gateway_token/"
}

// mintGatewayToken 用账号的 pha_ 凭据铸造一个 phe_ 短时 token。
func (s *Service) mintGatewayToken(ctx context.Context, acc Account, projectID, phaToken string) (*gatewayToken, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("账号未记录项目 ID，无法铸造网关 token")
	}
	return mintGatewayTokenAt(ctx, s.gatewayTokenMintURL(acc, projectID), phaToken)
}

// mintGatewayTokenAt 向给定铸造地址发起请求并解析响应。抽出便于单测。
func mintGatewayTokenAt(ctx context.Context, url, phaToken string) (*gatewayToken, error) {
	if strings.TrimSpace(phaToken) == "" {
		return nil, errMissingToken
	}
	reqCtx, cancel := context.WithTimeout(ctx, mintRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+phaToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: mintRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("铸造网关 token 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusCreated {
		var refusal struct {
			Reason string `json:"reason"`
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &refusal)
		reason := firstNonEmpty(refusal.Reason, refusal.Code)
		detail := strings.TrimSpace(refusal.Detail)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		if reason == "" && detail == "" {
			detail = strings.TrimSpace(string(raw))
			if len(detail) > 300 {
				detail = detail[:300]
			}
		}
		return nil, &mintError{status: resp.StatusCode, reason: reason, detail: detail}
	}

	var parsed struct {
		Token         string   `json:"token"`
		ExpiresAt     string   `json:"expires_at"`
		GatewayURL    string   `json:"gateway_url"`
		Plan          string   `json:"plan"`
		AllowedModels []string `json:"allowed_models"`
		CapUSD        string   `json:"cap_usd"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析铸造响应失败: %w", err)
	}
	if strings.TrimSpace(parsed.Token) == "" {
		return nil, fmt.Errorf("铸造响应缺少 token")
	}
	gatewayURL := strings.TrimRight(strings.TrimSpace(parsed.GatewayURL), "/")
	if gatewayURL == "" {
		return nil, fmt.Errorf("铸造响应缺少 gateway_url")
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(parsed.ExpiresAt))
	if err != nil {
		// 无法解析过期时间时给一个保守的短寿命（默认 TTL 300s）。
		expiresAt = time.Now().Add(4 * time.Minute)
	}
	return &gatewayToken{
		Token:         parsed.Token,
		GatewayURL:    gatewayURL,
		ExpiresAt:     expiresAt,
		Plan:          strings.TrimSpace(parsed.Plan),
		AllowedModels: parsed.AllowedModels,
		CapUSD:        parsed.CapUSD,
	}, nil
}

// ensureGatewayToken 返回账号当前可用的 phe_ 凭据：缓存命中则复用，否则（或临近过期）
// 重新铸造。铸造前先确保 pha_ OAuth 凭据新鲜（必要时刷新）。
//
// 全局串行化：铸造端点有每用户限流，并发重复铸造既是浪费也可能触发限流；铸造本身
// 很快（亚秒级），持锁成本可接受。
func (s *Service) ensureGatewayToken(ctx context.Context, acc *Account) (*gatewayToken, error) {
	pha, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return nil, err
	}
	if t := s.cachedGatewayToken(acc.ID); t != nil {
		return t, nil
	}

	s.mintMu.Lock()
	defer s.mintMu.Unlock()
	// 双检：等待锁期间可能已有别的请求铸造完成。
	if t := s.cachedGatewayTokenLocked(acc.ID); t != nil {
		return t, nil
	}
	projectID := s.resolveProjectID(ctx, acc, pha)
	if projectID == "" {
		return nil, fmt.Errorf("账号未记录项目 ID（凭据可能缺少 project:read），无法铸造网关 token")
	}
	t, err := s.mintGatewayToken(ctx, *acc, projectID, pha)
	if err != nil {
		return nil, err
	}
	if s.mintCache == nil {
		s.mintCache = map[string]*gatewayToken{}
	}
	s.mintCache[acc.ID] = t
	return t, nil
}

// cachedGatewayToken 读取仍有效的缓存凭据；无锁版需在持有 mintMu 时调用。
func (s *Service) cachedGatewayTokenLocked(id string) *gatewayToken {
	t := s.mintCache[id]
	if t == nil || !time.Now().Before(t.ExpiresAt.Add(-mintRefreshSkew)) {
		return nil
	}
	return t
}

// cachedGatewayToken 是 cachedGatewayTokenLocked 的加锁包装。
func (s *Service) cachedGatewayToken(id string) *gatewayToken {
	s.mintMu.Lock()
	defer s.mintMu.Unlock()
	return s.cachedGatewayTokenLocked(id)
}

// goModelName 把插件侧模型名映射成 Go 网关的写法：Go 不认 `@cf/` 别名。
func goModelName(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "@cf/")
}
