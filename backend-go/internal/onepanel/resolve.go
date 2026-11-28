package onepanel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 证书/网站的“自然键 → id”解析:让 Agent 用域名重试,而不是猜 id。
// 背景:1Panel 的 id 按实例独立、且资源重建会换 id,按 id 操作极易 record not found。
// 这两个只读接口先按域名定位资源,再回传其当前 id 与关键字段。

// panelSearchItems 调用 1Panel 的 search 类上游并返回 items 数组。
// 统一处理信封与错误,失败时给出带上下文的中文错误。
func (s *Service) panelSearchItems(ctx context.Context, serverID string, cfg *panelConfig, upstreamPath, body string) ([]map[string]interface{}, error) {
	result := s.execPanelCmd(ctx, serverID, cfg, http.MethodPost, upstreamPath, body)
	if result.err != nil {
		return nil, result.err
	}
	var env struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []map[string]interface{} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.output), &env); err != nil {
		return nil, fmt.Errorf("解析 1Panel 响应失败")
	}
	if env.Code != nil && *env.Code != 200 {
		msg := strings.TrimSpace(env.Message)
		if msg == "" {
			msg = "1Panel 返回错误"
		}
		return nil, fmt.Errorf("1Panel %s 查询失败: %s", upstreamPath, msg)
	}
	return env.Data.Items, nil
}

func (s *Service) resolveSSLID(w http.ResponseWriter, r *http.Request, serverID string) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		response.Error(w, http.StatusBadRequest, "缺少查询参数 domain(如 occ.dmuk.org 或 *.dmuk.org)")
		return
	}
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	items, err := s.panelSearchItems(r.Context(), serverID, cfg, "/websites/ssl/search",
		`{"page":1,"pageSize":200,"orderBy":"created_at","order":"descending"}`)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, item := range items {
		primary, _ := item["primaryDomain"].(string)
		domains, _ := item["domains"].(string)
		if certCoversDomain(primary, domains, domain) {
			response.OK(w, map[string]interface{}{
				"matched":     true,
				"matchedBy":   "证书 primaryDomain / domains / 泛域名覆盖匹配",
				"id":          item["id"],
				"certificate": item,
			})
			return
		}
	}
	response.Error(w, http.StatusNotFound,
		fmt.Sprintf("在该 serverId 上找不到覆盖域名 %q 的证书;请用 /api/onepanel/{serverId}/ssl/search 核对", domain))
}

func (s *Service) resolveWebsiteID(w http.ResponseWriter, r *http.Request, serverID string) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		response.Error(w, http.StatusBadRequest, "缺少查询参数 domain(如 occ.dmuk.org)")
		return
	}
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	items, err := s.panelSearchItems(r.Context(), serverID, cfg, "/websites/search",
		`{"page":1,"pageSize":200,"orderBy":"created_at","order":"descending"}`)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, item := range items {
		primary, _ := item["primaryDomain"].(string)
		alias, _ := item["alias"].(string)
		if domainEquals(primary, domain) || domainEquals(alias, domain) {
			response.OK(w, map[string]interface{}{
				"matched":   true,
				"matchedBy": "网站 primaryDomain / alias 精确匹配",
				"id":        item["id"],
				"website":   item,
			})
			return
		}
	}
	response.Error(w, http.StatusNotFound,
		fmt.Sprintf("在该 serverId 上找不到域名 %q 对应的网站;请用 /api/onepanel/{serverId}/websites/search 核对(注意网站列表只回 primaryDomain)", domain))
}

// certCoversDomain 判断证书是否覆盖查询域名:精确匹配、domains 列表命中,或泛域名覆盖子域。
func certCoversDomain(primary, domainsCSV, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	names := []string{primary}
	for _, d := range strings.Split(domainsCSV, ",") {
		if d = strings.TrimSpace(d); d != "" {
			names = append(names, d)
		}
	}
	for _, n := range names {
		if certificateNameMatches(strings.ToLower(strings.TrimSpace(n)), q) {
			return true
		}
	}
	return false
}

// certificateNameMatches 处理单个别名与查询域名的匹配,支持 *.example.com 泛域名覆盖子域。
func certificateNameMatches(certName, query string) bool {
	if certName == "" || query == "" {
		return false
	}
	if certName == query {
		return true
	}
	if strings.HasPrefix(certName, "*.") {
		suffix := certName[1:] // ".example.com"
		return query == certName[2:] || strings.HasSuffix(query, suffix)
	}
	return false
}

func domainEquals(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
