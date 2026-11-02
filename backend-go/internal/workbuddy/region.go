package workbuddy

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// WorkBuddy / CodeBuddy 分三个独立区域：各自的域名、账号凭据与额度（互不相通），
// 模型目录也各有一份。区域只在**账号层**表达——对外模型名不加前缀、网关路由不变，
// 插件内部按「该账号所属区域是否提供该模型」自动选号。
//
//   - cn（国内版 / CodeBuddy，copilot.tencent.com）：CLI 扫码登录，微信渠道；
//   - wb（办公版 / WorkBuddy 桌面端，www.workbuddy.cn）：桌面端登录页，含企业微信渠道；
//   - intl（国际版 / WorkBuddy，www.workbuddy.ai）：网页 OneID/SSO 登录。
const (
	// regionCN 是国内版（CodeBuddy / copilot.tencent.com）。
	regionCN = "cn"
	// regionWB 是办公版（WorkBuddy 桌面端 / www.workbuddy.cn），登录页含企业微信渠道。
	regionWB = "wb"
	// regionIntl 是国际版（WorkBuddy / www.workbuddy.ai）。
	regionIntl = "intl"
)

// regionHosts 是各区域的 API 基址（chat / 登录 / 目录都在该域下）。
var regionHosts = map[string]string{
	regionCN:   "https://copilot.tencent.com",
	regionWB:   "https://www.workbuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionReferers 是各区域的 Origin/Referer 基址（缺失会让上游对 /v3/config 返回 400）。
var regionReferers = map[string]string{
	regionCN:   "https://www.codebuddy.cn",
	regionWB:   "https://www.workbuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionUserAgents 是各区域对应的客户端标识（上游按 UA 识别终端类型）。
var regionUserAgents = map[string]string{
	regionCN: "CLI/2.63.2 CodeBuddy/2.63.2",
	regionWB: "WorkBuddy/5.8.2",
	// 国际版对齐 2026-10-16 对客户端 5.8.4 的 mitmproxy 抓包（UA 末段是内置 CLI 版本）。
	regionIntl: "WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0",
}

// regionDesktopVersions 是桌面端（WorkBuddy）身份头用的 X-IDE-Version / extVersion。
// 国内版（cn）走 CLI 身份，不使用这些头，故不在此表内。
// 办公版（wb）无对应抓包，沿用与其 UA 一致的 5.8.2；国际版按 5.8.4 抓包对齐。
var regionDesktopVersions = map[string]string{
	regionWB:   "5.8.2",
	regionIntl: "5.8.4",
}

// regionPlatforms 是各区域登录 auth/state 的 platform 查询参数。
// 办公版用 workbuddy，登录页据此展示企业微信渠道；国内版沿用 CLI，国际版走网页登录。
var regionPlatforms = map[string]string{
	regionCN:   "CLI",
	regionWB:   "workbuddy",
	regionIntl: "CLI",
}

// regionBillingHosts 是各区域「计费/余额」接口的基址。国内与 chat 域不同
// （chat=copilot.tencent.com，billing=codebuddy.cn）；办公版与国际版 chat、billing 同域。
var regionBillingHosts = map[string]string{
	regionCN:   "https://www.codebuddy.cn",
	regionWB:   "https://www.workbuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionWebHosts 是各区域「官网成长中心」的基址。国内版成长中心在 workbuddy.cn
// （与 billing 的 codebuddy.cn 不同域），办公版同域 workbuddy.cn，国际版 workbuddy.ai。
// 连登/兑换/抽奖/补签等 Web 端接口只在此域提供。
var regionWebHosts = map[string]string{
	regionCN:   "https://www.workbuddy.cn",
	regionWB:   "https://www.workbuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionLabels 是下发前端的中文区域名。
var regionLabels = map[string]string{
	regionCN:   "国内版",
	regionWB:   "办公版",
	regionIntl: "国际版",
}

// regionOrder 是区域在模型目录合并与前端展示时的固定顺序（第一个为合并打底区域）。
var regionOrder = []string{regionCN, regionWB, regionIntl}

// normalizeRegion 归一化区域标识：空值或未知值一律落到国内版（存量账号无该字段，
// 默认按国内版处理，行为与历史一致）。
func normalizeRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case regionIntl:
		return regionIntl
	case regionWB:
		return regionWB
	default:
		return regionCN
	}
}

// regionHost 返回区域 API 基址。
func regionHost(region string) string {
	return regionHosts[normalizeRegion(region)]
}

// regionReferer 返回区域 Origin/Referer 基址。
func regionReferer(region string) string {
	return regionReferers[normalizeRegion(region)]
}

// regionBillingHost 返回区域计费/余额接口的基址。
func regionBillingHost(region string) string {
	return regionBillingHosts[normalizeRegion(region)]
}

// regionUA 返回区域客户端标识。
func regionUA(region string) string {
	return regionUserAgents[normalizeRegion(region)]
}

// regionDesktopVersion 返回桌面端身份的 X-IDE-Version（无配置时回落到办公版默认值）。
func regionDesktopVersion(region string) string {
	if v := regionDesktopVersions[normalizeRegion(region)]; v != "" {
		return v
	}
	return regionDesktopVersions[regionWB]
}

// regionPlatform 返回区域登录 auth/state 的 platform 参数。
func regionPlatform(region string) string {
	return regionPlatforms[normalizeRegion(region)]
}

// regionLabel 返回区域中文名。
func regionLabel(region string) string {
	return regionLabels[normalizeRegion(region)]
}

// regionWebHost 返回区域官网成长中心的基址。
func regionWebHost(region string) string {
	return regionWebHosts[normalizeRegion(region)]
}

// regionOfDomain 按账号 domain 判定区域：含 workbuddy.ai 为国际版，含 workbuddy.cn 为
// 办公版，其余为国内版。
func regionOfDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	if strings.Contains(d, "workbuddy.ai") {
		return regionIntl
	}
	if strings.Contains(d, "workbuddy.cn") {
		return regionWB
	}
	return regionCN
}

// regionOfTokenIssuer 按 token 的 issuer 判定区域。国际版的 iss 形如
// https://www.workbuddy.ai/auth/realms/copilot；办公版含 workbuddy.cn；国内版不含以上域名。
func regionOfTokenIssuer(iss string) string {
	s := strings.ToLower(strings.TrimSpace(iss))
	if strings.Contains(s, "workbuddy.ai") {
		return regionIntl
	}
	if strings.Contains(s, "workbuddy.cn") {
		return regionWB
	}
	return regionCN
}

// jwtIssuer 从 JWT（access token）的载荷里取 iss 声明；解析失败返回空串。
// 不校验签名——这里只用于「凭据自证区域」的辅助判定，不做安全决策。
func jwtIssuer(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// 兼容带填充的 base64。
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Iss)
}

