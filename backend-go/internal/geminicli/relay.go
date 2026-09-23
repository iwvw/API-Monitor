package geminicli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/openai"
)

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// cooldownDuration 是账号失败后的冷却时长。
const cooldownDuration = 60 * time.Second

// serveRelay 处理 /api/geminicli/v1/* 的中继请求。
// 网关把本插件当作一个 OpenAI 兼容上游端点，因此这里只说 OpenAI 协议。
// stripped 是剥掉 /api/geminicli 前缀后的路径。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "Gemini CLI 插件未启用", "service_unavailable")
		return
	}
	switch {
	case r.Method == http.MethodGet && (stripped == "/v1/models" || stripped == "/v1/models/"):
		s.serveRelayModels(w)
	case r.Method == http.MethodPost && stripped == "/v1/chat/completions":
		s.serveChatCompletions(w, r)
	default:
		writeOpenAIError(w, http.StatusNotFound, "未支持的中继路径: "+stripped, "invalid_request_error")
	}
}

// serveRelayModels 输出带前缀、且过滤掉停用项的模型列表。
func (s *Service) serveRelayModels(w http.ResponseWriter) {
	models := s.catalog()
	disabled := s.disabledSet()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		if disabled[id] {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  0,
			"owned_by": providerName,
		})
	}
	responseJSON(w, map[string]interface{}{"object": "list", "data": out})
}

// serveChatCompletions 转发一次对话请求。
func (s *Service) serveChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxChatBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体不是合法 JSON", "invalid_request_error")
		return
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "缺少 model", "invalid_request_error")
		return
	}
	upstreamModel := s.stripModelPrefix(model)
	if !s.isModelEnabled(model) && !s.isModelEnabled(upstreamModel) {
		writeOpenAIError(w, http.StatusBadRequest, "模型已停用: "+model, "invalid_request_error")
		return
	}
	stream, _ := payload["stream"].(bool)

	inner, err := openai.OpenAIChatToGeminiGenerate(payload)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求转换失败: "+err.Error(), "invalid_request_error")
		return
	}

	if stream {
		s.proxyStream(r.Context(), w, upstreamModel, inner)
		return
	}
	s.proxyNonStream(r.Context(), w, upstreamModel, inner)
}

// proxyNonStream 非流式转发，失败换号重试。
func (s *Service) proxyNonStream(ctx context.Context, w http.ResponseWriter, model string, inner map[string]interface{}) {
	st := s.Settings()
	exclude := map[string]bool{}
	var lastErr error
	for attempt := 0; attempt < len(st.Accounts)+1; attempt++ {
		acc, ok := s.pickAccount(exclude)
		if !ok {
			break
		}
		exclude[acc.ID] = true
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		resp, err := s.doGenerate(ctx, acc, model, inner, false)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		body, uerr := classifyUpstreamResponse(resp)
		if uerr != nil {
			if uerr.retryable {
				s.setCooldown(acc.ID, cooldownDuration)
			}
			lastErr = uerr
			continue
		}
		s.clearCooldown(acc.ID)
		s.recordCall(acc.ID)
		innerResp := unwrapGenerateResponse(body)
		s.recordUsageFromResponse(acc.ID, model, innerResp)
		// 转发会消耗额度；异步刷新该账号的选号快照，让 least-used 策略及时感知消耗。
		s.maybeRefreshQuotaSnapshot(acc)
		out, convErr := openai.GeminiGenerateToOpenAIChat(innerResp, model)
		if convErr != nil {
			writeOpenAIError(w, http.StatusBadGateway, "响应转换失败: "+convErr.Error(), "upstream_error")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
		return
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// proxyStream 流式转发，失败（尚未写出任何字节时）换号重试。
func (s *Service) proxyStream(ctx context.Context, w http.ResponseWriter, model string, inner map[string]interface{}) {
	st := s.Settings()
	exclude := map[string]bool{}
	var lastErr error
	for attempt := 0; attempt < len(st.Accounts)+1; attempt++ {
		acc, ok := s.pickAccount(exclude)
		if !ok {
			break
		}
		exclude[acc.ID] = true
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		resp, err := s.doGenerate(ctx, acc, model, inner, true)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			uerr := &upstreamError{
				status:     resp.StatusCode,
				retryable:  isRetryableHTTP(resp.StatusCode),
				authBroken: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
				msg:        "上游返回 " + http.StatusText(resp.StatusCode),
			}
			if len(body) > 0 {
				uerr.msg = "上游返回: " + strings.TrimSpace(string(body))
			}
			if uerr.retryable {
				s.setCooldown(acc.ID, cooldownDuration)
			}
			lastErr = uerr
			continue
		}
		s.clearCooldown(acc.ID)
		s.recordCall(acc.ID)
		s.maybeRefreshQuotaSnapshot(acc)
		s.pipeStream(ctx, w, resp.Body, acc.ID, model)
		return
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// pipeStream 把上游 SSE 转成 OpenAI SSE 写出。
func (s *Service) pipeStream(ctx context.Context, w http.ResponseWriter, body io.ReadCloser, accountID, model string) {
	defer body.Close()
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	transformer := openai.NewGeminiGenerateSSETransformer(model)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	usageModels := map[string]interface{}{}
	var lastUsage []byte
	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.HasPrefix(string(line), "data:") {
			continue
		}
		data := strings.TrimSpace(string(line[len("data:"):]))
		if data == "" {
			continue
		}
		out := transformer.Consume([]byte(data))
		for _, chunk := range out {
			_, _ = w.Write(chunk)
		}
		if flusher != nil {
			flusher.Flush()
		}
		// 记录 usage（每个分片都可能是最后一块）。
		inner := unwrapGenerateResponse([]byte(data))
		if u, ok := openai.GeminiGenerateUsage(inner); ok {
			usageModels = u
			lastUsage = inner
		}
	}
	for _, chunk := range transformer.Finish() {
		_, _ = w.Write(chunk)
	}
	if flusher != nil {
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		applog.Warn(ctx, "geminicli", "read upstream stream failed", "error", err.Error())
	}
	if lastUsage != nil {
		s.recordUsageFromResponse(accountID, model, lastUsage)
	} else if len(usageModels) > 0 {
		s.recordUsage(accountID, model, 0, 0, 0)
	}
}

// ensureToken 确保账号 access token 可用，过期则刷新。
func (s *Service) ensureToken(ctx context.Context, acc *Account) error {
	if acc.AccessToken != "" && time.Until(time.Unix(acc.ExpiresAt, 0)) > tokenExpiringWindow {
		return nil
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		if acc.AccessToken == "" {
			return errors.New("账号缺少可用凭据")
		}
		return nil
	}
	if err := s.refreshAccessToken(ctx, acc); err != nil {
		return err
	}
	_ = s.upsertAccount(ctx, *acc)
	return nil
}
