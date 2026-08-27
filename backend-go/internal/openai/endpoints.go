package openai

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, models_url, created_at, last_used, last_checked, model_mappings, sort_order, priority, weight FROM openai_endpoints ORDER BY priority DESC, weight DESC, sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, batchesRaw, mappingsRaw, protocolRaw, apiKeysRaw, pricingRaw, modelsUrlRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, sortOrder, priority, weight int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &batchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &rateLimitRetryInt, &rateLimitRetryWaitSeconds, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &pricingRaw, &modelsUrlRaw, &created, &used, &checked, &mappingsRaw, &sortOrder, &priority, &weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

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
		Name         string       `json:"name"`
		BaseURL      string       `json:"baseUrl"`
		ModelsURL    string       `json:"modelsUrl"`
		APIKey       string       `json:"apiKey"`
		APIKeys      []string     `json:"apiKeys"`
		Notes        string       `json:"notes"`
		Headers      []HeaderItem `json:"headers"`
		ProxyPool    []string     `json:"proxyPool"`
		ProxyBatches []ProxyBatch `json:"proxyBatches"`
		AutoSwitch   bool         `json:"autoSwitch"`
		ProxyEnabled bool         `json:"proxyEnabled"`
		ForceProxy   bool         `json:"forceProxy"`
		RateLimitRetryEnabled *bool `json:"rateLimitRetryEnabled"`
		RateLimitRetryWaitSeconds *int `json:"rateLimitRetryWaitSeconds"`
		Protocol     string       `json:"protocol"`
		SkipVerify   bool         `json:"skipVerify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "名称与 API 地址必填"})
		return
	}

	normalizedURL := s.normalizeBaseURL(req.BaseURL)
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
	protocol := normalizeProtocol(req.Protocol)

	id := fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
	status := "unknown"
	modelsList := []string{}
	pricing := PricingMap{}
	var verification map[string]interface{}

	if !req.SkipVerify && req.APIKey != "" {
		// 验证与拉取模型加总超时：挂死的出口/上游不能把保存拖成「等超时」。
		verifyCtx, cancelVerify := context.WithTimeout(ctx, endpointVerifyTimeout)
		vOk, count, err := s.verifyAPIKeyRaw(verifyCtx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), req.ModelsURL, cleanHeaders(req.Headers))
		if err == nil && vOk {
			status = "valid"
			verification = map[string]interface{}{
				"valid":       true,
				"modelsCount": count,
			}
			mList, mPrice, mErr := s.listModelsWithPricing(verifyCtx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), req.ModelsURL, cleanHeaders(req.Headers))
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
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, models_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, created_at, last_checked, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, normalizedURL, strings.TrimSpace(req.ModelsURL), encryptedKey, encryptedAPIKeys, string(headersJSON), "[]", string(proxyJSON), string(batchesJSON), autoSwitchInt, boolToInt(req.ProxyEnabled), boolToInt(req.ForceProxy), rateLimitRetryInt, rateLimitRetryWaitSeconds, protocol, status, 1, string(modelsJSON), string(pricingJSON), createdAt, lastCheckedVal, time.Now().UnixMilli())
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
		Protocol:                  protocol,
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

// sqlExec 抽象 ExecContext，供事务与直连共用。
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
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

func (s *Service) verifyEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// 验证类请求使用独立短超时，避免上游无响应时拖住整个操作。
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer verifyCancel()

	var name, baseURL, apiKey string
	var modelsURL string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT name, base_url, models_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&name, &baseURL, &modelsURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	headers := decodeEndpointHeaders(headersRaw)
	pool := decodeProxyPool(proxyRaw)

	startTime := time.Now()
	status := "invalid"
	modelsList := []string{}
	pricing := PricingMap{}
	var errMsg string

	vOk, _, vErr := s.verifyAPIKeyRaw(verifyCtx, baseURL, apiKey, id, pool, modelsURL, headers)
	responseTime := time.Since(startTime).Milliseconds()

	if vErr == nil && vOk {
		status = "valid"
		mList, mPrice, mErr := s.listModelsWithPricing(verifyCtx, baseURL, apiKey, id, pool, modelsURL, headers)
		if mErr == nil {
			modelsList = mList
			pricing = mPrice
		}
	} else if vErr != nil {
		errMsg = vErr.Error()
	}

	checkedAt := time.Now().Format(time.RFC3339)

	// 验证失败时保留旧的模型列表：一次超时/临时网络故障不应清空已获取的模型。
	modelsJSON := "[]"
	if status == "valid" && len(modelsList) > 0 {
		modelsJSONBytes, _ := json.Marshal(modelsList)
		modelsJSON = string(modelsJSONBytes)
	} else if status == "valid" {
		// 验证成功但返回空列表：视为真实空（首次接入或上游确实无模型）。
		modelsJSON = "[]"
	}

	pricingJSON := "{}"
	if len(pricing) > 0 {
		pricingBytes, _ := json.Marshal(pricing)
		pricingJSON = string(pricingBytes)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, models = ?, pricing = ?, last_checked = ?
		WHERE id = ?`,
		status, modelsJSON, pricingJSON, checkedAt, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	res := map[string]interface{}{
		"status":       status,
		"responseTime": responseTime,
		"modelsCount":  len(modelsList),
		"models":       modelsList,
		"checkedAt":    checkedAt,
		"valid":        status == "valid",
	}
	if errMsg != "" {
		res["error"] = errMsg
	}

	response.JSON(w, http.StatusOK, res)
}

