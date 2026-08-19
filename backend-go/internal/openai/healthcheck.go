package openai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) testEndpointChat(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	modelName := req.Model
	if modelName == "" {
		modelName = "gpt-3.5-turbo"
	}

	ctx := r.Context()
	// 端点测试使用独立短超时，避免上游无响应时拖住整个操作。
	testCtx, testCancel := context.WithTimeout(ctx, 10*time.Second)
	defer testCancel()

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	pool := decodeProxyPool(proxyRaw)

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	chatPayload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "Say \"Hello, API test successful!\" in exactly those words."},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(chatPayload)

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))
	httpReq, err := http.NewRequestWithContext(testCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	applyCustomHeaders(httpReq, decodeEndpointHeaders(headersRaw))

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(id, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(id, selectedProxy, retryAfterFromHeader(resp))
		}
		respBytes, _ := io.ReadAll(resp.Body)
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))})
		return
	}

	var chatResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage interface{} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "无法解析响应 JSON"})
		return
	}

	reply := ""
	if len(chatResponse.Choices) > 0 {
		reply = chatResponse.Choices[0].Message.Content
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": reply,
		"usage":    chatResponse.Usage,
	})
}

func (s *Service) healthCheckModelRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model   string `json:"model"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Model == "" {
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

	var baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	pool := decodeProxyPool(proxyRaw)
	result := s.healthCheckSingleModel(ctx, id, baseURL, apiKey, req.Model, timeoutDuration, pool, decodeEndpointHeaders(headersRaw))

	// Save check to health history
	var errMsg sql.NullString
	if result.Error != "" {
		errMsg.Valid = true
		errMsg.String = result.Error
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, result.Status, result.Latency, errMsg, result.CheckedAt)

	response.JSON(w, http.StatusOK, result)
}

func (s *Service) healthCheckAllModelsRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey, modelsRaw string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, models, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &modelsRaw, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	endpointHeaders := decodeEndpointHeaders(headersRaw)
	endpointPool := decodeProxyPool(proxyRaw)

	var models []string
	if modelsRaw != "" {
		models = parseModelIDsFromRaw(modelsRaw)
	}

	if len(models) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"totalModels": 0,
			"message":     "该端点没有模型可供检测",
		})
		return
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := healthConcurrencyDefault
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}
	concurrency = min(max(concurrency, 1), healthConcurrencyMax)

	summary := s.runBatchHealthCheck(ctx, id, baseURL, apiKey, models, timeoutDuration, concurrency, endpointPool, endpointHeaders)

	// Save check results to db history
	for _, result := range summary.Results {
		var errMsg sql.NullString
		if result.Error != "" {
			errMsg.Valid = true
			errMsg.String = result.Error
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
			VALUES (?, ?, ?, ?, ?)`,
			id, result.Status, result.Latency, errMsg, result.CheckedAt)
	}

	// Update endpoint status
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, last_checked = ?
		WHERE id = ?`,
		summary.OverallStatus, summary.CheckedAt, id)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"summary": summary,
	})
}

// proxyRuntimeStateItem 是单个代理的运行时禁用状态，供前端在代理池管理里红底展示。
type proxyRuntimeStateItem struct {
	Proxy            string `json:"proxy"`
	CooldownUntil    string `json:"cooldownUntil,omitempty"`
	RateLimitedUntil string `json:"rateLimitedUntil,omitempty"`
	SunkUntil        string `json:"sunkUntil,omitempty"`
	Failures         int    `json:"failures"`
	Rate429          int    `json:"rate429"`
	LastTTFB         int64  `json:"lastTTFB,omitempty"`
	LastExitIP       string `json:"lastExitIP,omitempty"`
	LastProbeAt      string `json:"lastProbeAt,omitempty"`
}

// getEndpointProxyStateRoute 返回端点代理池各出口的运行时禁用状态：
//   - cooldownUntil：连接失败冷却到期时间（指数退避，1min~30min）
//   - rateLimitedUntil：上游 429 累计冻结到期时间（30min）
//   - sunkUntil：连续失败被判定的坏代理沉淀到期时间（6h）
//
// 前端据此把被冷却/冻结/沉淀的代理 IP 标红，便于发现「正在被禁用的出口」。
func (s *Service) getEndpointHealthRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var status, lastChecked sql.NullString
	err = db.QueryRowContext(ctx, "SELECT status, last_checked FROM openai_endpoints WHERE id = ?", id).Scan(&status, &lastChecked)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	// Fetch health history per model
	rows, err := db.QueryContext(ctx, `
		SELECT h.status, h.response_time, h.error_message, h.checked_at
		FROM openai_health_history h
		WHERE h.endpoint_id = ?
		ORDER BY h.checked_at DESC
		LIMIT 100`, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	history := []map[string]interface{}{}
	for rows.Next() {
		var hStatus, checked string
		var respTime sql.NullInt64
		var errMsg sql.NullString
		if err := rows.Scan(&hStatus, &respTime, &errMsg, &checked); err == nil {
			item := map[string]interface{}{
				"status":    hStatus,
				"checkedAt": checked,
			}
			if respTime.Valid {
				item["responseTime"] = respTime.Int64
			}
			if errMsg.Valid {
				item["errorMessage"] = errMsg.String
			}
			history = append(history, item)
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"endpointId":      id,
		"healthStatus":    status.String,
		"lastHealthCheck": lastChecked.String,
		"history":         history,
	})
}

func (s *Service) healthCheckAllRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, models, headers, proxy_pool FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, url, key, modelsRaw string
		headers                       []HeaderItem
		pool                          []string
	}
	items := []item{}
	for rows.Next() {
		var it item
		var headersRaw, proxyRaw sql.NullString
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.key, &it.modelsRaw, &headersRaw, &proxyRaw); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			it.headers = decodeEndpointHeaders(headersRaw)
			it.pool = decodeProxyPool(proxyRaw)
			items = append(items, it)
		}
	}

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := healthConcurrencyDefault
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}
	concurrency = min(max(concurrency, 1), healthConcurrencyMax)

	type endpointTarget struct {
		item
		models []string
	}
	targets := make([]endpointTarget, 0, len(items))
	for _, it := range items {
		models := parseModelIDsFromRaw(it.modelsRaw)
		targets = append(targets, endpointTarget{item: it, models: models})
	}

	resultsByEndpoint := make(map[string][]HealthRecord, len(targets))
	var resultsMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, target := range targets {
		for _, model := range target.models {
			wg.Add(1)
			go func(target endpointTarget, model string) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()

				result := s.healthCheckSingleModel(ctx, target.id, target.url, target.key, model, timeoutDuration, target.pool, target.headers)
				resultsMu.Lock()
				resultsByEndpoint[target.id] = append(resultsByEndpoint[target.id], result)
				resultsMu.Unlock()
			}(target, model)
		}
	}
	wg.Wait()

	results := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		if len(target.models) == 0 {
			results = append(results, map[string]interface{}{
				"endpointId":  target.id,
				"name":        target.name,
				"totalModels": 0,
				"skipped":     true,
			})
			continue
		}

		summary := summarizeHealthResults(target.models, resultsByEndpoint[target.id])
		for _, result := range summary.Results {
			var errMsg sql.NullString
			if result.Error != "" {
				errMsg.Valid = true
				errMsg.String = result.Error
			}
			_, _ = db.ExecContext(ctx, `
				INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
				VALUES (?, ?, ?, ?, ?)`,
				target.id, result.Status, result.Latency, errMsg, result.CheckedAt)
		}
		_, _ = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET status = ?, last_checked = ?
			WHERE id = ?`,
			summary.OverallStatus, summary.CheckedAt, target.id)

		results = append(results, map[string]interface{}{
			"endpointId":  target.id,
			"name":        target.name,
			"totalModels": summary.TotalModels,
			"operational": summary.Operational,
			"degraded":    summary.Degraded,
			"failed":      summary.Failed,
			"results":     summary.Results,
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"checkedAt": time.Now().Format(time.RFC3339),
		"endpoints": results,
	})
}

