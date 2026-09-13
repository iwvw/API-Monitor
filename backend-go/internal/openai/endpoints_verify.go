package openai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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
	var modelsURLRaw, upstreamTypeRaw sql.NullString
	var headersRaw, proxyRaw sql.NullString
	// models_url 为 NULL（插件注册等历史行）时按空串处理，避免 NULL→string 扫描报错误判「端点不存在」。
	err = db.QueryRowContext(ctx, "SELECT name, base_url, models_url, api_key, headers, proxy_pool, upstream_type FROM openai_endpoints WHERE id = ?", id).Scan(&name, &baseURL, &modelsURLRaw, &apiKey, &headersRaw, &proxyRaw, &upstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	modelsURL := modelsURLRaw.String
	apiKey = secure.SecureDecrypt(apiKey)
	upstreamType := normalizeUpstreamType(upstreamTypeRaw.String)
	headers := decodeEndpointHeaders(headersRaw)
	pool := decodeProxyPool(proxyRaw)

	startTime := time.Now()
	status := "invalid"
	modelsList := []string{}
	pricing := PricingMap{}
	var errMsg string

	listOK := false
	vOk, _, vErr := s.verifyAPIKeyRaw(verifyCtx, baseURL, apiKey, id, pool, modelsURL, upstreamType, headers)
	responseTime := time.Since(startTime).Milliseconds()

	if vErr == nil && vOk {
		status = "valid"
		mList, mPrice, mErr := s.listModelsWithPricing(verifyCtx, baseURL, apiKey, id, pool, modelsURL, upstreamType, headers)
		if mErr == nil {
			modelsList = mList
			pricing = mPrice
			listOK = true
		}
	} else if vErr != nil {
		errMsg = vErr.Error()
	}

	checkedAt := time.Now().Format(time.RFC3339)

	// 只有「验证成功且列表拉取成功」才用新结果覆盖 models/pricing；验证失败或
	// 拉取失败（端点不稳定/超时）时保留库中旧模型与旧定价，避免一次瞬时故障把
	// 端点模型清空成「暂无模型数据，可刷新端点获取」。
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
		var existingRaw, existingPricingRaw sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT models, pricing FROM openai_endpoints WHERE id = ?", id).Scan(&existingRaw, &existingPricingRaw); err == nil {
			if existingRaw.Valid && existingRaw.String != "" {
				modelsJSON = existingRaw.String
			}
			if existingPricingRaw.Valid && existingPricingRaw.String != "" {
				pricingJSON = existingPricingRaw.String
			}
		}
	}

	// Vertex AI 不提供模型列表端点，models 全部来自手动添加：刷新/验证时
	// 保留现有列表（不覆盖为拉取结果），仅更新状态与 last_checked。
	if upstreamType == upstreamTypeVertex {
		var existingRaw, pricingRaw sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT models, pricing FROM openai_endpoints WHERE id = ?", id).Scan(&existingRaw, &pricingRaw); err == nil && existingRaw.Valid && existingRaw.String != "" {
			modelsJSON = existingRaw.String
			modelsList = []string{}
			_ = json.Unmarshal([]byte(existingRaw.String), &modelsList)
		}
		if pricingRaw.Valid && pricingRaw.String != "" {
			pricingJSON = pricingRaw.String
		}
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
	var modelsURLRaw, upstreamTypeRaw sql.NullString
	var headersRaw, proxyRaw sql.NullString
	// models_url 为 NULL（插件注册等历史行）时按空串处理。
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, headers, proxy_pool, upstream_type FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &modelsURLRaw, &headersRaw, &proxyRaw, &upstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	modelsURL := modelsURLRaw.String
	upstreamType := normalizeUpstreamType(upstreamTypeRaw.String)

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
			results[idx] = s.checkAPIKeyStatus(ctx, baseURL, k, id, timeout, pool, modelsURL, upstreamType, headers, idx)
		}(i, key)
	}
	wg.Wait()

	response.JSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// checkAPIKeyStatus 用 GET {baseURL}/models 检测单个 key 的有效性。
// 2xx=valid；401/403=invalid（鉴权失败）；402=overdue（欠费）；其余/网络错误=error。
func (s *Service) checkAPIKeyStatus(ctx context.Context, baseURL, key, endpointID string, timeout time.Duration, pool []string, modelsURL, upstreamType string, headers []HeaderItem, index int) KeyCheckResult {
	result := KeyCheckResult{Index: index, Key: key}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gemini := normalizeUpstreamType(upstreamType) == upstreamTypeGemini
	vertex := normalizeUpstreamType(upstreamType) == upstreamTypeVertex
	var reqURL string
	var reqMethod string
	var reqBody io.Reader
	switch {
	case gemini:
		reqURL = geminiModelsURL(baseURL)
		reqMethod = "GET"
	case vertex:
		// Vertex 不提供模型列表端点（GET /models 404），用 generateContent
		// 简单请求判定 key 有效性。
		body, _ := json.Marshal(map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": "hi"}}},
			},
			"generationConfig": map[string]interface{}{"maxOutputTokens": 1},
		})
		reqURL = vertexGenerateURL(baseURL, "gemini-2.5-flash", false)
		reqMethod = "POST"
		reqBody = bytes.NewReader(body)
	default:
		reqURL = modelListURL(baseURL, modelsURL)
		reqMethod = "GET"
	}
	httpReq, err := http.NewRequestWithContext(childCtx, reqMethod, reqURL, reqBody)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}
	if gemini || vertex {
		httpReq.Header.Set("X-Goog-Api-Key", key)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	if vertex {
		httpReq.Header.Set("Content-Type", "application/json")
	}
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
	case vertex:
		// 与 verifyVertexKey 同口径：Vertex 探测用 generateContent，模型未启用/区域
		// 参数问题（4xx）不影响 key 有效性，仅 401/403/402 判为无效/欠费。
		result.Status = "valid"
	default:
		result.Status = "error"
		result.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}
