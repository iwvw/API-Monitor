package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// catalogURL 是 OpenCode 官方能力目录（opencode CLI 自身使用的模型元数据源）。
// 免费模型清单以此为准：免费 ID 会轮换，目录始终给出当前集合与成本/废弃状态。
// 参考实现 opencode2api 也用同一数据源做动态模型发现。
const catalogURL = "https://models.opencode.ai/api.json"

// 自动发现/探测的节奏（均为绝对时长，与时区无关，不涉及日期归属）。
const (
	catalogRefreshInterval = 24 * time.Hour   // 能力目录刷新周期
	catalogFetchTimeout    = 60 * time.Second // 单次目录抓取超时（共享客户端本身无超时）
	catalogRetryBackoff    = 30 * time.Minute // 目录抓取失败后的重试退避
	discoveryTickInterval  = time.Minute      // 后台检查周期
	probeRecheckInterval   = time.Hour        // 已启用/暂态模型复查间隔
	probeExcludedInterval  = 24 * time.Hour   // 确认退役模型复查间隔（自动恢复）
	probeTimeout           = 30 * time.Second
	probeConcurrency       = 4
)

// DiscoveredModel 是自动发现的免费模型及其可用性状态。
//
// 可用性状态机（对齐参考实现 opencode2api 的语义）：
//   - Available=true：探测成功，正常对外服务，定期复查。
//   - Excluded=true：上游明确报告模型不存在/退役，排除出目录，24h 后复查以便自动恢复。
//   - 其余（未探测/暂态失败）：保持乐观包含在目录里，不因限流/网络/FreeTierError 误杀。
type DiscoveredModel struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	ContextLength     int64  `json:"contextLength,omitempty"`
	MaxOutputTokens   int64  `json:"maxOutputTokens,omitempty"`
	SupportsReasoning bool   `json:"supportsReasoning,omitempty"`
	SupportsToolCall  bool   `json:"supportsToolCall,omitempty"`
	Description       string `json:"description,omitempty"`
	// Protocol 是上游协议："chat"（/zen/v1/chat/completions）或
	// "responses"（/zen/v1/responses）。目录不标注协议，探测/既有规则决定。
	Protocol string `json:"protocol,omitempty"`
	// Available 是否探测成功。
	Available bool `json:"available,omitempty"`
	// Excluded 是否明确确认退役/不存在。
	Excluded bool `json:"excluded,omitempty"`
	// NextProbeAt 下次探测时间（RFC3339 UTC）；空表示立即可探测。
	NextProbeAt string `json:"nextProbeAt,omitempty"`
	// LastChecked 最近一次探测时间（RFC3339 UTC）。
	LastChecked string `json:"lastChecked,omitempty"`
	// LastError 最近一次探测的错误摘要。
	LastError string `json:"lastError,omitempty"`
}

// toModelInfo 转成对外目录条目。
func (d DiscoveredModel) toModelInfo() ModelInfo {
	return ModelInfo{
		ID:                d.ID,
		DisplayName:       d.DisplayName,
		ContextLength:     d.ContextLength,
		MaxOutputTokens:   d.MaxOutputTokens,
		SupportsImages:    false,
		SupportsReasoning: d.SupportsReasoning,
		SupportsToolCall:  d.SupportsToolCall,
		Description:       d.Description,
	}
}

// catalogModel 是 models.opencode.ai/api.json 里单个模型的元数据。
type catalogModel struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Cost        map[string]interface{} `json:"cost"`
	Limit       map[string]interface{} `json:"limit"`
	Reasoning   bool                   `json:"reasoning"`
	ToolCall    bool                   `json:"tool_call"`
	Status      string                 `json:"status"`
}

// catalogProvider 是目录里一个 provider 的条目。
type catalogProvider struct {
	Models map[string]catalogModel `json:"models"`
}

// openCodeCatalog 是目录顶层，按 provider ID 索引。
type openCodeCatalog map[string]catalogProvider

