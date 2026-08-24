package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/sseutil"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// settingsLocationFromDB 读取用户设置中的系统时区（经统一 timeutil 门面）。
// 'system'/空/无效时回退服务器本地时区（与通知中心一致）。
func settingsLocationFromDB(ctx context.Context, db *sql.DB) *time.Location {
	return timeutil.LocationFromSettings(ctx, db)
}

// analyticsTimeWindow 按展示时区计算「近 N 天」过滤起点（转回 UTC 字符串用于与存储的 UTC 时间比较）。
func analyticsTimeWindow(ctx context.Context, db *sql.DB, days int) (string, *time.Location) {
	loc := settingsLocationFromDB(ctx, db)
	start := time.Now().In(loc).AddDate(0, 0, -days).UTC().Format("2006-01-02 15:04:05")
	return start, loc
}

// sqliteStrftimeOffset 生成 SQLite strftime 偏移 modifier（如 "+08:00"）与时区秒偏移，
// 用于按展示时区分桶/贴标签。DST 时区以当前时刻偏移为准，切换期桶边界可能偏移一小时。
func sqliteStrftimeOffset(loc *time.Location) (modifier string, offsetSec int) {
	_, offsetSec = time.Now().In(loc).Zone()
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	modifier = fmt.Sprintf("%s%02d:%02d", sign, offsetSec/3600, (offsetSec%3600)/60)
	return modifier, offsetSec
}

// AnalyticsError 描述一次失败请求的结构化报错信息，与调用日志一同落库，
// 供日志界面与 AI 排障直接读取：kind 指明错误环节，message 说明原因，
// response 保存报错 JSON（上游错误响应体或网关构造的错误体）。
type AnalyticsError struct {
	Kind     string
	Message  string
	Response string
}

// analyticsQueueSize 是异步落库队列的容量上限。满员时直接丢弃新增记录
// （日志页短暂缺行，网关吞吐不受影响），丢弃计数见 analyticsDrop。
const analyticsQueueSize = 512

// analyticsBatchLimit 是 worker 每轮批量落库的最大记录数。
const analyticsBatchLimit = 64

// analyticsWriteItem 承载一次网关调用日志的落库数据与可选的 flush 哨兵。
type analyticsWriteItem struct {
	route, endpointID, model           string
	realModel                          string
	gatewayKeyID, gatewayKeyName       string
	statusCode                         int
	latencyMs, ttfbMs                  int64
	promptTokens, completionTokens     int
	totalTokens, cachedTokens          int
	stream, viaProxy                   int
	clientIP, upstreamIP, failoverPath string
	keyIndex                           int
	errInfo                            *AnalyticsError
	flush                              chan struct{}
}

// enqueueAnalytics 将调用日志投递到异步落库队列，队列满时丢弃并计数。
func (s *Service) enqueueAnalytics(item analyticsWriteItem) {
	s.ensureAnalyticsWorker()
	select {
	case s.analyticsQueue <- item:
	default:
		dropped := s.analyticsDrop.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			applog.Warn(context.Background(), "openai", "gateway analytics queue full, dropping records", "dropped", dropped)
		}
	}
}

// ensureAnalyticsWorker 幂等启动常驻落库 worker（首条记录 / flush / Shutdown 时）。
func (s *Service) ensureAnalyticsWorker() {
	s.analyticsStartMu.Lock()
	defer s.analyticsStartMu.Unlock()
	if !s.analyticsStarted {
		s.analyticsStarted = true
		go s.analyticsWorker()
	}
}

// RecordAnalytics saves a gateway proxy metric to the SQLite database
func (s *Service) RecordAnalytics(ctx context.Context, route, endpointID, model string, statusCode int, latencyMs int64, ttfbMs int64, promptTokens, completionTokens, totalTokens, cachedTokens int, stream, viaProxy int, clientIP, upstreamIP string) {
	s.recordAnalyticsKey(ctx, route, endpointID, model, statusCode, latencyMs, ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, stream, viaProxy, clientIP, upstreamIP, -1, "", nil)
}

