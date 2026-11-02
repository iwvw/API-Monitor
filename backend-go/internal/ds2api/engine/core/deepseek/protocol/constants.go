package protocol

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	// 内嵌 IANA 时区库：运行镜像（debian slim / scratch）不保证自带 tzdata，
	// 缺失时 time.LoadLocation 会失败并让所有账号退回东八区。
	_ "time/tzdata"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/transport"
)

const (
	DeepSeekHost                    = "chat.deepseek.com"
	DeepSeekLoginURL                = "https://chat.deepseek.com/api/v0/users/login"
	DeepSeekCreateSessionURL        = "https://chat.deepseek.com/api/v0/chat_session/create"
	DeepSeekCreatePowURL            = "https://chat.deepseek.com/api/v0/chat/create_pow_challenge"
	DeepSeekCompletionURL           = "https://chat.deepseek.com/api/v0/chat/completion"
	DeepSeekContinueURL             = "https://chat.deepseek.com/api/v0/chat/continue"
	DeepSeekStopStreamURL           = "https://chat.deepseek.com/api/v0/chat/stop_stream"
	DeepSeekUploadFileURL           = "https://chat.deepseek.com/api/v0/file/upload_file"
	DeepSeekFetchFilesURL           = "https://chat.deepseek.com/api/v0/file/fetch_files"
	DeepSeekFetchSessionURL         = "https://chat.deepseek.com/api/v0/chat_session/fetch_page"
	DeepSeekDeleteSessionURL        = "https://chat.deepseek.com/api/v0/chat_session/delete"
	DeepSeekDeleteAllSessionsURL    = "https://chat.deepseek.com/api/v0/chat_session/delete_all"
	DeepSeekUpdateSettingsURL       = "https://chat.deepseek.com/api/v0/users/update_settings"
	DeepSeekClientSettingsURL       = "https://chat.deepseek.com/api/v0/client/settings"
	DeepSeekClientSettingsReportURL = "https://chat.deepseek.com/api/v0/client/settings/report"
	DeepSeekCompletionTargetPath    = "/api/v0/chat/completion"
	DeepSeekUploadTargetPath        = "/api/v0/file/upload_file"
)

// chromeMajorVersion 直接取自 transport 层，与 uTLS ClientHello 同源，
// 避免两边各写一份常量后随依赖升级悄悄错开
// （曾出现 TLS 指纹是 Chrome 133、User-Agent 却自称 128 的矛盾）。
const chromeMajorVersion = transport.ChromeMajorVersion

var chromeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + chromeMajorVersion + ".0.0.0 Safari/537.36"

// chromeSecChUA 的 GREASE 品牌串和品牌顺序都随 Chrome 版本变化，
// 这里的形式取自 chat.deepseek.com 网页版真实抓包（Chrome 150）。
// 换版本时必须重新抓包核对，不能照着旧版本推。
var chromeSecChUA = "\"Not;A=Brand\";v=\"8\", \"Chromium\";v=\"" + chromeMajorVersion + "\", \"Google Chrome\";v=\"" + chromeMajorVersion + "\""

var defaultStaticBaseHeaders = map[string]string{
	"Host":         "chat.deepseek.com",
	"Accept":       "application/json",
	"Content-Type": "application/json",
	// 真实网页版确实会发这个头。它一度被当作 App 专属头移除，属于误判——
	// 抓包显示 platform=web 的浏览器请求同样携带。
	"x-client-bundle-id": "com.deepseek.chat",
}

// 关于 x-hif-dliq / x-hif-leim：不要添加这两个头。
//
// 真实网页版在「正常窗口」下会额外发送这两个 base64 值。经抓包验证：
//   - 同一账号每次请求都相同，切模型、传文件、登出重登都不变；
//   - 无痕窗口下二者完全消失；
//   - login、chat_session/delete 等接口本来就不带。
//
// 无痕下消失说明它读的是持久化存储而非硬件环境（否则同机同浏览器应算出同值），
// 因此「不带」是一个真实且可复现的浏览器状态，本项目的请求就等价于无痕会话。
//
// 更重要的是：它是设备级标识。抓一份填进配置会让所有账号共享同一个设备指纹，
// 「N 个账号同一台设备」是比缺失强得多的关联信号。除非能为每个账号取得
// 各自独立的合法值，否则伪造只会让情况变糟。
//
// webBrowserHeaders 是 platform=web 时 DeepSeek 网页版浏览器应有的头。
var webBrowserHeaders = map[string]string{
	"User-Agent":         chromeUserAgent,
	"sec-ch-ua":          chromeSecChUA,
	"sec-ch-ua-mobile":   "?0",
	"sec-ch-ua-platform": "\"Windows\"",
	"Origin":             "https://chat.deepseek.com",
	"Referer":            "https://chat.deepseek.com/",
	"sec-fetch-site":     "same-origin",
	"sec-fetch-mode":     "cors",
	"sec-fetch-dest":     "empty",
	// 浏览器 fetch 发的是 */*，不是 application/json。
	// 只覆盖 web 平台：登录接口沿用 App 风格头（见 LoginHeaders）。
	"Accept": "*/*",
	// 必须显式声明：否则 transport 会自动补一个只含 gzip 的 accept-encoding，
	// 而自称 Chrome 却只接受 gzip 是明显异常。响应解压见 client.decompressBody。
	"Accept-Encoding": "gzip, deflate, br, zstd",
	// Chrome 12x+ 在 fetch/XHR 上会带 priority。
	"priority": "u=1, i",
}

