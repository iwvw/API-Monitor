package raccoon

// 本文件实现模型目录的动态发现：用已登录账号访问 `GET {llm_base}/model_catalog`
// 拉回真实目录并按上游 id 缓存。目录取不到时回退静态兜底表，避免列表为空。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

const (
	// modelCacheTTL 是模型目录缓存时长。
	modelCacheTTL = 10 * time.Minute
	// modelRetryBackoff 是回源失败后的最短重试间隔。
	modelRetryBackoff = time.Minute
	// modelFetchTimeout 是单次回源拉取目录的超时。
	modelFetchTimeout = 30 * time.Second
)

// staticModels 是兜底目录（取自上游 model_catalog 实测快照），仅在首次成功
// 回源前使用；目录以动态拉取为准。只保留上游模型名与实时倍率。
var staticModels = []ModelInfo{
	{ID: "raccoon-090dd3", BillingMultiplier: f64(1), BillingEffectiveMultiplier: f64(1)},
	{ID: "raccoon-19b265", BillingMultiplier: f64(1), BillingEffectiveMultiplier: f64(1)},
	{ID: "raccoon-405a1c", BillingMultiplier: f64(1), BillingEffectiveMultiplier: f64(1)},
	{ID: "sn-sensenova-6-8-flash", BillingMultiplier: f64(0.5), BillingEffectiveMultiplier: f64(0)},
	{ID: "sn-sensenova-6-8-flash-lite", BillingMultiplier: f64(0.5), BillingEffectiveMultiplier: f64(0)},
	{ID: "sn-glm-5-3", BillingMultiplier: f64(0.75), BillingEffectiveMultiplier: f64(0.75)},
	{ID: "sn-kimi-k3", BillingMultiplier: f64(1), BillingEffectiveMultiplier: f64(1)},
	{ID: "sn-glm-5-3-flash", BillingMultiplier: f64(0.2), BillingEffectiveMultiplier: f64(0.2)},
	{ID: "sn-deepseek-v4-1-flash-1017", BillingMultiplier: f64(0.25), BillingEffectiveMultiplier: f64(0.25)},
}

// f64 返回 *float64，便于区分「上游明确给 0（免费）」与「上游未提供」。
func f64(v float64) *float64 { return &v }

// catalog 返回当前模型目录快照（未加前缀）。优先新鲜缓存，否则静态兜底。
func (s *Service) catalog() []ModelInfo {
	s.modelMu.Lock()
	cached := s.models
	at := s.cacheAt
	s.modelMu.Unlock()
	if len(cached) > 0 && time.Since(at) < modelCacheTTL {
		return append([]ModelInfo(nil), cached...)
	}
	if len(cached) > 0 {
		// 缓存已过期但回源尚未成功：先用旧快照，避免目录跳变。
		return append([]ModelInfo(nil), cached...)
	}
	return append([]ModelInfo(nil), staticModels...)
}

// modelCacheStale 报告目录是否需要回源刷新。
func (s *Service) modelCacheStale() bool {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if !s.fetchFailAt.IsZero() && time.Since(s.fetchFailAt) < modelRetryBackoff {
		return false
	}
	if len(s.models) == 0 {
		return true
	}
	return time.Since(s.cacheAt) >= modelCacheTTL
}

func (s *Service) markModelFetchFailure() {
	s.modelMu.Lock()
	s.fetchFailAt = time.Now()
	s.modelMu.Unlock()
}

// ensureCatalog 用任一可用账号回源拉取目录（插件启用且有账号时）。
func (s *Service) ensureCatalog(ctx context.Context) {
	if !s.Settings().Enabled {
		return
	}
	if !s.modelCacheStale() {
		return
	}
	acc, ok := s.pickAccount(nil)
	if !ok {
		return
	}
	if err := s.ensureToken(ctx, &acc); err != nil {
		return
	}
	models, err := s.fetchCatalog(ctx, acc.AccessToken)
	if err != nil {
		s.markModelFetchFailure()
		applog.Warn(ctx, "raccoon", "refresh model catalog failed", "error", err.Error())
		return
	}
	s.storeCatalog(models)
}