// recordAnalyticsKey 与 RecordAnalytics 相同，但附带本次实际使用的 API Key 序号
// （keyIndex，0=主 key；-1 表示未知/未使用多 key），用于日志端点后的 key pill。
// failoverPath 为 JSON 数组，记录本轮请求尝试过的端点与状态码，便于前端展示迁移趋势。
// errInfo 仅在请求失败（statusCode >= 400）时由调用方传入：Kind 为错误环节
// （no_endpoint/bad_request/gateway/blocked/upstream/bad_gateway 等），Message 为
// 人类可读原因，Response 为报错 JSON（应经 errorResponseForLog 截断）。成功传 nil。
// realModel 为可选尾参：命中模型映射时传入上游真实模型名（model 存对外别名），
// 供日志页「模型」列弹出对照；未映射时留空。
//
// 本方法只做内存入队（毫秒级、无 DB 锁），真正的落库与 SSE 广播由常驻 worker
// 批量执行（见 analyticsWorker），请求路径不再承担同步 INSERT。
func (s *Service) recordAnalyticsKey(ctx context.Context, route, endpointID, model string, statusCode int, latencyMs int64, ttfbMs int64, promptTokens, completionTokens, totalTokens, cachedTokens int, stream, viaProxy int, clientIP, upstreamIP string, keyIndex int, failoverPath string, errInfo *AnalyticsError, realModel ...string) {
	gatewayKey := gatewayKeyFromContext(ctx)
	realModelName := ""
	if len(realModel) > 0 {
		realModelName = strings.TrimSpace(realModel[0])
	}
	s.ensureAnalyticsWorker()
	s.enqueueAnalytics(analyticsWriteItem{
		route:            route,
		endpointID:       endpointID,
		model:            model,
		realModel:        realModelName,
		gatewayKeyID:     gatewayKey.ID,
		gatewayKeyName:   gatewayKey.Name,
		statusCode:       statusCode,
		latencyMs:        latencyMs,
		ttfbMs:           ttfbMs,
		promptTokens:     promptTokens,
		completionTokens: completionTokens,
		totalTokens:      totalTokens,
		cachedTokens:     cachedTokens,
		stream:           stream,
		viaProxy:         viaProxy,
		clientIP:         clientIP,
		upstreamIP:       upstreamIP,
		failoverPath:     failoverPath,
		keyIndex:         keyIndex,
		errInfo:          errInfo,
	})
}

// analyticsWorker 常驻消费落库队列：批量取出记录，单事务逐条 INSERT
// （合并 fsync 与连接获取，锁持有仍为毫秒级），随后统一执行错误详情
// 保留清理并逐个 SSE 广播。flush 哨兵用于测试/优雅退出时的同步屏障。
// 队列被 Shutdown 关闭后，处理完当前批次即退出并通知（不残留 goroutine，
// 避免测试 TempDir 清理时后台线程仍占用 SQLite 文件）。
func (s *Service) analyticsWorker() {
	defer close(s.analyticsDone)
	for {
		item, ok := <-s.analyticsQueue
		if !ok {
			return
		}
		batch := make([]analyticsWriteItem, 1, analyticsBatchLimit)
		batch[0] = item
		for len(batch) < analyticsBatchLimit {
			select {
			case next, open := <-s.analyticsQueue:
				if !open {
					goto done
				}
				batch = append(batch, next)
			default:
				goto done
			}
		}
	done:
		s.persistAnalyticsBatch(batch)
	}
}

// Shutdown 优雅停止异步落库 worker：关闭队列并等待在途批次落库后再返回。
// 调用后不应再投递 analytics 记录（服务即将退出）。幂等（重复调用直接放行）。
// 供测试（避免 TempDir 清理竞态）与进程优雅停机使用。
func (s *Service) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.analyticsStartMu.Lock()
		started := s.analyticsStarted
		if !started {
			s.analyticsStarted = true
			go s.analyticsWorker()
		}
		s.analyticsStartMu.Unlock()
		close(s.analyticsQueue)
		if started {
			<-s.analyticsDone
		}
	})
}

