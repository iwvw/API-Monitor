package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, models_url, api_key, headers, proxy_pool, upstream_type FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type item struct {
		id, name, url, key, modelsURL, upstreamType string
		headers                                     []HeaderItem
		pool                                        []string
	}
	items := []item{}
	for rows.Next() {
		var it item
		var modelsURLRaw, headersRaw, proxyRaw, upstreamTypeRaw sql.NullString
		// models_url 为 NULL（插件注册等历史行）时按空串处理，避免该端点被静默跳过。
		if err := rows.Scan(&it.id, &it.name, &it.url, &modelsURLRaw, &it.key, &headersRaw, &proxyRaw, &upstreamTypeRaw); err == nil {
			it.modelsURL = modelsURLRaw.String
			it.upstreamType = normalizeUpstreamType(upstreamTypeRaw.String)
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

			listOK := false
			vOk, _, err := s.verifyAPIKeyRaw(ctx, it.url, it.key, it.id, it.pool, it.modelsURL, it.upstreamType, it.headers)
			if err == nil && vOk {
				status = "valid"
				mList, mPrice, mErr := s.listModelsWithPricing(ctx, it.url, it.key, it.id, it.pool, it.modelsURL, it.upstreamType, it.headers)
				if mErr == nil {
					modelsList = mList
					pricing = mPrice
					listOK = true
				}
			} else if err != nil {
				errStr = err.Error()
			}

			checkedAt := time.Now().Format(time.RFC3339)
			// 只有「验证成功且列表拉取成功」才用新结果覆盖 models/pricing；验证失败或
			// 拉取失败（端点不稳定/超时）时保留库中旧模型与旧定价，避免一次瞬时故障
			// 把端点模型清空成「暂无模型数据，可刷新端点获取」。
			modelsJSON := "[]"
			pricingJSON := "{}"
			if status == "valid" && listOK {
				if len(modelsList) > 0 {
					modelsJSONBytes, _ := json.Marshal(modelsList)
					modelsJSON = string(modelsJSONBytes)
				}
				if len(pricing) > 0 {
					pricingBytes, _ := json.Marshal(pricing)
					pricingJSON = string(pricingBytes)
				}
			} else {
				// 验证/拉取失败：保留库中旧模型与旧定价（瞬态故障不清空已获取列表）。
				if dbConn, dbErr := s.open(ctx); dbErr == nil {
					var existingRaw, existingPricingRaw sql.NullString
					if err := dbConn.QueryRowContext(ctx, "SELECT models, pricing FROM openai_endpoints WHERE id = ?", it.id).Scan(&existingRaw, &existingPricingRaw); err == nil {
						if existingRaw.Valid && existingRaw.String != "" {
							modelsJSON = existingRaw.String
						}
						if existingPricingRaw.Valid && existingPricingRaw.String != "" {
							pricingJSON = existingPricingRaw.String
						}
					}
					dbConn.Close()
				}
			}
			// Vertex AI 不提供模型列表端点，models 全部来自手动添加：刷新时
			// 保留现有列表（不覆盖为拉取结果），仅更新状态与 last_checked。
			// DB 读取失败时不覆盖 models（原值保留），避免误清空手动模型。
			preserveVertexModels := false
			if it.upstreamType == upstreamTypeVertex {
				if dbConn, dbErr := s.open(ctx); dbErr == nil {
					var existingRaw sql.NullString
					if err := dbConn.QueryRowContext(ctx, "SELECT models FROM openai_endpoints WHERE id = ?", it.id).Scan(&existingRaw); err == nil && existingRaw.Valid && existingRaw.String != "" {
						modelsJSON = existingRaw.String
						modelsList = []string{}
						_ = json.Unmarshal([]byte(existingRaw.String), &modelsList)
					} else {
						preserveVertexModels = true
					}
					dbConn.Close()
				} else {
					preserveVertexModels = true
				}
			}

			// Update in DB
			if dbConn, dbErr := s.open(ctx); dbErr == nil {
				defer dbConn.Close()
				if preserveVertexModels {
					_, _ = dbConn.ExecContext(ctx, `
						UPDATE openai_endpoints
						SET status = ?, pricing = ?, last_checked = ?
						WHERE id = ?`,
						status, pricingJSON, checkedAt, it.id)
				} else {
					_, _ = dbConn.ExecContext(ctx, `
						UPDATE openai_endpoints
						SET status = ?, models = ?, pricing = ?, last_checked = ?
						WHERE id = ?`,
						status, modelsJSON, pricingJSON, checkedAt, it.id)
				}
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

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, pricing, models_url, created_at, last_used, last_checked, model_mappings, priority, weight, key_retry_rounds, upstream_type FROM openai_endpoints ORDER BY sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, proxyBatchesRaw, mappingsRaw, apiKeysRaw, protocolRaw, pricingRaw, modelsUrlRaw, upstreamTypeRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, priority, weight, keyRetryRounds int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &proxyBatchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &pricingRaw, &modelsUrlRaw, &created, &used, &checked, &mappingsRaw, &priority, &weight, &keyRetryRounds, &upstreamTypeRaw)
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
		ep.UpstreamType = normalizeUpstreamType(upstreamTypeRaw.String)
		ep.Priority = priority
		ep.Weight = weight
		ep.KeyRetryRounds = keyRetryRounds
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
			INSERT OR REPLACE INTO openai_endpoints (id, name, base_url, models_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, pricing, model_mappings, created_at, last_used, last_checked, priority, weight, key_retry_rounds, upstream_type)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ep.Name, ep.BaseURL, strings.TrimSpace(ep.ModelsURL), encryptedKey, string(apiKeysJSON), string(headersJSON), string(disabledJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, normalizeProtocol(ep.Protocol), status, enabledInt, string(modelsJSON), string(pricingJSON), string(mappingsJSON), createdAt, ep.LastUsed, ep.LastChecked, ep.Priority, ep.Weight, ep.effectiveKeyRetryRounds(), normalizeUpstreamType(ep.UpstreamType))
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
