package zcode

// 本文件封装对 ZCode 上游的 HTTP 调用：共享客户端、错误文案、探活。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	req.Header.Set("User-Agent", "ZCode/1.0")
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
	for _, key := range []string{"msg", "message", "error_description", "error"} {
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

// probeAccount 用 accessToken 拉一次模型列表，验证推理平面可用。
//
// 上游 `{base}/models` 是标准 OpenAI 端点；返回 200 即说明凭证有效
// （不同套餐能用的模型由上游按调用时判，这里只验凭证）。
func (s *Service) probeAccount(ctx context.Context, acc Account) error {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return fmt.Errorf("账号缺少 access token")
	}
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodGet,
		openAIBaseFor(acc.Region)+"/models", nil, acc.AccessToken,
		map[string]string{"Accept": "application/json"}, 20*time.Second, maxCatalogBytes)
	if err != nil {
		return fmt.Errorf("连通性测试失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("连通性测试失败: HTTP %d: %s", status, jsonMessage(data))
	}
	return nil
}
