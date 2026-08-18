package system

import (
	"sort"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// routeAliases 为接口补充用户惯用说法（when_to_use 别名），
// 用于 find_api 的意图粗召回。key 为路由前缀或其父前缀（如
// "/api/server/sftp" 可覆盖其子路由），value 为用户可能使用的
// 自然说法或高频同义词；未登记的路由会回退到路径/描述自动索引。
var routeAliases = map[string][]string{
	"/api/auth/login":        {"登录", "登陆", "密码登录", "sign in"},
	"/api/auth/set-password": {"首次设置密码", "初始化密码", "创建管理员密码"},
	"/api/auth/2fa":          {"二次验证", "双重认证", "两步验证", "totp", "动态口令"},
	"/api/auth/webauthn":     {"通行密钥", "passkey", "生物识别登录", "指纹登录"},
	"/api/auth/session":      {"当前登录态", "会话状态", "是否登录"},

	"/api/settings":                           {"系统配置", "运行参数", "修改设置"},
	"/api/settings/sys-logs":                  {"系统日志", "日志列表", "查看日志"},
	"/api/settings/export-database":           {"导出数据库", "备份数据库文件"},
	"/api/settings/import-database":           {"导入数据库", "替换数据库"},
	"/api/settings/vacuum-database":           {"清理数据库", "压缩空间", "vacuum"},
	"/api/settings/cleanup-deprecated-tables": {"废弃表", "清理旧表"},

	"/api/system/host-metrics": {"资源占用", "cpu 内存", "机器负载", "运行指标"},
	"/api/system/api-stats":    {"接口调用次数", "api 统计", "调用量"},
	"/api/openapi.json":        {"接口文档", "openapi", "swagger"},
	"/api/system/api-docs":     {"接口文档", "路由清单", "api 列表"},

	"/api/cloudflare":          {"cf", "cloudflare", "域名服务"},
	"/api/cloudflare/accounts": {"cf 账号", "cloudflare token", "CF 令牌", "删除 cf 账号", "删除cloudflare账户", "删除账户"},
	"/api/cloudflare/accounts/{id}": {"删除cf账号", "删除账号", "删除账户"},
	"/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records":   {"DNS 记录", "解析记录", "域名解析", "添加解析", "dns", "解析列表", "cf dns"},
	"/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge":     {"清除缓存", "刷新缓存", "purge"},
	"/api/cloudflare/accounts/{accountId}/zones/{zoneId}/analytics": {"流量分析", "站点数据分析", "访问统计"},
	"/api/cloudflare/accounts/{id}/workers":                         {"worker 脚本", "边缘脚本", "cf worker"},
	"/api/cloudflare/accounts/{id}/pages":                           {"pages 项目", "静态网站托管", "cf pages"},
	"/api/cloudflare/accounts/{accountId}/r2/buckets":               {"R2 存储", "对象存储", "存储桶", "r2"},
	"/api/cloudflare/accounts/{id}/tunnels":                         {"隧道", "tunnel", "cloudflare tunnel"},
	"/api/cloudflare/templates":                                     {"DNS 模板", "批量解析模板"},

	"/api/aliyun":                                            {"阿里云", "aliyun"},
	"/api/aliyun/accounts/{id}/instances":                    {"ecs 实例", "云服务器", "阿里云机器"},
	"/api/aliyun/accounts/{id}/swas":                         {"轻量应用服务器", "轻量服务器"},
	"/api/aliyun/accounts/{id}/domains":                      {"域名解析", "阿里云 dns"},
	"/api/aliyun/accounts/{id}/domains/{domainName}/records": {"解析记录", "添加解析", "dns 记录", "阿里云 dns 记录"},

	"/api/tencent":                                        {"腾讯云", "tencent", "腾讯 云"},
	"/api/tencent/accounts/{id}/cvm":                      {"cvm 实例", "云服务器"},
	"/api/tencent/accounts/{id}/lighthouse":               {"轻量应用服务器"},
	"/api/tencent/accounts/{id}/domains/{domain}/records": {"解析记录", "dns 记录", "添加解析", "腾讯云 dns 记录"},

	"/api/flyio":                             {"fly.io", "flyio", "飞萤"},
	"/api/flyio/apps/{appName}/update-image": {"更新镜像", "升级镜像", "换镜像"},
	"/api/flyio/apps/{appName}/redeploy":     {"重新部署", "重启应用", "redeploy"},
	"/api/flyio/apps/{appName}/logs":         {"应用日志", "部署日志"},
	"/api/flyio/apps/{appName}/machines":     {"机器列表", "容器列表"},

	"/api/koyeb": {"koyeb"},
	"/api/koyeb/services/{serviceId}/redeploy": {"重新部署服务"},

	"/api/onepanel":                           {"1panel", "面板", "面板服务器"},
	"/api/onepanel/{serverId}/websites":       {"网站列表", "建站"},
	"/api/onepanel/{serverId}/containers":     {"容器列表", "docker 容器"},
	"/api/onepanel/{serverId}/apps/installed": {"已安装应用", "应用市场"},
	"/api/onepanel/{serverId}/apps/install":   {"安装应用", "应用安装"},
	"/api/onepanel/{serverId}/databases":      {"数据库列表", "创建数据库"},
	"/api/onepanel/{serverId}/runtimes":       {"运行环境", "php 环境"},
	"/api/onepanel/{serverId}/ssl":            {"ssl 证书", "https 证书"},
	"/api/onepanel/{serverId}/openresty":      {"openresty", "nginx", "反代配置"},

	"/api/github":                 {"github", "github 仓库"},
	"/api/github/repositories/{id}/refresh": {"刷新仓库", "仓库刷新", "刷新 github 仓库"},
	"/api/github/tokens":          {"github token", "access token"},
	"/api/github/webhook":         {"webhook", "钩子"},
	"/api/github/history/compact": {"github 事件历史", "压缩事件"},

	"/api/scheduler":              {"定时任务", "cron", "计划任务", "调度", "定时任务列表", "cron 任务"},
	"/api/scheduler/workflows":    {"工作流", "自动化流程", "工作流列表"},
	"/api/scheduler/cron/preview": {"cron 预览", "下次执行时间"},

	"/api/uptime":          {"可用性监测", "站点监测", "状态页", "拨测"},
	"/api/uptime/monitors": {"监测项", "监控项", "健康检查"},

	"/api/notification":          {"通知中心", "告警通知", "消息通知"},
	"/api/notification/channels": {"通知渠道", "推送渠道"},

	"/api/drawio":           {"图编辑器", "drawio", "绘图", "流程图"},
	"/api/drawio/documents": {"图文档", "图表"},

	"/api/prompts":         {"提示词库", "prompt", "提示词"},
	"/api/prompts/entries": {"提示词条目", "提示词列表"},

	"/api/openai":                             {"模型网关", "openai", "网关"},
	"/api/openai/endpoints":                   {"模型端点", "模型接口"},
	"/api/openai/endpoints/{id}/health-check": {"端点健康检查"},
	"/api/openai/analytics/summary":           {"网关用量", "调用量汇总"},
	"/api/openai/keys":                        {"网关 api key", "openai key"},

	"/api/subscription":        {"订阅分发", "订阅管理"},
	"/api/sub/{token}":         {"clash 订阅", "v2ray 订阅", "订阅链接"},
	"/api/m365":                {"microsoft 365", "m365", "office 365", "outlook"},
	"/api/m365/registrations": {"注册记录", "m365 注册记录", "注册列表", "m365 注册"},
	"/api/oracle":              {"oracle", "甲骨文", "oci", "oracle 云"},
	"/api/uptime/status-pages": {"状态页", "公开展示页"},

	"/api/server/info":            {"主机信息", "服务器信息"},
	"/api/server/action":          {"重启主机", "关机主机", "开机", "重启", "重启服务器", "关机", "reboot", "shutdown", "重启机器"},
	"/api/server/check-all":       {"批量检查", "在线状态"},
	"/api/server/accounts":        {"主机账号", "服务器账号", "机器列表"},
	"/api/server/credentials":     {"凭据", "连接凭据", "ssh 密钥"},
	"/api/server/network-quality": {"网络测速", "测速", "网络质量"},
	"/api/server/sftp":            {"sftp", "远程文件", "上传文件", "下载文件", "sftp 列表"},
	"/api/server/tasks":           {"主机任务", "批量任务"},
	"/api/server/agent/proxy":     {"托管代理", "代理节点", "梯子", "节点配置"},
	"/api/server/agent/heartbeat": {"agent 心跳"},
	"/api/server/remote-desktop":  {"远程桌面"},

	"/api/totp":    {"totp", "动态令牌", "一次性密码", "创建 totp"},
	"/api/totp/accounts": {"totp 账号", "动态口令账号", "创建 totp 账号", "totp 账号列表"},
	"/api/filebox": {"文件柜", "文件上传", "文件分享", "文件列表"},
	"/api/filebox/shares": {"分享列表", "文件列表", "文件柜列表", "我的分享"},
	"/api/backup":        {"备份", "数据备份", "备份任务", "备份列表", "创建备份"},
	"/api/backup/configs": {"备份配置列表", "备份配置", "备份列表"},
	"/api/api-keys":       {"api 密钥", "apikey", "密钥列表", "api key", "密钥管理"},

	"/api/openai/analytics/logs":  {"网关日志", "调用日志", "请求日志", "openai 日志"},
	"/api/openai/analytics":       {"网关统计", "用量统计", "令牌统计"},
	"/api/github/actions":         {"workflow 运行", "actions", "工作流运行", "github 运行"},
	"/api/github/repositories":    {"仓库列表", "github 仓库列表"},
	"/api/cron":                   {"定时任务列表", "cron 任务"},
}

// aliasesForPrefix 返回路由或其最近父前缀登记的别名列表。
func aliasesForPrefix(prefix string) []string {
	for {
		if list, ok := routeAliases[prefix]; ok {
			return list
		}
		idx := strings.LastIndex(prefix, "/")
		if idx <= 0 {
			return nil
		}
		prefix = prefix[:idx]
	}
}

// intentSignals 预解析意图：规范化整句、英文 token、中文三字片段与口语同义变体。
type intentSignals struct {
	joined   string
	tokens   []string
	trigrams []string
	synonyms []string
}

// intentSynonyms 口语/同义词映射：键为用户在意图里可能说的口语词，
// 值是接口描述/别名中使用的高频正式词。find_api 会把意图里的口语词
// 替换为正式词后追加一份匹配信号，覆盖「机器→主机」「梯子→代理」等词汇鸿沟。
var intentSynonyms = map[string][]string{
	"机器":    {"主机", "服务器", "实例", "server"},
	"服务器":   {"主机", "server"},
	"站点":    {"网站", "zone", "域名"},
	"梯子":    {"代理", "proxy", "节点"},
	"面板":    {"onepanel", "1panel"},
	"看":     {"读取", "查询", "list", "列"},
	"查":     {"读取", "查询", "list", "列"},
	"看一下":   {"读取", "查询", "list"},
	"查一下":   {"读取", "查询", "list"},
	"更新":    {"update", "升级"},
	"删除":    {"delete", "移除"},
	"添加":    {"新增", "create", "创建"},
	"解析":    {"dns", "records", "记录"},
	"记录":    {"dns", "records"},
	"密钥":    {"key", "token"},
	"token": {"key", "令牌"},
	"key":   {"token", "密钥"},
	"wf":    {"workflow", "工作流"},
	"cron":  {"scheduler", "定时任务"},
	"ssh":   {"远程", "sftp", "终端"},
}

// analyzeIntent 将意图拆为信号：规范化整句、英文 token、中文三字片段，
// 并基于口语同义词映射生成替换变体（提升词汇鸿沟召回）。
func analyzeIntent(query string) intentSignals {
	raw := strings.ToLower(strings.TrimSpace(query))
	joined := strings.Join(strings.Fields(raw), "")
	tokens := make([]string, 0, 4)
	for _, part := range strings.Fields(raw) {
		part = strings.Trim(part, ",.;:!?，。；：！？'\"")
		if isASCIIWord(part) {
			tokens = append(tokens, part)
		}
	}
	trigrams := make([]string, 0, 16)
	runes := []rune(joined)
	for i := 0; i+3 <= len(runes); i++ {
		chunk := string(runes[i : i+3])
		if !hasCJK(chunk) || containsString(trigrams, chunk) {
			continue
		}
		trigrams = append(trigrams, chunk)
	}
	synonyms := expandIntentSynonyms(joined)
	return intentSignals{joined: joined, tokens: tokens, trigrams: trigrams, synonyms: synonyms}
}

// expandIntentSynonyms 遍历口语同义词映射，把意图中出现口语词的位置替换为正式词，
// 产生一组「整句替换」变体供评分补充匹配。
func expandIntentSynonyms(joined string) []string {
	results := make([]string, 0, 8)
	for slang, formalList := range intentSynonyms {
		if !strings.Contains(joined, slang) {
			continue
		}
		for _, formal := range formalList {
			variant := strings.Replace(joined, slang, formal, -1)
			if variant != joined && !containsString(results, variant) {
				results = append(results, variant)
			}
		}
	}
	return results
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r > 0x2E7F {
			return true
		}
	}
	return false
}

