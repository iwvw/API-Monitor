package cline

// 本文件封装对 Cline 上游的 HTTP 调用：共享客户端、JWT 解析、错误文案、
// 模型目录与余额查询。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	maxCatalogBytes = 4 << 20
	maxErrorBytes   = 64 << 10
)

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 是无总超时的共享客户端（长流式回答不能被总超时截断）。
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

// httpClientFor 返回本次出网要用的客户端：配置了代理池时经 ProxyPoolSelector 选出口。
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

// httpProxyURL 解析代理字符串为 *url.URL；非法返回 error。
func httpProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("代理地址非法")
	}
	return u, nil
}

// doJSON 发一次请求并读取响应体（受 limit 限制）。timeout > 0 时套请求级总超时。
func (s *Service) doJSON(ctx context.Context, client *http.Client, method, target string, body []byte, token string, headers map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "Cline/3.0.62")
	req.Header.Set("X-CLIENT-TYPE", clientType)
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

// jsonMessage 从上游错误体里尽量取出人类可读文案。
func jsonMessage(data []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return truncate(strings.TrimSpace(string(data)), 300)
	}
	if raw, ok := payload["error"]; ok {
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
	for _, key := range []string{"message", "msg", "error_description"} {
		if raw, ok := payload[key]; ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && text != "" {
				return truncate(text, 300)
			}
		}
	}
	return truncate(strings.TrimSpace(string(data)), 300)
}

// -----------------------------------------------------------------------------
// 模型目录
// -----------------------------------------------------------------------------

// catalogGroup 是目录响应里的一个分组。
type catalogGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// fetchCatalog 拉取模型目录（`/ai/cline/recommended-models`，无鉴权也能拉）。
func (s *Service) fetchCatalog(ctx context.Context, token string) ([]ModelInfo, error) {
	status, data, err := s.doJSON(ctx, s.httpClientFor("catalog"), http.MethodGet,
		apiBaseURL+"/ai/cline/recommended-models", nil, token,
		map[string]string{"Accept": "application/json"}, 30*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("拉取模型目录失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("拉取模型目录失败: HTTP %d: %s", status, jsonMessage(data))
	}
	return parseCatalog(data)
}

// parseCatalog 把目录响应解析成归一化模型列表。
//
// 分组结构：`{recommended:[…], free:[…], clinePass:[…], clineCloud:[…]}`。
// **归池一律按分组判，不能按 id 前缀猜** —— 免费池里有别家裸 id
// （`z-ai/glm-5.3-flash`），按前缀猜会错判。`clineCloud` 实测打它返回 403，不收录。
func parseCatalog(data []byte) ([]ModelInfo, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("解析模型目录失败: %w", err)
	}
	// 兼容 `{data:{…}}` 信封。
	if inner, ok := probe["data"]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(inner, &nested) == nil && len(nested) > 0 {
			probe = nested
		}
	}
	out := make([]ModelInfo, 0)
	seen := map[string]bool{}
	// 只收 free 与 clinePass 两组；recommended 与两组有重叠，clineCloud 必 403。
	for _, group := range []struct{ key, pool string }{
		{"free", "free"},
		{"clinePass", "pass"},
	} {
		raw, ok := probe[group.key]
		if !ok {
			continue
		}
		var items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			continue
		}
		for _, item := range items {
			id := strings.TrimSpace(item.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ModelInfo{
				ID:   id,
				Name: strings.TrimSpace(item.Name),
				Pool: group.pool,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("上游模型目录为空")
	}
	return out, nil
}

// fetchCatalogProbe 只读地拉一次目录，返回条数（账号测试用）。
func (s *Service) fetchCatalogProbe(ctx context.Context, token string) (int, error) {
	models, err := s.fetchCatalog(ctx, token)
	if err != nil {
		return 0, err
	}
	return len(models), nil
}

// -----------------------------------------------------------------------------
// 余额
// -----------------------------------------------------------------------------

// Balance 是 Cline 的额度读数。
//
// 免费池贴 0 / 欠费是常态，因此这里只做透传，不做「必须 > 0」的判定。
type Balance struct {
	// Credits 是剩余额度（上游字段名随版本变化，取到哪个用哪个）。
	Credits *float64 `json:"credits,omitempty"`
	// Plan 是订阅套餐名（免费池通常为空）。
	Plan string `json:"plan,omitempty"`
	// Raw 是上游原始响应（排障用）。
	Raw json.RawMessage `json:"raw,omitempty"`
}

// fetchBalance 查询账号额度。
func (s *Service) fetchBalance(ctx context.Context, token string) (*Balance, error) {
	status, data, err := s.doJSON(ctx, s.httpClientFor("balance"), http.MethodGet,
		apiBaseURL+"/users/me", nil, token,
		map[string]string{"Accept": "application/json"}, 20*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("查询额度失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询额度失败: HTTP %d: %s", status, jsonMessage(data))
	}
	bal := &Balance{Raw: json.RawMessage(data)}
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		for _, key := range []string{"credits", "balance", "creditBalance"} {
			if v, ok := payload.Data[key]; ok {
				if f, ok := toFloat(v); ok {
					bal.Credits = &f
					break
				}
			}
		}
		for _, key := range []string{"plan", "planName", "subscription"} {
			if v, ok := payload.Data[key].(string); ok && v != "" {
				bal.Plan = v
				break
			}
		}
	}
	return bal, nil
}

// toFloat 把 JSON 数值（或数字字符串）转成 float64。
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// -----------------------------------------------------------------------------
// JWT 工具
// -----------------------------------------------------------------------------

// jwtExpiryMS 解码 JWT payload 的 exp（秒），返回毫秒；解不出返回 0（不验签）。
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

// jwtUserID 解码 JWT payload 里的用户 id（不验签）。
func jwtUserID(token string) string {
	claims, ok := jwtClaims(token)
	if !ok {
		return ""
	}
	for _, key := range []string{"clineUserId", "sub", "user_id", "userId"} {
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

// jwtEmail 解码 JWT payload 里的邮箱（不验签）。
func jwtEmail(token string) string {
	claims, ok := jwtClaims(token)
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(claims["email"], &text); err == nil {
		return text
	}
	return ""
}

// jwtClaims 解出 JWT payload（去掉 workos: 前缀后解码，不验签）。
func jwtClaims(token string) (map[string]json.RawMessage, bool) {
	raw := stripTokenPrefix(token)
	parts := strings.Split(raw, ".")
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

// strconvParseInt 是 strconv.ParseInt 的薄包装（避免在多个文件重复导入）。
func strconvParseInt(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}
