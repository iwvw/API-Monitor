package qoder

// 本文件定义 Qoder 的区域（国内版 / 国际版）与按区域派生的上游基址。
//
// 两套区域完全独立：域名、client_id、登录参数、对话网关都不同。账号级 Region
// 决定该账号走哪一套（空值按国内版处理，因当前以国内版为主）。
//
// 协议事实来自开源反代 qoder2api-worker（nostalgia296/qoder2api-worker，国内版）
// 与 qoder2api-hub（shuishuipingan/qoder2api-hub，双区域）实测记录。

import "strings"

const (
	// regionCN 是国内版（qoder.cn / openapi.qoder.com.cn / gateway.qoder.com.cn）。
	regionCN = "cn"
	// regionIntl 是国际版（qoder.com / openapi.qoder.sh / api1-3.qoder.sh）。
	regionIntl = "intl"
)

// regionConfig 是一个区域的上游基址与登录参数。
type regionConfig struct {
	name string
	// loginBase 是设备授权引导页。
	loginBase string
	// openAPI 是业务控制面基址（登录轮询、刷新、userinfo、余额、活动）。
	openAPI string
	// gateway 是对话流式网关基址。
	gateway string
	// website 是官网基址（Origin/Referer 用）。
	website string
	// clientID 是设备授权 client_id。
	clientID string
	// nonceDashed 表示 nonce 使用带横线的 UUID（国内版），否则 32 位 hex（国际版）。
	nonceDashed bool
	// sendRedirect 表示授权 URL 是否带 redirect_uri。
	sendRedirect bool
	redirectURI  string
	// ua 是该区域的默认 User-Agent。
	ua string
}

var regionConfigs = map[string]regionConfig{
	regionCN: {
		name:         "国内版",
		loginBase:    "https://qoder.cn/device/selectAccounts",
		openAPI:      "https://openapi.qoder.com.cn",
		gateway:      "https://gateway.qoder.com.cn",
		website:      "https://qoder.cn",
		clientID:     "732aef47-9cf2-46a2-95fe-4cebb5d0d1fa",
		nonceDashed:  true,
		sendRedirect: false,
		ua:           "QoderWork/1.1.64",
	},
	regionIntl: {
		name:         "国际版",
		loginBase:    "https://qoder.com/device/selectAccounts",
		openAPI:      "https://openapi.qoder.sh",
		gateway:      "https://api3.qoder.sh",
		website:      "https://qoder.com",
		clientID:     "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb",
		nonceDashed:  false,
		sendRedirect: false,
		ua:           "Qoder/1.1.64",
	},
}

// normalizeRegion 归一化区域标识：空值或未知值一律落到国内版（当前主用区域）。
func normalizeRegion(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), regionIntl) {
		return regionIntl
	}
	return regionCN
}

// regionConfigFor 返回区域配置。
func regionConfigFor(region string) regionConfig {
	return regionConfigs[normalizeRegion(region)]
}

// regionLabel 返回区域中文名。
func regionLabel(region string) string {
	return regionConfigFor(region).name
}

// openAPIBaseFor 返回区域 openapi 基址（去尾部斜杠）。
func (s *Service) openAPIBaseFor(region string) string {
	if v := strings.TrimSpace(openAPIBaseOverride); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(regionConfigFor(region).openAPI, "/")
}

// loginBaseFor 返回区域设备授权页基址（去尾部斜杠）。
func (s *Service) loginBaseFor(region string) string {
	if v := strings.TrimSpace(loginBaseOverride); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(regionConfigFor(region).loginBase, "/")
}

// gatewayFor 返回区域对话网关基址（去尾部斜杠）。
func (s *Service) gatewayFor(region string) string {
	if v := strings.TrimSpace(gatewayOverride); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(regionConfigFor(region).gateway, "/")
}

// chatStreamURLFor 返回区域对话流式端点完整 URL。
func (s *Service) chatStreamURLFor(region string) string {
	return s.gatewayFor(region) + legacyChatPath + "?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
}

// regionOfDomain 按账号 domain 判定区域：含 qoder.com.cn / qoder.cn 视为国内版，
// 含 qoder.sh / qoder.com（非 .cn）视为国际版。
func regionOfDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return regionCN
	}
	if strings.Contains(d, "qoder.com.cn") || strings.HasSuffix(d, "qoder.cn") {
		return regionCN
	}
	if strings.Contains(d, "qoder.sh") || strings.Contains(d, "qoder.com") {
		return regionIntl
	}
	return regionCN
}