func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// matchReason 记录单条路由的某次命中：命中的词元、命中层面与权重。
type matchReason struct {
	Term   string `json:"term"`
	Level  string `json:"level"`
	Weight int    `json:"weight"`
}

// routeMatch 是召回结果：路由 + 累计得分 + 命中原因清单。
type routeMatch struct {
	Route   apiDocRoute
	Score   int
	Reasons []matchReason
}

func appendReason(reasons []matchReason, term, level string, weight int) []matchReason {
	for _, r := range reasons {
		if r.Term == term && r.Level == level {
			return reasons
		}
	}
	return append(reasons, matchReason{Term: term, Level: level, Weight: weight})
}

// scoreRouteMatch 对单条路由打分。优先反向匹配：
//  1. 别名是意图整句的子串（强命中，+5/+2）
//  2. 中文三字片段命中描述/分组（+2）
//  3. 英文 token 命中路径/模块（+1）
//
// 全不命中时若意图整句是检索文本子串则给保底分兜底猜中场景。
// 返回得分与逐条命中原因（供 find_api 向 AI 透出解释）。
func scoreRouteMatch(route apiDocRoute, signals intentSignals) (int, []matchReason) {
	reasons := make([]matchReason, 0, 4)
	score := 0
	aliases := aliasesForPrefix(route.Prefix)
	exactAliases := routeAliases[route.Prefix]
	for _, alias := range aliases {
		aliasNorm := strings.ToLower(strings.ReplaceAll(alias, " ", ""))
		if aliasNorm == "" || !strings.Contains(signals.joined, aliasNorm) {
			continue
		}
		// 精确登记在自身前缀的别名权重更高；父前缀继承的别名降权，避免同族路由同分。
		weight := 2
		level := "alias(parent)"
		if containsString(exactAliases, alias) {
			weight = 5
			level = "alias"
		}
		score += weight
		reasons = appendReason(reasons, alias, level, weight)
	}
	lowerDetail := strings.ToLower(route.Detail)
	lowerDesc := strings.ToLower(route.Description)
	lowerGroup := strings.ToLower(route.Group)
	lowerModule := strings.ToLower(strings.TrimPrefix(route.Module, "-"))
	searchText := strings.ToLower(route.Prefix + " " + route.Detail + " " + route.Description + " " + route.Group + " " + route.Module)

	for _, tri := range signals.trigrams {
		if strings.Contains(lowerDetail, tri) || strings.Contains(lowerDesc, tri) {
			score += 2
			reasons = appendReason(reasons, tri, "description", 2)
		} else if strings.Contains(lowerGroup, tri) || strings.Contains(lowerModule, tri) {
			score += 2
			reasons = appendReason(reasons, tri, "group", 2)
		}
	}
	for _, token := range signals.tokens {
		if strings.Contains(searchText, token) {
			score += 1
			reasons = appendReason(reasons, token, "path", 1)
		}
	}
	// 口语同义词变体补充召回：仅当整句与词元信号都未命中（score==0 且无别名命中）时启用，
	// 避免「看→读取」这类宽泛映射把无关路由拉高排序。
	if len(reasons) == 0 {
		for _, variant := range signals.synonyms {
			variantJoined := strings.Join(strings.Fields(variant), "")
			if variantJoined == "" {
				continue
			}
			vRunes := []rune(variantJoined)
			for i := 0; i+3 <= len(vRunes); i++ {
				chunk := string(vRunes[i : i+3])
				if !hasCJK(chunk) {
					continue
				}
				if strings.Contains(lowerDetail, chunk) || strings.Contains(lowerDesc, chunk) ||
					strings.Contains(lowerGroup, chunk) || strings.Contains(lowerModule, chunk) {
					score += 2
					reasons = appendReason(reasons, "同义:"+variantJoined, "synonym", 2)
					break
				}
			}
		}
	}
	if score == 0 && signals.joined != "" && strings.Contains(searchText, signals.joined) {
		score += 1
		reasons = appendReason(reasons, signals.joined, "full-match", 1)
	}
	return score, reasons
}

