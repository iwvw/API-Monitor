package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseImportText(text string) []Node {
	return parseImportTextDepth(text, 0)
}

func parseImportTextDepth(text string, depth int) []Node {
	if nodes := parseClashProxyNodes(text); len(nodes) > 0 {
		return nodes
	}
	nodes := parseNodeURIs(text)
	if len(nodes) > 0 {
		return nodes
	}
	if len(nodes) == 0 && depth < 2 {
		if decoded, ok := decodeSubscriptionBase64(text); ok {
			return parseImportTextDepth(decoded, depth+1)
		}
	}
	return nodes
}

func parseClashProxyNodes(text string) []Node {
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil
	}
	rawProxies, ok := doc["proxies"].([]interface{})
	if !ok || len(rawProxies) == 0 {
		return nil
	}
	nodes := []Node{}
	for _, item := range rawProxies {
		proxy, ok := normalizeStringMap(item).(map[string]interface{})
		if !ok {
			continue
		}
		name := stringVal(proxy["name"])
		server := stringVal(proxy["server"])
		typ := strings.ToLower(stringVal(proxy["type"]))
		if name == "" || server == "" || typ == "" {
			continue
		}
		countryCode := countryCodeFromNodeName(name)
		cfg, _ := json.Marshal(proxy)
		nodes = append(nodes, Node{
			ID:          randomID("node"),
			Name:        name,
			Type:        typ,
			Server:      server,
			Port:        int(floatVal(proxy["port"])),
			CountryCode: countryCode,
			Enabled:     true,
			ConfigJSON:  string(cfg),
			SortOrder:   len(nodes) + 1,
			Source:      "managed",
		})
	}
	return nodes
}

func parseNodeURIs(text string) []Node {
	nodes := []Node{}
	seen := map[string]bool{}
	for _, candidate := range nodeURICandidates(text) {
		clean := cleanNodeURI(candidate)
		if clean == "" || seen[clean] || isLikelyPlainHTTPAsset(clean) {
			continue
		}
		seen[clean] = true
		nodes = append(nodes, parseURI(clean, len(nodes)+1))
	}
	return nodes
}

func nodeURICandidates(text string) []string {
	candidates := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if loc := nodeLinkPattern.FindStringIndex(trimmed); loc != nil {
			candidates = append(candidates, trimmed[loc[0]:])
		}
	}
	if len(candidates) > 0 {
		return candidates
	}
	return nodeLinkPattern.FindAllString(text, -1)
}

func cleanNodeURI(value string) string {
	clean := strings.TrimSpace(value)
	clean = strings.TrimLeft(clean, "- ")
	clean = strings.Trim(clean, `"' ,`)
	return strings.TrimSpace(clean)
}

func normalizeStringMap(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range v {
			out[key] = normalizeStringMap(item)
		}
		return out
	case map[interface{}]interface{}:
		out := map[string]interface{}{}
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeStringMap(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeStringMap(item))
		}
		return out
	default:
		return value
	}
}

func isLikelyPlainHTTPAsset(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	query := strings.ToLower(parsed.RawQuery)
	fragment := strings.TrimSpace(parsed.Fragment)
	if strings.Contains(query, "type=") || strings.Contains(query, "server=") || fragment != "" {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".yml") ||
		strings.HasSuffix(path, ".txt") ||
		strings.Contains(path, "/ruleset/") ||
		strings.Contains(path, "clash-rules")
}

func decodeSubscriptionBase64(text string) (string, bool) {
	clean := strings.TrimSpace(text)
	if clean == "" || strings.Contains(clean, "\n") && strings.Contains(clean, "://") {
		return "", false
	}
	clean = strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(clean)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(clean); err == nil && strings.Contains(string(decoded), "://") {
			return string(decoded), true
		}
	}
	if remainder := len(clean) % 4; remainder != 0 {
		padded := clean + strings.Repeat("=", 4-remainder)
		if decoded, err := base64.StdEncoding.DecodeString(padded); err == nil && strings.Contains(string(decoded), "://") {
			return string(decoded), true
		}
	}
	return "", false
}