// persistAnalyticsBatch 将一批调用日志落库并按序广播。
func (s *Service) persistAnalyticsBatch(batch []analyticsWriteItem) {
	// 写 ctx 需 ≥ 连接级 busy_timeout（30s），否则维护窗口内 busy 等待还没获得
	// 锁，ctx 先到期导致整批分析记录被放弃。
	writeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := s.open(writeCtx)
	if err != nil {
		applog.Error(writeCtx, "openai", "Failed to open db for recording analytics", "error", err.Error())
		s.completeAnalyticsBatch(batch)
		return
	}
	defer db.Close()

	// 批量加载端点定价用于按量计费：必须在事务外执行（SQLite 连接池单连接，
	// 事务内再查询会与写锁互相等待）。
	pricingByEndpoint := s.loadAnalyticsPricing(writeCtx, db, batch)

	tx, err := db.BeginTx(writeCtx, nil)
	if err != nil {
		applog.Error(writeCtx, "openai", "Failed to begin analytics batch", "error", err.Error())
		s.completeAnalyticsBatch(batch)
		return
	}
	records := make([]map[string]interface{}, 0, len(batch))
	hasError := false

	for _, item := range batch {
		if item.flush != nil {
			continue
		}
		cost, costCurrency := computeRecordCost(item, pricingByEndpoint[item.endpointID])
		result, execErr := tx.ExecContext(writeCtx, `
			INSERT INTO openai_gateway_analytics (endpoint_id, gateway_key_id, route, model, real_model, status_code, latency_ms, ttfb_ms, prompt_tokens, completion_tokens, total_tokens, cached_tokens, stream, via_proxy, client_ip, upstream_ip, key_index, failover_path, error_kind, error_message, response_body, cost, cost_currency)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.endpointID, item.gatewayKeyID, item.route, item.model, item.realModel, item.statusCode, item.latencyMs, item.ttfbMs, item.promptTokens, item.completionTokens, item.totalTokens, item.cachedTokens, item.stream, item.viaProxy, item.clientIP, item.upstreamIP, item.keyIndex, item.failoverPath, errorKindOf(item), errorMessageOf(item), errorResponseOf(item), cost, costCurrency)
		if execErr != nil {
			_ = tx.Rollback()
			applog.Error(writeCtx, "openai", "Failed to insert gateway analytics", "error", execErr.Error())
			s.completeAnalyticsBatch(batch)
			return
		}
		if item.errInfo != nil {
			hasError = true
		}
		recordID := int64(0)
		if id, idErr := result.LastInsertId(); idErr == nil {
			recordID = id
		}
		records = append(records, map[string]interface{}{
			"id":               recordID,
			"route":            item.route,
			"endpointId":       item.endpointID,
			"endpointName":     "",
			"gatewayKeyName":   item.gatewayKeyName,
			"model":            item.model,
			"realModel":        item.realModel,
			"statusCode":       item.statusCode,
			"latencyMs":        item.latencyMs,
			"ttfbMs":           item.ttfbMs,
			"promptTokens":     item.promptTokens,
			"completionTokens": item.completionTokens,
			"totalTokens":      item.totalTokens,
			"cachedTokens":     item.cachedTokens,
			"stream":           item.stream == 1,
			"viaProxy":         item.viaProxy == 1,
			"clientIp":         item.clientIP,
			"upstreamIp":       item.upstreamIP,
			"keyIndex":         item.keyIndex,
			"failoverPath":     item.failoverPath,
			"errorKind":        errorKindOf(item),
			"errorMessage":     errorMessageOf(item),
			"errorResponse":    errorResponseOf(item),
			"cost":             cost,
			"costCurrency":     costCurrency,
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
		})
	}
	if err := tx.Commit(); err != nil {
		applog.Error(writeCtx, "openai", "Failed to commit analytics batch", "error", err.Error())
		s.completeAnalyticsBatch(batch)
		return
	}

	s.enrichAnalyticsRecords(writeCtx, db, records)
	for _, record := range records {
		s.publishAnalytics(record)
	}
	if hasError {
		s.trimErrorDetailRetention(writeCtx, db)
	}
	s.completeAnalyticsBatch(batch)
}

// makePlaceholders 生成 n 个 SQL 占位符（用于 IN (...) 查询）。
func makePlaceholders(n int) []string {
	marks := make([]string, n)
	for i := range marks {
		marks[i] = "?"
	}
	return marks
}

// loadAnalyticsPricing 批量加载一批调用记录涉及的端点定价表（endpoint_id -> PricingMap）。
// 每个落库批次至多执行一次查询；端点无定价或查询失败时返回空表，费用记为 0。
func (s *Service) loadAnalyticsPricing(ctx context.Context, db *sql.DB, batch []analyticsWriteItem) map[string]PricingMap {
	idSet := map[string]bool{}
	for _, item := range batch {
		if item.endpointID != "" {
			idSet[item.endpointID] = true
		}
	}
	result := map[string]PricingMap{}
	if len(idSet) == 0 {
		return result
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	marks := strings.Join(makePlaceholders(len(ids)), ",")
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, "SELECT id, pricing FROM openai_endpoints WHERE id IN ("+marks+")", args...)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw sql.NullString
		if err := rows.Scan(&id, &raw); err != nil {
			break
		}
		if raw.Valid && raw.String != "" {
			var pm PricingMap
			if json.Unmarshal([]byte(raw.String), &pm) == nil && len(pm) > 0 {
				result[id] = pm
			}
		}
	}
	return result
}

// computeRecordCost 依据端点定价计算单次调用的费用。模型名优先用真实模型
// （命中模型映射时对外别名与上游模型不同），其次用对外 model 字段。
// 无定价、无 token 或费用为 0 时返回 (0, "")，表示该调用未计费。
func computeRecordCost(item analyticsWriteItem, pricing PricingMap) (float64, string) {
	if len(pricing) == 0 || item.totalTokens <= 0 {
		return 0, ""
	}
	model := item.realModel
	if model == "" {
		model = item.model
	}
	p, ok := pricing[model]
	if !ok {
		return 0, ""
	}
	cost := p.Cost(item.promptTokens, item.completionTokens, item.cachedTokens)
	if cost <= 0 {
		return 0, ""
	}
	return cost, p.CurrencyName()
}

// stringValue 取 map 值中的字符串，非字符串时返回 fallback。
func stringValue(v interface{}, fallback string) string {
	if text, ok := v.(string); ok {
		return text
	}
	return fallback
}

func errorKindOf(item analyticsWriteItem) string {
	if item.errInfo != nil {
		return item.errInfo.Kind
	}
	return ""
}

func errorMessageOf(item analyticsWriteItem) string {
	if item.errInfo != nil {
		return item.errInfo.Message
	}
	return ""
}

func errorResponseOf(item analyticsWriteItem) string {
	if item.errInfo != nil {
		return item.errInfo.Response
	}
	return ""
}

// enrichAnalyticsRecords 批量补齐端点名称（日志页展示用），减少逐条查询。
func (s *Service) enrichAnalyticsRecords(ctx context.Context, db *sql.DB, records []map[string]interface{}) {
	idSet := map[string]bool{}
	for _, record := range records {
		if id := stringValue(record["endpointId"], ""); id != "" {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	marks := strings.Join(makePlaceholders(len(ids)), ",")
	args := make([]interface{}, 0, len(ids)*2)
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, CASE WHEN u.id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END
		FROM (SELECT id FROM openai_endpoints WHERE id IN (`+marks+`)
			UNION SELECT endpoint_id AS id FROM openai_endpoint_name_archive WHERE endpoint_id IN (`+marks+`)) u
		LEFT JOIN openai_endpoints e ON e.id = u.id
		LEFT JOIN openai_endpoint_name_archive a ON a.endpoint_id = u.id
	`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			break
		}
		names[id] = name
	}
	for _, record := range records {
		if id := stringValue(record["endpointId"], ""); id != "" {
			if name, ok := names[id]; ok {
				record["endpointName"] = name
			}
		}
	}
}

