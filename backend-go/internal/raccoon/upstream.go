package raccoon

// 本文件封装对小浣熊（商汤 Raccoon）上游的 HTTP 调用：共享客户端、JWT 解析、
// token 刷新、积分查询与模型目录拉取。协议形状来自 xhx2api/raccoon2api 记录。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

const (
	maxCatalogBytes = 4 << 20
	maxErrorBytes   = 64 << 10
)

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 是无超时的共享客户端（长流式回答不能被总超时截断）。
func sharedHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		sharedClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				MaxIdleConns:          20,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConnsPerHost:   5,
			},
		}
	})
	return sharedClient
}

// httpClientFor 返回本次出网要用的客户端：配置了代理池时经 ProxyPoolSelector 选出口。
func (s *Service) httpClientFor(sessionKey string) *http.Client {
	poolID := strings.TrimSpace(s.Settings().ProxyPoolID)
	if poolID == "" {
		return sharedHTTPClient()
	}
	if s.externalPool == nil {
		return errClient(fmt.Errorf("代理池已配置但未初始化选择器"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := s.externalPool.SelectProxy(ctx, poolID, sessionKey)
	if err != nil {
		applog.Warn(ctx, "raccoon", "proxy pool select failed, refusing direct fallback", "pool", poolID, "error", err.Error())
		return errClient(fmt.Errorf("代理池无可用出口: %w", err))
	}
	if strings.TrimSpace(raw) == "" {
		applog.Warn(ctx, "raccoon", "proxy pool returned empty exit, refusing direct fallback", "pool", poolID)
		return errClient(fmt.Errorf("代理池无可用出口"))
	}
	u, err := httpProxyURL(strings.TrimSpace(raw))
	if err != nil {
		applog.Warn(ctx, "raccoon", "proxy address invalid, refusing direct fallback", "pool", poolID, "error", err.Error())
		return errClient(fmt.Errorf("代理地址非法: %w", err))
	}
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(u),
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   5,
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

func errClient(err error) *http.Client {
	return &http.Client{Timeout: 0, Transport: errTransport{err: err}}
}

type errTransport struct{ err error }

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// doJSON 发一次请求并读取响应体（受 limit 限制）。timeout > 0 时套请求级总超时。
func (s *Service) doJSON(ctx context.Context, client *http.Client, method, url string, body []byte, token string, headers map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "api-monitor/raccoon-plugin")
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

// upstreamRefresh 调鉴权 API 刷新 token（POST {auth_base}/refresh）。
func (s *Service) upstreamRefresh(ctx context.Context, refreshToken string) (string, string, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, data, err := s.doJSON(ctx, s.httpClientFor("refresh"), http.MethodPost, authBase+"/refresh", body, "",
		map[string]string{"Content-Type": "application/json"}, 0, maxErrorBytes)
	if err != nil {
		return "", "", fmt.Errorf("刷新 token 请求失败: %w", err)
	}
	var resp struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(data, &resp)
	if status < 200 || status >= 300 {
		return "", "", fmt.Errorf("刷新 token 失败: HTTP %d: %s", status, jsonMessage(data))
	}
	if resp.Code != nil && *resp.Code != 0 {
		return "", "", fmt.Errorf("刷新 token 失败: code=%d message=%s", *resp.Code, resp.Message)
	}
	if resp.Data.AccessToken == "" {
		return "", "", fmt.Errorf("刷新 token 失败: 上游未返回 access_token（HTTP %d message=%s）", status, resp.Message)
	}
	return resp.Data.AccessToken, resp.Data.RefreshToken, nil
}

// Balance 是积分余额（只取数值字段；上游同响应里还有布尔/字符串字段，
// 故不能整体反序列化到 map[string]*int64，否则会因类型不符整体失败）。
type Balance struct {
	AvailablePoints *int64 `json:"available_points"`
	DailyPoints     *int64 `json:"daily_points"`
	MonthlyPoints   *int64 `json:"monthly_points"`
	RewardPoints    *int64 `json:"reward_points"`
	TopupPoints     *int64 `json:"topup_points"`
}

// UnmarshalJSON 兼容数字与数字字符串两种形态，并忽略布尔等无关字段。
func (b *Balance) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.AvailablePoints = flexInt(raw["available_points"])
	b.DailyPoints = flexInt(raw["daily_points"])
	b.MonthlyPoints = flexInt(raw["monthly_points"])
	b.RewardPoints = flexInt(raw["reward_points"])
	b.TopupPoints = flexInt(raw["topup_points"])
	return nil
}

// flexInt 把 JSON 数字或数字字符串转成 *int64；缺失/null/非法返回 nil。
func flexInt(raw json.RawMessage) *int64 {
	if len(raw) == 0 {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "null" || text == `""` || text == "" {
		return nil
	}
	text = strings.Trim(text, `"`)
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	n := int64(value)
	return &n
}

// authorizationCode 是 login_with_authorization_code 换回的凭证。
type authorizationCode struct {
	AccessToken    string
	RefreshToken   string
	OfficeIdentity string
	OfficeOrgName  string
	OfficeOrgRole  string
}

// authCodeError 是用授权码换凭证失败（携带上游 HTTP 状态与业务码）。
type authCodeError struct {
	httpStatus int
	code       int
	hasCode    bool
	message    string
}

func (e *authCodeError) Error() string {
	if e.hasCode {
		return fmt.Sprintf("HTTP %d: code=%d message=%s", e.httpStatus, e.code, e.message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.httpStatus, e.message)
}

// exchangeAuthorizationCode 用一次性授权码换凭证（POST {auth_base}/login_with_authorization_code）。
func (s *Service) exchangeAuthorizationCode(ctx context.Context, code string) (*authorizationCode, error) {
	url := authBase + "/login_with_authorization_code"
	body, _ := json.Marshal(map[string]string{"authorization_code": code})
	status, data, err := s.doJSON(ctx, s.httpClientFor("login"), http.MethodPost, url, body, "",
		map[string]string{"Content-Type": "application/json"}, 30*time.Second, maxErrorBytes)
	if err != nil {
		return nil, fmt.Errorf("网页登录换取凭证请求失败: %w", err)
	}
	var resp struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Msg     string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(data, &resp)
	hasCode := resp.Code != nil
	upstreamCode := 0
	if hasCode {
		upstreamCode = *resp.Code
	}
	if hasCode && upstreamCode == codeExpired {
		return nil, &authCodeError{httpStatus: status, code: upstreamCode, hasCode: true, message: "授权码已失效"}
	}
	var dataMap map[string]json.RawMessage
	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		_ = json.Unmarshal(resp.Data, &dataMap)
	}
	textOf := func(key string) string { return stripBearer(jsonText(dataMap[key])) }
	access := textOf("access_token")
	refresh := textOf("refresh_token")
	if status < 200 || status >= 300 || access == "" || refresh == "" {
		msg := firstNonEmpty(resp.Message, resp.Msg, jsonMessage(data))
		return nil, &authCodeError{httpStatus: status, code: upstreamCode, hasCode: hasCode, message: msg}
	}
	return &authorizationCode{
		AccessToken:    access,
		RefreshToken:   refresh,
		OfficeIdentity: textOf("office_identity"),
		OfficeOrgName:  textOf("office_org_name"),
		OfficeOrgRole:  textOf("office_org_role"),
	}, nil
}

// jsonText 把一段原始 JSON 尽量取成字符串（兼容字符串与数字）。
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		return num.String()
	}
	return ""
}

// stripBearer 去掉可能出现的 "Bearer " 前缀并 trim。
func stripBearer(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "Bearer "))
}

