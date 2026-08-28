package transform

import (
	"regexp"
	"strings"
)

// camelRe 在小写字母/数字与紧随的大写字母之间插入下划线，正则 ([a-z0-9])([A-Z])。
var camelRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// SnakeToCamel 将 snake_case 转为 camelCase。
//
// 无下划线则原样返回（已是 camelCase 的键经此函数不变，这点对 generationConfig
// 的键转换很重要：temperature/topP/topK 等保持不动）。
func SnakeToCamel(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		b.WriteString(pyTitle(p))
	}
	return b.String()
}

// CamelToSnake 将 camelCase 转为 snake_case。
func CamelToSnake(s string) string {
	return strings.ToLower(camelRe.ReplaceAllString(s, "${1}_${2}"))
}

// pyTitle 把单个词归一为首字母大写、其余小写。
// （Go 的 strings.Title 不会把其余字母转小写，故自实现。）
func pyTitle(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