// completeAnalyticsBatch 唤醒等待 flush 哨兵的调用方；其余记录无等待者。
func (s *Service) completeAnalyticsBatch(batch []analyticsWriteItem) {
	for _, item := range batch {
		if item.flush != nil {
			close(item.flush)
		}
	}
}

// flushAnalyticsQueue 阻塞等待队列中已投递的记录全部落库。
// 供测试与优雅关闭使用；超时后返回（不阻塞生产路径）。
func (s *Service) flushAnalyticsQueue(timeout time.Duration) {
	done := make(chan struct{})
	s.ensureAnalyticsWorker()
	s.enqueueAnalytics(analyticsWriteItem{flush: done})
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// subscribeAnalytics 注册一个网关日志实时订阅者，返回事件 channel 与取消函数。
func (s *Service) subscribeAnalytics() (<-chan map[string]interface{}, func()) {
	s.analyticsStreamMu.Lock()
	defer s.analyticsStreamMu.Unlock()
	s.analyticsStreamNext++
	id := s.analyticsStreamNext
	ch := make(chan map[string]interface{}, 64)
	s.analyticsStreams[id] = ch
	return ch, func() {
		s.analyticsStreamMu.Lock()
		defer s.analyticsStreamMu.Unlock()
		if existing, ok := s.analyticsStreams[id]; ok {
			delete(s.analyticsStreams, id)
			close(existing)
		}
	}
}

// publishAnalytics 向所有日志订阅者广播一条网关事件（带缓冲防阻塞）。
func (s *Service) publishAnalytics(event map[string]interface{}) {
	s.analyticsStreamMu.Lock()
	defer s.analyticsStreamMu.Unlock()
	for _, ch := range s.analyticsStreams {
		select {
		case ch <- event:
		default:
		}
	}
}

// analyticsEventStream 以 SSE 推送网关实时日志：后端出现请求立即推送给前端。
func (s *Service) analyticsEventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.subscribeAnalytics()
	defer cancel()
	hello, _ := json.Marshal(map[string]interface{}{"connected": true})
	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return
	}
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