// KeyCheckResult 描述一次 API Key 校验结果：status 取值 valid/invalid/overdue/error。
type KeyCheckResult struct {
	Index      int    `json:"index"`
	Key        string `json:"key"`
	Status     string `json:"status"`
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`
}

// healthCheckKeysRoute 对端点配置的多个 API Key 逐个做有效性检测（GET /models）。
// 用于端点编辑弹窗里的 key 管理：进入弹窗时自动刷新状态，快速识别失效/欠费 key。
func (s *Service) healthCheckKeysRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Keys    []string `json:"keys"`
		Timeout int      `json:"timeout"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	keys := make([]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "keys 不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL string
	var modelsURL string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &modelsURL, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	timeout := 8 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}
	pool := decodeProxyPool(proxyRaw)
	headers := decodeEndpointHeaders(headersRaw)

	results := make([]KeyCheckResult, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			results[idx] = s.checkAPIKeyStatus(ctx, baseURL, k, id, timeout, pool, modelsURL, headers, idx)
		}(i, key)
	}
	wg.Wait()

	response.JSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// checkAPIKeyStatus 用 GET {baseURL}/models 检测单个 key 的有效性。
// 2xx=valid；401/403=invalid（鉴权失败）；402=overdue（欠费）；其余/网络错误=error。
func (s *Service) checkAPIKeyStatus(ctx context.Context, baseURL, key, endpointID string, timeout time.Duration, pool []string, modelsURL string, headers []HeaderItem, index int) KeyCheckResult {
	result := KeyCheckResult{Index: index, Key: key}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reqURL := modelListURL(baseURL, modelsURL)
	httpReq, err := http.NewRequestWithContext(childCtx, "GET", reqURL, nil)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	applyCustomHeaders(httpReq, headers)

	client := s.client
	if len(pool) > 0 {
		if poolClient, _ := s.auxClientForPool(endpointID, pool); poolClient != nil {
			client = poolClient
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		result.Status = "valid"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		result.Status = "invalid"
		result.Message = "鉴权失败"
	case resp.StatusCode == http.StatusPaymentRequired:
		result.Status = "overdue"
		result.Message = "欠费"
	default:
		result.Status = "error"
		result.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

func (s *Service) getEndpointModels(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// 模型列表拉取使用独立短超时，避免上游无响应时拖住整个操作。
	modelsCtx, modelsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer modelsCancel()

	var baseURL, apiKey string
	var modelsURL string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &modelsURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	modelsList, pricing, err := s.listModelsWithPricing(modelsCtx, baseURL, apiKey, id, decodeProxyPool(proxyRaw), modelsURL, decodeEndpointHeaders(headersRaw))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	modelsJSON, _ := json.Marshal(modelsList)
	pricingJSON, _ := json.Marshal(pricing)
	checkedAt := time.Now().Format(time.RFC3339)

	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET models = ?, pricing = ?, last_checked = ? WHERE id = ?", string(modelsJSON), string(pricingJSON), checkedAt, id)
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"models":  modelsList,
	})
}

func (s *Service) toggleEndpointModel(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model   string `json:"model"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型名称必填"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var disabledRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM openai_endpoints WHERE id = ?", id).Scan(&disabledRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	disabled := []string{}
	if disabledRaw.Valid && disabledRaw.String != "" {
		_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
	}

	disabledSet := make(map[string]bool, len(disabled)+1)
	for _, m := range disabled {
		disabledSet[m] = true
	}
	if req.Enabled {
		delete(disabledSet, model)
	} else {
		disabledSet[model] = true
	}

	next := make([]string, 0, len(disabledSet))
	for m := range disabledSet {
		next = append(next, m)
	}
	sort.Strings(next)

	disabledJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"model":          model,
		"enabled":        req.Enabled,
		"disabledModels": next,
	})
}

// toggleEndpointModelsBatch 批量启用/停用端点上的多个模型。
// 单次「读-改-写」原子完成，避免前端并发逐个 toggle 时互相覆盖丢失。
func (s *Service) toggleEndpointModelsBatch(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Models  []string `json:"models"`
		Enabled bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cleaned := make([]string, 0, len(req.Models))
	seen := make(map[string]bool, len(req.Models))
	for _, m := range req.Models {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型列表不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var disabledRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM openai_endpoints WHERE id = ?", id).Scan(&disabledRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	disabledSet := make(map[string]bool)
	if disabledRaw.Valid && disabledRaw.String != "" {
		var existing []string
		if err := json.Unmarshal([]byte(disabledRaw.String), &existing); err == nil {
			for _, m := range existing {
				disabledSet[m] = true
			}
		}
	}
	for _, m := range cleaned {
		if req.Enabled {
			delete(disabledSet, m)
		} else {
			disabledSet[m] = true
		}
	}

	next := make([]string, 0, len(disabledSet))
	for m := range disabledSet {
		next = append(next, m)
	}
	sort.Strings(next)

	disabledJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"enabled":        req.Enabled,
		"disabledModels": next,
	})
}

// subscriptionProxy 描述一个可从订阅节点导入到端点代理池的 socks/http 出口。
type subscriptionProxy struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Proxy    string `json:"proxy"`
	Location string `json:"location,omitempty"`
}

// listSubscriptionSocksProxies 读取订阅板块中的 socks/http 协议节点，转换为可直接使用的代理 URL。
func (s *Service) listSubscriptionSocksProxies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,''), COALESCE(type,''), COALESCE(server,''), COALESCE(port,0), COALESCE(location,''), COALESCE(raw_encrypted,'') FROM subscription_nodes WHERE enabled = 1`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for rows.Next() {
		var item subscriptionProxy
		var rawEnc string
		if err := rows.Scan(&item.NodeID, &item.Name, &item.Type, &item.Server, &item.Port, &item.Location, &rawEnc); err != nil {
			continue
		}
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		if item.Type != "socks" && item.Type != "socks5" && item.Type != "http" && item.Type != "https" {
			continue
		}
		raw := secure.SecureDecrypt(rawEnc)
		proxy, name, ok := convertNodeToProxy(item.Type, raw, item.Server, item.Port, item.Name)
		if !ok || proxy == "" {
			continue
		}
		item.Proxy = proxy
		item.Name = name
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, item)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// importProxyListRoute 解析用户上传的代理列表文本（例如 .txt 文件内容，每行一个代理），
// 清洗并去重后返回可直接写入端点代理池的代理 URL 列表及统计。
// 支持 http(s)://、socks5:// 与裸 host:port；也兼容 base64 编码的订阅文本。
func (s *Service) importProxyListRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	const maxImportBytes = 16 * 1024 * 1024
	if len(req.Text) > maxImportBytes {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "代理列表过大（上限 16MB）"})
		return
	}

	proxies := parseSubscriptionProxyText(req.Text)
	urls := make([]string, 0, len(proxies))
	for _, p := range proxies {
		urls = append(urls, p.Proxy)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(urls),
		"proxies": urls,
	})
}

