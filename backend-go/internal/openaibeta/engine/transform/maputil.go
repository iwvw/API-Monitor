package transform

import (
	"fmt"
	"strings"
)

// 本文件是 transform 包内处理 map[string]any / any 动态结构的小工具，
// 用来贴近动态 dict 的语义（truthy 判断、浅拷贝、字符串化等）。

// copyMap 浅拷贝一个 map[string]any。
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// toString 把任意值转成字符串：字符串原样，其它用 fmt.Sprint。
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// isTruthy 做动态 truthy 判定：nil/""/false/0/空容器为假，其余为真。
func isTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

// asMapSlice 把 any 规整成 []map[string]any（非 map 元素丢弃）。
func asMapSlice(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// trimLowerSuffix 是给响应/猜测 mime 用的小工具，去掉 query/fragment 后转小写。
func trimLowerSuffix(s string) string {
	if parts := strings.SplitN(s, "?", 2); len(parts) > 0 {
		s = parts[0]
	}
	if parts := strings.SplitN(s, "#", 2); len(parts) > 0 {
		s = parts[0]
	}
	return strings.ToLower(s)
}
