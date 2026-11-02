package geminicli

// 本文件实现「实际用量」统计：把上游 usageMetadata 里的 prompt/completion/
// cached tokens 按「站点时区日期 × 账号 × 模型」聚合落库，供前端看"谁在用"。
//
// 设计取舍：
//   - 内存累加 + 定期落盘：与 geminicli_call_stats 同一节拍（每分钟一次），
//     避免每个请求一次写库。
//   - 日期归属一律走站点时区（CONTEXT.md 硬性规则）：日期桶用 timeutil 取站点时区，
//     不直接用 time.Local / time.UTC。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

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

// jsonInt 容错地把 JSON 数值（float64 / 字符串 / json.Number）转成 int64。
func jsonInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// recordUsage 把一次用量计入内存增量。accountID / model 为空时用占位符，
// 保证"有消耗但归不到账号"的请求也不会被静默丢掉。
func (s *Service) recordUsage(accountID, model string, prompt, completion, cached int64) {
	if prompt == 0 && completion == 0 {
		return
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		accountID = "(未知账号)"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "(未知模型)"
	}
	delta := &usageDelta{
		requests:         1,
		promptTokens:     prompt,
		completionTokens: completion,
		cachedTokens:     cached,
	}
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

// recordUsageFromResponse 从内层 Gemini 响应体里取 usageMetadata 记账。
func (s *Service) recordUsageFromResponse(accountID, model string, inner []byte) {
	var parsed struct {
		UsageMetadata struct {
			PromptTokenCount        int64 `json:"promptTokenCount"`
			ToolUsePromptTokenCount int64 `json:"toolUsePromptTokenCount"`
			CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
			ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(inner, &parsed); err != nil {
		return
	}
	u := parsed.UsageMetadata
	prompt := u.PromptTokenCount + u.ToolUsePromptTokenCount
	completion := u.CandidatesTokenCount + u.ThoughtsTokenCount
	s.recordUsage(accountID, model, prompt, completion, u.CachedContentTokenCount)
}

// flushUsage 把内存增量合并进 geminicli_usage_daily（逐键 UPSERT 累加）。
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
		s.usageMu.Lock()
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
		s.usageMu.Unlock()
		return
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	var failed map[usageKey]*usageDelta
	for k, v := range pend {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO geminicli_usage_daily
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
			// 单行失败（如 SQLITE_BUSY）不能静默丢弃：这些增量已从 usagePending 摘除，
			// 不回队就永久丢失。收集起来下一轮重试。
			if failed == nil {
				failed = map[usageKey]*usageDelta{}
			}
			failed[k] = v
		}
	}
	if len(failed) > 0 {
		s.usageMu.Lock()
		if s.usagePending == nil {
			s.usagePending = map[usageKey]*usageDelta{}
		}
		for k, v := range failed {
			if cur := s.usagePending[k]; cur != nil {
				cur.add(v)
			} else {
				s.usagePending[k] = v
			}
		}
		s.usageMu.Unlock()
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
		SELECT `+usageTotalsColumns+` FROM geminicli_usage_daily WHERE day >= ?`, since).
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
			item := map[string]interface{}{
				"key":              key,
				"requests":         t.Requests,
				"promptTokens":     t.PromptTokens,
				"completionTokens": t.CompletionTokens,
				"cachedTokens":     t.CachedTokens,
				"cacheHitRate":     t.CacheHitRate,
			}
			out = append(out, item)
		}
		return out, rows.Err()
	}

	byAccount, err := collect(`SELECT account_id, ` + usageTotalsColumns + `
		FROM geminicli_usage_daily WHERE day >= ?
		GROUP BY account_id ORDER BY SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	byModel, err := collect(`SELECT model, ` + usageTotalsColumns + `
		FROM geminicli_usage_daily WHERE day >= ?
		GROUP BY model ORDER BY SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	daily, err := collect(`SELECT day, ` + usageTotalsColumns + `
		FROM geminicli_usage_daily WHERE day >= ?
		GROUP BY day ORDER BY day ASC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// 账号显示名：优先备注名，其次 email，最后原始 id。
	names := map[string]string{}
	for _, a := range s.Settings().Accounts {
		names[a.ID] = firstNonEmpty(a.Nickname, a.Email, a.ID)
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
