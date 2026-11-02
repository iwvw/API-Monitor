// Package mihomo 实现 Mihomo 代理桥：订阅解析、运行时配置生成、
// 子进程生命周期管理，以及“账号 ↔ 节点”绑定（一号一 IP）。
//
// 设计要点：桥不为请求路径新增拨号逻辑，而是为每个绑定的节点在本机
// 分配一个独立 SOCKS5 端口（mihomo listener 直出该节点），并以托管
// config.Proxy（ID 前缀 mihomo-）+ Account.ProxyID 的形式写回主配置，
// 从而完整复用 ds2api 既有的按账号代理分流能力。
package mihomo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

const (
	// subscriptionFetchTimeout 限制订阅抓取总耗时。
	subscriptionFetchTimeout = 30 * time.Second
	// subscriptionMaxBytes 防止异常订阅撑爆内存（一般 Clash 订阅 < 2MB）。
	subscriptionMaxBytes = 16 << 20
	// subscriptionUserAgent 伪装成 Clash 客户端，部分机场按 UA 下发不同格式。
	subscriptionUserAgent = "clash.meta/v1.18.0"
)

// subscriptionHTTPClient 可替换以便测试。
var subscriptionHTTPClient = &http.Client{Timeout: subscriptionFetchTimeout}

// FetchSubscription 抓取并解析订阅内容，返回节点列表。
func FetchSubscription(ctx context.Context, rawURL string) ([]config.MihomoNode, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("订阅链接为空")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("订阅链接必须是 http(s) URL: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)

	resp, err := subscriptionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("订阅请求失败: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close subscription body failed", "error", closeErr)
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("订阅返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, subscriptionMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("读取订阅内容失败: %w", err)
	}
	return ParseSubscription(body)
}

// ParseSubscription 解析订阅响应体。按以下顺序探测格式：
//  1. Clash/Mihomo YAML（含 proxies: 列表）
//  2. 整体 Base64 编码后的 Clash YAML
//  3. 按行分隔的分享链接（ss/vmess/vless/trojan/hysteria2，可 Base64）
func ParseSubscription(body []byte) ([]config.MihomoNode, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, errors.New("订阅内容为空")
	}
	if nodes, err := parseClashYAML([]byte(trimmed)); err == nil {
		return nodes, nil
	}
	if decoded, ok := decodeBase64Loose(trimmed); ok {
		if nodes, err := parseClashYAML(decoded); err == nil {
			return nodes, nil
		}
		if nodes := parseShareLinks(string(decoded)); len(nodes) > 0 {
			return nodes, nil
		}
	}
	if nodes := parseShareLinks(trimmed); len(nodes) > 0 {
		return nodes, nil
	}
	return nil, errors.New("无法识别的订阅格式（既不是 Clash YAML 也不是分享链接列表）")
}

// parseClashYAML 解析 Clash/Mihomo 格式订阅，提取 proxies 列表。
func parseClashYAML(body []byte) ([]config.MihomoNode, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if len(doc.Proxies) == 0 {
		return nil, errors.New("订阅中没有 proxies 节点")
	}
	nodes := make([]config.MihomoNode, 0, len(doc.Proxies))
	seen := map[string]struct{}{}
	for _, raw := range doc.Proxies {
		name, _ := raw["name"].(string)
		name = strings.TrimSpace(name)
		nodeType, _ := raw["type"].(string)
		nodeType = strings.ToLower(strings.TrimSpace(nodeType))
		if name == "" || nodeType == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue // 同名节点保留先出现的，保证键稳定
		}
		seen[name] = struct{}{}
		raw["name"] = name
		raw["type"] = nodeType
		nodes = append(nodes, config.MihomoNode{Name: name, Type: nodeType, Raw: raw})
	}
	if len(nodes) == 0 {
		return nil, errors.New("订阅中没有可用节点（缺少 name/type 字段）")
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

// decodeBase64Loose 尝试多种 Base64 变体解码整段文本（忽略空白换行）。
// 先快速扫描字符集：只要出现明显不属于 Base64 字母表的字符就立即放弃，
// 避免对最大 16MB 的普通文本白做多轮全量解码。
func decodeBase64Loose(raw string) ([]byte, bool) {
	var b strings.Builder
	b.Grow(len(raw))
	plausible := true
	for _, r := range raw {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			continue
		}
		if !isBase64AlphabetChar(r) {
			plausible = false
		}
		b.WriteRune(r)
	}
	compact := b.String()
	if compact == "" || !plausible {
		return nil, false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if decoded, err := enc.DecodeString(compact); err == nil && len(decoded) > 0 {
			return decoded, true
		}
	}
	return nil, false
}

// isBase64AlphabetChar 判断字符是否可能出现在 Base64 文本中
// （标准/URL-safe 两种字母表 + 填充符）。
func isBase64AlphabetChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	default:
		return r == '+' || r == '/' || r == '-' || r == '_' || r == '='
	}
}