// storeCatalog 写入目录缓存（回源成功时调用）。
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

// refreshCatalogAsync 在后台触发一次目录回源（不阻塞调用方）。面板读取用。
func (s *Service) refreshCatalogAsync() {
	if !s.Settings().Enabled {
		return
	}
	if !s.modelCacheStale() {
		return
	}
	s.refreshMu.Lock()
	if s.refreshRunning {
		s.refreshMu.Unlock()
		return
	}
	s.refreshRunning = true
	s.refreshMu.Unlock()
	go func() {
		defer func() {
			s.refreshMu.Lock()
			s.refreshRunning = false
			s.refreshMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), modelFetchTimeout)
		defer cancel()
		s.ensureCatalog(ctx)
	}()
}

// fetchCatalog 用给定 token 拉取并解析上游模型目录。
func (s *Service) fetchCatalog(ctx context.Context, token string) ([]ModelInfo, error) {
	url := llmBaseURI + "/model_catalog"
	status, data, err := s.doJSON(ctx, s.httpClientFor("catalog"), http.MethodGet, url, nil, token,
		map[string]string{"Accept": "application/json"}, modelFetchTimeout, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("拉取模型目录失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("拉取模型目录失败: HTTP %d: %s", status, jsonMessage(data))
	}
	return parseCatalog(data)
}

type catalogModel struct {
	Name                       string   `json:"name"`
	ModelName                  string   `json:"model_name"`
	Description                string   `json:"description"`
	DisplayDescription         string   `json:"display_description"`
	Tags                       []string `json:"tags"`
	Visible                    *bool    `json:"visible"`
	ContextWindow              *float64 `json:"context_window"`
	MaxTokens                  *float64 `json:"max_tokens"`
	BillingMultiplier          *float64 `json:"billing_multiplier"`
	BillingEffectiveMultiplier *float64 `json:"billing_effective_multiplier"`
	Params                     *struct {
		ContextWindow *float64 `json:"context_window"`
		MaxTokens     *float64 `json:"max_tokens"`
	} `json:"params"`
}

// parseCatalog 把上游目录响应体解析成归一化后的模型列表。
func parseCatalog(data []byte) ([]ModelInfo, error) {
	var probe struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("解析模型目录失败: %w", err)
	}
	if probe.Code != nil && *probe.Code != 0 {
		return nil, fmt.Errorf("模型目录返回异常 code=%d message=%s", *probe.Code, probe.Message)
	}
	root := json.RawMessage(data)
	if len(probe.Data) > 0 && string(probe.Data) != "null" {
		root = probe.Data
	}
	var parsed struct {
		Categories []struct {
			Type   string         `json:"type"`
			Models []catalogModel `json:"models"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(root, &parsed); err != nil {
		return nil, fmt.Errorf("解析模型目录失败: %w", err)
	}
	out := make([]ModelInfo, 0)
	seen := map[string]bool{}
	for _, category := range parsed.Categories {
		for i := range category.Models {
			m, ok := normalizeCatalogEntry(&category.Models[i])
			if !ok {
				continue
			}
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("上游模型目录为空")
	}
	return out, nil
}

func normalizeCatalogEntry(e *catalogModel) (ModelInfo, bool) {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		name = strings.TrimSpace(e.ModelName)
	}
	if name == "" {
		return ModelInfo{}, false
	}
	// 只保留上游模型名与实时倍率：能力标签、描述、上下文档位等元数据不再使用。
	return ModelInfo{
		ID:                         name,
		BillingMultiplier:          e.BillingMultiplier,
		BillingEffectiveMultiplier: e.BillingEffectiveMultiplier,
	}, true
}