func (s *Service) verifyAPIKeyRaw(ctx context.Context, u, key, endpointID string, pool []string, headers ...[]HeaderItem) (bool, int, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}
		return false, 0, fmt.Errorf("verify failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 0, err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			return true, len(dataArr), nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		return true, len(parsedArr), nil
	}

	return true, 0, nil
}

// listModelsRaw 拉取上游模型列表（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（见 verifyAPIKeyRaw）。
func (s *Service) listModelsRaw(ctx context.Context, u, key, endpointID string, pool []string, headers ...[]HeaderItem) ([]string, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}
		return nil, fmt.Errorf("list models failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	models := []string{}
	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		// OpenAI structure
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			for _, item := range dataArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					}
				}
			}
			return models, nil
		}
		// Custom models key
		if modelsArr, ok := parsed["models"].([]interface{}); ok {
			for _, item := range modelsArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					} else if name, ok := itemMap["name"].(string); ok {
						models = append(models, name)
					}
				}
			}
			return models, nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		for _, item := range parsedArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, ok := itemMap["id"].(string); ok {
					models = append(models, id)
				}
			}
		}
		return models, nil
	}

	return nil, fmt.Errorf("unexpected models structure")
}

func healthCheckFastFailStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

func (s *Service) healthCheckSingleModel(ctx context.Context, endpointID, baseURL, apiKey, model string, timeout time.Duration, pool []string, headers ...[]HeaderItem) HealthRecord {
	startTime := time.Now()
	record := HealthRecord{
		Model:     model,
		Status:    "failed",
		CheckedAt: startTime.Format(time.RFC3339),
	}

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))

	// 端点配置了代理池时，健康检测与真实转发走同一出口（池内代理），
	// 避免检测请求从网关本机直连出口（出口 IP 不属于代理池）。
	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	// 请求体模拟真实客户端常用字段，降低上游对缺字段请求的兼容性误判。
	payload := map[string]interface{}{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": "Reply with any short non-empty text."}},
		"stream":      false,
		"max_tokens":  1,
		"temperature": 0,
	}
	bodyBytes, _ := json.Marshal(payload)

	lastLatency := int64(0)
	var lastError string
	for attempt := 0; attempt < healthCheckAttempts; attempt++ {
		if attempt > 0 {
			// 重试前等待一小段退避，避免在限流窗口内反复撞墙。
			select {
			case <-ctx.Done():
				break
			case <-time.After(300 * time.Millisecond):
			}
		}

		childCtx, cancel := context.WithTimeout(ctx, timeout)
		httpReq, err := http.NewRequestWithContext(childCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
		if err != nil {
			cancel()
			record.Error = err.Error()
			record.Latency = time.Since(startTime).Milliseconds()
			return record
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		if len(headers) > 0 {
			applyCustomHeaders(httpReq, headers[0])
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			cancel()
			lastLatency = time.Since(startTime).Milliseconds()
			if childCtx.Err() != nil {
				lastError = "超时"
			} else {
				lastError = err.Error()
			}
			continue
		}
		lastLatency = time.Since(startTime).Milliseconds()
		record.StatusCode = resp.StatusCode

		// 健康检测也累计上游限流：半死出口（已限死的 IP）在健康检查里反复
		// 429 时同样触发冻结，避免只有真实流量才能发现问题代理。
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}

		// 状态码优先：2xx 视为可用（仅校验显式 error 结构），
		// 4xx 确定性失败立即返回，其余（429/5xx）进入下一轮重试。
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// 2xx 响应只校验是否携带显式 error 结构（部分上游会返回 200 + {"error": ...}）。
			// 空响应体、SSE、纯文本等一律视为有效，避免误伤兼容端点。
			// 只读前 4KB 判定，不等待完整响应体（生成慢的模型等完整 body 会浪费数秒）。
			sniff, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			cancel()
			if errMsg := healthCheckBodyError(sniff); errMsg != "" {
				lastError = errMsg
				continue
			}
			record.Latency = lastLatency
			if time.Duration(lastLatency)*time.Millisecond <= degradedThreshold {
				record.Status = "operational"
			} else {
				record.Status = "degraded"
			}
			return record
		}

		resp.Body.Close()
		cancel()
		if healthCheckFastFailStatus(resp.StatusCode) {
			record.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			record.Latency = lastLatency
			return record
		}
		lastError = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	record.Error = lastError
	record.Latency = lastLatency
	return record
}

