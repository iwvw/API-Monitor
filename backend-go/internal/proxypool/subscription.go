package proxypool

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"time"
)

// parseSubscriptionText 解析订阅内容：先尝试 base64 解码（Clash 等订阅常见），
// 再按行提取 http/https/socks5 代理 URL。返回去重后的代理列表。
func parseSubscriptionText(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	// 尝试 base64 解码（去除空白/换行后解码）。
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.ReplaceAll(text, "\n", ""))); err == nil && len(decoded) > 0 {
		text = string(decoded)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "proxies:" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "socks5://") {
			if !seen[line] {
				seen[line] = true
				out = append(out, line)
			}
		}
	}
	return out
}

// ResolveSubscription 拉取订阅链接并解析出代理 URL 列表。
func (s *Service) ResolveSubscription(ctx context.Context, url string) ([]string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "API-Monitor-Proxypool/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errSubHTTPStatus(resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	return parseSubscriptionText(string(body)), nil
}
