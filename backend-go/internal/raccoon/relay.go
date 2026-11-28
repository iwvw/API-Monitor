package raccoon

// 本文件中继面：把网关转发来的 OpenAI 请求打到小浣熊上游（标准 OpenAI 兼容），
// 失败换号重试，并透传/聚合上游响应。

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// maxChatBodyBytes 是对话请求体的大小上限（64MB）。
const maxChatBodyBytes int64 = 64 << 20

// serveRelay 处理 /api/raccoon/v1/* 的中继请求。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "小浣熊插件未启用", "service_unavailable")
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
	models := s.catalog()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		if !s.isModelEnabled(m.ID) {
			continue
		}
		entry := map[string]interface{}{
			"id":       s.prefixModel(m.ID),
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": providerName,
		}
		if eff := m.effectiveMultiplier(); eff != nil {
			entry["multiplier"] = *eff
		}
		out = append(out, entry)
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体不是合法 JSON", "invalid_request_error")
		return
	}
	clientModel := ""
	if raw, ok := fields["model"]; ok {
		_ = json.Unmarshal(raw, &clientModel)
	}
	clientModel = strings.TrimSpace(clientModel)
	if clientModel == "" {
		writeOpenAIError(w, http.StatusBadRequest, "缺少 model", "invalid_request_error")
		return
	}
	// 白名单校验：目录外模型拒绝（上游会对未知模型静默回落默认模型，必须本地拦截）。
	if !s.isModelEnabled(clientModel) {
		writeOpenAIError(w, http.StatusBadRequest, "未知模型 "+clientModel+"，可用模型见 /v1/models", "invalid_request_error")
		return
	}
	// 剥掉前缀得到上游 id（无前缀时原样）。
	upstreamModel := strings.TrimSpace(s.stripModelPrefix(clientModel))
	// 剥离 reasoning_effort：上游默认档即最深思考，下发档位反而可能削弱（raccoon2api 观察）。
	delete(fields, "reasoning_effort")

	modelJSON, _ := json.Marshal(upstreamModel)
	fields["model"] = json.RawMessage(modelJSON)
	upstreamBody, err := json.Marshal(fields)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体序列化失败: "+err.Error(), "invalid_request_error")
		return
	}

	stream := false
	if raw, ok := fields["stream"]; ok {
		_ = json.Unmarshal(raw, &stream)
	}

	ctx := r.Context()
	maxAttempts := len(s.Settings().Accounts) + 1
	tried := map[string]bool{}
	var lastStatus int
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		acc, ok := s.pickAccount(tried)
		if !ok {
			break
		}
		tried[acc.ID] = true
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			s.markAccountError(ctx, acc.ID, err.Error())
			lastErr = err
			continue
		}
		resp, err := s.sendChat(ctx, acc, upstreamBody)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}

		// 401 → 强制刷新同账号并重试一次。
		if resp.StatusCode == http.StatusUnauthorized {
			_ = resp.Body.Close()
			if strings.TrimSpace(acc.RefreshToken) != "" {
				if _, rerr := s.refreshTokenLocked(ctx, &acc, true); rerr != nil {
					s.markAccountError(ctx, acc.ID, rerr.Error())
					lastErr = errors.New("账号 " + acc.ID + " 强制刷新失败：" + rerr.Error())
					continue
				}
				resp, err = s.sendChat(ctx, acc, upstreamBody)
				if err != nil {
					s.setCooldown(acc.ID, cooldownDuration)
					lastErr = err
					continue
				}
			}
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			s.clearCooldown(acc.ID)
			s.recordCall(acc.ID)
			if stream {
				s.forwardStream(ctx, w, resp, acc.ID, upstreamModel, clientModel)
			} else {
				s.forwardJSON(w, resp, acc.ID, upstreamModel, clientModel)
			}
			return
		case resp.StatusCode == http.StatusUnauthorized:
			msg := readUpstreamError(resp)
			s.markAccountError(ctx, acc.ID, "上游返回 401")
			lastErr = errors.New("上游返回 401：" + msg)
			continue
		case resp.StatusCode == http.StatusTooManyRequests:
			_ = resp.Body.Close()
			s.setCooldown(acc.ID, cooldownDuration)
			s.markAccountError(ctx, acc.ID, "上游返回 429，已冷却")
			lastStatus = http.StatusTooManyRequests
			lastErr = errors.New("上游返回 429，账号已冷却")
			continue
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			s.markAccountError(ctx, acc.ID, fmt.Sprintf("上游返回 %d", resp.StatusCode))
			lastErr = errors.New(fmt.Sprintf("上游返回 %d", resp.StatusCode))
			continue
		default:
			// 其它 4xx：不换号，原样透传状态码与上游 message。
			msg := readUpstreamError(resp)
			writeOpenAIError(w, resp.StatusCode, msg, "upstream_error")
			return
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	if lastStatus == http.StatusTooManyRequests {
		writeOpenAIError(w, http.StatusTooManyRequests,
			"所有可用账号均被上游限流（429）："+lastErr.Error(), "rate_limit_error")
		return
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// sendChat 转发一次聊天请求（共享客户端，无总超时避免截断长回答）。
func (s *Service) sendChat(ctx context.Context, acc Account, body []byte) (*http.Response, error) {
	url := llmBaseURI + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	if strings.TrimSpace(acc.AccessToken) != "" {
		req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	}
	req.Header.Set("User-Agent", "api-monitor/raccoon-plugin")
	return s.httpClientFor(acc.ID).Do(req)
}

func readUpstreamError(resp *http.Response) string {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	msg := jsonMessage(data)
	if msg == "" {
		return http.StatusText(resp.StatusCode)
	}
	return truncate(msg, 300)
}

// forwardStream 逐行透传上游 SSE，并把每帧的 model 改写为客户端请求名，同时提取 usage 记账。
func (s *Service) forwardStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, accountID, upstreamModel, clientModel string) {
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	reader := bufio.NewReader(resp.Body)
	var lastUsage map[string]interface{}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			out, usage := rewriteSSELine(line, clientModel)
			if usage != nil {
				lastUsage = usage
			}
			_, _ = io.WriteString(w, out)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				applog.Warn(ctx, "raccoon", "upstream stream interrupted", "account", accountID, "error", err.Error())
			}
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastUsage != nil {
		s.recordUsageFromOpenAI(accountID, clientModel, lastUsage)
	}
}