// fetchBalance 查询积分余额（GET {points_base}/balance）。
func (s *Service) fetchBalance(ctx context.Context, token string) (*Balance, error) {
	url := pointsBase + "/balance"
	status, data, err := s.doJSON(ctx, s.httpClientFor("balance"), http.MethodGet, url, nil, token,
		map[string]string{"Accept": "application/json"}, 20*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("查询积分失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询积分失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析积分响应失败: %w", err)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return nil, fmt.Errorf("查询积分失败: code=%d message=%s", *resp.Code, resp.Message)
	}
	bal := Balance{}
	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		if err := json.Unmarshal(resp.Data, &bal); err != nil {
			return nil, fmt.Errorf("解析积分数据失败: %w", err)
		}
	}
	return &bal, nil
}

// fetchCatalogProbe 只读地拉一次模型目录，返回解析出的模型条数（账号测试用）。
func (s *Service) fetchCatalogProbe(ctx context.Context, token string) (int, error) {
	models, err := s.fetchCatalog(ctx, token)
	if err != nil {
		return 0, err
	}
	return len(models), nil
}

// jsonMessage 从上游错误体里尽量取出人类可读文案。
func jsonMessage(data []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return strings.TrimSpace(string(data))
	}
	for _, key := range []string{"message", "msg"} {
		if raw, ok := payload[key]; ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && text != "" {
				return text
			}
		}
	}
	if raw, ok := payload["error"]; ok {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(raw, &inner); err == nil {
			var text string
			if err := json.Unmarshal(inner["message"], &text); err == nil && text != "" {
				return text
			}
		}
	}
	return strings.TrimSpace(string(data))
}

// -----------------------------------------------------------------------------
// JWT 工具
// -----------------------------------------------------------------------------

// jwtExpiryMS 解码 JWT payload 的 exp（秒），返回毫秒；解不出返回 0（不验签）。
func jwtExpiryMS(token string) int64 {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return 0
		}
	}
	var claims struct {
		Exp json.Number `json:"exp"`
	}
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(claims.Exp.String(), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
}

// jwtUserID 解码 JWT payload 里的用户 id（不验签）。
func jwtUserID(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	for _, key := range []string{"user_id", "userId", "uid", "userID"} {
		raw, ok := claims[key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil && text != "" {
			return text
		}
		var num json.Number
		if err := json.Unmarshal(raw, &num); err == nil {
			if s := num.String(); s != "" && s != "0" {
				return s
			}
		}
	}
	return ""
}
