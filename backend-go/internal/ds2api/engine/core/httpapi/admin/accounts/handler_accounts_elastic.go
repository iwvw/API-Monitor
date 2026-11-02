package accounts

import (
	"encoding/json"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// updateElasticPool 处理 PUT /admin/accounts/elastic-pool 请求。
// 开启或关闭弹性号池，并设置每批启用账号数。
// 开启时立即执行 ReconcileElasticPool 重算账号启用状态；
// 关闭时将所有账号恢复为启用。
func (h *Handler) updateElasticPool(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}

	enabled, ok := fieldBoolOptional(req, "enabled")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "enabled is required"})
		return
	}

	perPool, _ := fieldBoolOptional(req, "per_pool")
	globalCount := fieldIntOptional(req, "global_count")
	defaultCount := fieldIntOptional(req, "default_count")
	noToolsCount := fieldIntOptional(req, "no_tools_count")
	toolsOnlyCount := fieldIntOptional(req, "tools_only_count")

	if globalCount < 0 || defaultCount < 0 || noToolsCount < 0 || toolsOnlyCount < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "count cannot be negative"})
		return
	}

	if enabled && !perPool {
		defaultCount = globalCount
		noToolsCount = globalCount
		toolsOnlyCount = globalCount
	}

	var result config.ElasticPoolConfig
	err := h.Store.Update(func(c *config.Config) error {
		c.ElasticPool = config.ElasticPoolConfig{
			Enabled:        enabled,
			PerPool:        perPool,
			GlobalCount:    globalCount,
			DefaultCount:   defaultCount,
			NoToolsCount:   noToolsCount,
			ToolsOnlyCount: toolsOnlyCount,
		}
		if enabled {
			account.ReconcileElasticPool(c)
		} else {
			account.DisableAllAccounts(c)
		}
		result = c.ElasticPool
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	h.notifyAccountsChanged()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"elastic_pool": elasticPoolToMap(result),
	})
}

func elasticPoolToMap(ep config.ElasticPoolConfig) map[string]any {
	return map[string]any{
		"enabled":          ep.Enabled,
		"per_pool":         ep.PerPool,
		"global_count":     ep.GlobalCount,
		"default_count":    ep.DefaultCount,
		"no_tools_count":   ep.NoToolsCount,
		"tools_only_count": ep.ToolsOnlyCount,
	}
}

func fieldIntOptional(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}
