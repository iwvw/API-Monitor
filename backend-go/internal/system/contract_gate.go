package system

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ValidateAICallBody 在写操作落库前按路由契约校验请求体参数（ACI 防错参数设计）。
// 仅对登记了请求契约（routeRequestContracts）的 POST/PUT/PATCH 路由启用；
// 无契约路由跳过校验，避免误拦历史接口。返回的错误包含具体字段名与期望值，
// AI 能据此修正参数而非猜。
func (s *Service) ValidateAICallBody(method, path string, body json.RawMessage) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return nil
	}
	if len(body) == 0 {
		return nil
	}
	items := s.apiDocs()["routes"].([]apiDocRoute)
	best := (*apiDocRoute)(nil)
	for i := range items {
		item := &items[i]
		if !routePrefixMatches(item.Prefix, item.MatchMode, path) {
			continue
		}
		if best == nil || len(item.Prefix) > len(best.Prefix) {
			best = item
		}
	}
	if best == nil || best.RequestSchema == nil {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("请求体必须是合法 JSON 对象: %v", err)
	}
	issues := validateBodyAgainstSchema(best.RequestSchema, payload)
	if len(issues) > 0 {
		return fmt.Errorf("请求体参数校验失败: %s", strings.Join(issues, "；"))
	}
	return nil
}

// validateBodyAgainstSchema 按 JSON Schema（properties/required/enum/type）逐字段校验请求体。
// required/enum 在契约中可能是 []string（obj/反射构造）也可能是 []interface{}（JSON 反序列化），两者兼容。
func validateBodyAgainstSchema(schema map[string]interface{}, payload map[string]interface{}) []string {
	issues := make([]string, 0, 4)
	props, _ := schema["properties"].(map[string]interface{})

	required := map[string]bool{}
	for _, item := range stringOrInterfaceList(schema["required"]) {
		required[item] = true
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value, exists := payload[name]
		if !exists {
			if required[name] {
				issues = append(issues, fmt.Sprintf("缺少必填字段 %q", name))
			}
			// 字段可选且未提供时也允许枚举字段缺省，避免误拦非必填项
			continue
		}
		prop, _ := props[name].(map[string]interface{})
		if prop == nil {
			continue
		}
		if msg := checkFieldType(name, prop, value); msg != "" {
			issues = append(issues, msg)
			continue
		}
		if msg := checkFieldEnum(name, prop, value); msg != "" {
			issues = append(issues, msg)
		}
	}
	return issues
}

// checkFieldType 校验字段类型（type 字段）。
func checkFieldType(name string, prop map[string]interface{}, value interface{}) string {
	t, _ := prop["type"].(string)
	switch t {
	case "":
		return ""
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Sprintf("字段 %q 应为字符串", name)
		}
	case "integer":
		switch n := value.(type) {
		case float64:
			if n != float64(int64(n)) {
				return fmt.Sprintf("字段 %q 应为整数", name)
			}
		case json.Number:
			if _, err := n.Int64(); err != nil {
				return fmt.Sprintf("字段 %q 应为整数", name)
			}
		default:
			return fmt.Sprintf("字段 %q 应为整数", name)
		}
	case "number":
		switch value.(type) {
		case float64, json.Number:
		default:
			return fmt.Sprintf("字段 %q 应为数字", name)
		}
	case "boolean":
		switch v := value.(type) {
		case bool:
		case float64:
			// 兼容 0/1 整数（部分契约如 enabled 声明 boolean 但服务端接受 0/1）
			if v != 0 && v != 1 {
				return fmt.Sprintf("字段 %q 应为布尔值", name)
			}
		case json.Number:
			n, err := v.Int64()
			if err != nil || (n != 0 && n != 1) {
				return fmt.Sprintf("字段 %q 应为布尔值", name)
			}
		default:
			return fmt.Sprintf("字段 %q 应为布尔值", name)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Sprintf("字段 %q 应为数组", name)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Sprintf("字段 %q 应为对象", name)
		}
	}
	return ""
}

// checkFieldEnum 校验枚举取值（enum 字段）。
func checkFieldEnum(name string, prop map[string]interface{}, value interface{}) string {
	rawEnum := stringOrInterfaceList(prop["enum"])
	if len(rawEnum) == 0 {
		return ""
	}
	for _, item := range rawEnum {
		if item == fmt.Sprint(value) {
			return ""
		}
	}
	return fmt.Sprintf("字段 %q 取值非法，可选值: %s", name, strings.Join(rawEnum, " | "))
}

// stringOrInterfaceList 兼容 []string 与 []interface{} 两种契约字段形态，
// 统一返回字符串切片；其余形态返回 nil。
func stringOrInterfaceList(v interface{}) []string {
	switch raw := v.(type) {
	case []string:
		return raw
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}