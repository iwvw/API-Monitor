package modelhub

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

// healthCheckPrompt 是端点连通性与模型可用性检测共用的探测提示词。
// 刻意避开 "Hello"、"test"、"ping"、"Reply with ..." 这类典型测活措辞，
// 免得被上游按探针请求识别并拒绝；用一句自然的日常请求，必然产生非空正文。
const healthCheckPrompt = "Tell me a short fun fact."

// healthCheckMaxTokens 是模型可用性检测的 max_tokens。
// 必须给 reasoning 模型留够思考开销：预算过小（如 1）时模型把额度耗在
// reasoning 上、正文为空，会被上游判成「空响应」而误报检测失败。
const healthCheckMaxTokens = 32

func (s *Service) testEndpointChat(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 探针模型:调用方未指定时不再写死 gpt-3.5-turbo(对不含该模型的上游会误报
	// 失败),改为在该端点已发现的模型里取第一个作默认;解析见下方端点读取之后。
	modelName := strings.TrimSpace(req.Model)

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
	var headersRaw, proxyRaw, proxyPoolIDRaw, upstreamTypeRaw, modelsRaw, protocolRaw sql.NullString
	var proxyEnabledInt, forceProxyInt int
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool, proxy_pool_id, proxy_enabled, force_proxy, protocol, upstream_type, models FROM modelhub_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw, &proxyPoolIDRaw, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &upstreamTypeRaw, &modelsRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	pool := decodeProxyPool(proxyRaw)
	upstreamType := normalizeUpstreamType(upstreamTypeRaw.String)

	// 调用方未指定 model 时,用该端点已发现的第一个模型做探针;仍无模型才退回默认,
	// 避免对不含 gpt-3.5-turbo 的上游把「探针选了不存在的模型」误报成连通性失败。
	if modelName == "" {
		var models []string
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &models)
		}
		if len(models) > 0 {
			modelName = models[0]
		} else {
			modelName = "gpt-3.5-turbo"
		}
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	chatPayload := map[string]interface{}{
		"model": modelName,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": healthCheckPrompt},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(chatPayload)

	// Gemini 上游：Interactions API（x-goog-api-key + 转换后的请求体）；
	// Vertex 上游：generateContent API；其余走 OpenAI /chat/completions。
	var reqURL string
	switch upstreamType {
	case upstreamTypeGemini:
		reqURL = geminiInteractionsURL(baseURL)
		if gb, gErr := openAIChatToGemini(chatPayload); gErr == nil {
			bodyBytes, _ = json.Marshal(gb)
		}
	case upstreamTypeVertex:
		reqURL = vertexGenerateURL(baseURL, modelName, false)
		if vb, vErr := openAIChatToVertex(chatPayload); vErr == nil {
			bodyBytes, _ = json.Marshal(vb)
		}
	default:
		reqURL = fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))
	}
	httpReq, err := http.NewRequestWithContext(testCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if upstreamType == upstreamTypeGemini || upstreamType == upstreamTypeVertex {
		httpReq.Header.Set("X-Goog-Api-Key", apiKey)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyCustomHeaders(httpReq, decodeEndpointHeaders(headersRaw))

	client, selectedProxy, clientErr := s.auxClientForEndpoint(ctx, auxEndpoint{
		id:           id,
		pool:         pool,
		proxyPoolID:  proxyPoolIDRaw.String,
		proxyEnabled: proxyEnabledInt == 1,
		forceProxy:   forceProxyInt == 1,
		protocol:     protocolRaw.String,
	})
	if clientErr != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": clientErr.Error()})
		return
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

	reply := ""
	var usage interface{}
	if upstreamType == upstreamTypeGemini || upstreamType == upstreamTypeVertex {
		// Gemini / Vertex 上游响应 → OpenAI chat 格式后提取文本与 usage。
		respBody, _ := io.ReadAll(resp.Body)
		var conv []byte
		var convErr error
		if upstreamType == upstreamTypeGemini {
			conv, convErr = geminiToOpenAIChat(respBody, modelName)
		} else {
			conv, convErr = vertexToOpenAIChat(respBody, modelName)
		}
		if convErr == nil {
			var oai struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Usage interface{} `json:"usage"`
			}
			if err := json.Unmarshal(conv, &oai); err == nil {
				if len(oai.Choices) > 0 {
					reply = oai.Choices[0].Message.Content
				}
				usage = oai.Usage
			}
		}
	} else {
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
		if len(chatResponse.Choices) > 0 {
			reply = chatResponse.Choices[0].Message.Content
		}
		usage = chatResponse.Usage
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": reply,
		"usage":    usage,
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
	var apiKeysRaw, headersRaw, proxyRaw, proxyPoolIDRaw, upstreamTypeRaw, protocolRaw sql.NullString
	var proxyEnabledInt, forceProxyInt int
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, api_keys, headers, proxy_pool, proxy_pool_id, proxy_enabled, force_proxy, protocol, upstream_type FROM modelhub_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &apiKeysRaw, &headersRaw, &proxyRaw, &proxyPoolIDRaw, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &upstreamTypeRaw)
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
	_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	pool := decodeProxyPool(proxyRaw)
	result := s.healthCheckSingleModel(ctx, healthCheckEndpoint{
		id:           id,
		baseURL:      baseURL,
		apiKey:       apiKey,
		apiKeys:      decodeEndpointAPIKeys(apiKeysRaw),
		pool:         pool,
		proxyPoolID:  proxyPoolIDRaw.String,
		proxyEnabled: proxyEnabledInt == 1,
		forceProxy:   forceProxyInt == 1,
		protocol:     protocolRaw.String,
		upstreamType: normalizeUpstreamType(upstreamTypeRaw.String),
		headers:      decodeEndpointHeaders(headersRaw),
	}, req.Model, timeoutDuration)

	// Save check to health history
	var errMsg sql.NullString
	if result.Error != "" {
		errMsg.Valid = true
		errMsg.String = result.Error
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO modelhub_health_history (endpoint_id, status, response_time, error_message, checked_at)
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
	var apiKeysRaw, headersRaw, proxyRaw, proxyPoolIDRaw, upstreamTypeRaw, protocolRaw sql.NullString
	var proxyEnabledInt, forceProxyInt int
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, api_keys, models, headers, proxy_pool, proxy_pool_id, proxy_enabled, force_proxy, protocol, upstream_type FROM modelhub_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &apiKeysRaw, &modelsRaw, &headersRaw, &proxyRaw, &proxyPoolIDRaw, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &upstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	ep := healthCheckEndpoint{
		id:           id,
		baseURL:      baseURL,
		apiKey:       apiKey,
		apiKeys:      decodeEndpointAPIKeys(apiKeysRaw),
		pool:         decodeProxyPool(proxyRaw),
		proxyPoolID:  proxyPoolIDRaw.String,
		proxyEnabled: proxyEnabledInt == 1,
		forceProxy:   forceProxyInt == 1,
		protocol:     protocolRaw.String,
		upstreamType: normalizeUpstreamType(upstreamTypeRaw.String),
		headers:      decodeEndpointHeaders(headersRaw),
	}

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
	_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := healthConcurrencyDefault
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}
	concurrency = min(max(concurrency, 1), healthConcurrencyMax)

	summary := s.runBatchHealthCheck(ctx, ep, models, timeoutDuration, concurrency)

	// Save check results to db history
	for _, result := range summary.Results {
		var errMsg sql.NullString
		if result.Error != "" {
			errMsg.Valid = true
			errMsg.String = result.Error
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO modelhub_health_history (endpoint_id, status, response_time, error_message, checked_at)
			VALUES (?, ?, ?, ?, ?)`,
			id, result.Status, result.Latency, errMsg, result.CheckedAt)
	}

	// Update endpoint status
	_, _ = db.ExecContext(ctx, `
		UPDATE modelhub_endpoints
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
	err = db.QueryRowContext(ctx, "SELECT status, last_checked FROM modelhub_endpoints WHERE id = ?", id).Scan(&status, &lastChecked)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	// Fetch health history per model
	rows, err := db.QueryContext(ctx, `
		SELECT h.status, h.response_time, h.error_message, h.checked_at
		FROM modelhub_health_history h
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

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, models, headers, proxy_pool, proxy_pool_id, proxy_enabled, force_proxy, protocol, upstream_type FROM modelhub_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, modelsRaw string
		ep                  healthCheckEndpoint
	}
	items := []item{}
	for rows.Next() {
		var it item
		var apiKeysRaw, headersRaw, proxyRaw, proxyPoolIDRaw, upstreamTypeRaw, protocolRaw sql.NullString
		var proxyEnabledInt, forceProxyInt int
		if err := rows.Scan(&it.id, &it.name, &it.ep.baseURL, &it.ep.apiKey, &apiKeysRaw, &it.modelsRaw, &headersRaw, &proxyRaw, &proxyPoolIDRaw, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &upstreamTypeRaw); err == nil {
			it.ep.id = it.id
			it.ep.apiKey = secure.SecureDecrypt(it.ep.apiKey)
			it.ep.apiKeys = decodeEndpointAPIKeys(apiKeysRaw)
			it.ep.pool = decodeProxyPool(proxyRaw)
			it.ep.proxyPoolID = proxyPoolIDRaw.String
			it.ep.proxyEnabled = proxyEnabledInt == 1
			it.ep.forceProxy = forceProxyInt == 1
			it.ep.protocol = protocolRaw.String
			it.ep.upstreamType = normalizeUpstreamType(upstreamTypeRaw.String)
			it.ep.headers = decodeEndpointHeaders(headersRaw)
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

				result := s.healthCheckSingleModel(ctx, target.ep, model, timeoutDuration)
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
				INSERT INTO modelhub_health_history (endpoint_id, status, response_time, error_message, checked_at)
				VALUES (?, ?, ?, ?, ?)`,
				target.id, result.Status, result.Latency, errMsg, result.CheckedAt)
		}
		_, _ = db.ExecContext(ctx, `
			UPDATE modelhub_endpoints
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

// firstVerifiedKey 依次用端点的各把 API Key 执行 verifyAPIKeyRaw,返回第一把验证
// 通过的 key。全部失败时返回 ok=false 与最后一次错误。端点配置了多把 key 时,
// 避免第一把失效 key 把「刷新模型列表 / 验证端点」直接判成失败。
func (s *Service) firstVerifiedKey(ctx context.Context, baseURL string, keys []string, ep auxEndpoint, modelsURL, upstreamType string, headers []HeaderItem) (string, bool, error) {
	if len(keys) == 0 {
		keys = []string{""}
	}
	var lastErr error
	for _, key := range keys {
		ok, _, err := s.verifyAPIKeyRaw(ctx, baseURL, key, ep, modelsURL, upstreamType, headers)
		if err == nil && ok {
			return key, true, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return "", false, lastErr
}

func (s *Service) verifyAPIKeyRaw(ctx context.Context, u, key string, ep auxEndpoint, modelsURL, upstreamType string, headers ...[]HeaderItem) (bool, int, error) {
	vertex := normalizeUpstreamType(upstreamType) == upstreamTypeVertex
	if vertex {
		return s.verifyVertexKey(ctx, u, key, ep, headers...)
	}
	gemini := normalizeUpstreamType(upstreamType) == upstreamTypeGemini
	var reqURL string
	if gemini {
		reqURL = geminiModelsURL(u)
	} else {
		reqURL = modelListURL(u, modelsURL)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, 0, err
	}
	if gemini {
		req.Header.Set("X-Goog-Api-Key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client, selectedProxy, clientErr := s.auxClientForEndpoint(ctx, ep)
	if clientErr != nil {
		return false, 0, clientErr
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(ep.id, selectedProxy, retryAfterFromHeader(resp))
		}
		return false, 0, fmt.Errorf("verify failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 0, err
	}

	if gemini {
		models := geminiModelsList(bodyBytes)
		return true, len(models), nil
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			return true, len(dataArr), nil
		}
		// 分类分组结构（如 Cline recommended-models 的 recommended/free/clinePass 分区）：
		// 非 data 结构时逐字段收集含 id 的数组元素，据此统计模型数量。
		var grouped []string
		pricing := PricingMap{}
		if count := collectGroupedModels(parsed, &grouped, &pricing); count > 0 {
			return true, count, nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		return true, len(parsedArr), nil
	}

	return true, 0, nil
}

// verifyVertexKey 验证 Vertex AI API Key：Vertex 不提供模型列表端点（GET /models
// 返回 404），改用 generateContent 简单请求判定鉴权。401/403 → key 无效；其余
// （200/400/404 等）说明鉴权已通过——模型/区域/参数问题不影响 key 有效性。
func (s *Service) verifyVertexKey(ctx context.Context, u, key string, ep auxEndpoint, headers ...[]HeaderItem) (bool, int, error) {
	reqURL := vertexGenerateURL(u, "gemini-2.5-flash", false)
	body, _ := json.Marshal(map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": "hi"}}},
		},
		"generationConfig": map[string]interface{}{"maxOutputTokens": 1},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("X-Goog-Api-Key", key)
	req.Header.Set("Content-Type", "application/json")
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}
	client, _, clientErr := s.auxClientForEndpoint(ctx, ep)
	if clientErr != nil {
		return false, 0, clientErr
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		detail := upstreamErrorMessage(bodySnippet)
		return false, 0, fmt.Errorf("verify failed: HTTP %d: %s", resp.StatusCode, detail)
	}
	return true, 0, nil
}

// collectGroupedModels 解析「按分类分组的模型列表」结构（如 Cline recommended-models 的
// recommended/free/clinePass 分区）：顶层为若干数组键值对，元素是含 id 的对象。
// 返回收集到的模型数量；未识别出任何模型时返回 0 且不修改调用方数据。
// 定价解析成功则写入 pricing（为 nil 时跳过）。
func collectGroupedModels(parsed map[string]interface{}, models *[]string, pricing *PricingMap) int {
	collected := 0
	seen := map[string]bool{}
	for _, v := range parsed {
		arr, ok := v.([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			id, ok := itemMap["id"].(string)
			if !ok || id == "" || seen[id] {
				continue
			}
			seen[id] = true
			*models = append(*models, id)
			if p, ok := parsePricingFromItem(itemMap); ok && pricing != nil {
				(*pricing)[id] = p
			}
			collected++
		}
	}
	return collected
}

// modelListURL 返回模型列表拉取地址：显式配置了 modelsURL 时按原样请求（可含完整路径，
// 如 Cline 的 /api/v1/ai/cline/recommended-models），否则回退 {baseURL}/models。
func modelListURL(baseURL, modelsURL string) string {
	if modelsURL = strings.TrimSpace(modelsURL); modelsURL != "" {
		return modelsURL
	}
	return fmt.Sprintf("%s/models", strings.TrimSuffix(baseURL, "/"))
}

// listModelsRaw 拉取上游模型列表（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（见 verifyAPIKeyRaw）。定价信息被丢弃，仅返回模型名。
func (s *Service) listModelsRaw(ctx context.Context, u, key string, ep auxEndpoint, modelsURL, upstreamType string, headers ...[]HeaderItem) ([]string, error) {
	models, _, err := s.listModelsWithPricing(ctx, u, key, ep, modelsURL, upstreamType, headers...)
	return models, err
}

// listModelsWithPricing 拉取上游模型列表（GET /models 或 Gemini 的 /v1beta/models）
// 并解析每个模型的定价。返回模型 id 列表与按 id 索引的定价表；无法解析出定价的
// 模型不出现在定价表中。Gemini 上游定价暂不解析（无 OpenAI 风格定价字段）。
func (s *Service) listModelsWithPricing(ctx context.Context, u, key string, ep auxEndpoint, modelsURL, upstreamType string, headers ...[]HeaderItem) ([]string, PricingMap, error) {
	vertex := normalizeUpstreamType(upstreamType) == upstreamTypeVertex
	if vertex {
		// Vertex AI 不提供模型列表端点（请求 404），模型需手动添加：
		// 返回空列表，不发起请求。
		return nil, PricingMap{}, nil
	}
	gemini := normalizeUpstreamType(upstreamType) == upstreamTypeGemini
	var reqURL string
	if gemini {
		reqURL = geminiModelsURL(u)
	} else {
		reqURL = modelListURL(u, modelsURL)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, nil, err
	}
	if gemini {
		req.Header.Set("X-Goog-Api-Key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client, selectedProxy, clientErr := s.auxClientForEndpoint(ctx, ep)
	if clientErr != nil {
		return nil, nil, clientErr
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(ep.id, selectedProxy, retryAfterFromHeader(resp))
		}
		return nil, nil, fmt.Errorf("list models failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if gemini {
		models := geminiModelsList(bodyBytes)
		return models, PricingMap{}, nil
	}

	models := []string{}
	pricing := PricingMap{}
	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		// OpenAI structure
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			for _, item := range dataArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok && id != "" {
						models = append(models, id)
						if p, ok := parsePricingFromItem(itemMap); ok {
							pricing[id] = p
						}
					}
				}
			}
			return models, pricing, nil
		}
		// Custom models key
		if modelsArr, ok := parsed["models"].([]interface{}); ok {
			for _, item := range modelsArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok && id != "" {
						models = append(models, id)
						if p, ok := parsePricingFromItem(itemMap); ok {
							pricing[id] = p
						}
					} else if name, ok := itemMap["name"].(string); ok && name != "" {
						models = append(models, name)
						if p, ok := parsePricingFromItem(itemMap); ok {
							pricing[name] = p
						}
					}
				}
			}
			return models, pricing, nil
		}
		// 分类分组结构（如 Cline recommended-models 的 recommended/free/clinePass 分区）：
		// 非 data/models 结构时逐字段收集含 id 的数组元素。
		if collectGroupedModels(parsed, &models, &pricing) > 0 {
			return models, pricing, nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		for _, item := range parsedArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, ok := itemMap["id"].(string); ok && id != "" {
					models = append(models, id)
					if p, ok := parsePricingFromItem(itemMap); ok {
						pricing[id] = p
					}
				}
			}
		}
		return models, pricing, nil
	}

	return nil, nil, fmt.Errorf("unexpected models structure")
}

func healthCheckFastFailStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

// healthCheckEndpoint 是一次健康检测所需的端点配置快照。
// 出口选择与真实转发保持一致:独立代理池优先,其次内联池 + 代理开关,最后按连接协议直连。
type healthCheckEndpoint struct {
	id           string
	baseURL      string
	apiKey       string
	apiKeys      []string
	pool         []string
	proxyPoolID  string
	proxyEnabled bool
	forceProxy   bool
	protocol     string
	upstreamType string
	headers      []HeaderItem
}

// keys 返回健康检测要依次尝试的 API Key:主 key + 扩展 key,去重保序。
// 全部为空时返回单个空串,保持「无 key 也发一次请求」的既有行为(便于暴露鉴权错误)。
func (ep healthCheckEndpoint) keys() []string {
	keys := cleanKeyList(append([]string{ep.apiKey}, ep.apiKeys...))
	if len(keys) == 0 {
		return []string{""}
	}
	return keys
}

// keyRejectedStatus 判断状态码是否表示「这把 key 本身被上游拒绝」:
// 401/403(鉴权失败)、402(欠费)。健康检测命中这些状态时会换下一把 key 重试,
// 避免端点配置了多把 key 时被第一把失效 key 拖成整体失败;模型不存在(404)、
// 参数错误(400)等与 key 无关的失败则不换 key。
func keyRejectedStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired:
		return true
	}
	return false
}

// healthCheckSingleModel 对端点的单个模型做可用性检测。端点配置了多把 API Key 时
// 逐把尝试:某把 key 被上游拒绝(401/403/402)则换下一把,避免第一把 key 失效就
// 把检测判成失败;与 key 无关的失败(模型不存在/网络异常等)直接返回,不再换 key。
func (s *Service) healthCheckSingleModel(ctx context.Context, ep healthCheckEndpoint, model string, timeout time.Duration) HealthRecord {
	var last HealthRecord
	for _, key := range ep.keys() {
		record := s.healthCheckSingleModelWithKey(ctx, ep, key, model, timeout)
		if record.Status == "operational" || record.Status == "degraded" {
			return record
		}
		last = record
		if !keyRejectedStatus(record.StatusCode) {
			return record
		}
	}
	return last
}

func (s *Service) healthCheckSingleModelWithKey(ctx context.Context, ep healthCheckEndpoint, key, model string, timeout time.Duration) HealthRecord {
	startTime := time.Now()
	record := HealthRecord{
		Model:     model,
		Status:    "failed",
		CheckedAt: startTime.Format(time.RFC3339),
	}

	// Gemini 上游：健康检测走 Interactions API（x-goog-api-key + 转换后的请求体）；
	// Vertex 上游：走 streamGenerateContent（SSE 首事件快速返回，避免思考模型超时）；
	// 其余走 OpenAI /chat/completions。
	upType := normalizeUpstreamType(ep.upstreamType)
	gemini := upType == upstreamTypeGemini
	var reqURL string
	switch upType {
	case upstreamTypeGemini:
		reqURL = geminiInteractionsURL(ep.baseURL)
	case upstreamTypeVertex:
		reqURL = vertexGenerateURL(ep.baseURL, model, true)
	default:
		reqURL = fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(ep.baseURL, "/"))
	}

	// 端点配置了出口代理时,健康检测与真实转发走同一出口,
	// 避免检测请求从网关本机直连出口（出口 IP 不属于代理池）。
	client, selectedProxy, clientErr := s.auxClientForEndpoint(ctx, auxEndpoint{
		id:           ep.id,
		pool:         ep.pool,
		proxyPoolID:  ep.proxyPoolID,
		proxyEnabled: ep.proxyEnabled,
		forceProxy:   ep.forceProxy,
		protocol:     ep.protocol,
	})
	if clientErr != nil {
		record.Error = clientErr.Error()
		return record
	}

	// 请求体模拟真实客户端常用字段，降低上游对缺字段请求的兼容性误判。
	// messages 用 []interface{}（map[string]interface{}），openAIChatToGemini
	// 依赖该类型断言做转换，[]map[string]string 会导致转换失败而透传 OpenAI 原始 body。
	payload := map[string]interface{}{
		"model":       model,
		"messages":    []interface{}{map[string]interface{}{"role": "user", "content": healthCheckPrompt}},
		"stream":      false,
		"max_tokens":  healthCheckMaxTokens,
		"temperature": 0,
	}
	bodyBytes, _ := json.Marshal(payload)
	if gemini {
		// Gemini 思考模型非流式需完成思考才返回，健康检测短超时下会误判超时；
		// 改流式请求——interactions 首事件（interaction.created）立即返回，
		// 响应头 2xx 即可判定可用。
		payload["stream"] = true
		if gb, gErr := openAIChatToGemini(payload); gErr == nil {
			bodyBytes, _ = json.Marshal(gb)
		}
	} else if upType == upstreamTypeVertex {
		if vb, vErr := openAIChatToVertex(payload); vErr == nil {
			bodyBytes, _ = json.Marshal(vb)
		}
	}

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
		if gemini || upType == upstreamTypeVertex {
			httpReq.Header.Set("X-Goog-Api-Key", key)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		if len(ep.headers) > 0 {
			applyCustomHeaders(httpReq, ep.headers)
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
			s.markProxy429(ep.id, selectedProxy, retryAfterFromHeader(resp))
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

		if healthCheckFastFailStatus(resp.StatusCode) {
			// 读取少量响应体，把上游具体错误（配额/参数/权限等）带进诊断。
			bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
			resp.Body.Close()
			cancel()
			msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
			if len(bodySnippet) > 0 {
				if detail := upstreamErrorMessage(bodySnippet); detail != "" {
					msg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, detail)
				} else {
					msg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
				}
			}
			record.Error = msg
			record.Latency = lastLatency
			return record
		}
		resp.Body.Close()
		cancel()
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

func (s *Service) runBatchHealthCheck(ctx context.Context, ep healthCheckEndpoint, models []string, timeout time.Duration, concurrency int) HealthSummary {
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

			res := s.healthCheckSingleModel(ctx, ep, m, timeout)

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
