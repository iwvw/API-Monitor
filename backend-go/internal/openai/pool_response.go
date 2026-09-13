package openai

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// isRateLimitResponse 判断上游响应是否命中限流（429/439 或正文含限流关键词）。
// 注意：503/529 是上游过载/停机信号，不属于客户端限流，不应计入「连续 429 冻结
// 代理」的累计（否则瞬时过载会把一个健康代理冻结 30 分钟）；正文关键词仍能覆盖
// 携带过载语义的 503 响应。
//
// 正文关键词分支仅在以下情形生效，避免把「200 成功回复正文恰好提到限流词」
// （如模型回答"如何处理 rate limit"）误判为限流（吞成功回复 + 误冻结出口）：
//   - 状态码 >= 400：错误响应正文，关键词可信；
//   - 任意状态码但正文解析为 JSON 且含非空顶层 "error" 成员：覆盖部分上游
//     「200 + 错误体」的设计，纯文本 200 正文含关键词不再命中。
func isRateLimitResponse(resp *http.Response, body []byte) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests, 439:
		return true
	}
	if resp.StatusCode < http.StatusBadRequest && !jsonBodyHasTopLevelError(body) {
		return false
	}
	if len(body) > 0 {
		lower := strings.ToLower(string(body))
		for _, keyword := range []string{"rate limit", "rate_limit", "too many requests", "overloaded", "throttled"} {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}
	return false
}

// jsonBodyHasTopLevelError 判断正文是否为 JSON 且携带非空的顶层 "error" 成员。
// 解析失败（纯文本/空正文）或 error 缺失/为 null 时返回 false。
func jsonBodyHasTopLevelError(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	raw, ok := parsed["error"]
	if !ok {
		return false
	}
	return string(bytes.TrimSpace(raw)) != "null"
}

// unavailableStatusCode 在所有候选端点均失败后，根据各端点失败码聚合决定返回给客户端的
// 状态码：全一致（如全部 429）→ 透传该码；不一致 → 503。供调用方在自己写响应时使用。
func unavailableStatusCode(model string, failCodes []int) int {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			return first
		}
	}
	return http.StatusServiceUnavailable
}

// writeRelayUnavailable 在所有候选端点均失败后，聚合各端点失败状态码决定返回给客户端的错误：
//   - 所有端点失败码一致（如全部 429）→ 透传该码，并说明网关无可用渠道。
//   - 失败码不一致或不在 4xx/5xx 内 → 返回 503 网关无可用渠道。
//
// 这类「所有渠道耗尽」属于网关自身状态，不额外写入调用日志（各尝试已在 relayLoop 内记录）。
func writeRelayUnavailable(w http.ResponseWriter, model string, failCodes []int) {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			msg := fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
			response.JSON(w, first, map[string]interface{}{
				"error": map[string]string{"message": msg, "type": "service_unavailable"},
			})
			return
		}
	}
	response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]string{
			"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
			"type":    "service_unavailable",
		},
	})
}

// isRetryableUpstreamResponse 判断上游响应是否值得切换到下一个代理重试：
// 限流（429/439/503/529 或限流关键词）与常见 5xx 服务器错误（500/502/504/599）。
// 501/505 等表示请求本身语义问题，重试无意义，不纳入。
func isRetryableUpstreamResponse(resp *http.Response, body []byte) bool {
	if isRateLimitResponse(resp, body) {
		return true
	}
	switch resp.StatusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout,
		http.StatusServiceUnavailable, 529, 599:
		return true
	}
	return false
}

// cleanHeaders 过滤掉空名称的条目，其余原样保留。
func cleanHeaders(headers []HeaderItem) []HeaderItem {
	out := make([]HeaderItem, 0, len(headers))
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		out = append(out, HeaderItem{Name: name, Value: h.Value})
	}
	return out
}

// isModelDisabled 判断给定模型是否在端点禁用的模型列表中。
func isModelDisabled(disabled []string, model string) bool {
	for _, m := range disabled {
		if m == model {
			return true
		}
	}
	return false
}

// decodeEndpointHeaders 从数据库的 headers JSON 列还原自定义请求头。
func decodeEndpointHeaders(raw sql.NullString) []HeaderItem {
	headers := []HeaderItem{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &headers)
	}
	return cleanHeaders(headers)
}

// decodeProxyPool 从数据库读取端点代理池（JSON 字符串数组）。
func decodeProxyPool(raw sql.NullString) []string {
	pool := []string{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &pool)
	}
	return cleanProxyPool(pool)
}

// applyCustomHeaders 把端点配置的自定义请求头写入待发请求。
// 网关自身的鉴权（Authorization 等）在调用方设置，自定义头允许覆盖非鉴权头。
func applyCustomHeaders(req *http.Request, headers []HeaderItem) {
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		req.Header.Set(name, h.Value)
	}
}
