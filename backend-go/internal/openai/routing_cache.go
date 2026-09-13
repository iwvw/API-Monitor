package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// routeCacheTTL 是已启用端点配置缓存的过期时间。端点配置变更最多延迟该时长生效，
// 无需在每条写路径逐处失效，避免遗漏导致的陈旧路由。
const routeCacheTTL = 2 * time.Second

// enabledEndpointsCached 返回已启用端点的完整配置列表（按 sort_order 升序），
// 结果缓存在内存中，TTL 到期或配置变更后自动重建。调用方不得修改返回的端点。
func (s *Service) enabledEndpointsCached(ctx context.Context, db *sql.DB) ([]Endpoint, error) {
	s.routeCacheMu.Lock()
	if s.routeCacheReady && time.Since(s.routeCacheAt) < routeCacheTTL {
		cached := s.routeCache
		s.routeCacheMu.Unlock()
		return cached, nil
	}
	s.routeCacheMu.Unlock()

	endpoints := []Endpoint{}
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, model_mappings, sort_order, priority, weight, key_retry_rounds, proxy_pool_id, upstream_type
		FROM openai_endpoints WHERE enabled = 1
		ORDER BY priority DESC, sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw, proxyPoolIDRaw, upstreamTypeRaw sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, sortOrder, priority, weight, keyRetryRounds int
		if errScan := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &rateLimitRetryInt, &rateLimitRetryWaitSeconds, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw, &sortOrder, &priority, &weight, &keyRetryRounds, &proxyPoolIDRaw, &upstreamTypeRaw); errScan == nil {
			ep.ProxyPoolID = proxyPoolIDRaw.String
			ep.UpstreamType = normalizeUpstreamType(upstreamTypeRaw.String)
			ep.APIKey = secure.SecureDecrypt(ep.APIKey)
			ep.Enabled = enabledInt == 1
			ep.AutoSwitch = autoSwitchInt == 1
			ep.ProxyEnabled = proxyEnabledInt == 1
			ep.ForceProxy = forceProxyInt == 1
			ep.RateLimitRetryEnabled = rateLimitRetryInt == 1
			ep.RateLimitRetryWaitSeconds = rateLimitRetryWaitSeconds
			ep.Protocol = normalizeProtocol(protocolRaw.String)
			ep.KeyRetryRounds = keyRetryRounds
			if apiKeysRaw.Valid && apiKeysRaw.String != "" {
				_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
			}
			if mappingsRaw.Valid && mappingsRaw.String != "" {
				_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
			}
			if modelsRaw.Valid {
				_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
			}
			ep.Headers = []HeaderItem{}
			if headersRaw.Valid && headersRaw.String != "" {
				_ = json.Unmarshal([]byte(headersRaw.String), &ep.Headers)
			}
			ep.DisabledModels = []string{}
			if disabledRaw.Valid && disabledRaw.String != "" {
				_ = json.Unmarshal([]byte(disabledRaw.String), &ep.DisabledModels)
			}
			ep.ProxyPool = []string{}
			if proxyRaw.Valid && proxyRaw.String != "" {
				_ = json.Unmarshal([]byte(proxyRaw.String), &ep.ProxyPool)
			}
			ep.Priority = priority
			ep.Weight = weight
			ep.HealthStatus = ""
			endpoints = append(endpoints, ep)
		}
	}

	s.routeCacheMu.Lock()
	s.routeCache = endpoints
	s.routeModelIndex = buildRouteModelIndex(endpoints)
	s.routeCacheAt = time.Now()
	s.routeCacheReady = true
	s.routeCacheMu.Unlock()
	return endpoints, nil
}

// invalidateRouteCache 主动清空路由缓存，供端点配置变更路径调用，
// 使新配置立即生效而无需等待 TTL 到期。
func (s *Service) invalidateRouteCache() {
	s.routeCacheMu.Lock()
	s.routeCacheReady = false
	s.routeCache = nil
	s.routeCacheMu.Unlock()
}

