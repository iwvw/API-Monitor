package qoder

// 本文件实现模型目录的**动态发现**：用已登录账号的 COSY 签名会话调用上游
// `GET /algo/api/v2/model/list`，把真实模型目录（display_name → key）拉回本地缓存。
//
// 为什么需要：静态表（models.go）取自某次客户端目录缓存实测，上游改版（增删模型、
// 重命名 display_name）后即过期，会出现「模型名不正确」；更严重的是旧静态表里
// 没有的别名（如上游重命名后的 DeepSeek 型号）能绕过按名匹配的停用名单。
//
// 协议事实来自开源反代 cli-proxy/qoder 的 models.go：目录按账号缓存，条目形如
// {key, display_name, enable, is_free, is_default, is_reasoning, is_vl,
// max_input_tokens}，按分组返回，只有 chat 分组用于对话。目录接口需签名会话，
// 无凭据时读不到（区别于公开目录）。
//
// 缓存策略：按区域缓存（同一区域多账号共享一份目录），TTL 到期后由
// ensureCatalog / 后台 StartModelRefresh 回源刷新；回源失败保留上一份快照，
// 首次拉取成功前回退静态表（models.go），避免上游抖动让列表变空。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// modelCacheTTL 是模型目录缓存时长；上游目录变动很慢，无需每次发现都回源。
const modelCacheTTL = 10 * time.Minute

// modelRetryBackoff 是回源失败后的最短重试间隔：避免上游持续失败时，
// 每次 handleModels（惰性回源）都阻塞等一次超时。
const modelRetryBackoff = time.Minute

// modelFetchTimeout 是单次回源拉取目录的超时。共享 HTTP 客户端无超时，
// 必须由 context 兜底，避免上游挂起把面板请求（handleModels 会惰性回源）拖死。
const modelFetchTimeout = 30 * time.Second

// upstreamCatalogEntry 是上游模型目录里的单项（只取对话与计费所需的字段）。
type upstreamCatalogEntry struct {
	Key            string `json:"key"`
	DisplayName    string `json:"display_name"`
	Enable         bool   `json:"enable"`
	IsFree         bool   `json:"is_free"`
	IsDefault      bool   `json:"is_default"`
	IsReasoning    bool   `json:"is_reasoning"`
	IsVL           bool   `json:"is_vl"`
	MaxInputTokens int64  `json:"max_input_tokens"`
	// PriceFactor 是**当前标价倍率**。必须用指针：0 是合法值（免费模型），
	// 只有字段缺失才代表「上游未提供」。
	PriceFactor *float64 `json:"price_factor"`
	// OriginalPriceFactor 是原价倍率（限时免费模型给出 0.0 + 原价 0.1）。
	OriginalPriceFactor *float64 `json:"original_price_factor"`
	// Promotion 是促销（错峰折扣）规则。
	Promotion *upstreamPromotion `json:"promotion"`
}

// upstreamPromotion 是上游 promotion 字段的原始结构。
type upstreamPromotion struct {
	Active          bool    `json:"active"`
	RuleID          string  `json:"rule_id"`
	DiscountFactor  float64 `json:"discount_factor"`
	BeforePromotion float64 `json:"before_promotion_price_factor"`
	WindowStart     string  `json:"window_start"`
	WindowEnd       string  `json:"window_end"`
	Timezone        string  `json:"timezone"`
	Badge           struct {
		Zh string `json:"zh"`
		En string `json:"en"`
	} `json:"badge"`
	LinkURL struct {
		Zh string `json:"zh"`
		En string `json:"en"`
	} `json:"link_url"`
}

// toPromotion 把上游结构转成对外的 Promotion；折扣非法时返回 nil。
//
// 注意：Active 只是上游的生效标记，**不参与过滤**。促销窗口由 Window* 判定，
// 上游在窗口外可能把 Active 置 false，此时仍要保留规则才能展示「标准价 + 低峰档」。
func (u *upstreamPromotion) toPromotion() *Promotion {
	if u == nil || u.DiscountFactor <= 0 {
		return nil
	}
	p := &Promotion{
		RuleID:      strings.TrimSpace(u.RuleID),
		Standard:    u.BeforePromotion,
		Discount:    u.DiscountFactor,
		WindowStart: strings.TrimSpace(u.WindowStart),
		WindowEnd:   strings.TrimSpace(u.WindowEnd),
		Timezone:    strings.TrimSpace(u.Timezone),
		Active:      u.Active,
	}
	if badge := firstNonEmpty(u.Badge.Zh, u.Badge.En); badge != "" {
		p.Badge = strings.TrimSpace(badge)
	}
	if link := firstNonEmpty(u.LinkURL.Zh, u.LinkURL.En); link != "" {
		p.LinkURL = strings.TrimSpace(link)
	}
	return p
}

