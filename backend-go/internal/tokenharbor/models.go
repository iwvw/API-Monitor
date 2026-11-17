package tokenharbor

// 本文件实现模型目录:上游模型清单(静态,来源 tokenharbor.ai /models 快照)、
// 名称归一化、免费窗口感知,以及逐模型启停。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// defaultModel 是站点默认兜底模型(免费无限)。
const defaultModel = "th-rudder:free"

// ModelInfo 是模型目录里的一项。
//
// ID 是**上游请求 id**(带 provider 前缀 / :free 后缀),转发时原样发给上游。
type ModelInfo struct {
	ID string `json:"id"`
	// Name 是友好展示名。
	Name string `json:"name,omitempty"`
	// Pool 是额度池:free / paid。
	Pool string `json:"pool,omitempty"`
	// ContextWindow / MaxOutput 来自官方模型文档(token)。
	ContextWindow int64 `json:"contextWindow,omitempty"`
	MaxOutput     int64 `json:"maxOutput,omitempty"`
}

// catalogModels 是内置模型清单(18 付费 + 4 免费,来源 models.rs 快照)。
var catalogModels = []ModelInfo{
	{ID: "anthropic/claude-opus-5.5", Name: "Claude Opus 5.5", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "anthropic/claude-fable-5.1", Name: "Claude Fable 5.1", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "openai/gpt-6-astra", Name: "GPT-6 Astra", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "openai/gpt-6-sol", Name: "GPT-6 Sol", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "openai/gpt-5.6-terra", Name: "GPT-5.6 Terra", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "openai/gpt-6-luna", Name: "GPT-6 Luna", Pool: "paid", ContextWindow: 200000, MaxOutput: 64000},
	{ID: "vercel-ai-gateway/muse-spark-1-3", Name: "Muse Spark 1.3", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "x-ai/grok-4.7", Name: "Grok 4.7", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "alibaba/qwen3.8-max", Name: "Qwen3.8 Max", Pool: "paid", ContextWindow: 256000, MaxOutput: 64000},
	{ID: "alibaba/qwen3.8-27b", Name: "Qwen3.8 27B", Pool: "paid", ContextWindow: 256000, MaxOutput: 64000},
	{ID: "z-ai/glm-5.3", Name: "GLM-5.3", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "z-ai/glm-5.3-flash", Name: "GLM 5.3 Flash", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "z-ai/glm-5.3-flashx", Name: "GLM 5.3 FlashX", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "moonshotai/kimi-k3", Name: "Kimi K3", Pool: "paid", ContextWindow: 256000, MaxOutput: 32000},
	{ID: "xiaomi/mimo-v2.6-pro", Name: "MiMo V2.6 Pro", Pool: "paid", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "google/gemini-3.8-flash", Name: "Gemini 3.8 Flash", Pool: "paid", ContextWindow: 1000000, MaxOutput: 64000},
	{ID: "vercel-ai-gateway/deepseek/deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Pool: "paid", ContextWindow: 1000000, MaxOutput: 384000},
	{ID: "alibaba/qwen3.8-flash", Name: "Qwen3.8 Flash", Pool: "paid", ContextWindow: 256000, MaxOutput: 64000},
	{ID: "xiaomi/mimo-v2.6-flash:free", Name: "MiMo V2.6 Flash", Pool: "free", ContextWindow: 200000, MaxOutput: 32000},
	{ID: "alibaba/qwen3.8-flash:free", Name: "Qwen3.8 Flash", Pool: "free", ContextWindow: 256000, MaxOutput: 64000},
	{ID: "vercel-ai-gateway/deepseek/deepseek-v4.1-flash:free", Name: "DeepSeek V4.1 Flash", Pool: "free", ContextWindow: 1000000, MaxOutput: 384000},
	{ID: "th-rudder:free", Name: "TH-Rudder", Pool: "free", ContextWindow: 1000000, MaxOutput: 4096},
}

// freeWindows 是免费模型的窗口结束时刻(空=常驻)。
var freeWindows = map[string]string{
	"alibaba/qwen3.8-flash:free": "2026-09-27T13:00:00Z",
}

// surfaceProvider 是裸 surface → provider 前缀映射(来自站点逆向)。
var surfaceProvider = map[string]string{
	"claude-opus-5.5":     "anthropic",
	"claude-fable-5.1":    "anthropic",
	"gpt-6-astra":         "openai",
	"gpt-6-sol":           "openai",
	"gpt-5.6-terra":       "openai",
	"gpt-6-luna":          "openai",
	"muse-spark-1-3":      "vercel-ai-gateway",
	"grok-4.7":            "x-ai",
	"qwen3.8-max":         "alibaba",
	"qwen3.8-flash":       "alibaba",
	"qwen3.8-27b":         "alibaba",
	"glm-5.3":             "z-ai",
	"glm-5.3-flash":       "z-ai",
	"glm-5.3-flashx":      "z-ai",
	"kimi-k3":             "moonshotai",
	"mimo-v2.6-flash":     "xiaomi",
	"mimo-v2.6-pro":       "xiaomi",
	"gemini-3.8-flash":    "google",
	"deepseek-v4.1-flash": "vercel-ai-gateway/deepseek",
	"th-rudder":           "th-rudder",
}

// catalog 返回模型目录快照。
func (s *Service) catalog() []ModelInfo {
	return catalogModels
}

// hasModel 判断给定 id 是否在目录中。
func hasModel(id string) bool {
	for _, m := range catalogModels {
		if m.ID == id {
			return true
		}
	}
	return false
}

// modelAvailable 判定模型当前是否可用(免费窗口到期视为不可用)。
func modelAvailable(id string, now time.Time) bool {
	if !hasModel(id) {
		return false
	}
	if until, ok := freeWindows[id]; ok {
		if t, err := time.Parse(time.RFC3339, until); err == nil && now.After(t) {
			return false
		}
	}
	return true
}

// normalizeModel 把请求 id 归一化到目录 id:
//   - `qwen3.8-flash` → 免费路线 `alibaba/qwen3.8-flash:free`
//   - 带 provider 前缀的完整 id 原样
//   - 未知原样返回(上游给可读错误)
func normalizeModel(requested string) string {
	r := strings.TrimSpace(requested)
	if r == "" {
		return defaultModel
	}
	if hasModel(r) {
		return r
	}
	surface := r
	wantFree := false
	if s, ok := strings.CutSuffix(r, ":free"); ok {
		surface = s
		wantFree = true
	}
	if strings.Contains(surface, "/") {
		return r
	}
	if provider, ok := surfaceProvider[surface]; ok {
		freeID := provider + "/" + surface + ":free"
		if hasModel(freeID) {
			return freeID
		}
		paidID := provider + "/" + surface
		if hasModel(paidID) {
			return paidID
		}
	}
	if wantFree {
		return surface + ":free"
	}
	return r
}

// resolveModel 归一化并做免费窗口降级:不可用时回退到首个可用免费模型,再回退兜底。
func resolveModel(requested string) string {
	norm := normalizeModel(requested)
	if modelAvailable(norm, time.Now()) {
		return norm
	}
	for _, m := range catalogModels {
		if m.Pool == "free" && modelAvailable(m.ID, time.Now()) {
			return m.ID
		}
	}
	return defaultModel
}

// isModelEnabled 判断给定模型名(可带前缀)是否启用。
func (s *Service) isModelEnabled(model string) bool {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return false
	}
	if !hasModel(name) {
		return false
	}
	return !s.disabledMatchTokens()[strings.ToLower(name)]
}

// disabledMatchTokens 返回停用名单归一化的 token 集合(小写、去前缀)。
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

// enabledModelIDs 返回对外(带前缀)且未被停用的模型 ID 列表。
func (s *Service) enabledModelIDs() []string {
	disabled := s.disabledMatchTokens()
	out := make([]string, 0, len(catalogModels))
	for _, m := range catalogModels {
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
	disabled := s.disabledMatchTokens()
	out := make([]map[string]interface{}, 0, len(catalogModels))
	for _, m := range catalogModels {
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
		"upstreamReady": len(catalogModels) > 0,
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
			// 启用:把停用名单里指向同一模型的条目(含旧前缀残留)一并移除。
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
