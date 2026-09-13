package uptime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func jsonPathValue(source interface{}, path string) interface{} {
	expr := strings.TrimSpace(path)
	if expr == "" || expr == "$" {
		return source
	}
	expr = strings.TrimPrefix(expr, "$.")
	expr = strings.TrimPrefix(expr, "$")
	expr = strings.ReplaceAll(expr, "[", ".")
	expr = strings.ReplaceAll(expr, "]", "")
	current := source
	for _, part := range strings.Split(expr, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			current = typed[part]
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func compareValues(actual, expected interface{}, operator string) bool {
	switch operator {
	case "exists":
		return actual != nil
	case "not_exists", "notExists":
		return actual == nil
	case "not_equals", "notEquals", "ne":
		return fmt.Sprint(coerce(actual)) != fmt.Sprint(coerce(expected))
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "not_contains", "notContains":
		return !strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "gt", "greater_than", "greaterThan":
		return floatValue(actual) > floatValue(expected)
	case "gte", "greater_or_equal", "greaterOrEqual":
		return floatValue(actual) >= floatValue(expected)
	case "lt", "less_than", "lessThan":
		return floatValue(actual) < floatValue(expected)
	case "lte", "less_or_equal", "lessOrEqual":
		return floatValue(actual) <= floatValue(expected)
	case "equals", "eq", "":
		return fmt.Sprint(coerce(actual)) == fmt.Sprint(coerce(expected))
	default:
		return fmt.Sprint(coerce(actual)) == fmt.Sprint(coerce(expected))
	}
}

func coerce(value interface{}) interface{} {
	text := strings.TrimSpace(fmt.Sprint(value))
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}
	if text == "true" {
		return true
	}
	if text == "false" {
		return false
	}
	if text == "null" {
		return nil
	}
	return text
}

func parseJSONMap(value interface{}) map[string]interface{} {
	return objectValue(parseJSONAny(value))
}

func parseJSONArray(value interface{}) []interface{} {
	parsed := parseJSONAny(value)
	switch typed := parsed.(type) {
	case []interface{}:
		return typed
	case nil:
		return []interface{}{}
	default:
		return []interface{}{typed}
	}
}

func parseJSONAny(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]interface{}, []interface{}:
		return typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return parsed
		}
	}
	return nil
}

func jsonOrNull(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	if mapValue, ok := value.(map[string]interface{}); ok && len(mapValue) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(data)
}

func jsonOrDefault(value interface{}, fallback string) string {
	data, err := json.Marshal(value)
	if err != nil || value == nil {
		return fallback
	}
	return string(data)
}

func structToMap(value interface{}) map[string]interface{} {
	data, _ := json.Marshal(value)
	out := map[string]interface{}{}
	_ = json.Unmarshal(data, &out)
	return out
}
