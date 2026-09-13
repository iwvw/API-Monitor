package subscription

import (
	"encoding/json"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

func preparePublishedNodes(nodes []Node) []Node {
	filtered := make([]Node, 0, len(nodes))
	seen := map[string]bool{}
	for _, node := range nodes {
		node.Name = strings.TrimSpace(node.Name)
		node.Raw = strings.TrimSpace(node.Raw)
		node.ConfigJSON = strings.TrimSpace(node.ConfigJSON)
		if node.Name == "" {
			node.Name = firstNonEmpty(strings.TrimSpace(node.Server), strings.TrimSpace(node.Type), "未命名节点")
		}
		if !hasValidPublishedRawURI(node) && validatedPublishedProxyConfig(node) == nil {
			continue
		}
		key := publishedNodeDedupKey(node)
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		filtered = append(filtered, node)
	}
	return ensureUniquePublishedNodeNames(filtered)
}

func publishedNodeDedupKey(node Node) string {
	if proxy := validatedPublishedProxyConfig(node); len(proxy) > 0 {
		canonical := make(map[string]interface{}, len(proxy))
		for key, value := range proxy {
			if key == "name" {
				continue
			}
			canonical[key] = value
		}
		if encoded, err := json.Marshal(canonical); err == nil {
			return "proxy:" + string(encoded)
		}
	}
	if hasValidPublishedRawURI(node) {
		return "raw:" + canonicalizePublishedRawURI(node.Raw)
	}
	return ""
}

func validatedPublishedProxyConfig(node Node) map[string]interface{} {
	proxy := nodeProxyConfig(node)
	if len(proxy) == 0 {
		return nil
	}
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port <= 0 {
		return nil
	}
	switch typ {
	case "vless", "vmess":
		if strings.TrimSpace(stringVal(proxy["uuid"])) == "" {
			return nil
		}
	case "trojan", "hysteria2", "tuic":
		if strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return nil
		}
	case "ss":
		if strings.TrimSpace(stringVal(proxy["cipher"])) == "" || strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return nil
		}
	case "http", "socks5", "socks":
	default:
		return nil
	}
	return proxy
}

func hasValidPublishedRawURI(node Node) bool {
	raw := strings.TrimSpace(node.Raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		return false
	}
	switch scheme {
	case "vmess":
		if parsed.Host == "" {
			_, ok := parseVMessBase64URI(raw, 1)
			return ok
		}
		return parsed.Hostname() != ""
	case "vless", "trojan", "ss", "hysteria2", "hy2", "tuic", "socks", "http":
		return parsed.Hostname() != ""
	default:
		return false
	}
}

func canonicalizePublishedRawURI(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Fragment = ""
	return parsed.String()
}

func proxiesYAML(nodes []Node, indent int) string {
	lines := []string{}
	pad := strings.Repeat(" ", indent)
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		proxy := validatedPublishedProxyConfig(node)
		if len(proxy) == 0 {
			continue
		}
		// The node management record is authoritative for the published name.
		// ConfigJSON may retain the original URI name after a node is renamed;
		// exporting that stale value can create duplicate Mihomo proxy names.
		proxy["name"] = node.Name
		encoded, err := yaml.Marshal([]interface{}{proxy})
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(string(encoded), "\n"), "\n") {
			lines = append(lines, pad+line)
		}
	}
	if len(lines) == 0 {
		return pad + "[]"
	}
	return strings.Join(lines, "\n")
}

func proxyNamesYAML(nodes []Node, stableOnly bool, indent int) string {
	lines := []string{}
	for _, node := range nodes {
		if !node.Enabled || (stableOnly && !node.Stable) {
			continue
		}
		lines = append(lines, yamlStringListItem(node.Name, indent))
	}
	if stableOnly && len(lines) == 0 {
		lines = append(lines, yamlStringListItem("🚀 手动", indent))
	}
	if len(lines) == 0 {
		lines = append(lines, yamlStringListItem("DIRECT", indent))
	}
	return strings.Join(lines, "\n")
}

func stableProxyNamesYAML(nodes []Node, indent int) string {
	lines := []string{}
	seen := map[string]bool{}
	for _, node := range nodes {
		if !node.Enabled || !node.Stable || seen[node.Name] {
			continue
		}
		lines = append(lines, yamlStringListItem(node.Name, indent))
		seen[node.Name] = true
	}
	if len(lines) == 0 {
		lines = append(lines, yamlStringListItem("DIRECT", indent))
	}
	return strings.Join(lines, "\n")
}

func yamlStringListItem(value string, indent int) string {
	encoded, err := yaml.Marshal([]string{value})
	if err != nil {
		return strings.Repeat(" ", indent) + "- " + value
	}
	line := strings.TrimSpace(strings.Split(strings.TrimRight(string(encoded), "\n"), "\n")[0])
	return strings.Repeat(" ", indent) + line
}

func nodeProxyConfig(node Node) map[string]interface{} {
	cfg := strings.TrimSpace(node.ConfigJSON)
	if cfg != "" {
		var parsed interface{}
		if json.Unmarshal([]byte(cfg), &parsed) == nil {
			if proxy, ok := normalizeStringMap(parsed).(map[string]interface{}); ok {
				if proxyConfigComplete(proxy) {
					return proxy
				}
			}
		}
		if yaml.Unmarshal([]byte(cfg), &parsed) == nil {
			if proxy, ok := normalizeStringMap(parsed).(map[string]interface{}); ok {
				if proxyConfigComplete(proxy) {
					return proxy
				}
			}
		}
	}
	return uriToClashMap(node)
}

func proxyConfigComplete(proxy map[string]interface{}) bool {
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port <= 0 {
		return false
	}
	switch typ {
	case "vless", "vmess":
		return strings.TrimSpace(stringVal(proxy["uuid"])) != ""
	case "trojan", "hysteria2", "tuic":
		return strings.TrimSpace(stringVal(proxy["password"])) != ""
	case "ss":
		return strings.TrimSpace(stringVal(proxy["cipher"])) != "" && strings.TrimSpace(stringVal(proxy["password"])) != ""
	case "http", "socks5", "socks":
		return true
	default:
		return false
	}
}

func rawURIList(nodes []Node) string {
	lines := []string{}
	for _, node := range nodes {
		if node.Enabled && hasValidPublishedRawURI(node) {
			lines = append(lines, strings.TrimSpace(node.Raw))
		}
	}
	return strings.Join(lines, "\n")
}

func filterNodesByIDs(nodes []Node, ids []string) []Node {
	if len(ids) == 0 {
		return nodes
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			allowed[id] = true
		}
	}
	if len(allowed) == 0 {
		return nodes
	}
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if allowed[node.ID] {
			out = append(out, node)
		}
	}
	return out
}

func encodeNodeFilterIDs(ids []string) string {
	clean := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(clean)
	return string(encoded)
}

func decodeNodeFilterIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) == nil {
		return compactStringList(ids)
	}
	return compactStringList(strings.Split(raw, ","))
}

func compactStringList(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