// getAnalyticsSummary returns aggregation metrics (requests, avg latency, error rate, tokens)
func (s *Service) getAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	daysStr := r.URL.Query().Get("days")
	days := 7
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	timeFilter, _ := analyticsTimeWindow(ctx, db, days)

	var totalRequests int
	var avgLatency float64
	var avgTtfbMs float64
	var totalTokens int
	var totalCachedTokens int
	var totalPromptTokens int
	var totalCompletionTokens int
	var totalCost float64
	var errorCount int

	err = db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*), 
			COALESCE(AVG(latency_ms), 0.0), 
			COALESCE(AVG(CASE WHEN ttfb_ms > 0 THEN ttfb_ms END), 0.0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cached_tokens), 0),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(cost), 0)
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
	`, timeFilter).Scan(&totalRequests, &avgLatency, &avgTtfbMs, &totalTokens, &errorCount, &totalCachedTokens, &totalPromptTokens, &totalCompletionTokens, &totalCost)

	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	errorRate := 0.0
	if totalRequests > 0 {
		errorRate = float64(errorCount) / float64(totalRequests)
	}
	// 缓存命中占比：缓存命中的 token 占上游提示词 token 的比例。
	cachedRatio := 0.0
	if totalPromptTokens > 0 {
		cachedRatio = float64(totalCachedTokens) / float64(totalPromptTokens)
	}

	// 按渠道（endpoint）分组的请求/错误统计，供看板上游错误率详情展示。
	type endpointErrorStat struct {
		EndpointID   string  `json:"endpointId"`
		EndpointName string  `json:"endpointName"`
		Requests     int     `json:"requests"`
		Errors       int     `json:"errors"`
		ErrorRate    float64 `json:"errorRate"`
	}
	endpointStats := []endpointErrorStat{}
	erRows, err := db.QueryContext(ctx, `
		SELECT
			COALESCE(g.endpoint_id, ''),
			CASE WHEN g.endpoint_id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END,
			COUNT(*),
			SUM(CASE WHEN g.status_code >= 400 THEN 1 ELSE 0 END)
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
		LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id
		WHERE g.timestamp >= ? AND g.route != 'models'
		GROUP BY g.endpoint_id
		ORDER BY COUNT(*) DESC
	`, timeFilter)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer erRows.Close()
	for erRows.Next() {
		var st endpointErrorStat
		if err := erRows.Scan(&st.EndpointID, &st.EndpointName, &st.Requests, &st.Errors); err == nil {
			if st.Requests > 0 {
				st.ErrorRate = float64(st.Errors) / float64(st.Requests)
			}
			endpointStats = append(endpointStats, st)
		}
	}

	// 按货币分组的费用汇总：多币种端点并存时避免把不同货币金额直接相加。
	type costStat struct {
		Currency string  `json:"currency"`
		Amount   float64 `json:"amount"`
	}
	costs := []costStat{}
	cRows, err := db.QueryContext(ctx, `
		SELECT COALESCE(cost_currency, ''), COALESCE(SUM(cost), 0)
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models' AND cost > 0
		GROUP BY COALESCE(cost_currency, '')
		ORDER BY SUM(cost) DESC
	`, timeFilter)
	if err == nil {
		for cRows.Next() {
			var cs costStat
			if cErr := cRows.Scan(&cs.Currency, &cs.Amount); cErr == nil {
				costs = append(costs, cs)
			}
		}
		cRows.Close()
	}

	// 按端点分组的费用明细（仅 cost > 0），每个端点下再按 gateway key 分层展示，
	// 供词元用量详情 Popover 展示「端点 → 多 key」的费用构成。
	type costKeyStat struct {
		KeyName  string  `json:"keyName"`
		Cost     float64 `json:"cost"`
		Currency string  `json:"currency"`
	}
	type costEndpointStat struct {
		EndpointID   string        `json:"endpointId"`
		EndpointName string        `json:"endpointName"`
		Cost         float64       `json:"cost"`
		Currency     string        `json:"currency"`
		Keys         []costKeyStat `json:"keys"`
	}
	costByEndpoint := []costEndpointStat{}
	ceRows, err := db.QueryContext(ctx, `
		SELECT
			COALESCE(g.endpoint_id, ''),
			CASE WHEN g.endpoint_id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END,
			COALESCE(SUM(g.cost), 0),
			COALESCE(MAX(g.cost_currency), '')
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
		LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id
		WHERE g.timestamp >= ? AND g.route != 'models'
		GROUP BY g.endpoint_id
		HAVING SUM(g.cost) > 0
		ORDER BY SUM(g.cost) DESC
		LIMIT 10
	`, timeFilter)
	if err == nil {
		for ceRows.Next() {
			var es costEndpointStat
			if eErr := ceRows.Scan(&es.EndpointID, &es.EndpointName, &es.Cost, &es.Currency); eErr == nil {
				es.Keys = []costKeyStat{}
				costByEndpoint = append(costByEndpoint, es)
			}
		}
		ceRows.Close()
	}

	// 按 端点 × gateway key 分组的费用（仅 cost > 0），挂接到对应端点下。
	ckRows, err := db.QueryContext(ctx, `
		SELECT
			COALESCE(g.endpoint_id, ''),
			COALESCE(k.name, '未识别密钥'),
			COALESCE(SUM(g.cost), 0),
			COALESCE(MAX(g.cost_currency), '')
		FROM openai_gateway_analytics g
		LEFT JOIN openai_gateway_keys k ON g.gateway_key_id = k.id
		WHERE g.timestamp >= ? AND g.route != 'models'
		GROUP BY g.endpoint_id, g.gateway_key_id
		HAVING SUM(g.cost) > 0
		ORDER BY SUM(g.cost) DESC
	`, timeFilter)
	if err == nil {
		endpointIndex := map[string]int{}
		for i, es := range costByEndpoint {
			endpointIndex[es.EndpointID] = i
		}
		for ckRows.Next() {
			var ks costKeyStat
			var endpointID string
			if kErr := ckRows.Scan(&endpointID, &ks.KeyName, &ks.Cost, &ks.Currency); kErr == nil {
				if i, ok := endpointIndex[endpointID]; ok {
					costByEndpoint[i].Keys = append(costByEndpoint[i].Keys, ks)
				}
			}
		}
		ckRows.Close()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"totalRequests":         totalRequests,
		"avgLatency":            avgLatency,
		"avgTtfbMs":             avgTtfbMs,
		"totalTokens":           totalTokens,
		"totalCachedTokens":     totalCachedTokens,
		"cachedRatio":           cachedRatio,
		"totalPromptTokens":     totalPromptTokens,
		"totalCompletionTokens": totalCompletionTokens,
		"errorRate":             errorRate,
		"errorCount":            errorCount,
		"endpointErrorRates":    endpointStats,
		"totalCost":             totalCost,
		"costs":                 costs,
		"costByEndpoint":        costByEndpoint,
	})
}

// getAnalyticsCharts returns daily timeseries data for the specified days range
func (s *Service) getAnalyticsCharts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	daysStr := r.URL.Query().Get("days")
	days := 7
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	timeFilter, loc := analyticsTimeWindow(ctx, db, days)

	// 时间粒度：hour / day / week（桶边界与标签按系统时区，非 UTC）
	granularity := r.URL.Query().Get("granularity")
	offsetModifier, offsetSec := sqliteStrftimeOffset(loc)
	var timeGroup string
	var tsExpr string
	switch granularity {
	case "hour":
		timeGroup = "strftime('%m-%d %H:00', timestamp, '" + offsetModifier + "')"
		tsExpr = fmt.Sprintf("(CAST(strftime('%%s', timestamp) AS INTEGER) + %d) / 3600 * 3600 - %d", offsetSec, offsetSec)
	case "week":
		timeGroup = "strftime('%Y-W%W', timestamp, '" + offsetModifier + "')"
		tsExpr = fmt.Sprintf("(CAST(strftime('%%s', timestamp) AS INTEGER) + %d) / 604800 * 604800 - %d", offsetSec, offsetSec)
	default:
		granularity = "day"
		timeGroup = "strftime('%m-%d', timestamp, '" + offsetModifier + "')"
		tsExpr = fmt.Sprintf("(CAST(strftime('%%s', timestamp) AS INTEGER) + %d) / 86400 * 86400 - %d", offsetSec, offsetSec)
	}

	// 1. Trend buckets（小时 / 天 / 周聚合，多指标）
	rows, err := db.QueryContext(ctx, `
		SELECT 
			`+timeGroup+` as bucket,
			`+tsExpr+` as ts_sec,
			COUNT(*) as count, 
			COALESCE(AVG(latency_ms), 0.0) as avg_latency, 
			COALESCE(AVG(CASE WHEN ttfb_ms > 0 THEN ttfb_ms END), 0.0) as avg_ttfb,
			COALESCE(SUM(total_tokens), 0) as tokens,
			COALESCE(SUM(cached_tokens), 0) as cached,
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) as errors
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY ts_sec
		ORDER BY ts_sec ASC
	`, timeFilter)

	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type ChartPoint struct {
		Day         string  `json:"day"`
		TsSec       int64   `json:"tsSec"`
		Count       int     `json:"count"`
		AvgLatency  float64 `json:"avgLatency"`
		AvgTtfbMs   float64 `json:"avgTtfbMs"`
		Tokens      int     `json:"tokens"`
		Cached      int     `json:"cachedTokens"`
		Errors      int     `json:"errors"`
		Granularity string  `json:"granularity"`
	}

	dailyPoints := []ChartPoint{}
	for rows.Next() {
		var p ChartPoint
		var bucket string
		if err := rows.Scan(&bucket, &p.TsSec, &p.Count, &p.AvgLatency, &p.AvgTtfbMs, &p.Tokens, &p.Cached, &p.Errors); err == nil {
			p.Day = bucket
			p.Granularity = granularity
			dailyPoints = append(dailyPoints, p)
		}
	}

	// 2. Model/endpoint distribution by both request count and token usage.
	type ModelShare struct {
		Model  string `json:"model"`
		Count  int    `json:"count"`
		Tokens int    `json:"tokens"`
	}
	buildShares := func(querySQL string) []ModelShare {
		rows, err := db.QueryContext(ctx, querySQL, timeFilter)
		if err != nil {
			return nil
		}
		defer rows.Close()
		shares := []ModelShare{}
		for rows.Next() {
			var m ModelShare
			if err := rows.Scan(&m.Model, &m.Count, &m.Tokens); err == nil {
				// 过滤空白维度名（错误/异常请求可能未解析出 model）。
				if strings.TrimSpace(m.Model) == "" {
					continue
				}
				shares = append(shares, m)
			}
		}
		return shares
	}
	modelShares := buildShares(`
		SELECT model, COUNT(*) as count, COALESCE(SUM(total_tokens), 0) as tokens
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY model
		ORDER BY count DESC, tokens DESC
	`)
	endpointShares := buildShares(`
		SELECT CASE WHEN g.endpoint_id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END, COUNT(*), COALESCE(SUM(g.total_tokens), 0)
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
		LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id
		WHERE g.timestamp >= ? AND g.route != 'models'
		GROUP BY g.endpoint_id
		ORDER BY COUNT(*) DESC, SUM(g.total_tokens) DESC
	`)

	// 3. 按“维度 × 时段”展开调用量，供全宽趋势图多系列使用。
	// 同时聚合词元（全部/未缓存），供「调用量 / 词元」切换展示。
	// querySQL 必须按 (name, ts_sec, count, tokens, cached) 顺序返回；维度名即系列名。
	type ModelSeriesGroup struct {
		Model          string  `json:"model"`
		Data           []int   `json:"data"`
		Tokens         []int64 `json:"tokens"`
		TokensUncached []int64 `json:"tokensUncached"`
	}
	tsToLabel := make(map[int64]string, len(dailyPoints))
	for _, point := range dailyPoints {
		tsToLabel[point.TsSec] = point.Day
	}
	bucketIndex := make(map[string]int, len(dailyPoints))
	bucketLabels := make([]string, 0, len(dailyPoints))
	for _, point := range dailyPoints {
		if _, ok := bucketIndex[point.Day]; !ok {
			bucketIndex[point.Day] = len(bucketLabels)
			bucketLabels = append(bucketLabels, point.Day)
		}
	}
	buildDimensionTrends := func(querySQL string) ([]ModelSeriesGroup, error) {
		rows, err := db.QueryContext(ctx, querySQL, timeFilter)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		counts := make(map[int64]map[string]int)   // ts -> dimension -> count
		tokens := make(map[int64]map[string]int64) // ts -> dimension -> tokens
		cached := make(map[int64]map[string]int64) // ts -> dimension -> cached tokens
		for rows.Next() {
			var name string
			var tsBucket int64
			var count int
			var tok, cachedTok int64
			if err := rows.Scan(&name, &tsBucket, &count, &tok, &cachedTok); err == nil {
				if counts[tsBucket] == nil {
					counts[tsBucket] = map[string]int{}
					tokens[tsBucket] = map[string]int64{}
					cached[tsBucket] = map[string]int64{}
				}
				counts[tsBucket][name] += count
				tokens[tsBucket][name] += tok
				cached[tsBucket][name] += cachedTok
			}
		}
		order := make(map[string]bool)
		for _, bucket := range counts {
			for name := range bucket {
				if strings.TrimSpace(name) == "" {
					continue
				}
				order[name] = true
			}
		}
		groups := make([]ModelSeriesGroup, 0, len(order))
		for name := range order {
			group := ModelSeriesGroup{Model: name, Data: make([]int, len(bucketLabels)), Tokens: make([]int64, len(bucketLabels)), TokensUncached: make([]int64, len(bucketLabels))}
			for idx, label := range bucketLabels {
				for ts, bucket := range counts {
					if tsToLabel[ts] == label {
						group.Data[idx] += bucket[name]
						group.Tokens[idx] += tokens[ts][name]
						// 未缓存 = 全部 − 缓存命中；数据异常（cached > total）时钳制为 0，避免负值污染图表。
						if uncached := tokens[ts][name] - cached[ts][name]; uncached > 0 {
							group.TokensUncached[idx] += uncached
						}
					}
				}
			}
			groups = append(groups, group)
		}
		// 输出顺序稳定：按维度名字母序排序。此前由 map 迭代生成，顺序每次随机，
		// 前端相同调用次数的系列会在图例上来回换位。
		sort.Slice(groups, func(i, j int) bool { return groups[i].Model < groups[j].Model })
		return groups, nil
	}

	byModel, err := buildDimensionTrends(`
		SELECT model, ` + tsExpr + ` as ts_sec, COUNT(*) as count,
			COALESCE(SUM(total_tokens), 0) as tokens,
			COALESCE(SUM(cached_tokens), 0) as cached
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY ts_sec, model
		ORDER BY model ASC, ts_sec ASC
	`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 站点（endpoint）维度：与模型维度同构，供前端切换「模型 / 站点调用次数」。
	byEndpoint, err := buildDimensionTrends(`
		SELECT CASE WHEN g.endpoint_id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END, ` + tsExpr + ` as ts_sec, COUNT(*) as count,
			COALESCE(SUM(g.total_tokens), 0) as tokens,
			COALESCE(SUM(g.cached_tokens), 0) as cached
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
		LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id
		WHERE g.timestamp >= ? AND g.route != 'models'
		GROUP BY ts_sec, g.endpoint_id
	`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"daily":      dailyPoints,
		"models":     modelShares,
		"endpoints":  endpointShares,
		"buckets":    bucketLabels,
		"byModel":    byModel,
		"byEndpoint": byEndpoint,
	})
}

// clearAnalyticsLogs 清空网关日志表（会话鉴权由路由层保证）。
func (s *Service) clearAnalyticsLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	result, err := db.ExecContext(ctx, "DELETE FROM openai_gateway_analytics")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	deleted, _ := result.RowsAffected()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "deleted": deleted})
}

// getAnalyticsLogs returns paginated raw gateway logs
func (s *Service) getAnalyticsLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	daysStr := r.URL.Query().Get("days")
	statusStr := r.URL.Query().Get("status")
	modelStr := strings.TrimSpace(r.URL.Query().Get("model"))
	endpointStr := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	failOnly := r.URL.Query().Get("errors") == "1"

	page := 1
	pageSize := 20

	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
		pageSize = min(ps, 100)
	}
	days := 7
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	offset := (page - 1) * pageSize
	timeFilter, _ := analyticsTimeWindow(ctx, db, days)

	// 动态筛选条件：按状态码(success/error/429/5xx)、模型、端点过滤，或只看失败。
	whereClauses := []string{"g.timestamp >= ?", "g.route != 'models'"}
	whereArgs := []interface{}{timeFilter}
	switch statusStr {
	case "error":
		whereClauses = append(whereClauses, "g.status_code >= 400")
	case "429":
		whereClauses = append(whereClauses, "g.status_code = 429")
	case "5xx":
		whereClauses = append(whereClauses, "g.status_code >= 500")
	case "success":
		whereClauses = append(whereClauses, "g.status_code >= 200 AND g.status_code < 400")
	}
	if failOnly {
		whereClauses = append(whereClauses, "g.status_code >= 400")
	}
	if modelStr != "" {
		whereClauses = append(whereClauses, "g.model = ?")
		whereArgs = append(whereArgs, modelStr)
	}
	if endpointStr != "" {
		whereClauses = append(whereClauses, "(e.id = ? OR e.name = ? OR a.name = ?)")
		whereArgs = append(whereArgs, endpointStr, endpointStr, endpointStr)
	}
	whereSQL := strings.Join(whereClauses, " AND ")

	// Get total count
	args := append([]interface{}{}, whereArgs...)
	var total int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_gateway_analytics g LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id WHERE "+whereSQL, args...).Scan(&total)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Query paginated logs
	args = append([]interface{}{}, whereArgs...)
	args = append(args, pageSize, offset)
	rows, err := db.QueryContext(ctx, `
		SELECT 
			g.id,
			g.route,
			g.endpoint_id,
			CASE WHEN g.endpoint_id = '' THEN '无可用端点' ELSE COALESCE(e.name, a.name, '已移除端点') END as endpoint_name,
			COALESCE(k.name, '未识别密钥') as gateway_key_name,
			g.model,
			COALESCE(g.real_model, '') as real_model,
			g.status_code,
			g.latency_ms,
			g.ttfb_ms,
			g.prompt_tokens,
			g.completion_tokens,
			g.total_tokens,
			g.cached_tokens,
			COALESCE(g.client_ip, '') as client_ip,
			COALESCE(g.upstream_ip, '') as upstream_ip,
			g.stream,
			g.via_proxy,
			g.key_index,
			g.timestamp,
			COALESCE(g.failover_path, '') as failover_path,
			COALESCE(g.error_kind, '') as error_kind,
			COALESCE(g.error_message, '') as error_message,
			COALESCE(g.response_body, '') as response_body,
			COALESCE(g.cost, 0) as cost,
			COALESCE(g.cost_currency, '') as cost_currency
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
		LEFT JOIN openai_endpoint_name_archive a ON g.endpoint_id = a.endpoint_id
		LEFT JOIN openai_gateway_keys k ON g.gateway_key_id = k.id
		WHERE `+whereSQL+`
		ORDER BY g.timestamp DESC
		LIMIT ? OFFSET ?
	`, args...)

	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type LogRecord struct {
		ID               int     `json:"id"`
		Route            string  `json:"route"`
		EndpointID       string  `json:"endpointId"`
		EndpointName     string  `json:"endpointName"`
		GatewayKeyName   string  `json:"gatewayKeyName"`
		Model            string  `json:"model"`
		RealModel        string  `json:"realModel"`
		StatusCode       int     `json:"statusCode"`
		LatencyMs        int64   `json:"latencyMs"`
		TTFbMs           int64   `json:"ttfbMs"`
		PromptTokens     int     `json:"promptTokens"`
		CompletionTokens int     `json:"completionTokens"`
		TotalTokens      int     `json:"totalTokens"`
		CachedTokens     int     `json:"cachedTokens"`
		ClientIP         string  `json:"clientIp"`
		UpstreamIP       string  `json:"upstreamIp"`
		Stream           bool    `json:"stream"`
		ViaProxy         bool    `json:"viaProxy"`
		KeyIndex         int     `json:"keyIndex"`
		Timestamp        string  `json:"timestamp"`
		FailoverPath     string  `json:"failoverPath"`
		ErrorKind        string  `json:"errorKind"`
		ErrorMessage     string  `json:"errorMessage"`
		ErrorResponse    string  `json:"errorResponse"`
		Cost             float64 `json:"cost"`
		CostCurrency     string  `json:"costCurrency"`
	}

	records := []LogRecord{}
	for rows.Next() {
		var rec LogRecord
		var streamVal, viaProxyVal int
		if err := rows.Scan(
			&rec.ID,
			&rec.Route,
			&rec.EndpointID,
			&rec.EndpointName,
			&rec.GatewayKeyName,
			&rec.Model,
			&rec.RealModel,
			&rec.StatusCode,
			&rec.LatencyMs,
			&rec.TTFbMs,
			&rec.PromptTokens,
			&rec.CompletionTokens,
			&rec.TotalTokens,
			&rec.CachedTokens,
			&rec.ClientIP,
			&rec.UpstreamIP,
			&streamVal,
			&viaProxyVal,
			&rec.KeyIndex,
			&rec.Timestamp,
			&rec.FailoverPath,
			&rec.ErrorKind,
			&rec.ErrorMessage,
			&rec.ErrorResponse,
			&rec.Cost,
			&rec.CostCurrency,
		); err == nil {
			rec.Stream = streamVal == 1
			rec.ViaProxy = viaProxyVal == 1
			records = append(records, rec)
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total":   total,
		"records": records,
	})
}