func parseURI(raw string, order int) Node {
	node := Node{ID: randomID("node"), Raw: raw, Enabled: true, SortOrder: order}
	parsed, err := url.Parse(raw)
	if err != nil {
		node.Name = fmt.Sprintf("节点 %d", order)
		return node
	}
	node.Type = strings.ToLower(parsed.Scheme)
	if node.Type == "hy2" {
		node.Type = "hysteria2"
	}
	if parsed.Fragment != "" {
		if name, err := url.QueryUnescape(parsed.Fragment); err == nil {
			node.Name = name
		}
	}
	if countryCode, displayName := normalizeImportedNodeName(node.Name); countryCode != "" {
		node.CountryCode = countryCode
		node.Name = displayName
	} else if countryCode := countryCodePrefix(node.Name); countryCode != "" {
		node.CountryCode = countryCode
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("%s-%d", node.Type, order)
	}
	node.Server = parsed.Hostname()
	if p, _ := strconv.Atoi(parsed.Port()); p > 0 {
		node.Port = p
	}
	if node.Type == "vmess" && parsed.Host == "" {
		if parsedNode, ok := parseVMessBase64URI(raw, order); ok {
			return parsedNode
		}
	}
	if cfg := uriToClashMap(node); len(cfg) > 0 {
		if encoded, err := json.Marshal(cfg); err == nil {
			node.ConfigJSON = string(encoded)
		}
	}
	return node
}

func countryCodeFromNodeName(name string) string {
	if countryCode, _ := normalizeImportedNodeName(name); countryCode != "" {
		return countryCode
	}
	return countryCodePrefix(name)
}

func normalizeImportedNodeName(name string) (string, string) {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) < 2 || !isRegionalIndicator(runes[0]) || !isRegionalIndicator(runes[1]) {
		return "", name
	}
	code := string([]rune{'A' + (runes[0] - 0x1F1E6), 'A' + (runes[1] - 0x1F1E6)})
	return code, name
}

func isRegionalIndicator(value rune) bool {
	return value >= 0x1F1E6 && value <= 0x1F1FF
}

var twoLetterCodeRegex = regexp.MustCompile(`^[A-Za-z]{2}$`)

func countryCodePrefix(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return ""
	}
	prefix := name[:2]
	if !twoLetterCodeRegex.MatchString(prefix) {
		return ""
	}
	if len(name) == 2 {
		return strings.ToUpper(prefix)
	}
	next := rune(name[2])
	if next == ' ' || next == '_' || next == '-' || (next >= 0x4e00 && next <= 0x9fff) {
		return strings.ToUpper(prefix)
	}
	return ""
}

func uriToClashJSON(node Node) string {
	m := uriToClashMap(node)
	b, _ := json.Marshal(m)
	return string(b)
}

func uriToClashMap(node Node) map[string]interface{} {
	m := map[string]interface{}{
		"name":   node.Name,
		"type":   node.Type,
		"server": node.Server,
		"port":   node.Port,
	}
	parsed, err := url.Parse(node.Raw)
	if err != nil {
		return m
	}
	query := parsed.Query()
	switch node.Type {
	case "vless":
		m["uuid"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("encryption"), "none"); value != "" {
			m["encryption"] = value
		}
		network := firstNonEmpty(query.Get("type"), query.Get("network"))
		if network != "" && network != "tcp" {
			m["network"] = network
		}
		if strings.EqualFold(query.Get("security"), "tls") {
			m["tls"] = true
		}
		if strings.EqualFold(query.Get("security"), "reality") {
			m["tls"] = true
			m["flow"] = firstNonEmpty(query.Get("flow"), "xtls-rprx-vision")
			reality := map[string]interface{}{}
			if value := firstNonEmpty(query.Get("pbk"), query.Get("public-key")); value != "" {
				reality["public-key"] = value
			}
			if value := firstNonEmpty(query.Get("sid"), query.Get("short-id")); value != "" {
				reality["short-id"] = value
			}
			if len(reality) > 0 {
				m["reality-opts"] = reality
			}
		}
		if value := firstNonEmpty(query.Get("sni"), query.Get("servername")); value != "" {
			m["servername"] = value
			m["sni"] = value
		}
		if value := query.Get("fp"); value != "" {
			m["client-fingerprint"] = value
		}
		if queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if network == "ws" {
			wsOpts := map[string]interface{}{}
			if value := query.Get("path"); value != "" {
				wsOpts["path"] = value
			}
			if value := firstNonEmpty(query.Get("host"), query.Get("Host"), query.Get("sni"), query.Get("servername")); value != "" {
				wsOpts["headers"] = map[string]interface{}{"Host": value}
			}
			if len(wsOpts) > 0 {
				m["ws-opts"] = wsOpts
			}
		}
	case "trojan":
		m["password"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("sni"), query.Get("peer"), query.Get("servername")); value != "" {
			sni, embeddedALPN := splitSNIAndEmbeddedALPN(value)
			m["sni"] = sni
			if len(embeddedALPN) > 0 {
				m["alpn"] = embeddedALPN
			}
		}
		m["tls"] = true
		if queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if alpn := splitCSV(query.Get("alpn")); len(alpn) > 0 {
			m["alpn"] = alpn
		}
		if network := firstNonEmpty(query.Get("type"), query.Get("network")); network != "" {
			m["network"] = network
		}
		if value := query.Get("fp"); value != "" {
			m["client-fingerprint"] = value
		}
	case "hysteria2":
		m["password"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("sni"), query.Get("peer"), query.Get("servername")); value != "" {
			m["sni"] = value
		}
		if queryBool(query, "insecure") || queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if alpn := splitCSV(query.Get("alpn")); len(alpn) > 0 {
			m["alpn"] = alpn
		}
	case "ss":
		if user := parsed.User.String(); user != "" {
			if decoded, ok := decodeMaybeBase64(user); ok {
				parts := strings.SplitN(decoded, ":", 2)
				if len(parts) == 2 {
					m["cipher"] = parts[0]
					m["password"] = parts[1]
				}
			}
		}
	}
	return m
}

