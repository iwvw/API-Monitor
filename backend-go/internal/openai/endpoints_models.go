package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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
	var modelsURLRaw, upstreamTypeRaw sql.NullString
	var headersRaw, proxyRaw sql.NullString
	// models_url 为 NULL（插件注册等历史行）时按空串处理。
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, api_key, headers, proxy_pool, upstream_type FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &modelsURLRaw, &apiKey, &headersRaw, &proxyRaw, &upstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	modelsURL := modelsURLRaw.String
	apiKey = secure.SecureDecrypt(apiKey)
	upstreamType := normalizeUpstreamType(upstreamTypeRaw.String)

	modelsList, pricing, err := s.listModelsWithPricing(modelsCtx, baseURL, apiKey, id, decodeProxyPool(proxyRaw), modelsURL, upstreamType, decodeEndpointHeaders(headersRaw))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	checkedAt := time.Now().Format(time.RFC3339)

	if upstreamType == upstreamTypeVertex {
		// Vertex AI 不提供模型列表端点，models 全部来自手动添加：刷新时
		// 保留现有列表（不覆盖），仅更新 last_checked。
		var modelsRaw, pricingRaw sql.NullString
		_ = db.QueryRowContext(ctx, "SELECT models, pricing FROM openai_endpoints WHERE id = ?", id).Scan(&modelsRaw, &pricingRaw)
		existing := []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &existing)
		}
		existingPricing := PricingMap{}
		if pricingRaw.Valid && pricingRaw.String != "" {
			_ = json.Unmarshal([]byte(pricingRaw.String), &existingPricing)
		}
		_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_checked = ? WHERE id = ?", checkedAt, id)
		s.invalidateRouteCache()
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"models":  existing,
			"pricing": existingPricing,
		})
		return
	}

	modelsJSON, _ := json.Marshal(modelsList)
	pricingJSON, _ := json.Marshal(pricing)

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

// addEndpointModels 手动维护端点模型列表。Vertex AI 等上游不提供模型列表端点
// （无法自动拉取），需要用户手动添加/修改模型名。默认追加合并（去重）；replace=true
// 时为全量替换（文本框内容即最终列表，删除行后提交即可移除模型）。
// 入参支持一次粘贴多个模型名（逗号/换行/分号分隔）。
func (s *Service) addEndpointModels(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Models  []string `json:"models"`
		Replace bool     `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cleaned := make([]string, 0, len(req.Models))
	seen := make(map[string]bool, len(req.Models))
	for _, m := range req.Models {
		for _, piece := range strings.FieldsFunc(m, func(c rune) bool { return c == ',' || c == '\n' || c == ';' }) {
			trimmed := strings.TrimSpace(piece)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 && !req.Replace {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型名称不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var modelsRaw, disabledRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT models, disabled_models FROM openai_endpoints WHERE id = ?", id).Scan(&modelsRaw, &disabledRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	var next []string
	if req.Replace {
		// 全量替换：提交列表即最终列表。清空文本提交即清空手动模型。
		next = cleaned
	} else {
		existing := []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &existing)
		}
		set := make(map[string]bool, len(existing)+len(cleaned))
		for _, m := range existing {
			set[m] = true
		}
		for _, m := range cleaned {
			set[m] = true
		}
		next = make([]string, 0, len(set))
		for m := range set {
			next = append(next, m)
		}
	}
	sort.Strings(next)

	modelsJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET models = ? WHERE id = ?", string(modelsJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	disabled := []string{}
	if disabledRaw.Valid && disabledRaw.String != "" {
		_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"models":         next,
		"disabledModels": disabled,
	})
}