// isAggregatePrefix 判断路由是否为聚合前缀（模块根/家族总入口，非可调用端点）。
// 判定：MatchPrefix 且在目录中存在以它开头的更深子路由（如 /api/backup 下有
// /api/backup/configs）。仅靠段数会把 /api/totp/accounts 这类 2 段具体端点误判。
func isAggregatePrefix(prefix string, mode manifest.MatchMode, items []apiDocRoute) bool {
	if mode != manifest.MatchPrefix {
		return false
	}
	if !strings.HasPrefix(prefix, "/api/") {
		return false
	}
	if strings.HasPrefix(prefix, "/sub") || strings.HasPrefix(prefix, "/v1") {
		return false
	}
	childPrefix := strings.TrimRight(prefix, "/") + "/"
	for _, other := range items {
		if other.Prefix != prefix && strings.HasPrefix(other.Prefix, childPrefix) {
			return true
		}
	}
	return false
}

// findTopRoutes 从路由清单中粗召回得分最高的 upTo 条路由（按分降序），
// 返回带命中原因的 routeMatch 列表。
func findTopRoutes(items []apiDocRoute, query string, upTo int) []routeMatch {
	signals := analyzeIntent(query)
	if signals.joined == "" {
		return nil
	}
	matched := make([]routeMatch, 0, len(items))
	for _, item := range items {
		s, reasons := scoreRouteMatch(item, signals)
		if s <= 0 {
			continue
		}
		// 聚合前缀降权（0.5）：其子路由才是可调用端点，
		// 避免名称命中让聚合根压过具体接口（业界「减少干扰工具」原则）。
		if isAggregatePrefix(item.Prefix, item.MatchMode, items) {
			s = int(float64(s) * 0.5)
			if s <= 0 {
				s = 1
			}
		}
		matched = append(matched, routeMatch{Route: item, Score: s, Reasons: reasons})
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Score > matched[j].Score
	})
	if upTo <= 0 || upTo > len(matched) {
		upTo = len(matched)
	}
	return matched[:upTo]
}
