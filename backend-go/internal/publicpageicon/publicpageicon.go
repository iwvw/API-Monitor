// Package publicpageicon 提供公开页 favicon 解析所需的共享逻辑：
// 从页面 config_json 提取自定义图标 ID，以及各类公开页的默认图标 SVG。
// 字段名与前端 src/js/modules/publicPageBranding.js 保持一致。
package publicpageicon

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
)

// ConfigKey 是公开页配置中图标 ID 的字段名（前端 PUBLIC_PAGE_ICON_CONFIG_KEY）。
const ConfigKey = "publicIconId"

// iconIDPattern 限制图标 ID 只能为安全 URL 路径字符，
// 防止从 config_json 读取到的值破坏 favicon 302 跳转路径。
var iconIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// 公开页类型，用于选择默认图标。
const (
	KindUptime    = "uptime"
	KindServer    = "server"
	KindGitHub    = "github"
	KindBookmarks = "bookmarks"
)

// DefaultIconColor 与前端 DEFAULT_ICON_COLOR 保持一致。
const DefaultIconColor = "#f48120"

const svgHeader = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="64" height="64" fill="` + DefaultIconColor + `" stroke="` + DefaultIconColor + `" stroke-width="16" stroke-linecap="round" stroke-linejoin="round">`

// 以下 path 数据取自 @phosphor-icons/react 的 regular 权重
// （Pulse=Activity、HardDrives=Server），GitHub 字形来自前端 publicPageBranding.js。
const pulsePath = `<path d="M240,128a8,8,0,0,1-8,8H204.94l-37.78,75.58A8,8,0,0,1,160,216h-.4a8,8,0,0,1-7.08-5.14L95.35,60.76,63.28,131.31A8,8,0,0,1,56,136H24a8,8,0,0,1,0-16H50.85L88.72,36.69a8,8,0,0,1,14.76.46l57.51,151,31.85-63.71A8,8,0,0,1,200,120h32A8,8,0,0,1,240,128Z"/>`

const hardDrivesPath = `<path d="M208,136H48a16,16,0,0,0-16,16v48a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V152A16,16,0,0,0,208,136Zm0,64H48V152H208v48Zm0-160H48A16,16,0,0,0,32,56v48a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V56A16,16,0,0,0,208,40Zm0,64H48V56H208v48ZM192,80a12,12,0,1,1-12-12A12,12,0,0,1,192,80Zm0,96a12,12,0,1,1-12-12A12,12,0,0,1,192,176Z"/>`

// bookmarkSimplePath 取自 @phosphor-icons/react regular 权重（BookmarkSimple）。
const bookmarkSimplePath = `<path d="M184,28H72A20,20,0,0,0,52,48V224a12,12,0,0,0,18.36,10.18l57.63-36,57.65,36A12,12,0,0,0,204,224V48A20,20,0,0,0,184,28Zm-4,174.35-45.65-28.53a12,12,0,0,0-12.72,0L76,202.35V52H180Z"/>`

const ( // GitHub 图标 viewBox 与前端一致。
	githubViewBox   = "0 0 1230 1200"
	githubSvgHeader = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="` + githubViewBox + `" width="64" height="64" fill="` + DefaultIconColor + `">`
	githubPathData  = "M615 0Q490 0 376 48Q265 95 180 180Q95 265 48 376Q0 490 0 615Q0 749 55 869Q108 986 203.5 1072Q299 1158 421 1199Q442 1203 453 1192Q463 1184 463 1169L462 1065Q414 1075 375 1070Q341 1066 315 1052Q294 1040 278 1022Q267 1009 260 994L255 982Q245 958 232 938Q222 923 210 911Q201 902 193 896L186 892Q167 879 163.5 871Q160 863 168 859Q173 856 182 855H191Q229 858 261 888Q277 903 285 918Q321 980 382 985Q421 989 464 969Q468 941 479 919Q489 900 503 887Q423 878 370 854Q301 823 265 763Q223 694 223 583Q223 487 286 418Q276 395 274 363Q270 310 291 255L301 254Q315 253 332 257Q356 262 385 275Q420 291 461 318Q534 298 614.5 298Q695 298 768 318Q789 304 808 293Q827 282 843 275Q859 268 872 263.5Q885 259 895.5 257Q906 255 913.5 254.5Q921 254 927 254L934 255Q937 255 937 255Q958 310 954 363Q952 394 943 418Q1006 487 1006 583Q1006 694 964 763Q927 823 859 854Q805 878 725 887Q743 902 754 929Q767 960 767 1000L766 1169Q766 1183 775 1192Q787 1202 808 1198Q931 1158 1027 1072Q1123 986 1176 869Q1230 749 1230 615Q1230 490 1182 376Q1135 265 1050 180Q965 95 855 48Q740 0 615 0Z"
)

// IconIDFromConfigJSON 从公开页 config_json 中提取自定义图标 ID；
// 未设置或格式无效时返回空字符串。
func IconIDFromConfigJSON(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}
	value, ok := config[ConfigKey]
	if !ok {
		return ""
	}
	id, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

// ValidIconID 报告图标 ID 是否为可安全拼入 URL 路径的格式
// （字母/数字/下划线/连字符，长度 1-64）。当前图标 ID 由
// settings 包生成为 "site-" + 12 位小写十六进制，此处保留余量。
func ValidIconID(id string) bool {
	return iconIDPattern.MatchString(id)
}

// LookupIconID 从公开页表查询自定义图标 ID；未找到时返回 ("", false, nil)。
// table 必须为包内各服务持有的内部常量表名（不允许用户输入）；
// byDomain 时按小写域名匹配，lookup 需已由各服务规范化（含端口剥离）。
func LookupIconID(ctx context.Context, db *sql.DB, table, lookup string, byDomain bool) (string, bool, error) {
	query := `SELECT COALESCE(config_json, '{}') FROM ` + table + ` WHERE public = 1 AND slug = ?`
	if byDomain {
		query = `SELECT COALESCE(config_json, '{}') FROM ` + table + ` WHERE public = 1 AND lower(domain) = lower(?)`
	}
	var raw string
	err := db.QueryRowContext(ctx, query, lookup).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return IconIDFromConfigJSON([]byte(raw)), true, nil
}

// DefaultGlyphSVG 返回指定公开页类型的默认 favicon SVG；未知类型回落为 uptime。
func DefaultGlyphSVG(kind string) string {
	switch kind {
	case KindServer:
		return svgHeader + hardDrivesPath + `</svg>`
	case KindGitHub:
		return githubSvgHeader + `<path d="` + githubPathData + `"/></svg>`
	case KindBookmarks:
		return svgHeader + bookmarkSimplePath + `</svg>`
	default:
		return svgHeader + pulsePath + `</svg>`
	}
}
