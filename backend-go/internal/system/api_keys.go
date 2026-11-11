package system

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) handleAPIKeyCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := s.apiKeys.List(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, apiKeyOverview(keys))
	case http.MethodPost:
		var input apikeys.Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			response.Error(w, http.StatusBadRequest, "请求参数无效")
			return
		}
		key, err := s.apiKeys.Create(r.Context(), input)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		response.JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "key": key, "apiKey": key.APIKey})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) handleAPIKeyItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/system/api-keys/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		response.Error(w, http.StatusNotFound, "API Key 不存在")
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodPut {
		var input apikeys.Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			response.Error(w, http.StatusBadRequest, "请求参数无效")
			return
		}
		if err := s.apiKeys.Update(r.Context(), id, input); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	if len(parts) == 2 && parts[1] == "rotate" && r.Method == http.MethodPost {
		key, err := s.apiKeys.Rotate(r.Context(), id)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "apiKey": key})
		return
	}
	if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
		if err := s.apiKeys.Revoke(r.Context(), id); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
}

func apiKeyOverview(keys []apikeys.Key) map[string]interface{} {
	active, expired, revoked := 0, 0, 0
	for _, key := range keys {
		if key.RevokedAt != nil || !key.Enabled {
			revoked++
		} else if key.ExpiresAt != nil {
			if deadline, err := time.Parse(time.RFC3339, *key.ExpiresAt); err == nil && !deadline.After(time.Now()) {
				expired++
			} else {
				active++
			}
		} else {
			active++
		}
	}
	return map[string]interface{}{
		"keys":    keys,
		"summary": map[string]int{"total": len(keys), "active": active, "expired": expired, "revoked": revoked},
		"permissions": []map[string]string{
			{"value": apikeys.ScopeTOTPRead, "label": "读取 TOTP 验证码"},
			{"value": apikeys.ScopeAIMCP, "label": "调用 AI/MCP 工具"},
			{"value": apikeys.ScopeModelHubGateway, "label": "调用 OpenAI 兼容网关"},
			{"value": apikeys.ScopeAPIRead, "label": "读取后台 API"},
			{"value": apikeys.ScopeAPIWrite, "label": "修改后台 API"},
		},
	}
}