// filterCatalogFreeModels 从目录里筛出 opencode provider 的可用免费模型。
// 规则与参考实现 opencode2api 一致：模型 ID 含 "free"（不区分大小写）且未标记
// 废弃且输入/输出成本为 0。目录本身已给出当前免费集合，退役模型会被标记
// deprecated 而自动排除；同时补全上下文/输出上限与能力元数据。
func filterCatalogFreeModels(cat openCodeCatalog) []DiscoveredModel {
	prov, ok := cat["opencode"]
	if !ok {
		return nil
	}
	out := make([]DiscoveredModel, 0, len(prov.Models))
	for id, m := range prov.Models {
		if !strings.Contains(strings.ToLower(id), "free") {
			continue
		}
		if strings.EqualFold(m.Status, "deprecated") {
			continue
		}
		if m.Cost != nil {
			in, _ := m.Cost["input"].(float64)
			outCost, _ := m.Cost["output"].(float64)
			if in != 0 || outCost != 0 {
				continue
			}
		}
		dm := DiscoveredModel{
			ID:                m.ID,
			DisplayName:       m.Name,
			SupportsReasoning: m.Reasoning,
			SupportsToolCall:  m.ToolCall,
			Description:       m.Description,
		}
		if dm.ID == "" {
			dm.ID = id
		}
		if lim := m.Limit; lim != nil {
			if c, ok := lim["context"].(float64); ok && c > 0 {
				dm.ContextLength = int64(c)
			}
			if o, ok := lim["output"].(float64); ok && o > 0 {
				dm.MaxOutputTokens = int64(o)
			}
		}
		// 沿用既有路由约定：muse-spark 系列只在 /zen/v1/responses 上游存在。
		if strings.HasPrefix(dm.ID, "muse-spark-") {
			dm.Protocol = "responses"
		} else {
			dm.Protocol = "chat"
		}
		out = append(out, dm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// fetchCatalog 拉取并解析 OpenCode 官方能力目录。
func fetchCatalog(ctx context.Context, client *http.Client) (openCodeCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("catalog http %d", resp.StatusCode)
	}
	var cat openCodeCatalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&cat); err != nil {
		return nil, err
	}
	if len(cat) == 0 {
		return nil, fmt.Errorf("empty catalog")
	}
	return cat, nil
}

// mergeDiscovered 把目录最新免费集合与既有探测状态合并：
// 保留还在目录里的模型的可用性/探测状态与协议，丢弃已不在目录里的模型，加入新模型。
func mergeDiscovered(existing, fresh []DiscoveredModel) []DiscoveredModel {
	byID := make(map[string]DiscoveredModel, len(existing))
	for _, d := range existing {
		byID[d.ID] = d
	}
	out := make([]DiscoveredModel, 0, len(fresh))
	for _, f := range fresh {
		if prev, ok := byID[f.ID]; ok {
			f.Available = prev.Available
			f.Excluded = prev.Excluded
			f.NextProbeAt = prev.NextProbeAt
			f.LastChecked = prev.LastChecked
			f.LastError = prev.LastError
			if f.Protocol == "" {
				f.Protocol = prev.Protocol
			}
		}
		out = append(out, f)
	}
	return out
}

// effectiveCatalogFrom 由设置快照计算应对外服务的模型目录：自动发现开启且
// 已有发现结果时用发现结果（排除确认退役项），否则回退内置种子（远端不可达时仍可用）。
func effectiveCatalogFrom(st Settings) []ModelInfo {
	if st.AutoDetect && len(st.DiscoveredModels) > 0 {
		out := make([]ModelInfo, 0, len(st.DiscoveredModels))
		for _, d := range st.DiscoveredModels {
			if d.Excluded {
				continue
			}
			out = append(out, d.toModelInfo())
		}
		return out
	}
	return append([]ModelInfo(nil), staticModels...)
}

// effectiveIDsFrom 由设置快照计算应对外暴露（带前缀）的模型 ID 集合，用于
// 判断目录变化是否需要同步网关端点 models 列。
func effectiveIDsFrom(st Settings) []string {
	models := effectiveCatalogFrom(st)
	p := strings.TrimSpace(st.ModelPrefix)
	out := make([]string, 0, len(models))
	for _, m := range models {
		if p == "" {
			out = append(out, m.ID)
		} else {
			out = append(out, p+m.ID)
		}
	}
	return out
}

// sessionReady 判断是否配置了可重放给上游的 OpenCode 会话身份。
func sessionReady(st Settings) bool {
	return strings.TrimSpace(st.Session.XSessionID) != ""
}

// probeDue 判断模型是否到了探测时间（NextProbeAt 为空表示立即可探测）。
func probeDue(d DiscoveredModel, now time.Time) bool {
	if d.NextProbeAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, d.NextProbeAt)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

// probeSSEOk 从上游 SSE 文本里判断是否出现有效完成事件（非错误事件）。
func probeSSEOk(text string) bool {
	ok := false
	forEachSSEData(text, func(obj map[string]interface{}) {
		if _, isErr := obj["error"]; isErr {
			return
		}
		if choices, hasChoices := obj["choices"].([]interface{}); hasChoices && len(choices) > 0 {
			ok = true
			return
		}
		if typ, _ := obj["type"].(string); strings.HasPrefix(typ, "response.") {
			ok = true
		}
	})
	return ok
}

// modelMissingText 判断上游错误文本是否明确表示模型不存在/已退役。
func modelMissingText(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range []string{
		"not found", "unknown model", "does not exist", "doesn't exist",
		"no such model", "model not available", "not exist",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// summarize 截断错误文本用于 LastError（避免存大块 HTML）。
func summarize(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 300 {
		return text[:300]
	}
	return text
}

// probeModel 对单个模型发起一次最小探测请求。
//
// 返回 (available, excluded, errText)：
//   - available=true：探测成功，模型可用。
//   - excluded=true：上游明确报告模型不存在/退役，应排除。
//   - 其余（FreeTierError/限流/5xx/网络/畸形响应）：暂态，不改变可用性结论。
//
// 探测不写回代理池健康状态（对齐参考实现：探测流量不改生产 cooldown）。
func (s *Service) probeModel(ctx context.Context, m DiscoveredModel, session SessionIdentity, apiKey, poolID string) (bool, bool, string) {
	var body map[string]interface{}
	path := "/zen/v1/chat/completions"
	if m.Protocol == "responses" {
		path = "/zen/v1/responses"
		body = map[string]interface{}{
			"model":  m.ID,
			"input":  []map[string]interface{}{{"role": "user", "content": "ping"}},
			"stream": true,
			"store":  false,
		}
		ensureResponsesTools(body)
	} else {
		body = map[string]interface{}{
			"model":    m.ID,
			"messages": []interface{}{map[string]interface{}{"role": "user", "content": "ping"}},
		}
		ensureChatTools(body)
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return false, false, "marshal: " + err.Error()
	}

	_, client := s.selectProxyAndClient(ctx, poolID, "probe")
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	resp, err := s.zenUpstream(pctx, client, path, apiKey, session, reqBody)
	if err != nil {
		return false, false, "transport: " + err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	text := string(raw)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if probeSSEOk(text) {
			return true, false, ""
		}
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != 439 {
		if modelMissingText(text) {
			return false, true, "model unavailable: " + summarize(text)
		}
	}
	return false, false, summarize(text)
}

// -----------------------------------------------------------------------------
// 后台调度
// -----------------------------------------------------------------------------

// StartDiscovery 启动后台自动发现/探测循环，由 server 在启动时以 warmupCtx 调用。
// 插件关闭或未开启自动发现时静默跳过，不消耗免费额度。
func (s *Service) StartDiscovery(ctx context.Context) {
	go s.discoveryLoop(ctx)
}

func (s *Service) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(discoveryTickInterval)
	defer ticker.Stop()
	s.discoveryPass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.discoveryPass(ctx)
		}
	}
}

// discoveryPass 单轮检查：目录刷新 + 可用性探测。
func (s *Service) discoveryPass(ctx context.Context) {
	st := s.Settings()
	if !st.Enabled || !st.AutoDetect {
		return
	}
	now := time.Now()
	if st.LastCatalogSync == "" || now.Sub(parseRFC3339(st.LastCatalogSync)) >= catalogRefreshInterval {
		if s.catalogBackoffPending(now) {
			return
		}
		s.refreshCatalog(ctx)
		st = s.Settings()
	}
	// 未配置会话身份时无法通过免费层门控，跳过探测（目录保持乐观包含）。
	if !sessionReady(st) {
		return
	}
	s.probeDueModels(ctx)
}

// catalogBackoffPending 判断目录抓取失败后是否仍在退避期（避免离线时每分钟重试）。
func (s *Service) catalogBackoffPending(now time.Time) bool {
	s.mu.RLock()
	last := s.lastCatalogFail
	s.mu.RUnlock()
	return !last.IsZero() && now.Sub(last) < catalogRetryBackoff
}

// refreshCatalog 拉取官方能力目录并合并现有状态；失败时保留现有目录并进入退避，下次再试。
func (s *Service) refreshCatalog(ctx context.Context) {
	st := s.Settings()
	_, client := s.selectProxyAndClient(ctx, st.ProxyPoolID, "catalog")
	cctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	cat, err := fetchCatalog(cctx, client)
	if err != nil {
		s.mu.Lock()
		s.lastCatalogFail = time.Now()
		s.mu.Unlock()
		return
	}
	fresh := filterCatalogFreeModels(cat)
	if len(fresh) == 0 {
		s.mu.Lock()
		s.lastCatalogFail = time.Now()
		s.mu.Unlock()
		return
	}
	next := s.Settings()
	next.DiscoveredModels = mergeDiscovered(next.DiscoveredModels, fresh)
	next.LastCatalogSync = time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	s.lastCatalogFail = time.Time{}
	s.mu.Unlock()
	_ = s.SaveSettings(ctx, next)
}

// probeDueModels 对到期的模型做并发探测并一次性落库。
func (s *Service) probeDueModels(ctx context.Context) {
	st := s.Settings()
	var due []int
	for i := range st.DiscoveredModels {
		if probeDue(st.DiscoveredModels[i], time.Now()) {
			due = append(due, i)
		}
	}
	if len(due) == 0 {
		return
	}

	type outcome struct {
		idx int
		dm  DiscoveredModel
	}
	results := make([]outcome, 0, len(due))
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, probeConcurrency)
	for _, i := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			dm := st.DiscoveredModels[i]
			avail, excl, errText := s.probeModel(ctx, dm, st.Session, st.APIKey, st.ProxyPoolID)
			now := time.Now().UTC()
			dm.Available = avail
			dm.LastChecked = now.Format(time.RFC3339)
			dm.LastError = errText
			if excl {
				dm.Excluded = true
				dm.NextProbeAt = now.Add(probeExcludedInterval).Format(time.RFC3339)
			} else {
				// 成功或暂态：不排除，一小时后复查。
				dm.Excluded = false
				dm.NextProbeAt = now.Add(probeRecheckInterval).Format(time.RFC3339)
			}
			mu.Lock()
			results = append(results, outcome{idx: i, dm: dm})
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	next := st
	for _, r := range results {
		next.DiscoveredModels[r.idx] = r.dm
	}
	_ = s.SaveSettings(ctx, next)
}

// parseRFC3339 解析 RFC3339 时间；解析失败返回零值（视为很久以前，立即可处理）。
func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