// localeTimezones 把 locale 映射到 IANA 时区。偏移在请求时从时区数据实时计算，
// 而不是写死常量：真实浏览器报告的是「当前」偏移，含夏令时。写死的话，
// 带夏令时的地区一年里有半年是错的。
//
// 单位是秒。此前 en_US 被写成分钟制的 -420，等于宣称自己在 UTC-00:07 ——
// 这个时区并不存在，比硬编码东八区更容易被识别。
var localeTimezones = map[string]string{
	"zh_CN": "Asia/Shanghai",
	"zh_TW": "Asia/Taipei",
	"en_US": "America/Los_Angeles",
	"en_GB": "Europe/London",
	"ja_JP": "Asia/Tokyo",
	"ko_KR": "Asia/Seoul",
	"de_DE": "Europe/Berlin",
	"fr_FR": "Europe/Paris",
	"ru_RU": "Europe/Moscow",
	"es_ES": "Europe/Madrid",
}

const defaultTimezoneOffset = "28800" // UTC+8，未知 locale 的回退值

// localeAcceptLanguages 按 locale 给出 Accept-Language。
//
// 这里用的是「只配了母语」的 Chrome 默认形态（如 zh-CN,zh;q=0.9）。
// 曾一度改成带英语兜底链的长形式 "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7"，
// 那是抓包自一个额外添加了英语的 Chrome 配置文件；对照组（无痕、默认配置）
// 发的是短形式。这个头取决于用户的语言设置，不是浏览器版本特征，
// 短形式对应默认安装，是更保守的选择。
var localeAcceptLanguages = map[string]string{
	"zh_CN": "zh-CN,zh;q=0.9",
	"zh_TW": "zh-TW,zh;q=0.9",
	"en_US": "en-US,en;q=0.9",
	"en_GB": "en-GB,en;q=0.9",
	"ja_JP": "ja-JP,ja;q=0.9",
	"ko_KR": "ko-KR,ko;q=0.9",
	"de_DE": "de-DE,de;q=0.9",
	"fr_FR": "fr-FR,fr;q=0.9",
	"ru_RU": "ru-RU,ru;q=0.9",
	"es_ES": "es-ES,es;q=0.9",
}

var defaultSkipContainsPatterns = []string{
	"quasi_status",
	"elapsed_secs",
	"token_usage",
	"pending_fragment",
	"conversation_mode",
	"fragments/-1/status",
	"fragments/-2/status",
	"fragments/-3/status",
}

var defaultSkipExactPaths = []string{
	"response/search_status",
}

var ClientVersion string
var BaseHeaders = map[string]string{}
var SkipContainsPatterns = cloneStringSlice(defaultSkipContainsPatterns)
var SkipExactPathSet = toStringSet(defaultSkipExactPaths)

type clientConstants struct {
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	Version         string `json:"version"`
	AndroidAPILevel string `json:"android_api_level"`
	Locale          string `json:"locale"`
}

type sharedConstants struct {
	Client              clientConstants   `json:"client"`
	BaseHeaders         map[string]string `json:"base_headers"`
	SkipContainsPattern []string          `json:"skip_contains_patterns"`
	SkipExactPaths      []string          `json:"skip_exact_paths"`
}

//go:embed constants_shared.json
var sharedConstantsJSON []byte

func init() {
	cfg := sharedConstants{}
	if err := json.Unmarshal(sharedConstantsJSON, &cfg); err != nil {
		panic(fmt.Errorf("load DeepSeek shared constants: %w", err))
	}
	sharedClient = normalizeClientConstants(cfg.Client)
	sharedBaseHeaderOverrides = cfg.BaseHeaders
	applySharedConstants(cfg)
}

func applySharedConstants(cfg sharedConstants) {
	client := normalizeClientConstants(cfg.Client)
	ClientVersion = client.Version
	BaseHeaders = BuildBaseHeaders(client, cfg.BaseHeaders)
	SkipContainsPatterns = cloneStringSlice(defaultSkipContainsPatterns)
	if len(cfg.SkipContainsPattern) > 0 {
		SkipContainsPatterns = cloneStringSlice(cfg.SkipContainsPattern)
	}
	SkipExactPathSet = toStringSet(defaultSkipExactPaths)
	if len(cfg.SkipExactPaths) > 0 {
		SkipExactPathSet = toStringSet(cfg.SkipExactPaths)
	}
}

