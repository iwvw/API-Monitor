package serveragent

import (
	"fmt"
)

func asMapList(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func asInterfaceList(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return []interface{}{}
	}
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringFromAny(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		if len(v) == 0 {
			return ""
		}
		return stringFromAny(v[0])
	case []string:
		if len(v) == 0 {
			return ""
		}
		return v[0]
	case fmt.Stringer:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}