// resolveSubscriptionProxies 拉取用户粘贴的订阅链接，解析其中的 socks/http 节点，
// 转换为可直接写入端点代理池的代理 URL。
func (s *Service) resolveSubscriptionProxies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请填写订阅链接"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(req.URL), "http://") && !strings.HasPrefix(strings.ToLower(req.URL), "https://") {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "订阅链接必须以 http:// 或 https:// 开头"})
		return
	}

	ctx := r.Context()
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, req.URL, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "构造请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "API-Monitor-OpenAI/1.0")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": "拉取订阅失败: " + err.Error()})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("订阅源返回 HTTP %d", resp.StatusCode)})
		return
	}

	proxies := parseSubscriptionProxyText(string(body))
	if len(proxies) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"proxies": []subscriptionProxy{},
			"message": "订阅内容中没有找到 socks/http 节点",
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// parseSubscriptionProxyText 解析订阅内容（可能是 base64 节点列表或纯文本），
// 提取其中的 socks/socks5/http/https 节点为代理 URL。
func parseSubscriptionProxyText(content string) []subscriptionProxy {
	text := strings.TrimSpace(content)
	lines := []string{}
	// 尝试 base64 解码：订阅常见格式是 base64 编码的每行一个节点 URI。
	decoded := decodeBase64Text(text)
	if decoded != "" {
		text = decoded
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for _, line := range lines {
		proxy, name, ok := convertNodeToProxy("", line, "", 0, "")
		if !ok || proxy == "" {
			continue
		}
		// 仅接受 socks/http 出口；裸 server:port 也归为 socks5 出口。
		scheme := proxy
		if idx := strings.Index(proxy, "://"); idx >= 0 {
			scheme = strings.ToLower(proxy[:idx])
		}
		if scheme != "socks5" && scheme != "http" && scheme != "https" {
			continue
		}
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, subscriptionProxy{
			Name:   name,
			Type:   scheme,
			Proxy:  proxy,
			Server: hostFromProxyURL(proxy),
		})
	}
	return proxies
}