func normalizeClientConstants(in clientConstants) clientConstants {
	if in.Name == "" {
		in.Name = "DeepSeek"
	}
	if in.Platform == "" {
		in.Platform = "web"
	}
	if in.Locale == "" {
		in.Locale = "zh_CN"
	}
	return in
}

// BuildBaseHeaders 根据 client 配置和 locale 构建请求头。
// 当 platform=web 时会补齐浏览器头，使与 Chrome TLS 指纹一致；
// x-client-timezone-offset 按 locale 动态设置。
func BuildBaseHeaders(client clientConstants, overrides map[string]string) map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	for k, v := range overrides {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}

	locale := strings.TrimSpace(client.Locale)
	if locale == "" {
		locale = "zh_CN"
	}
	out["x-client-timezone-offset"] = TimezoneOffsetFor(locale)

	if IsWebPlatform(client.Platform) {
		for k, v := range webBrowserHeaders {
			out[k] = v
		}
		out["Accept-Language"] = AcceptLanguageFor(locale)
	} else if client.Name != "" && client.Version != "" {
		// App / 非 web 平台保持 App 风格 UA。
		out["User-Agent"] = client.Name + "/" + client.Version
	}

	if client.Platform != "" {
		out["x-client-platform"] = client.Platform
	}
	if client.Version != "" {
		out["x-client-version"] = client.Version
	}
	if client.Locale != "" {
		out["x-client-locale"] = client.Locale
	}
	return out
}

// BaseHeadersFor 使用共享 client 配置，并按 locale 覆盖时区和语言头。
func BaseHeadersFor(locale string) map[string]string {
	client := normalizeClientConstants(sharedClient)
	client.Locale = strings.TrimSpace(locale)
	if client.Locale == "" {
		client.Locale = "zh_CN"
	}
	return BuildBaseHeaders(client, sharedBaseHeaderOverrides)
}

// LoginHeaders 返回登录/刷新 token 使用的保守头。
// 登录接口对浏览器头较敏感，使用不带 Chrome UA 和 sec-* 的原始 App 风格头可避免异常响应。
func LoginHeaders(locale string) map[string]string {
	client := normalizeClientConstants(sharedClient)
	client.Locale = strings.TrimSpace(locale)
	if client.Locale == "" {
		client.Locale = "zh_CN"
	}
	out := cloneStringMap(defaultStaticBaseHeaders)
	for k, v := range sharedBaseHeaderOverrides {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	out["x-client-timezone-offset"] = TimezoneOffsetFor(client.Locale)
	if client.Name != "" && client.Version != "" {
		out["User-Agent"] = client.Name + "/" + client.Version
	}
	if client.Platform != "" {
		out["x-client-platform"] = client.Platform
	}
	if client.Version != "" {
		out["x-client-version"] = client.Version
	}
	if client.Locale != "" {
		out["x-client-locale"] = client.Locale
	}
	return out
}

// sharedClient 和 sharedBaseHeaderOverrides 保存 init 时解析的原始值。
var sharedClient clientConstants
var sharedBaseHeaderOverrides map[string]string

// IsWebPlatform 判断 platform 是否代表网页版。
func IsWebPlatform(platform string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), "web")
}

// TimezoneOffsetFor 返回 locale 对应的当前时区偏移（秒，含夏令时），
// 未知 locale 或时区数据缺失时回退到东八区。
func TimezoneOffsetFor(locale string) string {
	zone, ok := localeTimezones[strings.TrimSpace(locale)]
	if !ok {
		return defaultTimezoneOffset
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return defaultTimezoneOffset
	}
	_, offset := time.Now().In(loc).Zone()
	return strconv.Itoa(offset)
}

// AcceptLanguageFor 返回 locale 对应的 Accept-Language，未知时回退到中文。
func AcceptLanguageFor(locale string) string {
	if lang, ok := localeAcceptLanguages[strings.TrimSpace(locale)]; ok {
		return lang
	}
	return localeAcceptLanguages["zh_CN"]
}

// ChatSessionReferer 返回某个会话页面的 URL。
// 真实浏览器在发送消息时，Referer 是当前会话页而不是站点根路径——
// 根路径只出现在还没有会话的新对话首帧。
func ChatSessionReferer(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "https://chat.deepseek.com/"
	}
	return "https://chat.deepseek.com/a/chat/s/" + sessionID
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func toStringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

const (
	KeepAliveTimeout  = 5
	StreamIdleTimeout = 300
	MaxKeepaliveCount = 40
)
