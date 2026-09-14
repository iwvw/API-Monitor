package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) updateModelMappings(w http.ResponseWriter, r *http.Request, endpointID string) {
	ctx := r.Context()
	var payload struct {
		Mappings map[string]string `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	clean := map[string]string{}
	for real, alias := range payload.Mappings {
		real = strings.TrimSpace(real)
		alias = strings.TrimSpace(alias)
		if real != "" && alias != "" {
			clean[real] = alias
		}
	}
	data, _ := json.Marshal(clean)
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "UPDATE openai_endpoints SET model_mappings = ? WHERE id = ?", string(data), endpointID); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "modelMappings": clean})
}

// updateEndpointRouting 保存端点的路由优先级/权重（对齐 model-mappings 的局部更新模式）：
// 只更新 priority 与 weight 两列，不影响端点其他配置；返回更新后的值供前端回填。
func (s *Service) updateEndpointRouting(w http.ResponseWriter, r *http.Request, endpointID string) {
	ctx := r.Context()
	var payload struct {
		Priority *int `json:"priority"`
		Weight   *int `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	if payload.Priority == nil && payload.Weight == nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "priority 或 weight 至少提供一个"})
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var exists int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", endpointID).Scan(&exists); err != nil || exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	var currentPriority, currentWeight int
	_ = db.QueryRowContext(ctx, "SELECT priority, weight FROM openai_endpoints WHERE id = ?", endpointID).Scan(&currentPriority, &currentWeight)
	priority := currentPriority
	if payload.Priority != nil {
		priority = *payload.Priority
	}
	weight := currentWeight
	if weight <= 0 {
		weight = 100
	}
	if payload.Weight != nil {
		weight = *payload.Weight
	}
	if _, err := db.ExecContext(ctx, "UPDATE openai_endpoints SET priority = ?, weight = ? WHERE id = ?", priority, weight, endpointID); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "priority": priority, "weight": weight})
}

func (s *Service) listEndpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, models_url, created_at, last_used, last_checked, model_mappings, sort_order, priority, weight, key_retry_rounds, plugin_id, proxy_pool_id, upstream_type FROM openai_endpoints ORDER BY priority DESC, weight DESC, sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, batchesRaw, mappingsRaw, protocolRaw, apiKeysRaw, pricingRaw, modelsUrlRaw, pluginIDRaw, proxyPoolIDRaw, upstreamTypeRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, sortOrder, priority, weight, keyRetryRounds int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &batchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &rateLimitRetryInt, &rateLimitRetryWaitSeconds, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &pricingRaw, &modelsUrlRaw, &created, &used, &checked, &mappingsRaw, &sortOrder, &priority, &weight, &keyRetryRounds, &pluginIDRaw, &proxyPoolIDRaw, &upstreamTypeRaw)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ep.PluginID = pluginIDRaw.String
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
		ep.Priority = priority
		ep.Weight = weight
		ep.KeyRetryRounds = keyRetryRounds
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
		}
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}

		ep.Models = []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
		}
		ep.Pricing = PricingMap{}
		if pricingRaw.Valid && pricingRaw.String != "" {
			_ = json.Unmarshal([]byte(pricingRaw.String), &ep.Pricing)
		}
		ep.ModelsURL = modelsUrlRaw.String
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
		ep.ProxyBatches = []ProxyBatch{}
		if batchesRaw.Valid && batchesRaw.String != "" {
			_ = json.Unmarshal([]byte(batchesRaw.String), &ep.ProxyBatches)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, endpoints)
}