// decodeBase64Text 尝试将内容按 base64 解码；成功且结果可读时返回解码文本。
func decodeBase64Text(text string) string {
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, text)
	raw, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return ""
	}
	decoded := string(raw)
	if strings.Contains(decoded, "://") || strings.Contains(decoded, "vmess://") || strings.Contains(decoded, "trojan://") || strings.Contains(decoded, "ss://") {
		return decoded
	}
	return ""
}

// hostFromProxyURL 从代理 URL 中提取 host 部分用于展示。
func hostFromProxyURL(proxy string) string {
	u, err := url.Parse(proxy)
	if err != nil {
		return proxy
	}
	return u.Host
}

// convertNodeToProxy 把订阅节点 raw URI 转换为网关可用的 socks5/http 代理 URL。
// 优先复用 raw 中的用户凭据；raw 无法解析时回退为 server:port。
func convertNodeToProxy(nodeType, raw, server string, port int, fallbackName string) (proxy, name string, ok bool) {
	name = strings.TrimSpace(fallbackName)
	if name == "" {
		name = server
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "socks://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "socks5://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "http://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		u, err := url.Parse(trimmed)
		if err == nil && u.Host != "" {
			scheme := u.Scheme
			if scheme == "socks" || scheme == "socks5" {
				scheme = "socks5"
			}
			u.Scheme = scheme
			// 去掉 fragment（节点名常放在 # 后）。
			u.Fragment = ""
			if fragName := parseNodeFragment(trimmed); fragName != "" {
				name = fragName
			}
			return u.String(), name, true
		}
	}
	// 无 raw 或解析失败：直接用 server:port 构造成 socks5。
	if server == "" || port < 1 || port > 65535 {
		return "", name, false
	}
	return fmt.Sprintf("socks5://%s", net.JoinHostPort(server, strconv.Itoa(port))), name, true
}

func parseNodeFragment(raw string) string {
	idx := strings.LastIndex(raw, "#")
	if idx < 0 || idx+1 >= len(raw) {
		return ""
	}
	name := strings.TrimSpace(raw[idx+1:])
	name = strings.Trim(name, "\"")
	return name
}

func (s *Service) refreshAllEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	results, err := s.refreshAllModels(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "results": results})
}

