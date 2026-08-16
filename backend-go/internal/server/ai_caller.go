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
	if strings.HasPrefix(targetPath, "/api/ai/") {
		return systemmetrics.AICallResponse{}, fmt.Errorf("AI 接入路由不允许递归调用")
	}
	if strings.HasPrefix(targetPath, "/api/system/ai-access/key") || strings.HasPrefix(targetPath, "/api/ai-access/key") {
		return systemmetrics.AICallResponse{}, fmt.Errorf("密钥管理接口不允许通过 Agent 调用")
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
