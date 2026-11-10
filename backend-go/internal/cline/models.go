package cline

// 本文件实现模型目录的对外视图与逐模型启停。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// ModelInfo 是模型目录里的一项。
//
// ID 是**上游完整 id**（含 `cline-free/` / `cline-pass/` 池前缀）——
// 那是计费通道选择器，转发时必须原样带上，剥掉会 404 `model not found`。
type ModelInfo struct {
	ID string `json:"id"`
	// Name 是上游给的友好名（可能不含池前缀）。
	Name string `json:"name,omitempty"`
	// Pool 是额度池：free / pass（来自目录分组，不按 id 前缀猜）。
	Pool string `json:"pool,omitempty"`
}

// modelCacheTTL 是模型目录缓存时长。
const modelCacheTTL = 10 * time.Minute

// modelRetryBackoff 是回源失败后的最短重试间隔。
const modelRetryBackoff = time.Minute

// catalog 返回当前模型目录快照（未加前缀）。
func (s *Service) catalog() []ModelInfo {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	return append([]ModelInfo(nil), s.models...)
}

// ensureCatalog 用任一可用账号回源拉取目录（插件启用且有账号时）。
func (s *Service) ensureCatalog(ctx context.Context) {
	if !s.Settings().Enabled {
		return
	}
	s.modelMu.Lock()
	stale := len(s.models) == 0 || time.Since(s.cacheAt) >= modelCacheTTL
	backoff := !s.fetchFailAt.IsZero() && time.Since(s.fetchFailAt) < modelRetryBackoff
	s.modelMu.Unlock()
	if !stale || backoff {
		return
	}
	acc, ok := s.pickAccount(nil)
	if !ok {
		return
	}
	if err := s.ensureToken(ctx, &acc); err != nil {
		applog.Warn(ctx, "cline", "catalog token refresh failed", "account", acc.ID, "error", err.Error())
		return
	}
	models, err := s.fetchCatalog(ctx, acc.AccessToken)
	if err != nil {
		s.modelMu.Lock()
		s.fetchFailAt = time.Now()
		s.modelMu.Unlock()
		applog.Warn(ctx, "cline", "fetch model catalog failed", "error", err.Error())
		return
	}
	s.storeCatalog(models)
}

// storeCatalog 写入目录缓存。
func (s *Service) storeCatalog(models []ModelInfo) {
	if len(models) == 0 {
		return
	}
	s.modelMu.Lock()
	s.models = models
	s.cacheAt = time.Now()
	s.fetchFailAt = time.Time{}
	s.modelMu.Unlock()
}

// StartModelRefresh 后台定期回源刷新模型目录；ctx 取消时退出。启动时先立刻拉一次。
func (s *Service) StartModelRefresh(ctx context.Context) {
	go func() {
		s.ensureCatalog(ctx)
		s.syncLinkedEndpointModels(ctx)
		ticker := time.NewTicker(modelCacheTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.ensureCatalog(ctx)
				s.syncLinkedEndpointModels(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// isModelEnabled 判断给定模型名（可带前缀）是否启用。
//
// 白名单语义：不在目录内的模型一律拒绝，避免上游对未知模型回 404 或静默降级。
func (s *Service) isModelEnabled(model string) bool {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return false
	}
	found := false
	for _, m := range s.catalog() {
		if strings.EqualFold(m.ID, name) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return !s.disabledMatchTokens()[strings.ToLower(name)]
}

// disabledMatchTokens 返回停用名单归一化的 token 集合（小写、去前缀）。
func (s *Service) disabledMatchTokens() map[string]bool {
	set := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(d)))
		if name != "" {
			set[strings.ToLower(name)] = true
		}
	}
	return set
}

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表。
func (s *Service) enabledModelIDs() []string {
	models := s.catalog()
	disabled := s.disabledMatchTokens()
	out := make([]string, 0, len(models))
	for _, m := range models {
		if disabled[strings.ToLower(m.ID)] {
			continue
		}
		out = append(out, s.prefixModel(m.ID))
	}
	return out
}

func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	models := s.catalog()
	if len(models) == 0 {
		// 目录为空时同步回源一次：让「刷新」与首次打开不必等 10 分钟定时器。
		s.ensureCatalog(r.Context())
		models = s.catalog()
	}
	disabled := s.disabledMatchTokens()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		entry := map[string]interface{}{
			"id":      s.prefixModel(m.ID),
			"enabled": !disabled[strings.ToLower(m.ID)],
		}
		if m.Name != "" {
			entry["name"] = m.Name
		}
		if m.Pool != "" {
			entry["pool"] = m.Pool
		}
		out = append(out, entry)
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": len(models) > 0,
		"models":        out,
	})
}

func (s *Service) handleToggleModel(w http.ResponseWriter, r *http.Request, modelID string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 enabled"})
		return
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少模型 ID"})
		return
	}
	if err := s.setModelsEnabled(r.Context(), []string{modelID}, *body.Enabled); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

func (s *Service) handleBatchToggleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Enabled *bool    `json:"enabled"`
		Models  []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 enabled"})
		return
	}
	if err := s.setModelsEnabled(r.Context(), body.Models, *body.Enabled); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// setModelsEnabled 把给定模型名批量置为启用/停用并持久化。
func (s *Service) setModelsEnabled(ctx context.Context, ids []string, enabled bool) error {
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		prefixed := s.prefixModel(strings.TrimSpace(s.stripModelPrefix(id)))
		if enabled {
			// 启用：把停用名单里指向同一模型的条目（含旧前缀残留）一并移除。
			name := strings.ToLower(strings.TrimSpace(s.stripModelPrefix(id)))
			for existing := range disabled {
				if strings.ToLower(strings.TrimSpace(s.stripModelPrefix(existing))) == name {
					delete(disabled, existing)
				}
			}
		} else {
			disabled[prefixed] = true
		}
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	sort.Strings(st.DisabledModels)
	return s.SaveSettings(ctx, st)
}
