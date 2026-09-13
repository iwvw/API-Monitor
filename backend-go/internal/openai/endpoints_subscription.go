package openai

import (
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
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// listSubscriptionSocksProxies 读取订阅板块中的 socks/http 协议节点，转换为可直接使用的代理 URL。
func (s *Service) listSubscriptionSocksProxies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,''), COALESCE(type,''), COALESCE(server,''), COALESCE(port,0), COALESCE(location,''), COALESCE(raw_encrypted,'') FROM subscription_nodes WHERE enabled = 1`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for rows.Next() {
		var item subscriptionProxy
		var rawEnc string
		if err := rows.Scan(&item.NodeID, &item.Name, &item.Type, &item.Server, &item.Port, &item.Location, &rawEnc); err != nil {
			continue
		}
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		if item.Type != "socks" && item.Type != "socks5" && item.Type != "http" && item.Type != "https" {
			continue
		}
		raw := secure.SecureDecrypt(rawEnc)
		proxy, name, ok := convertNodeToProxy(item.Type, raw, item.Server, item.Port, item.Name)
		if !ok || proxy == "" {
			continue
		}
		item.Proxy = proxy
		item.Name = name
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, item)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// importProxyListRoute 解析用户上传的代理列表文本（例如 .txt 文件内容，每行一个代理），
// 清洗并去重后返回可直接写入端点代理池的代理 URL 列表及统计。
// 支持 http(s)://、socks5:// 与裸 host:port；也兼容 base64 编码的订阅文本。
func (s *Service) importProxyListRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	const maxImportBytes = 16 * 1024 * 1024
	if len(req.Text) > maxImportBytes {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "代理列表过大（上限 16MB）"})
		return
	}

	proxies := parseSubscriptionProxyText(req.Text)
	urls := make([]string, 0, len(proxies))
	for _, p := range proxies {
		urls = append(urls, p.Proxy)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(urls),
		"proxies": urls,
	})
}

// resolveSubscriptionProxies 拉取用户粘贴的订阅链接，解析其中的 socks/http 节点，
// 转换为可直接写入端点代理池的代理 URL。
func (s *Service) resolveSubscriptionProxies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请填写订阅链接"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(req.URL), "http://") && !strings.HasPrefix(strings.ToLower(req.URL), "https://") {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "订阅链接必须以 http:// 或 https:// 开头"})
		return
	}

	ctx := r.Context()
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, req.URL, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "构造请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "API-Monitor-OpenAI/1.0")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": "拉取订阅失败: " + err.Error()})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("订阅源返回 HTTP %d", resp.StatusCode)})
		return
	}

	proxies := parseSubscriptionProxyText(string(body))
	if len(proxies) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"proxies": []subscriptionProxy{},
			"message": "订阅内容中没有找到 socks/http 节点",
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// parseSubscriptionProxyText 解析订阅内容（可能是 base64 节点列表或纯文本），
// 提取其中的 socks/socks5/http/https 节点为代理 URL。
func parseSubscriptionProxyText(content string) []subscriptionProxy {
	text := strings.TrimSpace(content)
	lines := []string{}
	// 尝试 base64 解码：订阅常见格式是 base64 编码的每行一个节点 URI。
	decoded := decodeBase64Text(text)
	if decoded != "" {
		text = decoded
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for _, line := range lines {
		proxy, name, ok := convertNodeToProxy("", line, "", 0, "")
		if !ok || proxy == "" {
			continue
		}
		// 仅接受 socks/http 出口；裸 server:port 也归为 socks5 出口。
		scheme := proxy
		if idx := strings.Index(proxy, "://"); idx >= 0 {
			scheme = strings.ToLower(proxy[:idx])
		}
		if scheme != "socks5" && scheme != "http" && scheme != "https" {
			continue
		}
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, subscriptionProxy{
			Name:   name,
			Type:   scheme,
			Proxy:  proxy,
			Server: hostFromProxyURL(proxy),
		})
	}
	return proxies
}

// decodeBase64Text 尝试将内容按 base64 解码；成功且结果可读时返回解码文本。
func decodeBase64Text(text string) string {
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, text)
	raw, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return ""
	}
	decoded := string(raw)
	if strings.Contains(decoded, "://") || strings.Contains(decoded, "vmess://") || strings.Contains(decoded, "trojan://") || strings.Contains(decoded, "ss://") {
		return decoded
	}
	return ""
}

// hostFromProxyURL 从代理 URL 中提取 host 部分用于展示。
func hostFromProxyURL(proxy string) string {
	u, err := url.Parse(proxy)
	if err != nil {
		return proxy
	}
	return u.Host
}

// convertNodeToProxy 把订阅节点 raw URI 转换为网关可用的 socks5/http 代理 URL。
// 优先复用 raw 中的用户凭据；raw 无法解析时回退为 server:port。
func convertNodeToProxy(nodeType, raw, server string, port int, fallbackName string) (proxy, name string, ok bool) {
	name = strings.TrimSpace(fallbackName)
	if name == "" {
		name = server
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "socks://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "socks5://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "http://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		u, err := url.Parse(trimmed)
		if err == nil && u.Host != "" {
			scheme := u.Scheme
			if scheme == "socks" || scheme == "socks5" {
				scheme = "socks5"
			}
			u.Scheme = scheme
			// 去掉 fragment（节点名常放在 # 后）。
			u.Fragment = ""
			if fragName := parseNodeFragment(trimmed); fragName != "" {
				name = fragName
			}
			return u.String(), name, true
		}
	}
	// 无 raw 或解析失败：直接用 server:port 构造成 socks5。
	if server == "" || port < 1 || port > 65535 {
		return "", name, false
	}
	return fmt.Sprintf("socks5://%s", net.JoinHostPort(server, strconv.Itoa(port))), name, true
}

func parseNodeFragment(raw string) string {
	idx := strings.LastIndex(raw, "#")
	if idx < 0 || idx+1 >= len(raw) {
		return ""
	}
	name := strings.TrimSpace(raw[idx+1:])
	name = strings.Trim(name, "\"")
	return name
}
