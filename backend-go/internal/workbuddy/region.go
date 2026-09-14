package workbuddy

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// WorkBuddy / CodeBuddy 分「国内版」与「国际版」两个独立区域：两套域名、两套账号凭据、
// 两套额度（互不相通），模型目录也各有一份。区域只在**账号层**表达——对外模型名不加
// 前缀、网关路由不变，插件内部按「该账号所属区域是否提供该模型」自动选号。
const (
	// regionCN 是国内版（CodeBuddy / copilot.tencent.com）。
	regionCN = "cn"
	// regionIntl 是国际版（WorkBuddy / www.workbuddy.ai）。
	regionIntl = "intl"
)

// regionHosts 是各区域的 API 基址（chat / 登录 / 目录都在该域下）。
var regionHosts = map[string]string{
	regionCN:   "https://copilot.tencent.com",
	regionIntl: "https://www.workbuddy.ai",
}

// regionReferers 是各区域的 Origin/Referer 基址（缺失会让上游对 /v3/config 返回 400）。
var regionReferers = map[string]string{
	regionCN:   "https://www.codebuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionUserAgents 是各区域对应的客户端标识（上游按 UA 识别终端类型）。
var regionUserAgents = map[string]string{
	regionCN:   "CLI/2.63.2 CodeBuddy/2.63.2",
	regionIntl: "WorkBuddy/5.5.6 WorkBuddy/5.5.6 CLI/2.137.1",
}

// regionBillingHosts 是各区域「计费/余额」接口的基址。国内与 chat 域不同
// （chat=copilot.tencent.com，billing=codebuddy.cn）；国际版两者同域（workbuddy.ai）。
var regionBillingHosts = map[string]string{
	regionCN:   "https://www.codebuddy.cn",
	regionIntl: "https://www.workbuddy.ai",
}

// regionLabels 是下发前端的中文区域名。
var regionLabels = map[string]string{
	regionCN:   "国内版",
	regionIntl: "国际版",
}

// normalizeRegion 归一化区域标识：空值或未知值一律落到国内版（存量账号无该字段，
// 默认按国内版处理，行为与历史一致）。
func normalizeRegion(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), regionIntl) {
		return regionIntl
	}
	return regionCN
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

// regionLabel 返回区域中文名。
func regionLabel(region string) string {
	return regionLabels[normalizeRegion(region)]
}

// regionOfDomain 按账号 domain 判定区域：含 workbuddy.ai 视为国际版，否则国内版。
func regionOfDomain(domain string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(domain)), "workbuddy.ai") {
		return regionIntl
	}
	return regionCN
}

// regionOfTokenIssuer 按 token 的 issuer 判定区域。国际版的 iss 形如
// https://www.workbuddy.ai/auth/realms/copilot；国内版不含 workbuddy.ai。
func regionOfTokenIssuer(iss string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(iss)), "workbuddy.ai") {
		return regionIntl
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
// 国际版加 "intl-" 前缀，保证同一 uid 在两个区域是两条互不覆盖的记录
// （冷却/限流/用量/选号权重都按 ID 记账，因此天然按区域隔离）。
func accountIDForRegion(region, rawID string) string {
	rawID = strings.TrimSpace(rawID)
	if normalizeRegion(region) == regionIntl {
		return "intl-" + rawID
	}
	return rawID
}
