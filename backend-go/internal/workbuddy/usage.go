package workbuddy

// 本文件实现「实际用量与扣费」统计：把上游 usage 里的
// prompt/completion/cached tokens 与 `credit`（本次真实扣费，实测字段）按
// 「站点时区日期 × 账号 × 模型」聚合落库，供前端看"谁在烧额度"。
//
// 设计取舍：
//   - **不写进网关的 openai_gateway_analytics**：那属于 openai 模块（列结构另有归属），
//     且它的统计维度是端点/key 而非"插件账号"。用量属于插件自己的域，落自己的表最干净。
//   - **内存累加 + 定期落盘**：与既有 workbuddy_call_stats 同一套节拍（每分钟一次），
//     避免每个请求一次写库。
//   - **日期归属一律走站点时区**（CONTEXT.md 硬性规则）：日期桶用 timeutil 取站点时区，
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
	credit           float64
}

func (d *usageDelta) add(o *usageDelta) {
	d.requests += o.requests
	d.promptTokens += o.promptTokens
	d.completionTokens += o.completionTokens
	d.cachedTokens += o.cachedTokens
	d.credit += o.credit
}

// usageCreditKeys 是「本次扣费」的候选字段名。实测 CodeBuddy 用 `credit`；
// 另外两个是防御性别名，命中即用。
var usageCreditKeys = []string{"credit", "credits", "total_credit"}

// siteLocCacheTTL 是站点时区缓存时长：避免每个请求都开库读设置，
// 又能让用户在设置里改时区后几分钟内生效。
const siteLocCacheTTL = 5 * time.Minute

// siteLocation 返回站点时区。读取失败时回退到 timeutil 的默认约定（服务器本地时区），
// 不在此直接引用 time.Local，保持"日期归属只经 timeutil"的约定。
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

func jsonFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// usageCachedTokens 取缓存命中 token：优先已归一的 details，其次各家别名。
func usageCachedTokens(usage map[string]any) int64 {
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		if v, has := details["cached_tokens"]; has {
			return jsonInt(v)
		}
	}
	for _, k := range cacheHitKeys {
		if v, has := usage[k]; has {
			return jsonInt(v)
		}
	}
	return 0
}

// usageCredit 取本次请求的真实扣费（实测字段 `usage.credit`）。
// 第二个返回值表示上游是否**上报了**该字段 —— 免费模型报 0 与"上游没给字段"要能区分。
func usageCredit(usage map[string]any) (float64, bool) {
	for _, k := range usageCreditKeys {
		if v, has := usage[k]; has && v != nil {
			return jsonFloat(v), true
		}
	}
	return 0, false
}

