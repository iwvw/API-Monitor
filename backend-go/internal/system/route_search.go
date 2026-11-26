package system

import (
	"sort"
	"strings"
)

// schemaSearchWords 汇总请求契约里可检索的词:字段名 + 其 description(可含中文别名)。
// 让 find_api / list_apis 能按「请求体字段语义」召回接口(如“备注”对应 description),
// 而不仅仅依赖路径与中文描述文本。
func schemaSearchWords(schema map[string]interface{}) string {
	if schema == nil {
		return ""
	}
	props, _ := schema["properties"].(map[string]interface{})
	if len(props) == 0 {
		return ""
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(" ")
		b.WriteString(name)
		if prop, ok := props[name].(map[string]interface{}); ok {
			if d, ok := prop["description"].(string); ok && d != "" {
				b.WriteString(" ")
				b.WriteString(d)
			}
		}
	}
	return b.String()
}
