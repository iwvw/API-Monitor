package proxypool

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ServeHTTP 是代理池插件的管理入口（/api/proxypool）：
//   - GET    /api/proxypool               列出全部池
//   - POST   /api/proxypool               新建池
//   - PUT    /api/proxypool/{id}          更新池
//   - DELETE /api/proxypool/{id}          删除池
//   - POST   /api/proxypool/{id}/unban    一键解封
//   - GET    /api/proxypool/{id}/state    健康状态（禁用数）
// 代理选择（SelectProxy / ReportResult）由 openai 端点与 openaibeta 等调用方复用。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	rest := strings.TrimPrefix(path, "/api/proxypool")
	rest = strings.Trim(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.list(w, r)
	case rest == "" && r.Method == http.MethodPost:
		s.create(w, r)
	case rest != "" && !strings.Contains(rest, "/") && r.Method == http.MethodPut:
		s.update(w, r, rest)
	case rest != "" && !strings.Contains(rest, "/") && r.Method == http.MethodDelete:
		s.delete(w, r, rest)
	case rest != "" && strings.HasSuffix(rest, "/unban") && r.Method == http.MethodPost:
		s.unban(w, r, strings.TrimSuffix(rest, "/unban"))
	case rest != "" && strings.HasSuffix(rest, "/probe") && r.Method == http.MethodPost:
		s.probe(w, r, strings.TrimSuffix(rest, "/probe"))
	case rest == "subscription" && r.Method == http.MethodPost:
		s.subscription(w, r)
	case rest != "" && strings.HasSuffix(rest, "/state") && r.Method == http.MethodGet:
		s.state(w, r, strings.TrimSuffix(rest, "/state"))
	default:
		response.Error(w, http.StatusNotFound, "proxypool route not found")
	}
}

type poolPayload struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Proxies []string `json:"proxies"`
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	pools, err := s.List(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "pools": pools})
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var body poolPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if body.ID == "" {
		response.Error(w, http.StatusBadRequest, "缺少池 ID")
		return
	}
	pool, err := s.Create(r.Context(), body.ID, body.Name, body.Proxies)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "pool": pool})
}

func (s *Service) update(w http.ResponseWriter, r *http.Request, id string) {
	var body poolPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if err := s.Update(r.Context(), id, body.Name, body.Proxies); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.Delete(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) unban(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.UnbanPool(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) state(w http.ResponseWriter, r *http.Request, id string) {
	items, err := s.State(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	blocked, err := s.BlockedCount(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": items, "blocked": blocked})
}

func (s *Service) probe(w http.ResponseWriter, r *http.Request, id string) {
	total, reachable, err := s.ProbeAll(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": total, "reachable": reachable})
}

func (s *Service) subscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	if body.URL == "" {
		response.Error(w, http.StatusBadRequest, "请填写订阅链接")
		return
	}
	lower := strings.ToLower(body.URL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		response.Error(w, http.StatusBadRequest, "订阅链接必须以 http:// 或 https:// 开头")
		return
	}
	lines, err := s.ResolveSubscription(r.Context(), body.URL)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "拉取订阅失败: "+err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "lines": lines})
}