func queryBool(values url.Values, key string) bool {
	value := strings.ToLower(strings.TrimSpace(values.Get(key)))
	return value == "1" || value == "true" || value == "yes"
}

func splitSNIAndEmbeddedALPN(value string) (string, []string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	markers := []string{"h3,", "h2,", "http/1.1"}
	cut := -1
	for _, marker := range markers {
		if idx := strings.Index(value, marker); idx >= 0 && (cut == -1 || idx < cut) {
			cut = idx
		}
	}
	if cut <= 0 {
		return value, nil
	}
	sni := strings.TrimSpace(strings.TrimRight(value[:cut], ","))
	alpn := splitCSV(value[cut:])
	if sni == "" {
		return value, nil
	}
	return sni, alpn
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func decodeMaybeBase64(value string) (string, bool) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	return "", false
}

func parseVMessBase64URI(raw string, order int) (Node, bool) {
	payload := strings.TrimPrefix(raw, "vmess://")
	decoded, ok := decodeMaybeBase64(payload)
	if !ok {
		return Node{}, false
	}
	var cfg map[string]interface{}
	if json.Unmarshal([]byte(decoded), &cfg) != nil {
		return Node{}, false
	}
	name := firstNonEmpty(stringVal(cfg["ps"]), fmt.Sprintf("vmess-%d", order))
	server := stringVal(cfg["add"])
	port, _ := strconv.Atoi(stringVal(cfg["port"]))
	proxy := map[string]interface{}{
		"name":    name,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    stringVal(cfg["id"]),
		"alterId": int(floatVal(cfg["aid"])),
		"cipher":  firstNonEmpty(stringVal(cfg["scy"]), "auto"),
	}
	if network := stringVal(cfg["net"]); network != "" && network != "tcp" {
		proxy["network"] = network
	}
	if tls := stringVal(cfg["tls"]); tls == "tls" {
		proxy["tls"] = true
	}
	if sni := stringVal(cfg["sni"]); sni != "" {
		proxy["servername"] = sni
	}
	if stringVal(cfg["net"]) == "ws" {
		wsOpts := map[string]interface{}{}
		if path := stringVal(cfg["path"]); path != "" {
			wsOpts["path"] = path
		}
		if host := stringVal(cfg["host"]); host != "" {
			wsOpts["headers"] = map[string]interface{}{"Host": host}
		}
		if len(wsOpts) > 0 {
			proxy["ws-opts"] = wsOpts
		}
	}
	encoded, _ := json.Marshal(proxy)
	return Node{ID: randomID("node"), Raw: raw, Enabled: true, SortOrder: order, Name: name, Type: "vmess", Server: server, Port: port, ConfigJSON: string(encoded)}, true
}

func parseUserInfo(raw string) TrafficInfo {
	info := TrafficInfo{Status: "active"}
	for _, part := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		value, _ := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		switch strings.TrimSpace(kv[0]) {
		case "upload":
			info.Upload = value
		case "download":
			info.Download = value
		case "total":
			info.Total = value
		case "expire":
			info.Expire = value
		}
	}
	return info
}