// enabledEndpointSnapshot 返回已启用端点列表与其对应的模型倒排索引，二者在
// 同一次锁持有内读取，保证下标与索引一致（避免缓存重建窗口下选择到错位端点）。
func (s *Service) enabledEndpointSnapshot(ctx context.Context, db *sql.DB) ([]Endpoint, map[string][]int, error) {
	s.routeCacheMu.Lock()
	if s.routeCacheReady && time.Since(s.routeCacheAt) < routeCacheTTL {
		eps := s.routeCache
		idx := s.routeModelIndex
		s.routeCacheMu.Unlock()
		return eps, idx, nil
	}
	s.routeCacheMu.Unlock()

	eps, err := s.enabledEndpointsCached(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	// enabledEndpointsCached 在同一锁内写入 routeCache 与 routeModelIndex，
	// 此处再次加锁读取索引即可保证与刚返回的切片同代。
	s.routeCacheMu.Lock()
	idx := s.routeModelIndex
	s.routeCacheMu.Unlock()
	return eps, idx, nil
}

// selectEndpointCandidates 根据模型名返回能服务该模型的全部候选端点（已按侧栏
// sort_order 升序排序；同 order 内按创建时间稳定）。优先使用管理面板 x-endpoint-id
// 指定的端点（若它也服务该模型则仅返回它一个，强制指定时不参与 failover）。
// sessionKey 非空时优先复用该会话最近成功使用的端点（Channel Affinity）。
// chosen 为调用方实际首选使用的端点（主 key 端），index 为 chosen 在返回切片中的下标。
func (s *Service) selectEndpointCandidates(ctx context.Context, db *sql.DB, model, targetEndpointID, sessionKey string) (candidates []Endpoint, chosen Endpoint, chosenIndex int, selectedModel string, found bool) {
	selectedModel = model

	loadEndpoint := func(ep *Endpoint, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds int) {
		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
		ep.KeyRetryRounds = keyRetryRounds
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}
		if modelsRaw.Valid {
			_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
		}
		ep.Headers = []HeaderItem{}
		if headersRaw.Valid && headersRaw.String != "" {
			_ = json.Unmarshal([]byte(headersRaw.String), &ep.Headers)
		}
		ep.DisabledModels = []string{}
		if disabledRaw.Valid && disabledRaw.String != "" {
			_ = json.Unmarshal([]byte(disabledRaw.String), &ep.DisabledModels)
		}
		ep.ProxyPool = []string{}
		if proxyRaw.Valid && proxyRaw.String != "" {
			_ = json.Unmarshal([]byte(proxyRaw.String), &ep.ProxyPool)
		}
	}

	if targetEndpointID != "" {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds int
		err := db.QueryRowContext(ctx, `
			SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings, key_retry_rounds
			FROM openai_endpoints WHERE id = ? AND enabled = 1`, targetEndpointID).
			Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw, &keyRetryRounds)
		if err == nil {
			loadEndpoint(&ep, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds)
			if s.endpointHasModel(ep, model) {
				if real, routable := s.resolveEndpointModel(ep, model); routable {
					selectedModel = real
					return []Endpoint{ep}, ep, 0, selectedModel, true
				}
			}
		}
	}

	// Enabled is the administrator's routing decision; status is the latest verification result.
	endpoints, routeModelIndex, err := s.enabledEndpointSnapshot(ctx, db)
	if err != nil {
		return nil, chosen, -1, selectedModel, false
	}

	// 使用预构建的内存倒排索引（Ability）：按模型名直接定位候选端点下标，
	// 避免每请求遍历全部已启用端点。索引中的端点可能已被禁用模型（disabled_models），
	// 仍需二次过滤，故在候选组装时统一做禁用模型检查。
	modelIdx := make([]int, 0, 4)
	if routeModelIndex != nil {
		modelIdx = append(modelIdx, routeModelIndex[model]...)
	}

	if len(modelIdx) > 0 {
		for _, idx := range modelIdx {
			if idx < 0 || idx >= len(endpoints) {
				continue
			}
			ep := endpoints[idx]
			if _, routable := s.resolveEndpointModel(ep, model); routable {
				candidates = append(candidates, ep)
			}
		}
	}
	if len(candidates) == 0 {
		// 兜底：索引未命中（模型列表尚未刷新/模型名未收录）时按原逻辑遍历全部端点。
		for _, ep := range endpoints {
			if s.endpointHasModel(ep, model) {
				if _, routable := s.resolveEndpointModel(ep, model); routable {
					candidates = append(candidates, ep)
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil, chosen, -1, selectedModel, false
	}

	// 会话亲和（Channel Affinity）：同一会话上次成功使用的端点优先复用（若仍在候选池）。
	if sessionKey != "" {
		if affinityID := s.preferredAffinityEndpoint(sessionKey); affinityID != "" {
			if idx := affinityEndpointIndex(affinityID, candidates); idx > 0 {
				cand := candidates[idx]
				candidates = append(candidates[:idx], candidates[idx+1:]...)
				candidates = append([]Endpoint{cand}, candidates...)
			}
		}
	}

	// 按侧栏顺序稳定的候选列表；但首选在健康端中按延迟加权挑选，
	// 以保持原有「快的端点优先」行为，同时整体列表仍按 sort_order 渐变。
	latencies := make([]int64, len(candidates))
	known := make([]bool, len(candidates))
	weights := make([]int64, len(candidates))
	for i, ep := range candidates {
		latencies[i], known[i] = s.getEndpointLatency(ep.ID)
		weights[i] = endpointWeight(ep)
	}
	chosenIndex = weightedEndpointPickWeighted(latencies, known, weights)
	// 测试专用确定性选路钩子（生产恒为 nil）：覆盖延迟加权随机，
	// 供依赖「端点 A 先被选中」的 failover 测试消除 flake。
	if endpointPickOverride != nil {
		if i := endpointPickOverride(candidates); i >= 0 && i < len(candidates) {
			chosenIndex = i
		}
	}
	chosen = candidates[chosenIndex]
	if real, routable := s.resolveEndpointModel(chosen, model); routable {
		selectedModel = real
	}
	return candidates, chosen, chosenIndex, selectedModel, true
}

func parseModelIDsFromRaw(raw string) []string {
	if raw == "" {
		return nil
	}
	var strList []string
	if err := json.Unmarshal([]byte(raw), &strList); err == nil && len(strList) > 0 {
		out := make([]string, 0, len(strList))
		for _, s := range strList {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	var anyList []interface{}
	if err := json.Unmarshal([]byte(raw), &anyList); err == nil {
		out := make([]string, 0, len(anyList))
		for _, item := range anyList {
			switch v := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					out = append(out, trimmed)
				}
			case map[string]interface{}:
				if id, ok := v["id"].(string); ok {
					if trimmed := strings.TrimSpace(id); trimmed != "" {
						out = append(out, trimmed)
					}
				} else if name, ok := v["name"].(string); ok {
					if trimmed := strings.TrimSpace(name); trimmed != "" {
						out = append(out, trimmed)
					}
				}
			}
		}
		return out
	}
	return nil
}
