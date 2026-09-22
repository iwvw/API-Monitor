package notification

import (
	"fmt"
	"strings"
	"time"

)

func generateFingerprint(rule Rule, data map[string]interface{}) string {
	parts := []string{rule.SourceModule, rule.EventType}
	switch {
	case stringValue(data["assetId"]) != "":
		parts = append(parts, "asset:"+stringValue(data["assetId"]))
	case stringValue(data["monitorId"]) != "":
		parts = append(parts, "monitor:"+stringValue(data["monitorId"]))
	case stringValue(data["serverId"]) != "":
		parts = append(parts, "server:"+stringValue(data["serverId"]))
	case stringValue(data["accountId"]) != "":
		parts = append(parts, "account:"+stringValue(data["accountId"]))
	default:
		parts = append(parts, "global")
	}
	return strings.Join(parts, ":")
}

func evaluateConditions(conditions map[string]interface{}, data map[string]interface{}) conditionResult {
	mode := "all"
	items := []map[string]interface{}{}
	if value := stringValue(conditions["mode"]); value == "any" || value == "or" {
		mode = "any"
	}
	if rawItems, ok := conditions["items"].([]interface{}); ok {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if rawItems, ok := conditions["rules"].([]interface{}); ok && len(items) == 0 {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if rawItems, ok := conditions["conditions"].([]interface{}); ok && len(items) == 0 {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if len(items) == 0 {
		for key, value := range conditions {
			if key == "mode" || key == "match" || key == "operator" || key == "items" || key == "rules" || key == "conditions" {
				continue
			}
			items = append(items, map[string]interface{}{"field": key, "operator": "equals", "value": value})
		}
	}
	results := []map[string]interface{}{}
	if len(items) == 0 {
		return conditionResult{Allowed: true, Mode: mode, Results: results}
	}
	allowed := mode != "any"
	for _, item := range items {
		field := firstNonEmpty(stringValue(item["field"]), stringValue(item["key"]), stringValue(item["path"]), stringValue(item["name"]))
		operator := stringDefault(item["operator"], "equals")
		expected := item["value"]
		if expected == nil {
			expected = item["expected"]
		}
		actual := pathValue(data, field)
		passed := compare(actual, expected, operator)
		results = append(results, map[string]interface{}{"field": field, "operator": operator, "expected": expected, "actual": actual, "passed": passed})
		if mode == "any" {
			allowed = allowed || passed
		} else {
			allowed = allowed && passed
		}
	}
	return conditionResult{Allowed: allowed, Mode: mode, Results: results}
}

func compare(actual, expected interface{}, operator string) bool {
	switch operator {
	case "exists":
		return actual != nil
	case "not_exists", "notExists":
		return actual == nil
	case "not_equals", "notEquals", "ne":
		return fmt.Sprint(actual) != fmt.Sprint(expected)
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "not_contains", "notContains":
		return !strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "gt", "greater_than", "greaterThan":
		return number(actual) > number(expected)
	case "gte", "greater_or_equal", "greaterOrEqual":
		return number(actual) >= number(expected)
	case "lt", "less_than", "lessThan":
		return number(actual) < number(expected)
	case "lte", "less_or_equal", "lessOrEqual":
		return number(actual) <= number(expected)
	default:
		return fmt.Sprint(actual) == fmt.Sprint(expected)
	}
}

func checkTimeWindow(window map[string]interface{}, loc *time.Location) bool {
	if !boolValue(window["enabled"], false) {
		return true
	}
	if loc == nil {
		loc = time.Local
	}
	start := stringDefault(window["start"], "00:00")
	end := stringDefault(window["end"], "23:59")
	startMinutes, okStart := parseClock(start)
	endMinutes, okEnd := parseClock(end)
	if !okStart || !okEnd {
		return true
	}
	now := time.Now().In(loc)
	current := now.Hour()*60 + now.Minute()
	if endMinutes < startMinutes {
		return current >= startMinutes || current <= endMinutes
	}
	return current >= startMinutes && current <= endMinutes
}

func pathValue(data map[string]interface{}, path string) interface{} {
	clean := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$."), "$")
	if clean == "" {
		return data
	}
	var current interface{} = data
	for _, part := range strings.Split(clean, ".") {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
		if current == nil {
			return nil
		}
	}
	return current
}