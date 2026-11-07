package autoclaw

// 本文件封装对 AutoClaw 上游的 HTTP 调用：签名头、共享客户端、模型目录、JWT 解析。
//
// ── `X-Auth-Sign` 是客户端指纹，不是我们的密钥 ────────────────
// 公式 `MD5(appId&ts&appKey)` 里的 appId/appKey 是**官方客户端内嵌的常量**
// （两地逐字相同，实测确认）。它标识「请求来自 AutoClaw 客户端」，
// 所以硬编码在这里是正确的做法 —— 换成可配置只会让签名对不上。

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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

// 客户端指纹常量（官方客户端内嵌值）。
const (
	authAppID   = "100003"
	authAppKey  = "38d2391985e2369a5fb8227d8e6cd5e5"
	clientVer   = "1.18.5"
	maxCatalogB = 4 << 20
	maxErrorB   = 64 << 10
)

var (
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

// sharedHTTPClient 是无总超时的共享客户端。
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

// httpClientFor 返回本次出网要用的客户端（配置了代理池时经 ProxyPoolSelector 选出口）。
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

func httpProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("代理地址非法")
	}
	return u, nil
}

// signedHeaders 构造 userapi 域的签名头（刷新 / 余额 / 签到 / 目录共用）。
//
// **保留 `X-Harness-Type: zcode`**：转发那条路刻意不发它（会 403 pay-view），
// 但 userapi 域实测带着它照常 200，且它与 X-Auth-Sign 一起构成官方客户端指纹 ——
// 在这条链路上删减的风险方向是签名失败（code 400002），收益是零。
func signedHeaders(acc Account) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := md5.Sum([]byte(authAppID + "&" + ts + "&" + authAppKey))
	headers := map[string]string{
		"Content-Type":   "application/json",
		"Accept":         "*/*",
		"X-Version":      clientVer,
		"X-Product":      "autoclaw",
		"X-Client-Type":  "pc",
		"X-Harness-Type": "zcode",
		"X-Tm":           ts,
		"X-Auth-Sign":    hex.EncodeToString(sum[:]),
	}
	if acc.AccessToken != "" {
		headers["Authorization"] = "Bearer " + acc.AccessToken
	}
	if acc.DeviceID != "" {
		headers["X-Device-Id"] = acc.DeviceID
	}
	return headers
}

// doJSON 发一次请求并读取响应体。
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

// jsonMessage 从上游错误体里尽量取出人类可读文案。
func jsonMessage(data []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return truncate(strings.TrimSpace(string(data)), 300)
	}
	for _, key := range []string{"msg", "message", "error"} {
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

// envelopeCode 取出上游信封里的业务码（缺失返回 0）。
func envelopeCode(data []byte) int {
	var payload struct {
		Code *int `json:"code"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Code == nil {
		return 0
	}
	return *payload.Code
}

// -----------------------------------------------------------------------------
// 模型目录
// -----------------------------------------------------------------------------

// fetchCatalog 拉取模型目录（`autoclaw-model-config`）。
//
// `X-Version` 是版本门控：不带它时国际版只下发 4 条、国内版只下发 3 条；
// 补上 clientVer 后两地都下发全量。signedHeaders 已带。
func (s *Service) fetchCatalog(ctx context.Context, acc Account) ([]ModelInfo, error) {
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodGet,
		userAPIBaseFor(acc.Region)+"/userapi/v1/autoclaw-model-config", nil,
		signedHeaders(acc), 20*time.Second, maxCatalogB)
	if err != nil {
		return nil, fmt.Errorf("拉取模型目录失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("拉取模型目录失败: HTTP %d: %s", status, jsonMessage(data))
	}
	return parseCatalog(data)
}

// parseCatalog 把目录响应解析成归一化模型列表。
func parseCatalog(data []byte) ([]ModelInfo, error) {
	var probe struct {
		Code *int            `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("解析模型目录失败: %w", err)
	}
	if probe.Code != nil && *probe.Code != 0 {
		return nil, fmt.Errorf("模型目录返回异常 code=%d %s", *probe.Code, probe.Msg)
	}
	root := json.RawMessage(data)
	if len(probe.Data) > 0 && string(probe.Data) != "null" {
		root = probe.Data
	}
	// 兼容三种形态：{models:[…]} / {data:{models:[…]}} / 直接数组。
	var wrapper struct {
		Models []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(root, &wrapper); err == nil && len(wrapper.Models) > 0 {
		return normalizeCatalogModels(wrapper.Models), nil
	}
	var direct []struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(root, &direct); err == nil && len(direct) > 0 {
		return normalizeCatalogModels(direct), nil
	}
	return nil, fmt.Errorf("上游模型目录为空")
}

// normalizeCatalogModels 把目录条目归一化（优先 id，其次 key）。
func normalizeCatalogModels(items []struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}) []ModelInfo {
	out := make([]ModelInfo, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = strings.TrimSpace(item.Key)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ModelInfo{ID: id, Name: strings.TrimSpace(item.Name)})
	}
	return out
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
	for _, key := range []string{"user_id", "userId", "uid", "sub"} {
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

// jwtClaims 解出 JWT payload（不验签）。
func jwtClaims(token string) (map[string]json.RawMessage, bool) {
	raw := strings.TrimPrefix(strings.TrimSpace(token), "Bearer ")
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
