package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

func (s *Server) callAPIFromAI(ctx context.Context, call systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(call.Method))
	if method == "" {
		method = http.MethodGet
	}
	targetPath := strings.TrimSpace(call.Path)
	if targetPath == "" {
		return systemmetrics.AICallResponse{}, fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	if !allowedAIMethod(method) {
		return systemmetrics.AICallResponse{}, fmt.Errorf("不支持的 Agent 调用方法: %s", method)
	}
	// 防自毁拦截（所有模式生效）：AI 递归调用 AI 接入面、密钥轮换
	if strings.HasPrefix(targetPath, "/api/ai/") {
		return systemmetrics.AICallResponse{}, fmt.Errorf("AI 接入路由不允许递归调用")
	}
	if strings.HasPrefix(targetPath, "/api/system/ai-access/key") || strings.HasPrefix(targetPath, "/api/ai-access/key") {
		return systemmetrics.AICallResponse{}, fmt.Errorf("密钥管理接口不允许通过 Agent 调用")
	}

	// 权限模式：minimal 只读 / standard 写需开关且管理 AI 路由不可达 / full 全放开（单用户自用）
	accessPolicy, err := s.system.AIAgentAccessPolicy(ctx)
	if err != nil {
		return systemmetrics.AICallResponse{}, err
	}
	if accessPolicy == systemmetrics.AIAccessPolicyMinimal && isWriteAIMethod(method) {
		return systemmetrics.AICallResponse{}, fmt.Errorf("AI 接入处于只读模式（minimal），写操作已禁用")
	}
	if accessPolicy == systemmetrics.AIAccessPolicyStandard {
		if strings.HasPrefix(targetPath, "/api/admin-ai/") {
			return systemmetrics.AICallResponse{}, fmt.Errorf("管理 AI 路由不允许通过 Agent 调用")
		}
		if isWriteAIMethod(method) {
			writeAllowed, err := s.system.AIAgentWriteAllowed(ctx)
			if err != nil {
				return systemmetrics.AICallResponse{}, err
			}
			if !writeAllowed {
				return systemmetrics.AICallResponse{}, fmt.Errorf("Agent 写入操作未启用；请在「AI 接入」设置中开启允许写入")
			}
		}
	}
	if len(call.Body) > 1024*1024 {
		return systemmetrics.AICallResponse{}, fmt.Errorf("Agent 调用请求体超过 1MB 限制")
	}
	parsed, err := url.ParseRequestURI(targetPath)
	if err != nil {
		return systemmetrics.AICallResponse{}, fmt.Errorf("invalid path: %w", err)
	}
	if len(call.Body) > 0 {
		if err := s.system.ValidateAICallBody(method, parsed.EscapedPath(), call.Body); err != nil {
			return systemmetrics.AICallResponse{}, err
		}
	}
	route, ok := manifest.Match(parsed.EscapedPath())
	if !ok {
		return systemmetrics.AICallResponse{}, fmt.Errorf("API 路由不存在: %s", parsed.Path)
	}
	if route.Owner != manifest.OwnerGo {
		return systemmetrics.AICallResponse{}, fmt.Errorf("接口不可调用: %s", route.Module)
	}
	if route.ResponseMode == manifest.ResponseWebSocket || route.ResponseMode == manifest.ResponseStream {
		return systemmetrics.AICallResponse{}, fmt.Errorf("流式或 WebSocket 接口不允许通过 Agent 直接调用")
	}

	var body io.Reader
	if len(call.Body) > 0 {
		body = bytes.NewReader(call.Body)
	}
	req := httptest.NewRequest(method, "http://ai.internal"+targetPath, body).WithContext(ctx)
	for key, value := range call.Headers {
		// 保留内部专用头不可由 Agent 伪造：X-Internal-Cron 仅服务端 cron 执行器会携带，
		// 否则侧栏 AI 可借 call_api 触发仅限本机调用的内部端点。
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Cookie") || strings.EqualFold(key, "X-Admin-Password") || strings.EqualFold(key, "X-Internal-Cron") {
			continue
		}
		req.Header.Set(key, value)
	}
	if len(call.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-AI-Agent", "api-monitor")

	rec := httptest.NewRecorder()
	s.system.RecordAPICall(method, parsed.Path)
	s.serveGoRoute(rec, req, route)

	result := rec.Result()
	defer result.Body.Close()
	raw, _ := io.ReadAll(result.Body)
	payload := interface{}(nil)
	if len(raw) > 0 {
		var decoded interface{}
		if err := json.Unmarshal(raw, &decoded); err == nil {
			payload = decoded
		}
	}
	response := systemmetrics.AICallResponse{
		StatusCode: result.StatusCode,
		Headers:    result.Header,
		Body:       payload,
		Raw:        string(raw),
	}
	if payload != nil {
		response.Raw = ""
	}
	if result.StatusCode >= 200 && result.StatusCode < 300 {
		if businessErr := systemmetrics.EnvelopeError(payload); businessErr != "" {
			return systemmetrics.AICallResponse{}, fmt.Errorf("接口返回 HTTP %d 但业务失败: %s", result.StatusCode, businessErr)
		}
		return response, nil
	}

	// 失败响应附可操作修复建议（业界原则：错误信息要具体、可修正，
	// 避免 AI 拿到裸状态码后连环重试）。
	businessErr := systemmetrics.EnvelopeError(payload)
	hint := ""
	switch result.StatusCode {
	case http.StatusMethodNotAllowed:
		hint = fmt.Sprintf("该接口不支持 %s 方法；请先用 get_route 工具查询 %s 的真实可用方法，再以正确方法调用", method, targetPath)
	case http.StatusNotFound:
		hint = fmt.Sprintf("路径 %s 未能命中接口或资源不存在：请确认路径参数已替换为真实 ID（可先用 find_api 定位对应 list 接口获取 ID），或用 find_api 复核路径拼写", targetPath)
	case http.StatusBadRequest:
		hint = fmt.Sprintf("请求不合法：请先用 get_route 工具查看 %s 的 requestSchema/requestExample，按契约修正请求体或参数", targetPath)
	case http.StatusUnauthorized, http.StatusForbidden:
		hint = "该接口需要更高权限或会话，当前调用凭证不足"
	}
	if businessErr != "" && hint != "" {
		return systemmetrics.AICallResponse{}, fmt.Errorf("%s (HTTP %d)：%s。修复建议：%s", businessErr, result.StatusCode, strings.TrimSpace(string(raw)), hint)
	}
	if hint != "" {
		return systemmetrics.AICallResponse{}, fmt.Errorf("HTTP %d：%s。修复建议：%s", result.StatusCode, strings.TrimSpace(string(raw)), hint)
	}
	return response, nil
}

func allowedAIMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isWriteAIMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