func (s *Service) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                      string       `json:"name"`
		BaseURL                   string       `json:"baseUrl"`
		ModelsURL                 string       `json:"modelsUrl"`
		APIKey                    string       `json:"apiKey"`
		APIKeys                   []string     `json:"apiKeys"`
		Notes                     string       `json:"notes"`
		Headers                   []HeaderItem `json:"headers"`
		ProxyPool                 []string     `json:"proxyPool"`
		ProxyBatches              []ProxyBatch `json:"proxyBatches"`
		AutoSwitch                bool         `json:"autoSwitch"`
		ProxyEnabled              bool         `json:"proxyEnabled"`
		ForceProxy                bool         `json:"forceProxy"`
		RateLimitRetryEnabled     *bool        `json:"rateLimitRetryEnabled"`
		RateLimitRetryWaitSeconds *int         `json:"rateLimitRetryWaitSeconds"`
		KeyRetryRounds            *int         `json:"keyRetryRounds"`
		Protocol                  string       `json:"protocol"`
		UpstreamType              string       `json:"upstreamType"`
		SkipVerify                bool         `json:"skipVerify"`
		// ProxyPoolID 引用独立代理池插件（/api/proxypool）中的池；空串表示不引用。
		ProxyPoolID string `json:"proxyPoolId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "名称与 API 地址必填"})
		return
	}

	// Gemini / Vertex 上游 baseURL 不追加 OpenAI 风格的 /v1 版本路径：
	// Gemini 在 /v1beta 下，Vertex 的 baseURL 已含 /v1/publishers/google（按区域填写）。
	var normalizedURL string
	switch normalizeUpstreamType(req.UpstreamType) {
	case upstreamTypeGemini:
		normalizedURL = normalizeGeminiBaseURL(req.BaseURL)
	case upstreamTypeVertex:
		normalizedURL = normalizeVertexBaseURL(req.BaseURL)
	default:
		normalizedURL = s.normalizeBaseURL(req.BaseURL)
	}
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	headersJSON, _ := json.Marshal(cleanHeaders(req.Headers))
	batchesJSON, _ := json.Marshal(cleanProxyBatches(req.ProxyBatches))
	// 运行时只消费 proxy_pool：无论客户端是否已合并，都保证池 = 手动代理 ∪ 全部批次代理。
	proxyJSON, _ := json.Marshal(mergeProxyPoolWithBatches(req.ProxyPool, req.ProxyBatches))
	autoSwitchInt := boolToInt(req.AutoSwitch)
	// 未显式设置时默认开启（对低 RPM 端点有净收益；仅 429 且预算内才等待，不影响健康请求）。
	rateLimitRetryInt := 1
	if req.RateLimitRetryEnabled != nil && !*req.RateLimitRetryEnabled {
		rateLimitRetryInt = 0
	}
	// 未显式设置等待秒数时用默认 10s（无 Retry-After 响应时的缺省配额恢复窗口）。
	rateLimitRetryWaitSeconds := 10
	if req.RateLimitRetryWaitSeconds != nil && *req.RateLimitRetryWaitSeconds >= 1 {
		rateLimitRetryWaitSeconds = *req.RateLimitRetryWaitSeconds
	}
	// 每 key 单请求尝试次数：未显式设置时默认 2（多 key 轮询循环两遍）。
	keyRetryRounds := defaultKeyRetryRounds
	if req.KeyRetryRounds != nil && *req.KeyRetryRounds >= 1 {
		keyRetryRounds = *req.KeyRetryRounds
	}
	protocol := normalizeProtocol(req.Protocol)
	upstreamType := normalizeUpstreamType(req.UpstreamType)

	id := fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
	status := "unknown"
	modelsList := []string{}
	pricing := PricingMap{}
	var verification map[string]interface{}

	if !req.SkipVerify && req.APIKey != "" {
		// 验证与拉取模型加总超时：挂死的出口/上游不能把保存拖成「等超时」。
		verifyCtx, cancelVerify := context.WithTimeout(ctx, endpointVerifyTimeout)
		vOk, count, err := s.verifyAPIKeyRaw(verifyCtx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), req.ModelsURL, upstreamType, cleanHeaders(req.Headers))
		if err == nil && vOk {
			status = "valid"
			verification = map[string]interface{}{
				"valid":       true,
				"modelsCount": count,
			}
			mList, mPrice, mErr := s.listModelsWithPricing(verifyCtx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), req.ModelsURL, upstreamType, cleanHeaders(req.Headers))
			if mErr == nil {
				modelsList = mList
				pricing = mPrice
			}
		} else {
			status = "invalid"
			errMsg := "API Key 验证失败"
			if err != nil {
				errMsg = err.Error()
			}
			verification = map[string]interface{}{
				"valid": false,
				"error": errMsg,
			}
		}
		cancelVerify()
	}

	modelsJSON, _ := json.Marshal(modelsList)
	pricingJSON, _ := json.Marshal(pricing)
	createdAt := time.Now().Format(time.RFC3339)
	var lastCheckedVal interface{} = nil
	if !req.SkipVerify {
		lastCheckedVal = createdAt
	}

	encryptedKey, err := secure.SecureEncrypt(req.APIKey)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
		return
	}
	apiKeysJSON, _ := json.Marshal(req.APIKeys)
	encryptedAPIKeys, err := secure.SecureEncrypt(string(apiKeysJSON))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "扩展 Key 加密失败"})
		return
	}
	proxyPoolID := strings.TrimSpace(req.ProxyPoolID)
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, models_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, created_at, last_checked, sort_order, proxy_pool_id, key_retry_rounds, upstream_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, normalizedURL, strings.TrimSpace(req.ModelsURL), encryptedKey, encryptedAPIKeys, string(headersJSON), "[]", string(proxyJSON), string(batchesJSON), autoSwitchInt, boolToInt(req.ProxyEnabled), boolToInt(req.ForceProxy), rateLimitRetryInt, rateLimitRetryWaitSeconds, protocol, status, 1, string(modelsJSON), string(pricingJSON), createdAt, lastCheckedVal, time.Now().UnixMilli(), proxyPoolID, keyRetryRounds, upstreamType)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var checkedStr *string
	if !req.SkipVerify {
		checkedStr = &createdAt
	}

	resEndpoint := Endpoint{
		ID:                        id,
		Name:                      req.Name,
		BaseURL:                   normalizedURL,
		ModelsURL:                 strings.TrimSpace(req.ModelsURL),
		APIKey:                    req.APIKey,
		Notes:                     req.Notes,
		Headers:                   cleanHeaders(req.Headers),
		ProxyPool:                 mergeProxyPoolWithBatches(req.ProxyPool, req.ProxyBatches),
		ProxyBatches:              cleanProxyBatches(req.ProxyBatches),
		AutoSwitch:                req.AutoSwitch,
		RateLimitRetryEnabled:     rateLimitRetryInt == 1,
		RateLimitRetryWaitSeconds: rateLimitRetryWaitSeconds,
		KeyRetryRounds:            keyRetryRounds,
		Protocol:                  protocol,
		UpstreamType:              upstreamType,
		ProxyPoolID:               proxyPoolID,
		Status:                    status,
		Enabled:                   true,
		Models:                    modelsList,
		Pricing:                   pricing,
		CreatedAt:                 createdAt,
		LastChecked:               checkedStr,
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"endpoint":     resEndpoint,
		"verification": verification,
	})
}

func (s *Service) toggleEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var exists int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", id).Scan(&exists)
	if err != nil || exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	enabledVal := 0
	if req.Enabled {
		enabledVal = 1
	}

	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET enabled = ? WHERE id = ?", enabledVal, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "enabled": req.Enabled})
}

// archiveEndpointName 将端点名称写入历史归档表，删除后 analytics 等历史记录仍能展示原名称。
// 幂等：同一 id 重复归档时更新为最近一次删除时的名称。exec 兼容 *sql.DB / *sql.Tx。
func (s *Service) archiveEndpointName(ctx context.Context, exec sqlExec, id, name string) {
	if id == "" || name == "" {
		return
	}
	_, _ = exec.ExecContext(ctx, `
		INSERT INTO openai_endpoint_name_archive (endpoint_id, name) VALUES (?, ?)
		ON CONFLICT(endpoint_id) DO UPDATE SET name = excluded.name, deleted_at = CURRENT_TIMESTAMP
	`, id, name)
}

func (s *Service) deleteEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var name string
	err = db.QueryRowContext(ctx, "SELECT name FROM openai_endpoints WHERE id = ?", id).Scan(&name)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	res, err := db.ExecContext(ctx, "DELETE FROM openai_endpoints WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rows, err := res.RowsAffected()
	if err != nil || rows == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	s.archiveEndpointName(ctx, db, id, name)
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// reorderEndpoints 保存端点拖拽排序结果：按传入顺序重写 sort_order。
// 使用事务保证全部成功或全部失败；仅校验 id 存在，不要求全部端点都在列表内。
func (s *Service) reorderEndpoints(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EndpointIDs []string `json:"endpointIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(req.EndpointIDs) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "endpointIds 不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	for idx, endpointID := range req.EndpointIDs {
		res, err := tx.ExecContext(ctx,
			"UPDATE openai_endpoints SET sort_order = ? WHERE id = ?",
			(idx+1)*1000, endpointID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			response.JSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("端点不存在: %s", endpointID)})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
