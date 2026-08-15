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
)

// settingsLocationFromDB 读取用户设置中的系统时区；
// 'system'/空/无效时回退 UTC（保持历史行为，与存储的 UTC 时间字符串一致）。
func settingsLocationFromDB(ctx context.Context, db *sql.DB) *time.Location {
	var zone sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT time_zone FROM user_settings WHERE id = 1`).Scan(&zone); err != nil || !zone.Valid {
		return time.UTC
	}
	name := strings.TrimSpace(zone.String)
	if name == "" || name == "system" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
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

// RecordAnalytics saves a gateway proxy metric to the SQLite database
func (s *Service) RecordAnalytics(ctx context.Context, route, endpointID, model string, statusCode int, latencyMs int64, ttfbMs int64, promptTokens, completionTokens, totalTokens, cachedTokens int, stream, viaProxy int, clientIP, upstreamIP string) {
	s.recordAnalyticsKey(ctx, route, endpointID, model, statusCode, latencyMs, ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, stream, viaProxy, clientIP, upstreamIP, -1, "")
}

// recordAnalyticsKey 与 RecordAnalytics 相同，但附带本次实际使用的 API Key 序号
// （keyIndex，0=主 key；-1 表示未知/未使用多 key），用于日志端点后的 key pill。
// failoverPath 为 JSON 数组，记录本轮请求尝试过的端点与状态码，便于前端展示迁移趋势。
func (s *Service) recordAnalyticsKey(ctx context.Context, route, endpointID, model string, statusCode int, latencyMs int64, ttfbMs int64, promptTokens, completionTokens, totalTokens, cachedTokens int, stream, viaProxy int, clientIP, upstreamIP string, keyIndex int, failoverPath string) {
	if ctx == nil {
		ctx = context.Background()
	}
	gatewayKey := gatewayKeyFromContext(ctx)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	db, err := s.open(writeCtx)
	if err != nil {
		applog.Error(writeCtx, "openai", "Failed to open db for recording analytics", "error", err.Error())
		return
	}
	defer db.Close()

	result, err := db.ExecContext(writeCtx, `
		INSERT INTO openai_gateway_analytics (endpoint_id, gateway_key_id, route, model, status_code, latency_ms, ttfb_ms, prompt_tokens, completion_tokens, total_tokens, cached_tokens, stream, via_proxy, client_ip, upstream_ip, key_index, failover_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, endpointID, gatewayKey.ID, route, model, statusCode, latencyMs, ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, stream, viaProxy, clientIP, upstreamIP, keyIndex, failoverPath)

	if err != nil {
		applog.Error(writeCtx, "openai", "Failed to insert gateway analytics", "error", err.Error())
	}

	// 实时推送：网关出现请求即广播给日志页订阅者（SSE）。
	// 字段与分页查询接口（getAnalyticsLogs）保持一致，保证实时插入顶部的行
	// 各列（端点、调用密钥、出口 IP、首字等）都能即时显示。
	recordID := int64(0)
	if result != nil {
		if id, idErr := result.LastInsertId(); idErr == nil {
			recordID = id
		}
	}
	endpointName := "unknown"
	if endpointID != "" {
		var name string
		if err := db.QueryRowContext(writeCtx, `SELECT COALESCE(name, 'unknown') FROM openai_endpoints WHERE id = ?`, endpointID).Scan(&name); err == nil {
			endpointName = name
		}
	}
	s.publishAnalytics(map[string]interface{}{
		"id":               recordID,
		"route":            route,
		"endpointId":       endpointID,
		"endpointName":     endpointName,
		"gatewayKeyName":   gatewayKey.Name,
		"model":            model,
		"statusCode":       statusCode,
		"latencyMs":        latencyMs,
		"ttfbMs":           ttfbMs,
		"promptTokens":     promptTokens,
		"completionTokens": completionTokens,
		"totalTokens":      totalTokens,
		"cachedTokens":     cachedTokens,
		"stream":           stream == 1,
		"viaProxy":         viaProxy == 1,
		"clientIp":         clientIP,
		"upstreamIp":       upstreamIP,
		"keyIndex":         keyIndex,
		"failoverPath":     failoverPath,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	})
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
	var totalTokens int
	var totalCachedTokens int
	var totalPromptTokens int
	var totalCompletionTokens int
	var errorCount int

	err = db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*), 
			COALESCE(AVG(latency_ms), 0.0), 
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cached_tokens), 0),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0)
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
	`, timeFilter).Scan(&totalRequests, &avgLatency, &totalTokens, &errorCount, &totalCachedTokens, &totalPromptTokens, &totalCompletionTokens)

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

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"totalRequests":         totalRequests,
		"avgLatency":            avgLatency,
		"totalTokens":           totalTokens,
		"totalCachedTokens":     totalCachedTokens,
		"cachedRatio":           cachedRatio,
		"totalPromptTokens":     totalPromptTokens,
		"totalCompletionTokens": totalCompletionTokens,
		"errorRate":             errorRate,
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
		Tokens      int     `json:"tokens"`
		Cached      int     `json:"cachedTokens"`
		Errors      int     `json:"errors"`
		Granularity string  `json:"granularity"`
	}

	dailyPoints := []ChartPoint{}
	for rows.Next() {
		var p ChartPoint
		var bucket string
		if err := rows.Scan(&bucket, &p.TsSec, &p.Count, &p.AvgLatency, &p.Tokens, &p.Cached, &p.Errors); err == nil {
			p.Day = bucket
			p.Granularity = granularity
			dailyPoints = append(dailyPoints, p)
		}
	}

	// 2. Model distribution by both request count and token usage.
	rowsModels, err := db.QueryContext(ctx, `
		SELECT 
			model,
			COUNT(*) as count,
			COALESCE(SUM(total_tokens), 0) as tokens
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY model
		ORDER BY count DESC, tokens DESC
	`, timeFilter)

	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rowsModels.Close()

	type ModelShare struct {
		Model  string `json:"model"`
		Count  int    `json:"count"`
		Tokens int    `json:"tokens"`
	}

	modelShares := []ModelShare{}
	for rowsModels.Next() {
		var m ModelShare
		if err := rowsModels.Scan(&m.Model, &m.Count, &m.Tokens); err == nil {
			// 过滤空白模型名（错误/异常请求可能未解析出 model）。
			if strings.TrimSpace(m.Model) == "" {
				continue
			}
			modelShares = append(modelShares, m)
		}
	}

	// 3. 按“模型 × 时段”展开调用量，供全宽趋势图多系列使用。
	rowsByModel, err := db.QueryContext(ctx, `
		SELECT 
			model,
			`+tsExpr+` as ts_sec,
			COUNT(*) as count
		FROM openai_gateway_analytics
		WHERE timestamp >= ? AND route != 'models'
		GROUP BY ts_sec, model
		ORDER BY model ASC, ts_sec ASC
	`, timeFilter)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rowsByModel.Close()

	tsToLabel := make(map[int64]string, len(dailyPoints))
	for _, point := range dailyPoints {
		tsToLabel[point.TsSec] = point.Day
	}

	type ModelSeriesGroup struct {
		Model string `json:"model"`
		Data  []int  `json:"data"`
	}
	bucketIndex := make(map[string]int, len(dailyPoints))
	bucketLabels := make([]string, 0, len(dailyPoints))
	for _, point := range dailyPoints {
		if _, ok := bucketIndex[point.Day]; !ok {
			bucketIndex[point.Day] = len(bucketLabels)
			bucketLabels = append(bucketLabels, point.Day)
		}
	}
	byModelCounts := make(map[int64]map[string]int) // ts -> model -> count
	for rowsByModel.Next() {
		var modelName string
		var tsBucket int64
		var count int
		if err := rowsByModel.Scan(&modelName, &tsBucket, &count); err == nil {
			if byModelCounts[tsBucket] == nil {
				byModelCounts[tsBucket] = map[string]int{}
			}
			byModelCounts[tsBucket][modelName] += count
		}
	}
	modelOrder := make(map[string]bool)
	for _, bucket := range byModelCounts {
		for name := range bucket {
			if strings.TrimSpace(name) == "" {
				continue
			}
			modelOrder[name] = true
		}
	}
	byModel := make([]ModelSeriesGroup, 0, len(modelOrder))
	for name := range modelOrder {
		group := ModelSeriesGroup{Model: name, Data: make([]int, len(bucketLabels))}
		for idx, label := range bucketLabels {
			for ts, bucket := range byModelCounts {
				if tsToLabel[ts] == label {
					group.Data[idx] += bucket[name]
				}
			}
		}
		byModel = append(byModel, group)
	}
	// 输出顺序稳定：按模型名字母序排序。此前由 map 迭代生成，顺序每次随机，
	// 前端相同调用次数的模型会在图例上来回换位。
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].Model < byModel[j].Model })

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"daily":   dailyPoints,
		"models":  modelShares,
		"buckets": bucketLabels,
		"byModel": byModel,
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
		whereClauses = append(whereClauses, "e.id = ?")
		whereArgs = append(whereArgs, endpointStr)
	}
	whereSQL := strings.Join(whereClauses, " AND ")

	// Get total count
	args := append([]interface{}{}, whereArgs...)
	var total int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_gateway_analytics g LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id WHERE "+whereSQL, args...).Scan(&total)
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
			COALESCE(e.name, 'unknown') as endpoint_name,
			COALESCE(k.name, '未识别密钥') as gateway_key_name,
			g.model,
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
			COALESCE(g.failover_path, '') as failover_path
		FROM openai_gateway_analytics g
		LEFT JOIN openai_endpoints e ON g.endpoint_id = e.id
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
		ID               int    `json:"id"`
		Route            string `json:"route"`
		EndpointName     string `json:"endpointName"`
		GatewayKeyName   string `json:"gatewayKeyName"`
		Model            string `json:"model"`
		StatusCode       int    `json:"statusCode"`
		LatencyMs        int64  `json:"latencyMs"`
		TTFbMs           int64  `json:"ttfbMs"`
		PromptTokens     int    `json:"promptTokens"`
		CompletionTokens int    `json:"completionTokens"`
		TotalTokens      int    `json:"totalTokens"`
		CachedTokens     int    `json:"cachedTokens"`
		ClientIP         string `json:"clientIp"`
		UpstreamIP       string `json:"upstreamIp"`
		Stream           bool   `json:"stream"`
		ViaProxy         bool   `json:"viaProxy"`
		KeyIndex         int    `json:"keyIndex"`
		Timestamp        string `json:"timestamp"`
		FailoverPath     string `json:"failoverPath"`
	}

	records := []LogRecord{}
	for rows.Next() {
		var rec LogRecord
		var streamVal, viaProxyVal int
		if err := rows.Scan(
			&rec.ID,
			&rec.Route,
			&rec.EndpointName,
			&rec.GatewayKeyName,
			&rec.Model,
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
