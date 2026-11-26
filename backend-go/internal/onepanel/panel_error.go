package onepanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// panelEnvelope 对应 1Panel 的统一响应外壳 {code,message,data}。
// Code 用指针区分「不是 1Panel 信封」(字段缺失)与「code=0」两种情形,
// 避免把上游返回的裸 JSON(如通用代理透传的第三方响应)误判为失败。
type panelEnvelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// writePanelResult 统一收敛「1Panel 上游结果 → 面板响应」,解决两个长期问题:
//
//  1. 失败上下文丢失:此前代理把 1Panel 的 {code:500,message:"record not found"}
//     原样转发,调用方(尤其 Agent)既不知道是哪个 serverId、哪个上游路径、
//     传了什么 id,也无从重试。这里统一补上上下文并按语义映射 HTTP 状态。
//  2. 逻辑失败仍返回 200:此前无论 1Panel 成功失败都回 200,按 HTTP 状态或
//     response.success 判断的调用方会把失败当成功。现在失败走 response.Error。
//
// 成功时保留 1Panel 顶层字段(向后兼容既有调用方按 .data / .items 取值),
// 仅附加 success:true,使其与面板统一信封对齐。
func (s *Service) writePanelResult(w http.ResponseWriter, outwardPrefix, upstreamPath, serverID string, params map[string]string, output string) {
	var env panelEnvelope
	if err := json.Unmarshal([]byte(output), &env); err == nil && env.Code != nil && *env.Code != 200 {
		status := panelFailureStatus(*env.Code, env.Message)
		response.Error(w, status, panelFailureMessage(outwardPrefix, upstreamPath, serverID, params, env.Message))
		return
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(output), &payload); err != nil || payload == nil {
		payload = map[string]interface{}{}
	}
	if obj, ok := payload.(map[string]interface{}); ok {
		obj["success"] = true
	}
	response.JSON(w, http.StatusOK, payload)
}

// panelFailureStatus 把 1Panel 的 code + message 映射成对调用方有意义的 HTTP 状态。
// 1Panel 会把「记录不存在」也塞进 code:500,所以要结合 message 判定。
func panelFailureStatus(code int, message string) int {
	switch code {
	case http.StatusBadRequest:
		return http.StatusBadRequest
	case http.StatusUnauthorized:
		return http.StatusUnauthorized
	case http.StatusForbidden:
		return http.StatusForbidden
	case http.StatusNotFound:
		return http.StatusNotFound
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "record not found"), strings.Contains(lower, "not found"):
		return http.StatusNotFound
	case strings.Contains(lower, "permission"), strings.Contains(lower, "forbidden"), strings.Contains(message, "权限"):
		return http.StatusForbidden
	case strings.Contains(lower, "unauthorized"), strings.Contains(lower, "token"), strings.Contains(message, "认证"):
		return http.StatusUnauthorized
	default:
		return http.StatusBadGateway
	}
}

// panelFailureMessage 组装带完整上下文的失败信息:上游原文 + 对外接口 + 上游路径
// + serverID + 实际发送的路径参数,并对「记录不存在」给出可执行的取 id 指引。
func panelFailureMessage(outwardPrefix, upstreamPath, serverID string, params map[string]string, upstream string) string {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		upstream = "未知错误"
	}
	var b strings.Builder
	b.WriteString("1Panel 上游返回错误: ")
	b.WriteString(upstream)
	fmt.Fprintf(&b, "(接口 %s,上游 %s,serverId=%s", outwardPrefix, upstreamPath, serverID)
	if echo := panelParamEcho(params); echo != "" {
		b.WriteString(",")
		b.WriteString(echo)
	}
	b.WriteString(")")
	if hint := panelFailureHint(upstreamPath, upstream); hint != "" {
		b.WriteString(";")
		b.WriteString(hint)
	}
	return b.String()
}

// panelParamEcho 回显路径参数(排除 serverId),按字母序稳定输出,便于日志与重试。
func panelParamEcho(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "serverId" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, ",")
}

// panelFailureHint 针对「资源不存在」这类高频失败,按上游资源给出取正确 id 的指引。
// 只对确认存在的列表接口给提示,避免编造不存在的路径。
func panelFailureHint(upstreamPath, message string) string {
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "not found") {
		return ""
	}
	switch {
	case strings.Contains(upstreamPath, "/websites/ssl/"):
		return "证书 id 需取自 /api/onepanel/{serverId}/ssl/search 返回的 items[].id,且每台 1Panel 的 id 相互独立,不能跨主机复用"
	case strings.Contains(upstreamPath, "/websites/"):
		return "网站 id 需取自 /api/onepanel/{serverId}/websites/search 返回的 items[].id,且每台 1Panel 的 id 相互独立"
	case strings.Contains(upstreamPath, "/databases/"):
		return "数据库 id 需取自 /api/onepanel/{serverId}/databases 列表,且每台 1Panel 的 id 相互独立"
	case strings.Contains(upstreamPath, "/apps/installed/"):
		return "应用安装 id 需取自 /api/onepanel/{serverId}/apps/installed 列表"
	default:
		return "目标资源在该 serverId 对应的 1Panel 上不存在,请先用对应列表接口确认 id"
	}
}