// refreshAllModels 并发刷新所有启用端点的上游模型列表并写库：逐个验证 API Key 后
// 拉取 /v1/models。验证/取模型失败时保留旧模型列表（一次超时/临时故障不应清空
// 已获取的模型）。供人工刷新路由（refreshAllEndpointsRoute）与后台定时刷新
// （StartModelAutoRefresh）复用；返回逐端点结果供调用方汇总。
func (s *Service) refreshAllModels(ctx context.Context) (results []map[string]interface{}, err error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, models_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type item struct {
		id, name, url, key, modelsURL string
		headers                       []HeaderItem
		pool                          []string
	}
	items := []item{}
	for rows.Next() {
		var it item
		var headersRaw, proxyRaw sql.NullString
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.modelsURL, &it.key, &headersRaw, &proxyRaw); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			it.headers = decodeEndpointHeaders(headersRaw)
			it.pool = decodeProxyPool(proxyRaw)
			items = append(items, it)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results = []map[string]interface{}{}

	for _, it := range items {
		wg.Add(1)
		go func(it item) {
			defer wg.Done()
			status := "invalid"
			modelsList := []string{}
			pricing := PricingMap{}
			var errStr string

			vOk, _, err := s.verifyAPIKeyRaw(ctx, it.url, it.key, it.id, it.pool, it.modelsURL, it.headers)
			if err == nil && vOk {
				status = "valid"
				mList, mPrice, mErr := s.listModelsWithPricing(ctx, it.url, it.key, it.id, it.pool, it.modelsURL, it.headers)
				if mErr == nil {
					modelsList = mList
					pricing = mPrice
				}
			} else if err != nil {
				errStr = err.Error()
			}

			checkedAt := time.Now().Format(time.RFC3339)
			// 验证失败时保留旧模型列表：一次超时/临时故障不应清空已获取的模型。
			modelsJSON := "[]"
			if status == "valid" && len(modelsList) > 0 {
				modelsJSONBytes, _ := json.Marshal(modelsList)
				modelsJSON = string(modelsJSONBytes)
			}

			pricingJSON := "{}"
			if len(pricing) > 0 {
				pricingBytes, _ := json.Marshal(pricing)
				pricingJSON = string(pricingBytes)
			}

			// Update in DB
			if dbConn, dbErr := s.open(ctx); dbErr == nil {
				defer dbConn.Close()
				_, _ = dbConn.ExecContext(ctx, `
					UPDATE openai_endpoints
					SET status = ?, models = ?, pricing = ?, last_checked = ?
					WHERE id = ?`,
					status, modelsJSON, pricingJSON, checkedAt, it.id)
			}

			mu.Lock()
			res := map[string]interface{}{
				"id":          it.id,
				"name":        it.name,
				"success":     status == "valid",
				"modelsCount": len(modelsList),
			}
			if errStr != "" {
				res["error"] = errStr
			}
			results = append(results, res)
			mu.Unlock()
		}(it)
	}

	wg.Wait()
	return results, nil
}

func (s *Service) exportEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, pricing, models_url, created_at, last_used, last_checked, model_mappings, priority, weight FROM openai_endpoints ORDER BY sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, proxyBatchesRaw, mappingsRaw, apiKeysRaw, protocolRaw, pricingRaw, modelsUrlRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, priority, weight int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &proxyBatchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &pricingRaw, &modelsUrlRaw, &created, &used, &checked, &mappingsRaw, &priority, &weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
		ep.Priority = priority
		ep.Weight = weight
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
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
		if proxyBatchesRaw.Valid && proxyBatchesRaw.String != "" {
			_ = json.Unmarshal([]byte(proxyBatchesRaw.String), &ep.ProxyBatches)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"endpoints":  endpoints,
		"exportedAt": time.Now().Format(time.RFC3339),
	})
}

