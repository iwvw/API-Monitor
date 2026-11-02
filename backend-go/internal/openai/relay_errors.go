package openai

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// recordRelayError 记录一次推理转发失败事件：写入内存环形缓冲（供 relay-errors 接口读取），
// 并按严重度输出结构化日志。Proxy 字段必须传脱敏后的 host:port，禁止传完整代理 URL，
// 避免把代理凭据写进日志文件或接口响应。
func (s *Service) recordRelayError(rec RelayErrorRecord) {
	rec.Time = time.Now().UTC()
	// 按环节粗分类失败结果（对齐 opencode2api 上游账本的 outcome 语义
	// success/rejected/retryable_failure/transport_error；成功无记录，
	// 故此处只出现三种失败分类）。
	switch rec.Kind {
	case "dial", "timeout", "stream_closed":
		rec.Outcome = "transport_error"
	case "upstream", "failover", "bad_gateway":
		rec.Outcome = "retryable_failure"
	default:
		rec.Outcome = "rejected"
	}

	s.relayErrMu.Lock()
	s.relayErrors = append(s.relayErrors, rec)
	if len(s.relayErrors) > relayErrorBufferSize {
		s.relayErrors = s.relayErrors[len(s.relayErrors)-relayErrorBufferSize:]
	}
	s.relayErrMu.Unlock()

	logAttrs := []any{
		"route", rec.Route,
		"kind", rec.Kind,
		"endpoint", rec.Endpoint,
		"endpoint_id", rec.EndpointID,
		"key_index", rec.KeyIndex,
		"model", rec.Model,
		"stream", rec.Stream,
		"proxy", rec.Proxy,
		"client_ip", rec.ClientIP,
		"attempts", rec.Attempts,
		"elapsed_ms", rec.ElapsedMs,
		"err", rec.Error,
	}
	if rec.StatusCode > 0 {
		logAttrs = append(logAttrs, "upstream_status", rec.StatusCode)
	}
	if rec.Upstream != "" {
		logAttrs = append(logAttrs, "upstream_body", rec.Upstream)
	}
	switch rec.Kind {
	case "no_endpoint", "config", "gateway", "bad_gateway":
		applog.Error(context.Background(), "openai", "openai relay failed", logAttrs...)
	default:
		applog.Warn(context.Background(), "openai", "openai relay degraded", logAttrs...)
	}
}

// handleRelayErrors 返回最近发生的推理转发失败明细（最新在前），供管理界面与 AI 排障调用。
// limit 参数默认 50，上限与环形缓冲一致。
func (s *Service) handleRelayErrors(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= relayErrorBufferSize {
			limit = n
		}
	}

	s.relayErrMu.Lock()
	defer s.relayErrMu.Unlock()
	total := len(s.relayErrors)
	out := make([]RelayErrorRecord, 0, min(limit, total))
	for i := total - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.relayErrors[i])
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   total,
		"records": out,
	})
}

// truncateForLog 把任意字符串截断到指定字符数，避免上游错误体把日志或缓冲撑爆。
func truncateForLog(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + " ...(truncated)"
}

// errorResponseForLog 决定错误响应体（报错 JSON）是否写入调用日志：
// 仅当请求失败（状态码 >= 400）时返回截断后的报错 JSON，成功请求返回空串。
func errorResponseForLog(body []byte, statusCode int) string {
	if statusCode >= 200 && statusCode < 400 {
		return ""
	}
	return truncateForLog(string(body), relayErrorResponseLimit)
}

// gatewayBodyReadStatus 判定请求体读取失败的类型：MaxBytesReader 超限
// （*http.MaxBytesError）返回 413（客户端违约），其余读取失败返回 502。
func gatewayBodyReadStatus(err error) (int, string) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge, "body_too_large"
	}
	return http.StatusBadGateway, "gateway"
}

// trimErrorDetailRetention 清空超出保留上限（relayErrorResponseRetention）的错误详情：
// 只更新 error_kind/error_message/response_body 列，保留调用日志行（统计不受影响）。
// 最新记录按 timestamp DESC, id DESC 判定（同秒多记录时 id 越新越靠前）。
func (s *Service) trimErrorDetailRetention(ctx context.Context, db *sql.DB) {
	if _, err := db.ExecContext(ctx, `
		UPDATE openai_gateway_analytics
		SET error_kind = '', error_message = '', response_body = ''
		WHERE error_kind IS NOT NULL AND error_kind != ''
		  AND id NOT IN (
			SELECT id FROM (
				SELECT id FROM openai_gateway_analytics
				WHERE error_kind IS NOT NULL AND error_kind != ''
				ORDER BY timestamp DESC, id DESC
				LIMIT ?
			)
		  )
	`, relayErrorResponseRetention); err != nil {
		applog.Error(ctx, "openai", "Failed to trim error detail retention", "error", err.Error())
	}
}
