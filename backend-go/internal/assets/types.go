package assets

import (
	"strings"
	"time"
)

const (
	categoryPhysical = "physical"
	categoryVirtual  = "virtual"
)

// 类型学：实体类与虚拟类。面板管得到的对象全部落虚拟类，
// 实体类基本来自手工登记。
var typesByCategory = map[string][]string{
	categoryPhysical: {"server", "network", "storage", "terminal", "other_hw"},
	categoryVirtual:  {"cloud_instance", "domain", "ssl_cert", "subscription", "license", "api_key", "proxy_node", "saas"},
}

var validStatuses = map[string]bool{
	statusActive:  true,
	statusRetired: true,
	statusOrphan:  true,
	statusUnknown: true,
}

func validCategory(value string) bool {
	_, ok := typesByCategory[value]
	return ok
}

func validTypeForCategory(category, assetType string) bool {
	types, ok := typesByCategory[category]
	if !ok {
		return false
	}
	for _, item := range types {
		if item == assetType {
			return true
		}
	}
	return false
}

// normalizeAssetStatus 只允许用户显式设置的持久状态；
// active/expiring/expired 由到期派生，不在此接受。
func normalizeAssetStatus(value string) string {
	status := strings.ToLower(strings.TrimSpace(value))
	if status == "" {
		return statusActive
	}
	if validStatuses[status] {
		return status
	}
	return statusActive
}

// normalizeExpireAt 归一化到期时刻为 UTC RFC3339。
// 接受 RFC3339、RFC3339 带毫秒、纯日期（按 UTC 当日零点）。
func normalizeExpireAt(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return text
}

func objectFromPayload(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	if text, ok := value.(string); ok {
		return parseObject(text)
	}
	return map[string]interface{}{}
}

// nowUTC 返回当前 UTC 时刻，供到期派生使用。
func nowUTC() time.Time {
	return time.Now().UTC()
}