// rewriteSSELine 重写一行 SSE 的 model 字段，返回改写后的行与原样 usage。
func rewriteSSELine(line, clientModel string) (string, map[string]interface{}) {
	trimmed := strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(trimmed, "data:") {
		return line, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return line, nil
	}
	frame := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return line, nil
	}
	var usage map[string]interface{}
	if raw, ok := frame["usage"]; ok {
		_ = json.Unmarshal(raw, &usage)
	}
	if modelJSON, err := json.Marshal(clientModel); err == nil {
		frame["model"] = json.RawMessage(modelJSON)
	}
	rewritten, err := json.Marshal(frame)
	if err != nil {
		return line, usage
	}
	return "data: " + string(rewritten) + "\n", usage
}

// forwardJSON 改写非流式响应体的 model 后原样下发，并提取 usage 记账。
func (s *Service) forwardJSON(w http.ResponseWriter, resp *http.Response, accountID, upstreamModel, clientModel string) {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChatBodyBytes))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "读取上游响应失败: "+err.Error(), "upstream_error")
		return
	}
	frame := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &frame); err != nil {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	if raw, ok := frame["usage"]; ok {
		var usage map[string]interface{}
		if err := json.Unmarshal(raw, &usage); err == nil {
			s.recordUsageFromOpenAI(accountID, clientModel, usage)
		}
	}
	if modelJSON, err := json.Marshal(clientModel); err == nil {
		frame["model"] = json.RawMessage(modelJSON)
	}
	rewritten, err := json.Marshal(frame)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rewritten)
}

// recordUsageFromOpenAI 从聚合后的 OpenAI 响应里取 usage 记账。
func (s *Service) recordUsageFromOpenAI(accountID, model string, usage map[string]interface{}) {
	if usage == nil {
		return
	}
	prompt := jsonInt(usage["prompt_tokens"])
	completion := jsonInt(usage["completion_tokens"])
	cached := cachedTokens(usage)
	s.recordUsage(accountID, model, prompt, completion, cached)
}
