package mihomo

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// parseShareLinks 解析按行分隔的节点分享链接（ss://、vmess://、vless://、
// trojan://、hysteria2://、hy2://），转换为 Clash proxy map。
// 无法解析的行被跳过；全部失败时返回空切片。
func parseShareLinks(body string) []config.MihomoNode {
	nodes := []config.MihomoNode{}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		var node config.MihomoNode
		var err error
		switch scheme := shareLinkScheme(line); scheme {
		case "vmess":
			node, err = parseVMessLink(line)
		case "ss":
			node, err = parseSSLink(line)
		case "trojan":
			node, err = parseTrojanLink(line)
		case "vless":
			node, err = parseVLessLink(line)
		case "hysteria2", "hy2":
			node, err = parseHysteria2Link(line)
		default:
			continue
		}
		if err != nil || node.Name == "" {
			continue
		}
		if _, dup := seen[node.Name]; dup {
			continue
		}
		seen[node.Name] = struct{}{}
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

func shareLinkScheme(line string) string {
	idx := strings.Index(line, "://")
	if idx <= 0 || idx > 12 {
		return ""
	}
	return strings.ToLower(line[:idx])
}

func linkNameFromFragment(u *url.URL, fallback string) string {
	if u.Fragment != "" {
		return strings.TrimSpace(u.Fragment)
	}
	return strings.TrimSpace(fallback)
}

func queryBool(q url.Values, keys ...string) bool {
	for _, key := range keys {
		v := strings.ToLower(strings.TrimSpace(q.Get(key)))
		if v == "1" || v == "true" || v == "yes" {
			return true
		}
	}
	return false
}

func queryFirst(q url.Values, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func linkPort(u *url.URL) (int, error) {
	port := u.Port()
	if port == "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.Atoi(port)
}

func linkHost(u *url.URL) string {
	host := u.Hostname()
	return strings.Trim(host, "[]")
}

// parseVMessLink 解析 vmess://<base64(json)>。
func parseVMessLink(line string) (config.MihomoNode, error) {
	payload := strings.TrimPrefix(line, "vmess://")
	decoded, ok := decodeBase64Loose(payload)
	if !ok {
		return config.MihomoNode{}, strconv.ErrSyntax
	}
	var doc struct {
		Name string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		AID  any    `json:"aid"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
		ALPN string `json:"alpn"`
	}
	if err := json.Unmarshal(decoded, &doc); err != nil {
		return config.MihomoNode{}, err
	}
	raw := map[string]any{
		"name":    strings.TrimSpace(doc.Name),
		"type":    "vmess",
		"server":  strings.TrimSpace(doc.Add),
		"port":    jsonNumberToInt(doc.Port),
		"uuid":    strings.TrimSpace(doc.ID),
		"alterId": jsonNumberToInt(doc.AID),
		"cipher":  "auto",
	}
	if strings.TrimSpace(doc.Scy) != "" {
		raw["cipher"] = strings.TrimSpace(doc.Scy)
	}
	network := strings.ToLower(strings.TrimSpace(doc.Net))
	if network != "" && network != "tcp" {
		raw["network"] = network
	}
	if network == "ws" {
		wsOpts := map[string]any{}
		if strings.TrimSpace(doc.Path) != "" {
			wsOpts["path"] = strings.TrimSpace(doc.Path)
		}
		if strings.TrimSpace(doc.Host) != "" {
			wsOpts["headers"] = map[string]any{"Host": strings.TrimSpace(doc.Host)}
		}
		if len(wsOpts) > 0 {
			raw["ws-opts"] = wsOpts
		}
	}
	if strings.EqualFold(strings.TrimSpace(doc.TLS), "tls") {
		raw["tls"] = true
		servername := strings.TrimSpace(doc.SNI)
		if servername == "" {
			servername = strings.TrimSpace(doc.Host)
		}
		if servername != "" {
			raw["servername"] = servername
		}
		if strings.TrimSpace(doc.ALPN) != "" {
			parts := strings.Split(doc.ALPN, ",")
			alpn := make([]any, 0, len(parts))
			for _, p := range parts {
				if v := strings.TrimSpace(p); v != "" {
					alpn = append(alpn, v)
				}
			}
			if len(alpn) > 0 {
				raw["alpn"] = alpn
			}
		}
	}
	return nodeFromRaw(raw)
}

// parseSSLink 解析 SIP002 ss:// 链接：
// ss://base64(method:password)@host:port#name 或 ss://base64(method:password@host:port)#name
func parseSSLink(line string) (config.MihomoNode, error) {
	payload := strings.TrimPrefix(line, "ss://")
	name := ""
	if idx := strings.Index(payload, "#"); idx >= 0 {
		if decoded, err := url.QueryUnescape(payload[idx+1:]); err == nil {
			name = decoded
		}
		payload = payload[:idx]
	}
	if idx := strings.Index(payload, "/?"); idx >= 0 {
		payload = payload[:idx]
	}

	var method, password, host string
	var port int
	if at := strings.LastIndex(payload, "@"); at >= 0 {
		userInfo, err := decodeSSUserInfo(payload[:at])
		if err != nil {
			return config.MihomoNode{}, err
		}
		serverPart := payload[at+1:]
		u, err := url.Parse("ss://" + serverPart)
		if err != nil {
			return config.MihomoNode{}, err
		}
		method, password = splitMethodPassword(userInfo)
		host = linkHost(u)
		port, err = linkPort(u)
		if err != nil {
			return config.MihomoNode{}, err
		}
	} else {
		decoded, ok := decodeBase64Loose(payload)
		if !ok {
			return config.MihomoNode{}, strconv.ErrSyntax
		}
		full := string(decoded)
		at := strings.LastIndex(full, "@")
		if at < 0 {
			return config.MihomoNode{}, strconv.ErrSyntax
		}
		method, password = splitMethodPassword(full[:at])
		u, err := url.Parse("ss://" + full[at+1:])
		if err != nil {
			return config.MihomoNode{}, err
		}
		host = linkHost(u)
		port, err = linkPort(u)
		if err != nil {
			return config.MihomoNode{}, err
		}
	}
	if name == "" {
		name = host
	}
	return nodeFromRaw(map[string]any{
		"name":     strings.TrimSpace(name),
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
	})
}

func decodeSSUserInfo(raw string) (string, error) {
	if decoded, ok := decodeBase64Loose(raw); ok {
		return string(decoded), nil
	}
	unescaped, err := url.QueryUnescape(raw)
	if err != nil {
		return "", err
	}
	return unescaped, nil
}

func splitMethodPassword(userInfo string) (string, string) {
	method, password, ok := strings.Cut(userInfo, ":")
	if !ok {
		return userInfo, ""
	}
	return method, password
}

// parseTrojanLink 解析 trojan://password@host:port?params#name。
func parseTrojanLink(line string) (config.MihomoNode, error) {
	u, err := url.Parse(line)
	if err != nil {
		return config.MihomoNode{}, err
	}
	port, err := linkPort(u)
	if err != nil {
		return config.MihomoNode{}, err
	}
	q := u.Query()
	raw := map[string]any{
		"name":     linkNameFromFragment(u, linkHost(u)),
		"type":     "trojan",
		"server":   linkHost(u),
		"port":     port,
		"password": u.User.String(),
	}
	if sni := queryFirst(q, "sni", "peer"); sni != "" {
		raw["sni"] = sni
	}
	if queryBool(q, "allowInsecure", "allow_insecure", "insecure") {
		raw["skip-cert-verify"] = true
	}
	if alpn := strings.TrimSpace(q.Get("alpn")); alpn != "" {
		parts := strings.Split(alpn, ",")
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			if v := strings.TrimSpace(p); v != "" {
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			raw["alpn"] = out
		}
	}
	if network := strings.ToLower(strings.TrimSpace(q.Get("type"))); network == "ws" {
		raw["network"] = "ws"
		wsOpts := map[string]any{}
		if path := strings.TrimSpace(q.Get("path")); path != "" {
			wsOpts["path"] = path
		}
		if host := queryFirst(q, "host"); host != "" {
			wsOpts["headers"] = map[string]any{"Host": host}
		}
		if len(wsOpts) > 0 {
			raw["ws-opts"] = wsOpts
		}
	}
	return nodeFromRaw(raw)
}

// parseVLessLink 解析 vless://uuid@host:port?params#name。
func parseVLessLink(line string) (config.MihomoNode, error) {
	u, err := url.Parse(line)
	if err != nil {
		return config.MihomoNode{}, err
	}
	port, err := linkPort(u)
	if err != nil {
		return config.MihomoNode{}, err
	}
	q := u.Query()
	raw := map[string]any{
		"name":   linkNameFromFragment(u, linkHost(u)),
		"type":   "vless",
		"server": linkHost(u),
		"port":   port,
		"uuid":   u.User.String(),
	}
	security := strings.ToLower(strings.TrimSpace(q.Get("security")))
	switch security {
	case "tls":
		raw["tls"] = true
	case "reality":
		raw["tls"] = true
		realityOpts := map[string]any{}
		if pbk := strings.TrimSpace(q.Get("pbk")); pbk != "" {
			realityOpts["public-key"] = pbk
		}
		if sid := strings.TrimSpace(q.Get("sid")); sid != "" {
			realityOpts["short-id"] = sid
		}
		if len(realityOpts) > 0 {
			raw["reality-opts"] = realityOpts
		}
	}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		raw["servername"] = sni
	}
	if flow := strings.TrimSpace(q.Get("flow")); flow != "" {
		raw["flow"] = flow
	}
	if fp := strings.TrimSpace(q.Get("fp")); fp != "" {
		raw["client-fingerprint"] = fp
	}
	network := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if network != "" && network != "tcp" {
		raw["network"] = network
	}
	if network == "ws" {
		wsOpts := map[string]any{}
		if path := strings.TrimSpace(q.Get("path")); path != "" {
			wsOpts["path"] = path
		}
		if host := strings.TrimSpace(q.Get("host")); host != "" {
			wsOpts["headers"] = map[string]any{"Host": host}
		}
		if len(wsOpts) > 0 {
			raw["ws-opts"] = wsOpts
		}
	}
	if queryBool(q, "allowInsecure", "allow_insecure", "insecure") {
		raw["skip-cert-verify"] = true
	}
	return nodeFromRaw(raw)
}

// parseHysteria2Link 解析 hysteria2://password@host:port?params#name。
func parseHysteria2Link(line string) (config.MihomoNode, error) {
	scheme := shareLinkScheme(line)
	u, err := url.Parse("hysteria2" + strings.TrimPrefix(line, scheme))
	if err != nil {
		return config.MihomoNode{}, err
	}
	port, err := linkPort(u)
	if err != nil {
		return config.MihomoNode{}, err
	}
	q := u.Query()
	raw := map[string]any{
		"name":     linkNameFromFragment(u, linkHost(u)),
		"type":     "hysteria2",
		"server":   linkHost(u),
		"port":     port,
		"password": u.User.String(),
	}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		raw["sni"] = sni
	}
	if queryBool(q, "insecure", "allowInsecure", "allow_insecure") {
		raw["skip-cert-verify"] = true
	}
	if obfs := strings.TrimSpace(q.Get("obfs")); obfs != "" {
		raw["obfs"] = obfs
		if obfsPassword := strings.TrimSpace(q.Get("obfs-password")); obfsPassword != "" {
			raw["obfs-password"] = obfsPassword
		}
	}
	return nodeFromRaw(raw)
}

func jsonNumberToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		out, _ := strconv.Atoi(strings.TrimSpace(n))
		return out
	case json.Number:
		out, _ := n.Int64()
		return int(out)
	default:
		return 0
	}
}

// nodeFromRaw 校验并构造节点；缺关键字段时返回错误。
func nodeFromRaw(raw map[string]any) (config.MihomoNode, error) {
	name, _ := raw["name"].(string)
	nodeType, _ := raw["type"].(string)
	server, _ := raw["server"].(string)
	port, _ := raw["port"].(int)
	if strings.TrimSpace(name) == "" || nodeType == "" || strings.TrimSpace(server) == "" || port <= 0 {
		return config.MihomoNode{}, strconv.ErrSyntax
	}
	return config.MihomoNode{
		Name: strings.TrimSpace(name),
		Type: strings.ToLower(nodeType),
		Raw:  raw,
	}, nil
}
