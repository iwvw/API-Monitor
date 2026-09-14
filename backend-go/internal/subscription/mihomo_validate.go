package subscription

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func validateTemplateDefinition(format, content string) error {
	format = normalizeTemplateFormat(format)
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("模板内容不能为空")
	}
	if format != "clash" && format != "raw" && format != "base64" {
		return fmt.Errorf("不支持的模板格式 %q", format)
	}
	if format != "clash" {
		if unresolved := unresolvedTemplatePattern.FindString(content); unresolved != "" && unresolved != "{{ raw_uri_list }}" {
			return fmt.Errorf("包含未知占位符 %s", unresolved)
		}
		return nil
	}
	fixtureNodes := preparePublishedNodes([]Node{
		{Name: "模板验证节点", Type: "vless", Server: "node.example.com", Port: 443, ConfigJSON: `{"name":"模板验证节点","type":"vless","server":"node.example.com","port":443,"uuid":"00000000-0000-4000-8000-000000000001"}`, Enabled: true, Stable: true},
	})
	fixture := Subscription{Name: "模板验证", Traffic: TrafficInfo{Total: 1024}}
	rendered := renderTemplate(content, fixture, fixtureNodes)
	if unresolved := unresolvedTemplatePattern.FindString(rendered); unresolved != "" {
		return fmt.Errorf("包含未知占位符 %s", unresolved)
	}
	return validateMihomoOutput(rendered)
}

func validateMihomoOutput(body string) error {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(body), &document); err != nil {
		return fmt.Errorf("YAML 语法错误: %w", err)
	}
	if err := validateYAMLMappingKeys(&document); err != nil {
		return err
	}
	var raw interface{}
	if err := document.Decode(&raw); err != nil {
		return fmt.Errorf("YAML 解析失败: %w", err)
	}
	root, ok := normalizeStringMap(raw).(map[string]interface{})
	if !ok {
		return errors.New("顶层配置必须是对象")
	}
	proxyNames := map[string]string{}
	if rawProxies, exists := root["proxies"]; exists {
		proxies, ok := rawProxies.([]interface{})
		if !ok {
			return errors.New("proxies 必须是列表")
		}
		for index, item := range proxies {
			proxy, ok := normalizeStringMap(item).(map[string]interface{})
			if !ok {
				return fmt.Errorf("proxies[%d] 必须是对象", index)
			}
			name := strings.TrimSpace(stringVal(proxy["name"]))
			if name == "" {
				return fmt.Errorf("proxies[%d] 缺少名称", index)
			}
			key := strings.ToLower(name)
			if previous, duplicate := proxyNames[key]; duplicate {
				return fmt.Errorf("代理名称重复: %s（与 %s 冲突）", name, previous)
			}
			proxyNames[key] = name
			if err := validateMihomoProxy(proxy); err != nil {
				return fmt.Errorf("代理 %s 配置无效: %w", name, err)
			}
		}
	}

	groupNames := map[string]string{}
	groups := []map[string]interface{}{}
	if rawGroups, exists := root["proxy-groups"]; exists {
		items, ok := rawGroups.([]interface{})
		if !ok {
			return errors.New("proxy-groups 必须是列表")
		}
		for index, item := range items {
			group, ok := normalizeStringMap(item).(map[string]interface{})
			if !ok {
				return fmt.Errorf("proxy-groups[%d] 必须是对象", index)
			}
			name := strings.TrimSpace(stringVal(group["name"]))
			if name == "" || strings.TrimSpace(stringVal(group["type"])) == "" {
				return fmt.Errorf("proxy-groups[%d] 缺少名称或类型", index)
			}
			key := strings.ToLower(name)
			if previous, duplicate := groupNames[key]; duplicate {
				return fmt.Errorf("代理组名称重复: %s（与 %s 冲突）", name, previous)
			}
			if proxyName, collision := proxyNames[key]; collision {
				return fmt.Errorf("代理组名称 %s 与代理 %s 冲突", name, proxyName)
			}
			groupNames[key] = name
			groups = append(groups, group)
		}
	}
	allowed := map[string]bool{"direct": true, "reject": true, "reject-drop": true, "pass": true, "compatible": true}
	for key := range proxyNames {
		allowed[key] = true
	}
	for key := range groupNames {
		allowed[key] = true
	}
	graph := map[string][]string{}
	for _, group := range groups {
		groupName := strings.TrimSpace(stringVal(group["name"]))
		groupKey := strings.ToLower(groupName)
		rawRefs, exists := group["proxies"]
		if !exists {
			continue
		}
		refs, ok := rawRefs.([]interface{})
		if !ok {
			return fmt.Errorf("代理组 %s 的 proxies 必须是列表", groupName)
		}
		seenRefs := map[string]bool{}
		for _, rawRef := range refs {
			ref := strings.TrimSpace(stringVal(rawRef))
			key := strings.ToLower(ref)
			if ref == "" || !allowed[key] {
				return fmt.Errorf("代理组 %s 引用了不存在的代理或代理组 %q", groupName, ref)
			}
			if seenRefs[key] {
				return fmt.Errorf("代理组 %s 重复引用 %s", groupName, ref)
			}
			seenRefs[key] = true
			if _, isGroup := groupNames[key]; isGroup {
				graph[groupKey] = append(graph[groupKey], key)
			}
		}
	}
	if err := validateProxyGroupGraph(graph); err != nil {
		return err
	}
	return nil
}

func validateYAMLMappingKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := strings.TrimSpace(node.Content[index].Value)
			if seen[key] {
				return fmt.Errorf("YAML 字段重复: %s", key)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLMappingKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func validateMihomoProxy(proxy map[string]interface{}) error {
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port < 1 || port > 65535 {
		return errors.New("缺少有效的 type、server 或 port")
	}
	switch typ {
	case "vless", "vmess":
		if strings.TrimSpace(stringVal(proxy["uuid"])) == "" {
			return errors.New("缺少 uuid")
		}
	case "trojan", "hysteria", "hysteria2", "tuic", "anytls":
		if strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return errors.New("缺少 password")
		}
	case "ss":
		if strings.TrimSpace(firstNonEmpty(stringVal(proxy["cipher"]), stringVal(proxy["method"]))) == "" || strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return errors.New("缺少加密方式或 password")
		}
	case "http", "socks", "socks5", "wireguard", "snell", "mieru":
	default:
		return fmt.Errorf("不支持的代理类型 %q", typ)
	}
	return nil
}

func validateProxyGroupGraph(graph map[string][]string) error {
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("代理组存在循环引用: %s", name)
		case 2:
			return nil
		}
		state[name] = 1
		for _, next := range graph[name] {
			if err := visit(next); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
