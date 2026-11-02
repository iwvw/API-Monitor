package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// admissionRequest 是准入控制模块的输入：请求上下文与记账所需的元信息。
type admissionRequest struct {
	Route            string
	Model            string
	Stream           bool
	TargetEndpointID string
	SessionKey       string
	ClientIP         string
	Started          time.Time
}

// admissionResult 是准入控制模块的输出。通过时携带过滤后的候选端点、首选端点
// 及其下标与代理标记；被拒时携带拒绝原因与状态码，由调用方按自身协议写回。
type admissionResult struct {
	Candidates  []Endpoint
	Selected    Endpoint
	ChosenIndex int
	ViaProxy    int
	Rejected    bool
	Reason      string
	StatusCode  int
}

// admitGatewayRequest 是三个转发入口（chat / responses / messages）共享的准入控制
// seam：候选组装 → 端点白名单过滤 → 模型/端点/配额校验 → 拒绝记录。授权判断集中
// 于此，failover 循环与协议写回留在各入口。
//
// 关键不变量：端点白名单必须在候选组装阶段过滤。failover 循环逐候选尝试时不再
// 校验白名单，若此处漏过滤，白名单内端点故障时会把请求打到白名单外的端点。
func (s *Service) admitGatewayRequest(ctx context.Context, db *sql.DB, req admissionRequest) admissionResult {
	candidates, selected, chosenIndex, _, found := s.selectEndpointCandidates(ctx, db, req.Model, req.TargetEndpointID, req.SessionKey)
	if !found {
		msg := fmt.Sprintf("网关无可用渠道（模型 %s）", req.Model)
		s.recordRelayError(RelayErrorRecord{
			Route: req.Route, Kind: "no_endpoint",
			Model: req.Model, Stream: req.Stream, ClientIP: req.ClientIP,
			ElapsedMs: time.Since(req.Started).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", req.Model, req.TargetEndpointID),
		})
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{"message": msg, "type": "service_unavailable"},
		})
		s.recordAnalyticsKey(ctx, req.Route, "", req.Model, http.StatusServiceUnavailable, time.Since(req.Started).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(req.Stream), 0, req.ClientIP, "", -1, "", &AnalyticsError{
			Kind:     "no_endpoint",
			Message:  fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", req.Model, req.TargetEndpointID),
			Response: errorResponseForLog(errBody, http.StatusServiceUnavailable),
		})
		return admissionResult{Rejected: true, Reason: msg, StatusCode: http.StatusServiceUnavailable}
	}

	// 代理标记先于白名单/限额分支计算，使早退路径的调用日志也能正确标注代理。
	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	if identity := gatewayKeyFromContext(ctx); len(identity.AllowedEndpoints) > 0 {
		filtered, newChosen := filterCandidatesByKeyIdentity(identity, candidates, chosenIndex)
		if len(filtered) == 0 {
			reason := s.recordDisallowedEndpoints(ctx, req.Route, req.Model, req.Stream, req.ClientIP, viaProxy, req.Started)
			return admissionResult{Rejected: true, Reason: reason, StatusCode: http.StatusForbidden, ViaProxy: viaProxy}
		}
		candidates = filtered
		chosenIndex = newChosen
		selected = candidates[chosenIndex]
	}

	if identity := gatewayKeyFromContext(ctx); identity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, identity, req.Model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: req.Route, Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: req.Model, Stream: req.Stream, ClientIP: req.ClientIP,
				ElapsedMs: time.Since(req.Started).Milliseconds(),
				Error:     limitErr,
			})
			errBody, _ := json.Marshal(map[string]interface{}{
				"error": map[string]string{"message": limitErr, "type": "forbidden"},
			})
			s.recordAnalyticsKey(ctx, req.Route, selected.ID, req.Model, http.StatusForbidden, time.Since(req.Started).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(req.Stream), viaProxy, req.ClientIP, "", -1, "", &AnalyticsError{
				Kind:     "blocked",
				Message:  limitErr,
				Response: errorResponseForLog(errBody, http.StatusForbidden),
			})
			return admissionResult{Rejected: true, Reason: limitErr, StatusCode: http.StatusForbidden, ViaProxy: viaProxy}
		}
	}

	return admissionResult{Candidates: candidates, Selected: selected, ChosenIndex: chosenIndex, ViaProxy: viaProxy}
}

// writeAdmissionRejection 按 OpenAI 风格错误格式写回准入拒绝（chat / responses
// 入口共用）。Anthropic 入口有独立错误格式，不使用本函数。
func writeAdmissionRejection(w http.ResponseWriter, res admissionResult) {
	errType := "service_unavailable"
	if res.StatusCode == http.StatusForbidden {
		errType = "forbidden"
	}
	response.JSON(w, res.StatusCode, map[string]interface{}{
		"error": map[string]string{"message": res.Reason, "type": errType},
	})
}
