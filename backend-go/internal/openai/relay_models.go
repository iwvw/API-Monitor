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
)

func (s *Service) endpointHasModel(ep Endpoint, requested string) bool {
	for _, m := range ep.Models {
		if m == requested {
			return true
		}
	}
	for _, alias := range ep.ModelMappings {
		if alias == requested {
			return true
		}
	}
	return false
}

func (s *Service) GetModelsList(ctx context.Context, anonymizeOwner bool) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, enabled, status, models, disabled_models, model_mappings FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]map[string]interface{})
	for rows.Next() {
		var name, status, modelsRaw string
		var enabledInt int
		var disabledRaw, mappingsRaw sql.NullString
		if err := rows.Scan(&name, &enabledInt, &status, &modelsRaw, &disabledRaw, &mappingsRaw); err == nil {
			var models []string
			if modelsRaw != "" {
				_ = json.Unmarshal([]byte(modelsRaw), &models)
			}
			disabled := []string{}
			if disabledRaw.Valid && disabledRaw.String != "" {
				_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
			}
			mappings := map[string]string{}
			if mappingsRaw.Valid && mappingsRaw.String != "" {
				_ = json.Unmarshal([]byte(mappingsRaw.String), &mappings)
			}
			for _, mID := range models {
				if isModelDisabled(disabled, mID) {
					continue
				}
				// 对外名称：存在映射时使用别名。
				externalID := mID
				if alias := mappings[mID]; alias != "" {
					externalID = alias
				}
				if _, ok := modelMap[externalID]; !ok {
					owner := name
					if anonymizeOwner {
						// 外部统一出口不泄漏内部端点名。
						owner = "api-monitor-gateway"
					}
					modelMap[externalID] = map[string]interface{}{
						"id":       externalID,
						"object":   "model",
						"created":  time.Now().Unix(),
						"owned_by": owner,
					}
				}
			}
		}
	}

	modelList := []map[string]interface{}{}
	for _, m := range modelMap {
		modelList = append(modelList, m)
	}
	return modelList, nil
}

func (s *Service) proxyModels(w http.ResponseWriter, r *http.Request) {
	// 外部统一出口（/v1）匿名化 owned_by，不泄漏内部端点名；
	// 管理面板入口（/api/openai）保留端点归属用于按端点筛选模型。
	anonymize := !strings.HasPrefix(r.URL.Path, "/api/openai")
	modelList, err := s.GetModelsList(r.Context(), anonymize)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 网关密钥白名单：不在白名单里的模型不暴露给该密钥。
	if keyIdentity := gatewayKeyFromContext(r.Context()); keyIdentity.ID != "" {
		modelList = filterModelsByKey(keyIdentity, modelList)
	}

	sort.Slice(modelList, func(i, j int) bool {
		idI, _ := modelList[i]["id"].(string)
		idJ, _ := modelList[j]["id"].(string)
		return idI < idJ
	})

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   modelList,
	})
}

// ==================== Helper methods ====================

// ensureVersionPath 保证基址带 OpenAI 兼容版本段：已含 /v\d+ 段（如 /v1、
// /api/v3）则原样返回，否则补 /v1。ark 等上游以 /api/v3 为版本前缀，若沿用
// 「以 /v1 结尾或含 /v1/」的旧判断会把 /api/v3 误拼成 /api/v3/v1/... 而 404。
func ensureVersionPath(baseURL string) string {
	u := strings.TrimSuffix(baseURL, "/")
	if versionPathRegex.MatchString(u) {
		return u
	}
	return u + "/v1"
}

func (s *Service) normalizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")

	stripSuffixes := []string{"/chat/completions", "/completions", "/models", "/embeddings"}
	for _, suffix := range stripSuffixes {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			u = u[:len(u)-len(suffix)]
			u = strings.TrimSuffix(u, "/")
		}
	}

	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}

	// Append version path if missing
	hasVersion := false
	if reg := versionPathRegex; reg.MatchString(u) {
		hasVersion = true
	}
	if !hasVersion {
		u += "/v1"
	}

	return u
}
