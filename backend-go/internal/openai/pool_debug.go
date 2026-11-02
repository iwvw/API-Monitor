package openai

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) getEndpointProxyStateRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	items := make([]proxyRuntimeStateItem, 0, len(pool))
	for _, proxy := range pool {
		item := proxyRuntimeStateItem{Proxy: proxy}
		if ok {
			item.Failures = state.failures[proxy]
			item.Rate429 = state.rate429[proxy]
			item.LastTTFB = state.lastTTFB[proxy]
			item.LastExitIP = state.lastExitIP[proxy]
			if probeAt, probed := state.lastProbeAt[proxy]; probed && !probeAt.IsZero() {
				item.LastProbeAt = probeAt.Format(time.RFC3339)
			}
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				item.CooldownUntil = until.Format(time.RFC3339)
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				item.RateLimitedUntil = until.Format(time.RFC3339)
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				item.SunkUntil = until.Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}
	s.proxyMu.Unlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": items})
}

// unbanEndpointProxies 一键解封端点代理池全部出口：清除冷却、429 冻结与坏代理沉淀
// 状态，使被临时/长期禁用的代理立即恢复可选。代理池的禁用都是运行时内存状态，
// 不修改配置，故解封无需写库。
func (s *Service) unbanEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	cleared := 0
	if ok {
		for _, proxy := range pool {
			clearedFrom := false
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				delete(state.cooldown, proxy)
				clearedFrom = true
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				delete(state.rateLimited, proxy)
				delete(state.rate429, proxy)
				clearedFrom = true
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				delete(state.sunk, proxy)
				delete(state.failures, proxy)
				clearedFrom = true
			}
			if clearedFrom {
				cleared++
				s.persistProxyState(id, proxy, "cooldown", time.Time{})
				s.persistProxyState(id, proxy, "rate_limited", time.Time{})
				s.persistProxyState(id, proxy, "sunk", time.Time{})
			}
		}
	}
	s.proxyMu.Unlock()

	applog.Info(ctx, "openai", "proxy pool unbanned",
		"endpoint_id", id,
		"cleared", cleared,
		"pool_size", len(pool),
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "cleared": cleared})
}

// probeEndpointProxies 立即对端点代理池全体出口做一次手动探活：
//  1. 经代理向端点 /models 发起请求，判定链路连通性（成功清冷却/沉淀，失败按
//     失败计数指数冷却且连续失败达阈值沉淀为坏代理）
//  2. 经代理访问 ipify 记录出口公网 IP
//
// 并发执行（上限 20），响应返回每个代理的探测结果（成功后记入运行时状态）。
// 用于前端「批量测试」：探活结果随后通过 /proxy-state 读取。
func (s *Service) probeEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)
	if len(pool) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": 0, "reachable": 0})
		return
	}

	sem := make(chan struct{}, 20)
	var probe sync.WaitGroup
	var okMu sync.Mutex
	reachable := 0
	for _, proxyURL := range pool {
		proxyURL := proxyURL
		probe.Add(1)
		go func() {
			defer probe.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s.probeEndpointProxyOnce(ctx, id, baseURL, apiKey, proxyURL) {
				okMu.Lock()
				reachable++
				okMu.Unlock()
			}
		}()
	}
	probe.Wait()

	applog.Info(ctx, "openai", "proxy pool manually probed",
		"endpoint_id", id,
		"pool_size", len(pool),
		"reachable", reachable,
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": len(pool), "reachable": reachable})
}

// probeEndpointProxyOnce 对单个代理做一次手动探活，返回是否链路可达。
// 链路可达：清冷却/沉淀，记录出口 IP；不可达：指数冷却 + 连续失败沉淀。
func (s *Service) probeEndpointProxyOnce(ctx context.Context, endpointID, baseURL, apiKey, proxyURL string) bool {
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return false
	}
	fullURL := ensureVersionPath(baseURL)
	fullURL += "/models"

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return false
	}
	if apiKey != "" && apiKey != "public" {
		req.Header.Set("Authorization", "Bearer "+secure.SecureDecrypt(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		s.markProxyFailed(endpointID, proxyURL)
		if s.proxyFailCount(endpointID, proxyURL) >= proxySinkThreshold {
			s.sinkProxy(endpointID, proxyURL)
		}
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	resp.Body.Close()
	if resp.StatusCode != 0 && resp.StatusCode >= 300 {
		// 上游 4xx/5xx：链路可达但上游拒绝；不沉淀代理（可能是 key/额度问题）。
		s.markProxySuccess(endpointID, proxyURL)
		s.unsinkProxy(endpointID, proxyURL)
		s.probeProxyExitIP(endpointID, proxyURL)
		return true
	}
	s.markProxySuccess(endpointID, proxyURL)
	s.unsinkProxy(endpointID, proxyURL)
	s.probeProxyExitIP(endpointID, proxyURL)
	return true
}
