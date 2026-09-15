package lobsterai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// cooldownDuration 是账号失败后的冷却时长。
const cooldownDuration = 60 * time.Second

// serveRelay 处理 /api/lobsterai/v1/* 的中继请求。
// 网关把本插件当作一个 OpenAI 兼容上游端点，因此这里只说 OpenAI 协议。
// stripped 是剥掉 /api/lobsterai 前缀后的路径。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "LobsterAI 插件未启用", "service_unavailable")
		return
	}
	switch {
	case r.Method == http.MethodGet && (stripped == "/v1/models" || stripped == "/v1/models/"):
		s.serveRelayModels(w, r)
	case r.Method == http.MethodPost && stripped == "/v1/chat/completions":
		s.serveChatCompletions(w, r)
	default:
		writeOpenAIError(w, http.StatusNotFound, "未支持的中继路径: "+stripped, "invalid_request_error")
	}
}

// serveRelayModels 输出带前缀、且过滤掉停用项的模型列表。
func (s *Service) serveRelayModels(w http.ResponseWriter, r *http.Request) {
	_ = s.refreshCatalog(r.Context())
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
	// 上游模型名必须是裸 ID，转发前把模型名替换回去。
	payload["model"] = upstreamModel
	normalized, err := json.Marshal(payload)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体序列化失败: "+err.Error(), "invalid_request_error")
		return
	}
	stream, _ := payload["stream"].(bool)

	if stream {
		s.proxyStream(r.Context(), w, upstreamModel, normalized)
		return
	}
	s.proxyNonStream(r.Context(), w, upstreamModel, normalized)
}

// proxyNonStream 非流式转发：上游只支持流式，这里聚合成整包；失败换号重试。
func (s *Service) proxyNonStream(ctx context.Context, w http.ResponseWriter, model string, body []byte) {
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
		rc, status, lastBody, err := s.ChatStream(ctx, acc, body)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		if status >= 400 {
			lastErr = s.noteUpstreamFailure(acc, status, string(lastBody))
			continue
		}
		resp, aerr := aggregateSSE(rc)
		_ = rc.Close()
		if aerr != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = aerr
			continue
		}
		s.clearCooldown(acc.ID)
		s.recordCall(acc.ID)
		s.recordUsageFromOpenAI(acc.ID, model, resp)
		out, merr := json.Marshal(resp)
		if merr != nil {
			writeOpenAIError(w, http.StatusBadGateway, "响应序列化失败: "+merr.Error(), "upstream_error")
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
func (s *Service) proxyStream(ctx context.Context, w http.ResponseWriter, model string, body []byte) {
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
		rc, status, lastBody, err := s.ChatStream(ctx, acc, body)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		if status >= 400 {
			lastErr = s.noteUpstreamFailure(acc, status, string(lastBody))
			continue
		}
		s.clearCooldown(acc.ID)
		s.recordCall(acc.ID)
		usage := s.pipeStreamCounted(ctx, w, rc, acc.ID, model)
		if usage != nil {
			s.recordUsageFromOpenAI(acc.ID, model, usage)
		}
		return
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// noteUpstreamFailure 记录一次上游非 2xx 失败：可重试错误（429/5xx/余额/限流）
// 给账号设冷却，返回分类后的错误供换号重试。
func (s *Service) noteUpstreamFailure(acc Account, status int, body string) error {
	kind := classifyMessage(body)
	retryable := isRetryableHTTP(status) || kind == "credit" || kind == "rate"
	if retryable {
		s.setCooldown(acc.ID, cooldownDuration)
	}
	msg := strings.TrimSpace(body)
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &upstreamError{
		status:     status,
		retryable:  retryable,
		authBroken: status == http.StatusUnauthorized || status == http.StatusForbidden,
		msg:        "上游返回 " + truncate(msg, 300),
	}
}

// pipeStreamCounted 把上游 SSE 透传给下游，同时从流里提取 usage（最后一个非空
// usage 为准）。返回提取到的 usage（可能为 nil）。
func (s *Service) pipeStreamCounted(ctx context.Context, w http.ResponseWriter, rc io.ReadCloser, accountID, model string) map[string]interface{} {
	defer rc.Close()
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}
	var lastUsage map[string]interface{}
	err := streamAndCapture(rc, w, flusher, func(chunk map[string]interface{}) {
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			lastUsage = u
		}
	})
	if err != nil {
		applog.Warn(ctx, "lobsterai", "read upstream stream failed", "error", err.Error())
	}
	return lastUsage
}

// recordUsageFromOpenAI 从聚合后的 OpenAI 响应里取 usage 记账。
func (s *Service) recordUsageFromOpenAI(accountID, model string, resp map[string]interface{}) {
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return
	}
	prompt := jsonInt(usage["prompt_tokens"])
	completion := jsonInt(usage["completion_tokens"])
	cached := cachedTokens(usage)
	s.recordUsage(accountID, model, prompt, completion, cached)
}