// recordUsage 把一次 usage 计入内存增量。accountID / model 为空时用占位符，
// 保证"有消耗但归不到账号"的请求也不会被静默丢掉。
func (s *Service) recordUsage(ctx context.Context, accountID, model string, usage map[string]any) {
	if len(usage) == 0 {
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
	credit, creditPresent := usageCredit(usage)
	delta := &usageDelta{
		requests:         1,
		promptTokens:     jsonInt(usage["prompt_tokens"]),
		completionTokens: jsonInt(usage["completion_tokens"]),
		cachedTokens:     usageCachedTokens(usage),
		credit:           credit,
	}
	// 完全没有可用数字时不计入，避免脏数据把"调用数"虚增。
	if delta.promptTokens == 0 && delta.completionTokens == 0 && !creditPresent {
		return
	}
	day := usageDay(time.Now(), s.siteLocation(ctx))
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
	if creditPresent {
		s.creditSeen = true
	}
	s.usageMu.Unlock()

	// 选号权重即时累加：否则"刚消耗掉的额度"要等到下一次快照才影响选号。
	s.bumpCreditDay(day, accountID, credit)
}

// bumpCreditDay 把一次消耗即时计入「今天」的选号权重；跨天自动清零。
func (s *Service) bumpCreditDay(day, accountID string, credit float64) {
	s.creditDayMu.Lock()
	defer s.creditDayMu.Unlock()
	if s.creditDay != day {
		s.creditDay = day
		s.creditDayUsed = map[string]float64{}
	}
	if credit > 0 {
		s.creditDayUsed[accountID] += credit
	}
}

// refreshCreditDaySnapshot 从库里重算「今天各账号已消耗的 credit」，作为选号权重。
//
// 必须在 flushUsage 之后调用：先落盘再读，避免把内存里的在途增量覆盖掉。
// 即便如此仍做逐账号「取大」，防住"刷新与 recordUsage 并发"的窗口。
func (s *Service) refreshCreditDaySnapshot(ctx context.Context) {
	day := usageDay(time.Now(), s.siteLocation(ctx))
	s.creditDayMu.Lock()
	if s.creditDay != day {
		s.creditDay = day
		s.creditDayUsed = map[string]float64{}
	}
	s.creditDayMu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT account_id, COALESCE(SUM(credit),0) FROM workbuddy_usage_daily
		WHERE day = ? GROUP BY account_id`, day)
	if err != nil {
		return
	}
	defer rows.Close()
	next := map[string]float64{}
	for rows.Next() {
		var id string
		var c float64
		if rows.Scan(&id, &c) == nil {
			next[id] = c
		}
	}
	s.creditDayMu.Lock()
	for id, c := range s.creditDayUsed {
		if c > next[id] {
			next[id] = c
		}
	}
	s.creditDayUsed = next
	s.creditDayAt = time.Now()
	s.creditDayMu.Unlock()
}

// pickLeastConsumed 在可用账号里挑「站点时区今天已消耗 credit 最少」的一个。
//
// 权重取自内存快照（后台每分钟刷新一次，加上 recordUsage 即时累加），因此**不查库**，
// 可以承受每个请求调用一次。消耗相同时取列表顺序靠前者 —— 保持确定性，便于排障与测试。
// 快照尚未就绪时所有账号都是 0，行为退化为「首个可用」，不会因为统计缺失而不可用。
func (s *Service) pickLeastConsumed(accounts []Account) (Account, bool) {
	s.creditDayMu.RLock()
	defer s.creditDayMu.RUnlock()
	bestIdx := -1
	bestUsed := 0.0
	for i, a := range accounts {
		if !accountAvailable(a) {
			continue
		}
		u := s.creditDayUsed[a.ID]
		if bestIdx < 0 || u < bestUsed {
			bestIdx, bestUsed = i, u
		}
	}
	if bestIdx < 0 {
		return Account{}, false
	}
	return accounts[bestIdx], true
}

// recordUsageFromChunk 从**已清洗归一**的 chunk 里取 usage 记账。
// 用归一后的版本，才能拿到标准的 prompt_tokens_details.cached_tokens。
func (s *Service) recordUsageFromChunk(ctx context.Context, accountID, model, chunk string) {
	if !strings.Contains(chunk, `"usage"`) {
		return
	}
	var obj map[string]any
	if json.Unmarshal([]byte(chunk), &obj) != nil {
		return
	}
	usage, ok := obj["usage"].(map[string]any)
	if !ok {
		return
	}
	s.recordUsage(ctx, accountID, model, usage)
}

// flushUsage 把内存增量合并进 workbuddy_usage_daily（逐键 UPSERT 累加）。
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
	for k, v := range pend {
		_, _ = db.ExecContext(ctx, `
			INSERT INTO workbuddy_usage_daily
				(day, account_id, model, requests, prompt_tokens, completion_tokens, cached_tokens, credit, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(day, account_id, model) DO UPDATE SET
				requests = requests + excluded.requests,
				prompt_tokens = prompt_tokens + excluded.prompt_tokens,
				completion_tokens = completion_tokens + excluded.completion_tokens,
				cached_tokens = cached_tokens + excluded.cached_tokens,
				credit = credit + excluded.credit,
				updated_at = excluded.updated_at`,
			k.day, k.accountID, k.model,
			v.requests, v.promptTokens, v.completionTokens, v.cachedTokens, v.credit, now)
	}
}

// usageTotals 是一组用量的合计值。
type usageTotals struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	Credit           float64 `json:"credit"`
	// CacheHitRate 是缓存命中占输入的比例（0~1）；无输入时为 0。
	CacheHitRate float64 `json:"cacheHitRate"`
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

// scanUsageTotals 从一行聚合结果扫描出合计值。
func scanUsageTotals(scan func(...any) error) (usageTotals, error) {
	var t usageTotals
	err := scan(&t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CachedTokens, &t.Credit)
	t.finish()
	return t, err
}

const usageTotalsColumns = `COALESCE(SUM(requests),0), COALESCE(SUM(prompt_tokens),0),
	COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cached_tokens),0), COALESCE(SUM(credit),0)`

// handleUsage 返回按账号/模型/日期聚合的实际用量与扣费。
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

	totals, err := scanUsageTotals(db.QueryRowContext(ctx, `
		SELECT `+usageTotalsColumns+` FROM workbuddy_usage_daily WHERE day >= ?`, since).Scan)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	type groupRow struct {
		Key    string      `json:"-"`
		Label  string      `json:"-"`
		Totals usageTotals `json:"totals"`
	}
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
			if err := rows.Scan(&key, &t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CachedTokens, &t.Credit); err != nil {
				return nil, err
			}
			t.finish()
			item := map[string]interface{}{"key": key}
			item["requests"] = t.Requests
			item["promptTokens"] = t.PromptTokens
			item["completionTokens"] = t.CompletionTokens
			item["cachedTokens"] = t.CachedTokens
			item["credit"] = t.Credit
			item["cacheHitRate"] = t.CacheHitRate
			out = append(out, item)
		}
		return out, rows.Err()
	}

	byAccount, err := collect(`SELECT account_id, ` + usageTotalsColumns + `
		FROM workbuddy_usage_daily WHERE day >= ?
		GROUP BY account_id ORDER BY SUM(credit) DESC, SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	byModel, err := collect(`SELECT model, ` + usageTotalsColumns + `
		FROM workbuddy_usage_daily WHERE day >= ?
		GROUP BY model ORDER BY SUM(credit) DESC, SUM(requests) DESC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	daily, err := collect(`SELECT day, ` + usageTotalsColumns + `
		FROM workbuddy_usage_daily WHERE day >= ?
		GROUP BY day ORDER BY day ASC`)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// 账号显示名：优先备注名，其次 uid，最后原始 id。
	names := map[string]string{}
	for _, a := range s.Settings().Accounts {
		names[a.ID] = firstNonEmpty(a.Nickname, a.UID, a.ID)
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
		// 便于前端提示：有消耗时是否真的看到了上游的 credit 字段。
		"creditReported": s.usageCreditSeen(),
	})
}

// usageCreditSeen 表示上游是否上报过 credit 字段（用于前端区分
// "确实没花钱" 与 "上游没给扣费字段"）。
func (s *Service) usageCreditSeen() bool {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	return s.creditSeen
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