func (s *Service) importEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoints []Endpoint `json:"endpoints"`
		Overwrite bool       `json:"overwrite"`
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	if req.Overwrite {
		// 覆盖导入前先归档现有端点名称，保留被替换端点的历史名称展示。
		if epRows, qErr := tx.QueryContext(ctx, "SELECT id, name FROM openai_endpoints"); qErr == nil {
			type namedEndpoint struct{ id, name string }
			all := []namedEndpoint{}
			for epRows.Next() {
				var ne namedEndpoint
				if err := epRows.Scan(&ne.id, &ne.name); err == nil {
					all = append(all, ne)
				}
			}
			epRows.Close()
			for _, ne := range all {
				s.archiveEndpointName(ctx, tx, ne.id, ne.name)
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM openai_endpoints")
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	importedCount := 0
	skippedCount := 0

	for _, ep := range req.Endpoints {
		if ep.Name == "" || ep.BaseURL == "" || ep.APIKey == "" {
			skippedCount++
			continue
		}

		if !req.Overwrite {
			var exists int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE base_url = ?", ep.BaseURL).Scan(&exists)
			if exists > 0 {
				skippedCount++
				continue
			}
		}

		id := ep.ID
		if id == "" {
			id = fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
		}

		enabledInt := 0
		if ep.Enabled {
			enabledInt = 1
		}
		status := ep.Status
		if status == "" {
			status = "unknown"
		}
		createdAt := ep.CreatedAt
		if createdAt == "" {
			createdAt = time.Now().Format(time.RFC3339)
		}
		var modelsJSON []byte
		if len(ep.Models) > 0 {
			modelsJSON, _ = json.Marshal(ep.Models)
		} else {
			modelsJSON = []byte("[]")
		}
		var pricingJSON []byte
		if len(ep.Pricing) > 0 {
			pricingJSON, _ = json.Marshal(ep.Pricing)
		} else {
			pricingJSON = []byte("{}")
		}

		var encryptedKey, err_enc = secure.SecureEncrypt(ep.APIKey)
		if err_enc != nil {
			skippedCount++
			continue
		}
		headersJSON, _ := json.Marshal([]HeaderItem{})
		if len(ep.Headers) > 0 {
			headersJSON, _ = json.Marshal(cleanHeaders(ep.Headers))
		}
		disabledJSON, _ := json.Marshal([]string{})
		if len(ep.DisabledModels) > 0 {
			disabledJSON, _ = json.Marshal(ep.DisabledModels)
		}
		proxyJSON, _ := json.Marshal([]string{})
		if len(ep.ProxyPool) > 0 {
			proxyJSON, _ = json.Marshal(cleanProxyPool(ep.ProxyPool))
		}
		batchesJSON, _ := json.Marshal([]ProxyBatch{})
		if len(ep.ProxyBatches) > 0 {
			batchesJSON, _ = json.Marshal(ep.ProxyBatches)
		}
		mappingsJSON, _ := json.Marshal(map[string]string{})
		if len(ep.ModelMappings) > 0 {
			mappingsJSON, _ = json.Marshal(ep.ModelMappings)
		}
		// 扩展 key 与创建/更新端点保持同一存储格式：对整个明文 JSON 数组字符串
		// 整串加密（读取端 SecureDecrypt 整串解密后再 Unmarshal）。若逐 key 加密
		// 后组数组，读取端会把密文当明文解出，导入的扩展 key 全部失效（上游 401）。
		apiKeysJSON, _ := json.Marshal([]string{})
		if len(ep.APIKeys) > 0 {
			plaintextKeys, _ := json.Marshal(ep.APIKeys)
			encryptedAPIKeys, encErr := secure.SecureEncrypt(string(plaintextKeys))
			if encErr != nil {
				skippedCount++
				continue
			}
			apiKeysJSON = []byte(encryptedAPIKeys)
		}
		autoSwitchInt := boolToInt(ep.AutoSwitch)
		proxyEnabledInt := boolToInt(ep.ProxyEnabled)
		forceProxyInt := boolToInt(ep.ForceProxy)
		rateLimitRetryInt := 1
		if !ep.RateLimitRetryEnabled {
			rateLimitRetryInt = 0
		}
		rateLimitRetryWaitSeconds := ep.RateLimitRetryWaitSeconds
		if rateLimitRetryWaitSeconds < 1 {
			rateLimitRetryWaitSeconds = 10
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO openai_endpoints (id, name, base_url, models_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, model_mappings, created_at, last_used, last_checked, priority, weight)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ep.Name, ep.BaseURL, strings.TrimSpace(ep.ModelsURL), encryptedKey, string(apiKeysJSON), string(headersJSON), string(disabledJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, normalizeProtocol(ep.Protocol), status, enabledInt, string(modelsJSON), string(pricingJSON), string(mappingsJSON), createdAt, ep.LastUsed, ep.LastChecked, ep.Priority, ep.Weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		importedCount++
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"imported": importedCount,
		"skipped":  skippedCount,
		"total":    importedCount + skippedCount,
	})
}

// resolveTargetEndpoint 仅在管理面板入口（/api/openai 前缀，会话鉴权）读取
// x-endpoint-id 强制指定端点，用于调试/聊天测试；外部统一出口（/v1）忽略该头，
// 保证模型池路由不外泄、外部无法锁定特定上游。