// detectTokenRegion 判定一个 access token 所属区域，并报告是否**确凿判定**。
//
// 判定信号（按可靠性排序）：
//  1. JWT 的 iss 声明 —— 国际版含 workbuddy.ai，国内版不含；
//  2. 调用方给出的 domain 兜底。
//
// 只要拿到了 JWT 的 iss，就认为判定确凿（含或不含 workbuddy.ai 都算）；
// 完全无从判定时返回 (regionCN, false)，调用方应放行而不是拒绝。
func detectTokenRegion(accessToken, domain string) (string, bool) {
	if iss := jwtIssuer(accessToken); iss != "" {
		return regionOfTokenIssuer(iss), true
	}
	if d := strings.TrimSpace(domain); d != "" {
		return regionOfDomain(d), true
	}
	return regionCN, false
}

// accountIDForRegion 构造账号的稳定标识。国内版沿用原始 uid（存量兼容）；
// 国际版加 "intl-"、办公版加 "wb-" 前缀，保证同一 uid 在不同区域是互不覆盖的记录
// （限流/用量/选号权重都按 ID 记账，因此天然按区域隔离）。
func accountIDForRegion(region, rawID string) string {
	rawID = strings.TrimSpace(rawID)
	switch normalizeRegion(region) {
	case regionIntl:
		return "intl-" + rawID
	case regionWB:
		return "wb-" + rawID
	default:
		return rawID
	}
}