// regionOrder 是区域在模型目录合并与前端展示时的固定顺序（第一个为合并打底区域）。
var regionOrder = []string{regionCN, regionIntl}

// anyAccountForRegion 返回该区域首个可用（未停用、有 token）账号。
func (s *Service) anyAccountForRegion(region string) (Account, bool) {
	want := normalizeRegion(region)
	for _, a := range s.Settings().Accounts {
		if normalizeRegion(a.Region) != want {
			continue
		}
		if a.Disabled || a.ID == "" || a.AccessToken == "" {
			continue
		}
		return a, true
	}
	return Account{}, false
}

// regionModels 返回某区域的当前目录：优先用新鲜缓存，否则用静态表兜底。
func (s *Service) regionModels(region string) []ModelInfo {
	region = normalizeRegion(region)
	s.modelMu.Lock()
	cached := s.modelCacheByRegion[region]
	at := s.modelCacheAt[region]
	s.modelMu.Unlock()
	if len(cached) > 0 && time.Since(at) < modelCacheTTL {
		return append([]ModelInfo(nil), cached...)
	}
	if len(cached) > 0 {
		// 缓存已过期但回源尚未成功：先用旧快照，避免列表回退到静态表导致模型跳变。
		return append([]ModelInfo(nil), cached...)
	}
	// 从未成功拉取：回退静态表（此时才是「静态兜底」）。
	return withRegion(staticModelsFor(region), region)
}

// modelCacheStale 报告某区域目录是否需要回源刷新。刚失败过的区域在退避窗口内
// 视为「不 stale」，避免面板连续读取时反复阻塞等超时。
func (s *Service) modelCacheStale(region string) bool {
	region = normalizeRegion(region)
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if failAt := s.modelFetchFailAt[region]; !failAt.IsZero() && time.Since(failAt) < modelRetryBackoff {
		return false
	}
	if len(s.modelCacheByRegion[region]) == 0 {
		return true
	}
	return time.Since(s.modelCacheAt[region]) >= modelCacheTTL
}

// markModelFetchFailure 记录某区域回源失败时刻，用于退避。
func (s *Service) markModelFetchFailure(region string) {
	region = normalizeRegion(region)
	s.modelMu.Lock()
	if s.modelFetchFailAt == nil {
		s.modelFetchFailAt = map[string]time.Time{}
	}
	s.modelFetchFailAt[region] = time.Now()
	s.modelMu.Unlock()
}

// storeRegionCatalog 写入某区域的目录缓存（回源成功时调用）。写入前统一补上
// 区域标记，保证缓存内容与 regionModels 的对外视图口径一致。
func (s *Service) storeRegionCatalog(region string, models []ModelInfo) {
	if len(models) == 0 {
		return
	}
	region = normalizeRegion(region)
	tagged := withRegion(models, region)
	s.modelMu.Lock()
	if s.modelCacheByRegion == nil {
		s.modelCacheByRegion = map[string][]ModelInfo{}
	}
	if s.modelCacheAt == nil {
		s.modelCacheAt = map[string]time.Time{}
	}
	s.modelCacheByRegion[region] = tagged
	s.modelCacheAt[region] = time.Now()
	s.modelMu.Unlock()
}

