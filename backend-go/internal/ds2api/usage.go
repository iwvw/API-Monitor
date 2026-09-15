package ds2api

// 本文件实现 DS2API 插件的实际用量统计：把引擎每次成功请求的 token 用量按
// 「站点时区日期 × 账号 × 模型」聚合落库，供前端看"谁在用"。
//
// 为什么不用引擎自带的 usagestats：
//   - 引擎的 Store 按硬编码 GMT+8 分桶（AddCosted 里 time.FixedZone("GMT+8")），
//     与 CONTEXT.md「日期归属一律走站点时区」的硬性规则冲突；
//   - 引擎 Store 的维度是「日期 × 模型 × API Key」，不含插件账号维度。
// 因此引擎新增 SetUsageObserver 钩子把每次请求的原始 token 交给插件，由插件按
// 站点时区落自己的表（与 geminicli/workbuddy/lobsterai/antigravity 对齐）。

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	usagestats "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// usageKey 是「站点时区日期 + 账号 + 模型」的聚合键。
type usageKey struct {
	day       string
	accountID string
	model     string
}

// usageDelta 是一次上报的用量增量（同键累加）。
type usageDelta struct {
	requests         int64
	promptTokens     int64
	completionTokens int64
	cachedTokens     int64
}

func (d *usageDelta) add(o *usageDelta) {
	d.requests += o.requests
	d.promptTokens += o.promptTokens
	d.completionTokens += o.completionTokens
	d.cachedTokens += o.cachedTokens
}

// siteLocCacheTTL 是站点时区缓存时长。
const siteLocCacheTTL = 5 * time.Minute

// siteLocation 返回站点时区。读取失败时回退到 timeutil 的默认约定。
func (s *Service) siteLocation(ctx context.Context) *time.Location {
	s.locMu.Lock()
	defer s.locMu.Unlock()
	if s.locCache != nil && time.Since(s.locCacheAt) < siteLocCacheTTL {
		return s.locCache
	}
	loc := timeutil.LocationFromName("")
	if db, err := s.open(ctx); err == nil {
		loc = timeutil.LocationFromSettings(ctx, db)
		db.Close()
	}
	s.locCache, s.locCacheAt = loc, time.Now()
	return loc
}

