package modelhub

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
	var headersRaw, proxyRaw, proxyPoolIDRaw, protocolRaw sql.NullString
	var proxyEnabledInt, forceProxyInt int
	// models_url 为 NULL（插件注册等历史行）时按空串处理。
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, api_key, headers, proxy_pool, proxy_pool_id, proxy_enabled, force_proxy, protocol, upstream_type FROM modelhub_endpoints WHERE id = ?", id).Scan(&baseURL, &modelsURLRaw, &apiKey, &headersRaw, &proxyRaw, &proxyPoolIDRaw, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &upstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	modelsURL := modelsURLRaw.String
	apiKey = secure.SecureDecrypt(apiKey)
	upstreamType := normalizeUpstreamType(upstreamTypeRaw.String)
	ep := auxEndpoint{
		id:           id,
		pool:         decodeProxyPool(proxyRaw),
		proxyPoolID:  proxyPoolIDRaw.String,
		proxyEnabled: proxyEnabledInt == 1,
		forceProxy:   forceProxyInt == 1,
		protocol:     protocolRaw.String,
	}

	modelsList, pricing, err := s.listModelsWithPricing(modelsCtx, baseURL, apiKey, ep, modelsURL, upstreamType, decodeEndpointHeaders(headersRaw))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	checkedAt := time.Now().Format(time.RFC3339)

	if upstreamType == upstreamTypeVertex {
		// Vertex AI 不提供模型列表端点，models 全部来自手动添加：刷新时
		// 保留现有列表（不覆盖），仅更新 last_checked。
		var modelsRaw, pricingRaw sql.NullString
		_ = db.QueryRowContext(ctx, "SELECT models, pricing FROM modelhub_endpoints WHERE id = ?", id).Scan(&modelsRaw, &pricingRaw)
		existing := []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &existing)
		}
		existingPricing := PricingMap{}
		if pricingRaw.Valid && pricingRaw.String != "" {
			_ = json.Unmarshal([]byte(pricingRaw.String), &existingPricing)
		}
		_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET last_checked = ? WHERE id = ?", checkedAt, id)
		s.invalidateRouteCache()
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"models":  existing,
			"pricing": existingPricing,
		})
		return
	}

	if len(modelsList) == 0 {
		// 拉取「成功」但上游返回空列表：视为上游异常/临时响应，保留库中旧模型
		// 与旧定价——否则一次空响应会清空 models，下次刷新会把全部模型判成
		// 「新增」并入停用名单，静默批量停用用户已启用的模型。仅更新 last_checked。
		_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET last_checked = ? WHERE id = ?", checkedAt, id)
		s.invalidateRouteCache()
		var emptyModelsRaw, emptyPricingRaw, emptyDisabledRaw sql.NullString
		_ = db.QueryRowContext(ctx, "SELECT models, pricing, disabled_models FROM modelhub_endpoints WHERE id = ?", id).Scan(&emptyModelsRaw, &emptyPricingRaw, &emptyDisabledRaw)
		existing := []string{}
		if emptyModelsRaw.Valid && strings.TrimSpace(emptyModelsRaw.String) != "" {
			_ = json.Unmarshal([]byte(emptyModelsRaw.String), &existing)
		}
		existingPricing := PricingMap{}
		if emptyPricingRaw.Valid && strings.TrimSpace(emptyPricingRaw.String) != "" {
			_ = json.Unmarshal([]byte(emptyPricingRaw.String), &existingPricing)
		}
		curDisabled := []string{}
		if emptyDisabledRaw.Valid && strings.TrimSpace(emptyDisabledRaw.String) != "" {
			_ = json.Unmarshal([]byte(emptyDisabledRaw.String), &curDisabled)
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"models":         existing,
			"pricing":        existingPricing,
			"disabledModels": curDisabled,
			"emptyUpstream":  true,
		})
		return
	}

	modelsJSON, _ := json.Marshal(modelsList)
	pricingJSON, _ := json.Marshal(pricing)

	// 新增/刷新模型默认停用：新出现（不在旧 models）的模型并入 disabled_models。
	// 读取旧状态失败时保守跳过 disabled 覆盖——把全量模型误判为新增会批量
	// 停用用户已启用的模型，比少合并一次严重得多。
	var curModelsRaw, disabledRaw sql.NullString
	disabledJSON := ""
	if err := db.QueryRowContext(ctx, "SELECT models, disabled_models FROM modelhub_endpoints WHERE id = ?", id).Scan(&curModelsRaw, &disabledRaw); err == nil {
		oldModels := []string{}
		if curModelsRaw.Valid && strings.TrimSpace(curModelsRaw.String) != "" {
			_ = json.Unmarshal([]byte(curModelsRaw.String), &oldModels)
		}
		curDisabled := []string{}
		if disabledRaw.Valid && strings.TrimSpace(disabledRaw.String) != "" {
			_ = json.Unmarshal([]byte(disabledRaw.String), &curDisabled)
		}
		disabledList := mergeNewModelDisabled(oldModels, modelsList, curDisabled)
		disabledBytes, _ := json.Marshal(disabledList)
		disabledJSON = string(disabledBytes)
	}
	if disabledJSON != "" {
		_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET models = ?, pricing = ?, disabled_models = ?, last_checked = ? WHERE id = ?", string(modelsJSON), string(pricingJSON), disabledJSON, checkedAt, id)
	} else {
		_, _ = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET models = ?, pricing = ?, last_checked = ? WHERE id = ?", string(modelsJSON), string(pricingJSON), checkedAt, id)
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"models":  modelsList,
	})
}

// setOf 把切片转成 map[string]bool 去重。
func setOf(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, i := range items {
		s[i] = true
	}
	return s
}

// mergeNewModelDisabled 返回「新出现模型默认停用」合并后的停用名单：newModels 里
// 不在 oldModels 的模型并入 disabled。用于新增/自动同步上游模型时保持默认关闭。
func mergeNewModelDisabled(oldModels, newModels, disabled []string) []string {
	oldSet := setOf(oldModels)
	out := setOf(disabled)
	for _, m := range newModels {
		if !oldSet[m] {
			out[m] = true
		}
	}
	list := make([]string, 0, len(out))
	for m := range out {
		list = append(list, m)
	}
	sort.Strings(list)
	return list
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
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM modelhub_endpoints WHERE id = ?", id).Scan(&disabledRaw)
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
	_, err = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
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
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM modelhub_endpoints WHERE id = ?", id).Scan(&disabledRaw)
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
	_, err = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
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
	err = db.QueryRowContext(ctx, "SELECT models, disabled_models FROM modelhub_endpoints WHERE id = ?", id).Scan(&modelsRaw, &disabledRaw)
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

	// 新增模型默认停用：next 里不在原 models 的并入 disabled_models。
	oldModels := []string{}
	if modelsRaw.Valid && strings.TrimSpace(modelsRaw.String) != "" {
		_ = json.Unmarshal([]byte(modelsRaw.String), &oldModels)
	}
	disabled := []string{}
	if disabledRaw.Valid && strings.TrimSpace(disabledRaw.String) != "" {
		_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
	}
	disabled = mergeNewModelDisabled(oldModels, next, disabled)
	disabledJSON, _ := json.Marshal(disabled)

	modelsJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE modelhub_endpoints SET models = ?, disabled_models = ? WHERE id = ?", string(modelsJSON), string(disabledJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"models":         next,
		"disabledModels": disabled,
	})
}