// ensureCatalog 确保**当前有账号的区域**目录是新鲜的：对过期/未拉取的区域逐一回源。
// 单个区域失败只记日志并保留旧快照/静态兜底，不影响其它区域。
// 插件未启用时直接跳过（不回源），面板仍可用静态兜底展示目录用于配置。
func (s *Service) ensureCatalog(ctx context.Context) {
	if !s.Settings().Enabled {
		return
	}
	regions := s.activeRegions()
	if len(regions) == 0 {
		return
	}
	for _, region := range regionOrder {
		if !regions[region] {
			continue
		}
		if !s.modelCacheStale(region) {
			continue
		}
		models, err := s.fetchRegionCatalog(ctx, region)
		if err != nil {
			s.markModelFetchFailure(region)
			applog.Warn(ctx, "qoder", "refresh model catalog failed", "region", region, "error", err.Error())
			continue
		}
		s.storeRegionCatalog(region, models)
	}
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
				// 目录变化后同步端点 models 列（如上游新增/重命名模型）。
				s.syncLinkedEndpointModels(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// refreshCatalogAsync 在后台触发一次目录回源（不阻塞调用方）。面板读取用：
// 立即返回当前缓存/静态兜底，同时把过期目录的刷新放到后台，避免面板请求
// 被上游超时拖住。refreshMu 保证同一时刻只有一次回源在跑，避免并发风暴。
func (s *Service) refreshCatalogAsync() {
	if !s.Settings().Enabled {
		return
	}
	// 仅当确有区域需要刷新时才起 goroutine（退避窗口内或新鲜时为空操作）。
	stale := false
	for region := range s.activeRegions() {
		if s.modelCacheStale(region) {
			stale = true
			break
		}
	}
	if !stale {
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
		ctx, cancel := context.WithTimeout(context.Background(), modelFetchTimeout*2)
		defer cancel()
		s.ensureCatalog(ctx)
	}()
}

// fetchRegionCatalog 用该区域任一可用账号拉取并解析上游模型目录。
func (s *Service) fetchRegionCatalog(ctx context.Context, region string) ([]ModelInfo, error) {
	acc, ok := s.anyAccountForRegion(region)
	if !ok {
		return nil, fmt.Errorf("区域 %s 无可用账号", region)
	}
	if err := s.ensureToken(ctx, &acc); err != nil {
		return nil, err
	}
	return s.fetchUpstreamCatalog(ctx, region, acc)
}

// fetchUpstreamCatalog 发一次签名 GET /algo/api/v2/model/list 并解析。
func (s *Service) fetchUpstreamCatalog(ctx context.Context, region string, acc Account) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, modelFetchTimeout)
	defer cancel()
	sess, err := newLegacySession(acc)
	if err != nil {
		return nil, err
	}
	cosyDate := fmt.Sprintf("%d", time.Now().Unix())
	payloadB64, err := legacyBuildPayloadB64(sess.Info)
	if err != nil {
		return nil, err
	}
	// GET 无请求体：签名里的 body 段为空串，path 剥去 /algo 前缀（与上游约定一致）。
	sig := legacySignRequest(payloadB64, sess.CosyKey, cosyDate, "", legacyModelListSigPath)

	url := s.modelAPIBaseFor(region) + legacyModelListPath + "?Encode=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("cosy-data-policy", "AGREE")
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("authorization", "Bearer COSY."+payloadB64+"."+sig)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("cosy-clientip", "169.254.198.161")
	req.Header.Set("cosy-date", cosyDate)
	req.Header.Set("cosy-key", sess.CosyKey)
	req.Header.Set("cosy-machineid", sess.MachineID)
	req.Header.Set("cosy-machinetoken", sess.MachineToken)
	req.Header.Set("cosy-machinetype", sess.MachineType)
	req.Header.Set("cosy-user", acc.ID)
	req.Header.Set("cosy-version", legacyCosyVersion)
	req.Header.Set("login-version", "v2")
	req.Header.Set("user-agent", "Go-http-client/2.0")

	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取模型目录失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型目录返回 HTTP %d", resp.StatusCode)
	}
	models, err := parseUpstreamCatalog(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("上游模型目录为空")
	}
	return models, nil
}

// parseUpstreamCatalog 解析上游目录响应：按分组返回，只取 chat 分组用于对话。
// 条目 display_name 作对外 id，key 作上游短 key；倍率取**上游实时值**，
// 上游未给出时才回填静态表（并标记来源 static）。
func parseUpstreamCatalog(raw []byte) ([]ModelInfo, error) {
	var groups map[string][]upstreamCatalogEntry
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("模型目录解析失败: %w", err)
	}
	entries, ok := groups["chat"]
	if !ok || len(entries) == 0 {
		// 部分部署可能不带分组键，退化为把顶层当数组解析。
		var list []upstreamCatalogEntry
		if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
			return nil, fmt.Errorf("上游目录无 chat 分组")
		}
		entries = list
	}
	out := make([]ModelInfo, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		key := strings.TrimSpace(e.Key)
		if key == "" || seen[strings.ToLower(key)] {
			continue
		}
		seen[strings.ToLower(key)] = true
		id := strings.TrimSpace(e.DisplayName)
		if id == "" {
			id = key
		}
		m := ModelInfo{ID: id, Key: key}
		// 厂商标识只能来自静态表（动态目录不带 vendor）。
		if st, ok := staticModelByKey(key); ok {
			m.Provider = st.Provider
		}
		m.LiveMultiplier = e.PriceFactor
		m.OriginalMultiplier = e.OriginalPriceFactor
		m.Free = e.IsFree || (e.PriceFactor != nil && *e.PriceFactor == 0)
		m.Promotion = e.Promotion.toPromotion()
		if m.Promotion == nil {
			// 上游没下发促销规则时用静态兜底，保持错峰折扣展示不缺失。
			m.Promotion = staticPromotionFor(key)
		}
		if e.PriceFactor != nil {
			// 实时倍率为准；但若上游把「折后价」写进 price_factor，标准倍率
			// 仍以促销规则里的原价为准（见 offpeak.go 的裁决顺序）。
			m.CreditsMultiplier = *e.PriceFactor
			m.CreditsParsed = true
			m.PriceSource = priceLive
			if m.Promotion != nil && m.Promotion.Standard > 0 {
				m.CreditsMultiplier = m.Promotion.Standard
			}
		} else if st, ok := staticModelByKey(key); ok {
			m.CreditsMultiplier = st.CreditsMultiplier
			m.CreditsParsed = true
			m.PriceSource = priceStatic
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
