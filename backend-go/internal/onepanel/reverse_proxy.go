package onepanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 1Panel 站点反代信息：GET /websites/list 的返回里每个站点带 proxy（上游地址，
// 如 http://127.0.0.1:8081）与 domains（绑定域名）。这里只提取「上游端口 → 域名」，
// 由前端再和容器的宿主端口映射做匹配 —— 容器映射端口只有 Docker 知道，1Panel 不报。

type panelSiteDomain struct {
	Domain string `json:"domain"`
	SSL    bool   `json:"ssl"`
	Port   int    `json:"port"`
}

type panelSite struct {
	ID            int               `json:"id"`
	PrimaryDomain string            `json:"primaryDomain"`
	Alias         string            `json:"alias"`
	Type          string            `json:"type"`
	Protocol      string            `json:"protocol"`
	Status        string            `json:"status"`
	Proxy         string            `json:"proxy"`
	Domains       []panelSiteDomain `json:"domains"`
}

type reverseProxyDomain struct {
	Domain   string   `json:"domain"`
	URL      string   `json:"url"`
	SiteID   int      `json:"siteId"`
	SiteName string   `json:"siteName"`
	Status   string   `json:"status"`
	Domains  []string `json:"domains"`
}

// reverseProxyTarget 汇总一个上游端口对应的全部域名。同端口多站点、同站点多别名
// 都要合并，前端点开时能选到任意一个。
type reverseProxyTarget struct {
	UpstreamPort int                  `json:"upstreamPort"`
	Domains      []reverseProxyDomain `json:"domains"`
}

// 反代接口按宿主端口聚合，容器的宿主端口映射由前端解析（与容器列表同一份数据）。
func (s *Service) reverseProxyDomains(w http.ResponseWriter, r *http.Request, serverID string) {
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	res := s.execPanelCmd(r.Context(), serverID, cfg, http.MethodGet, "/websites/list", "")
	if res.err != nil {
		response.Error(w, http.StatusBadGateway, res.err.Error())
		return
	}

	targets := buildReverseProxyTargets(res.output)
	response.OK(w, map[string]interface{}{
		"serverId": serverID,
		"targets":  targets,
	})
}

// buildReverseProxyTargets 把 1Panel 站点列表整理成「上游端口 → 域名」，输出稳定排序，
// 便于前端缓存与测试断言。非 proxy 类型站点（静态站、运行环境等）没有 proxy 上游，跳过。
func buildReverseProxyTargets(raw string) []reverseProxyTarget {
	data := panelData(raw)
	sites, ok := data.([]interface{})
	if !ok {
		return []reverseProxyTarget{}
	}

	byPort := map[int][]reverseProxyDomain{}
	for _, item := range sites {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		var site panelSite
		buf, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(buf, &site); err != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(site.Type)) != "proxy" {
			continue
		}
		port, hasPort, host := parseUpstreamHostPort(site.Proxy)
		if !hasPort {
			continue
		}
		_ = host

		primary := strings.TrimSpace(site.PrimaryDomain)
		if primary == "" && len(site.Domains) > 0 {
			primary = strings.TrimSpace(site.Domains[0].Domain)
		}
		if primary == "" {
			continue
		}

		var all []string
		seen := map[string]bool{}
		for _, d := range site.Domains {
			name := strings.TrimSpace(d.Domain)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			all = append(all, name)
		}
		if len(all) == 0 {
			all = []string{primary}
		}

		scheme := reverseProxyScheme(site)
		byPort[port] = append(byPort[port], reverseProxyDomain{
			Domain:   primary,
			URL:      fmt.Sprintf("%s://%s", scheme, primary),
			SiteID:   site.ID,
			SiteName: primary,
			Status:   site.Status,
			Domains:  all,
		})
	}

	ports := make([]int, 0, len(byPort))
	for port := range byPort {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	targets := make([]reverseProxyTarget, 0, len(ports))
	for _, port := range ports {
		domains := byPort[port]
		sort.SliceStable(domains, func(i, j int) bool {
			return domains[i].Domain < domains[j].Domain
		})
		targets = append(targets, reverseProxyTarget{UpstreamPort: port, Domains: domains})
	}
	return targets
}

// reverseProxyScheme：站点显式启用 HTTPS 时用 https，否则 spoof 成 http。
// 1Panel 的 sites 通用字段里 Protocol 为 HTTPS 即该站点有证书且强制跳转。
func reverseProxyScheme(site panelSite) string {
	if strings.EqualFold(strings.TrimSpace(site.Protocol), "HTTPS") {
		return "https"
	}
	for _, d := range site.Domains {
		if d.SSL {
			return "https"
		}
	}
	return "http"
}

// parseUpstreamHostPort 解析 proxy_pass 上游（如 http://127.0.0.1:8081/）。
// 返回端口与是否解析成功。
func parseUpstreamHostPort(upstream string) (int, bool, string) {
	raw := strings.TrimSpace(upstream)
	if raw == "" {
		return 0, false, ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return 0, false, ""
	}
	portStr := u.Port()
	if portStr == "" {
		switch u.Scheme {
		case "https":
			return 443, true, u.Hostname()
		case "http":
			return 80, true, u.Hostname()
		}
		return 0, false, ""
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false, ""
	}
	return port, true, u.Hostname()
}
