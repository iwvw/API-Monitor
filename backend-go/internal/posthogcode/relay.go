package posthogcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// maxAccountAttempts 是单次转发最多尝试的账号数（首号 + 最多两次换号）。
const maxAccountAttempts = 3

// serveRelay 处理 /api/posthogcode/v1/* 的中继请求。
// 网关把本插件当作一个 OpenAI 兼容上游端点，因此这里只说 OpenAI 协议。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "PostHog Code 插件未启用", "service_unavailable")
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
	ids := s.visibleModels(r.Context())
	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  0,
			"owned_by": "posthog",
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
	if len(body) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "请求体为空", "invalid_request_error")
		return
	}

	requested := clientsModelName(body)
	if requested == "" {
		writeOpenAIError(w, http.StatusBadRequest, "请求缺少 model 字段", "invalid_request_error")
		return
	}
	// 停用模型先判、且不依赖是否有可用账号：这是最廉价也最准确的一道闸，
	// 也覆盖了直连 /api/posthogcode/v1（绕过网关）的情况。
	if s.disabledSet()[requested] {
		writeOpenAIError(w, http.StatusNotFound,
			"模型 "+requested+" 已在 PostHog Code 插件中停用", "model_not_found")
		return
	}

	model := s.stripModelPrefix(requested)
	st := s.Settings()
	if st.FreeTierOnly && !isFreeTierModel(model) {
		writeOpenAIError(w, http.StatusForbidden, fmt.Sprintf(
			"模型 %s 需要付费 PostHog 计划。当前插件已开启「仅免费层」，可用模型：%s",
			model, strings.Join(freeTierModels, ", ")), "permission_error")
		return
	}

	payload := rewriteModel(body, model)
	stream := clientWantsStream(body)

	var lastUpstream *upstreamError
	var lastLocal error
	tried := make(map[string]bool, maxAccountAttempts)
	attempted := false

	for attempt := 0; attempt < maxAccountAttempts; attempt++ {
		acc, ok := s.pickAccount(model, tried)
		if !ok {
			break
		}
		tried[acc.ID] = true
		attempted = true

		token, err := s.ensureFreshToken(r.Context(), &acc)
		if err != nil {
			lastLocal = err
			s.markCooldown(acc.ID, err.Error())
			continue
		}

		target := relayTarget{account: acc, token: token, url: s.chatCompletionsURL(acc)}
		err = s.forwardOnce(r.Context(), w, target, payload, stream)
		if err == nil {
			s.recordCall(acc.ID)
			// 转发会消耗额度。流式透传无法从响应里准确取到本次费用，
			// 故按需异步刷新该账号的真实额度，让 least-used 策略及时感知消耗。
			s.maybeRefreshQuotaSnapshot(acc)
			return
		}
		ue, ok := err.(*upstreamError)
		if !ok {
			lastLocal = err
			break
		}
		lastUpstream = ue
		if !ue.retryable {
			break
		}
		// 401 Invalid token 说明 access token 已被上游吊销（可能本地未到
		// 过期时刻但 PostHog 侧已撤销）。这与刷新失败同源：确定性故障，
		// 标记账号停用让选号跳过，而不只是 5 分钟冷却。
		if ue.status == http.StatusUnauthorized {
			s.markRevokedOnAuthFailure(r.Context(), &acc, fmt.Errorf("%s", ue.msg))
		}
		s.markCooldown(acc.ID, ue.msg)
	}

	if lastUpstream != nil {
		status := lastUpstream.status
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		writeOpenAIError(w, status, lastUpstream.msg, "upstream_error")
		return
	}
	if lastLocal != nil {
		writeOpenAIError(w, http.StatusBadGateway, lastLocal.Error(), "upstream_error")
		return
	}
	if !attempted {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"没有可用账号：请先在插件中完成 PostHog 授权，或检查账号是否已停用/token 是否已失效", "service_unavailable")
		return
	}
	writeOpenAIError(w, http.StatusBadGateway, "上游转发失败", "upstream_error")
}

// forwardOnce 执行一次上游转发。成功时把响应完整写回客户端并返回 nil；
// 失败时在写出任何响应体之前返回错误，保证调用方可安全换号重试。
func (s *Service) forwardOnce(ctx context.Context, w http.ResponseWriter, target relayTarget, payload []byte, stream bool) error {
	resp, err := doUpstream(ctx, target, payload, stream)
	if err != nil {
		return &upstreamError{msg: "连接上游失败: " + err.Error(), status: http.StatusBadGateway, retryable: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return classifyUpstream(resp.StatusCode, string(raw))
	}

	for _, h := range []string{"Content-Type", "Cache-Control", "X-Request-Id"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 32<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	return nil
}

// clientsModelName 从请求体读出客户端模型名（仅顶层 model 字段），不去前缀。
func clientsModelName(body []byte) string {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	m, _ := payload["model"].(string)
	return strings.TrimSpace(m)
}

// clientWantsStream 读取客户端原始请求里的 stream 意图。
func clientWantsStream(body []byte) bool {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	v, _ := payload["stream"].(bool)
	return v
}

// rewriteModel 把请求体里的 model 换成剥前缀后的上游模型名，其余字段原样保留。
func rewriteModel(body []byte, model string) []byte {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	payload["model"] = model
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}