// usageDay 把时刻归属到站点时区下的日期（YYYY-MM-DD）。
func usageDay(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// SetUsageObserver 把插件自身的用量落库逻辑注册进引擎（在 New 里调用一次）。
func (s *Service) SetUsageObserver() {
	usagestats.SetUsageObserver(s.recordUsageFromEngine)
}

// recordUsageFromEngine 是引擎每次成功请求后的回调：按站点时区累加用量。
func (s *Service) recordUsageFromEngine(model, callerID string, prompt, completion, reasoning, total int64) {
	// 引擎的 total 含 reasoning，故 completion 用「completion + reasoning」以对齐
	// 其它插件的 completion_tokens 口径（不含缓存的输出总量）。
	s.recordUsage(callerID, model, prompt, completion+reasoning, 0)
}

// recordUsage 把一次用量计入内存增量。accountID / model 为空时用占位符，
// 保证"有消耗但归不到账号"的请求也不会被静默丢掉。
func (s *Service) recordUsage(accountID, model string, prompt, completion, cached int64) {
	if prompt == 0 && completion == 0 {
		return
	}
	if strings.TrimSpace(accountID) == "" {
		accountID = "(未知账号)"
	}
	if strings.TrimSpace(model) == "" {
		model = "(未知模型)"
	}
	delta := &usageDelta{requests: 1, promptTokens: prompt, completionTokens: completion, cachedTokens: cached}
	day := usageDay(time.Now(), s.siteLocation(context.Background()))
	key := usageKey{day: day, accountID: accountID, model: model}
	s.usageMu.Lock()
	if s.usagePending == nil {
		s.usagePending = map[usageKey]*usageDelta{}
	}
	if cur := s.usagePending[key]; cur != nil {
		cur.add(delta)
	} else {
		s.usagePending[key] = delta
	}
	s.usageMu.Unlock()
}

// flushUsage 把内存增量合并进 ds2api_usage_daily（逐键 UPSERT 累加）。
// 落库失败时把增量放回，等待下轮重试，避免用量丢失。
func (s *Service) flushUsage(ctx context.Context) {
	s.usageMu.Lock()
	if len(s.usagePending) == 0 {
		s.usageMu.Unlock()
		return
	}
	pend := s.usagePending
	s.usagePending = map[usageKey]*usageDelta{}
	s.usageMu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		s.requeueUsage(pend)
		return
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	var failed map[usageKey]*usageDelta
	for k, v := range pend {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO ds2api_usage_daily
				(day, account_id, model, requests, prompt_tokens, completion_tokens, cached_tokens, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(day, account_id, model) DO UPDATE SET
				requests = requests + excluded.requests,
				prompt_tokens = prompt_tokens + excluded.prompt_tokens,
				completion_tokens = completion_tokens + excluded.completion_tokens,
				cached_tokens = cached_tokens + excluded.cached_tokens,
				updated_at = excluded.updated_at`,
			k.day, k.accountID, k.model,
			v.requests, v.promptTokens, v.completionTokens, v.cachedTokens, now); err != nil {
			// 单行失败（如 SQLITE_BUSY）不能静默丢弃：增量已从 usagePending 摘除，不回队就永久丢失。
			if failed == nil {
				failed = map[usageKey]*usageDelta{}
			}
			failed[k] = v
		}
	}
	if len(failed) > 0 {
		s.requeueUsage(failed)
	}
}

// requeueUsage 把增量放回待落盘 map。
func (s *Service) requeueUsage(pend map[usageKey]*usageDelta) {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	if s.usagePending == nil {
		s.usagePending = map[usageKey]*usageDelta{}
	}
	for k, v := range pend {
		if cur := s.usagePending[k]; cur != nil {
			cur.add(v)
		} else {
			s.usagePending[k] = v
		}
	}
}

// usageTotals 是一组用量的合计值。
type usageTotals struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
}

func (t *usageTotals) finish() {
	if t.PromptTokens > 0 {
		rate := float64(t.CachedTokens) / float64(t.PromptTokens)
		if rate > 1 {
			rate = 1
		}
		t.CacheHitRate = rate
	}
}

const usageTotalsColumns = `COALESCE(SUM(requests),0), COALESCE(SUM(prompt_tokens),0),
	COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cached_tokens),0)`

// handleUsage 返回按账号/模型/日期聚合的实际用量。
// 查询范围是「站点时区下的最近 N 天」，N 由 ?days= 控制（1~90，默认 7）。
func (s *Service) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	days := 7
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}

	ctx := r.Context()
	// 先落盘，保证刚发生的调用本次就能查到。
	s.flushUsage(ctx)

	loc := s.siteLocation(ctx)
	since := usageDay(time.Now().In(loc).AddDate(0, 0, -(days-1)), loc)

	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()

	var totals usageTotals
	if err := db.QueryRowContext(ctx, `
		SELECT `+usageTotalsColumns+` FROM ds2api_usage_daily WHERE day >= ?`, since).
		Scan(&totals.Requests, &totals.PromptTokens, &totals.CompletionTokens, &totals.CachedTokens); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	totals.finish()

	collect := func(query string) ([]map[string]interface{}, error) {
		rows, err := db.QueryContext(ctx, query, since)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var key string
			var t usageTotals
			if err := rows.Scan(&key, &t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CachedTokens); err != nil {
				return nil, err
			}
			t.finish()
			out = append(out, map[string]interface{}{
				"key":              key,
				"requests":         t.Requests,
				"promptTokens":     t.PromptTokens,
				"completionTokens": t.CompletionTokens,
				"cachedTokens":     t.CachedTokens,
				"cacheHitRate":     t.CacheHitRate,
			})
		}
		return out, rows.Err()
	}

	byAccount, err := collect(`SELECT account_id, ` + usageTotalsColumns + `
		FROM ds2api_usage_daily WHERE day >= ?
		GROUP BY account_id ORDER BY SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	byModel, err := collect(`SELECT model, ` + usageTotalsColumns + `
		FROM ds2api_usage_daily WHERE day >= ?
		GROUP BY model ORDER BY SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	daily, err := collect(`SELECT day, ` + usageTotalsColumns + `
		FROM ds2api_usage_daily WHERE day >= ?
		GROUP BY day ORDER BY day ASC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// 账号显示名：从引擎配置取账号，优先备注名，其次邮箱/手机号，最后原始标识。
	names := map[string]string{}
	if store, err := s.loadEngineStore(); err == nil {
		for _, a := range store.Snapshot().Accounts {
			names[a.Identifier()] = firstNonEmpty(a.Name, a.Email, a.Mobile, a.Identifier())
		}
	}
	for _, item := range byAccount {
		id, _ := item["key"].(string)
		item["accountId"] = id
		item["accountName"] = firstNonEmpty(names[id], id)
	}
	for _, item := range byModel {
		item["model"] = item["key"]
	}
	for _, item := range daily {
		item["day"] = item["key"]
	}

	responseJSON(w, map[string]interface{}{
		"success":   true,
		"days":      days,
		"since":     since,
		"totals":    totals,
		"byAccount": byAccount,
		"byModel":   byModel,
		"daily":     daily,
	})
}

// firstNonEmpty 返回第一个非空字符串（本包内未定义同名辅助，故本地提供）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sortedUsageKeys 仅用于测试与调试：按确定性顺序返回当前待落盘的键。
func (s *Service) sortedUsageKeys() []usageKey {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	out := make([]usageKey, 0, len(s.usagePending))
	for k := range s.usagePending {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].day != out[j].day {
			return out[i].day < out[j].day
		}
		if out[i].accountID != out[j].accountID {
			return out[i].accountID < out[j].accountID
		}
		return out[i].model < out[j].model
	})
	return out
}