// healthCheckBodyError 在 2xx 响应中探测明确的错误结构（如 {"error": {"message": "..."}}），
// 未发现错误时返回空字符串。兼容 JSON、SSE（data: 行）与纯文本响应。
func healthCheckBodyError(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "data:") {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			if msg := healthCheckBodyError([]byte(payload)); msg != "" {
				return msg
			}
		}
		return ""
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return ""
	}
	switch errValue := parsed["error"].(type) {
	case map[string]interface{}:
		if message, ok := errValue["message"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
	case string:
		if strings.TrimSpace(errValue) != "" {
			return strings.TrimSpace(errValue)
		}
	}
	return ""
}

func (s *Service) runBatchHealthCheck(ctx context.Context, endpointID, baseURL, apiKey string, models []string, timeout time.Duration, concurrency int, pool []string, headers ...[]HeaderItem) HealthSummary {
	var mu sync.Mutex
	results := []HealthRecord{}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := s.healthCheckSingleModel(ctx, endpointID, baseURL, apiKey, m, timeout, pool, headers...)

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(model)
	}

	wg.Wait()

	return summarizeHealthResults(models, results)
}

func summarizeHealthResults(models []string, results []HealthRecord) HealthSummary {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Model < results[j].Model
	})

	operationalCount := 0
	degradedCount := 0
	failedCount := 0

	for _, r := range results {
		switch r.Status {
		case "operational":
			operationalCount++
		case "degraded":
			degradedCount++
		default:
			failedCount++
		}
	}

	overall := "unknown"
	if len(results) > 0 {
		if failedCount == len(results) {
			overall = "failed"
		} else if operationalCount == len(results) {
			overall = "operational"
		} else {
			overall = "degraded"
		}
	}

	return HealthSummary{
		TotalModels:   len(models),
		Operational:   operationalCount,
		Degraded:      degradedCount,
		Failed:        failedCount,
		OverallStatus: overall,
		Results:       results,
		CheckedAt:     time.Now().Format(time.RFC3339),
	}
}
