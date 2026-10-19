package qoder

// 本文件中继面：把网关转发来的 OpenAI 请求打到 Qoder 上游，失败换号重试，
// 并透传/聚合上游的加密 SSE。

import (
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

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// cooldownDuration 是账号瞬时故障（429/5xx/网络）后的冷却时长。
const cooldownDuration = 60 * time.Second

// quotaCooldownDuration 是账号「额度/次数用尽」后的冷却时长。
//
// 上游的 Billing daily count exceeded 按天重置，因此不能只冷却 60 秒——否则每 1 分钟
// 就会重新选中这个已经用尽的账号，把一次失败放大成持续的换号风暴。
// 取 30 分钟作为折中：既不会整天弃用一个可能已恢复的账号，又能避免高频空转；
// 真正的恢复时刻由上游返回（若带了重置时间则以它为准，见 envelopeRetryAfter）。
const quotaCooldownDuration = 30 * time.Minute

// autoRefreshInterval 是后台自动刷新的扫描周期。
const autoRefreshInterval = 5 * time.Minute

// serveRelay 处理 /api/qoder/v1/* 的中继请求。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "Qoder 插件未启用", "service_unavailable")
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
		id := s.prefixModel(m.ID)
		if !s.isModelEnabled(m.ID) {
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

// catalogEntryFor 按上游短 key（或对外名）返回当前目录条目，供请求前校验用。
func (s *Service) catalogEntryFor(model string) (ModelInfo, bool) {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return ModelInfo{}, false
	}
	for _, m := range s.catalog() {
		if strings.EqualFold(m.Key, name) || strings.EqualFold(m.ID, name) {
			return m, true
		}
	}
	return ModelInfo{}, false
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
	if !s.isModelEnabled(model) {
		writeOpenAIError(w, http.StatusBadRequest, "模型已停用: "+model, "invalid_request_error")
		return
	}
	// 对外名（官方模型名或短 key）解析为上游短 key，转发时写进 model_config.key。
	upstreamModel := s.lookupModelKey(model)
	payload["model"] = upstreamModel

	// 请求前校验：上下文超限直接拒绝，避免绕一圈等上游报错（且换号重试会浪费额度）。
	if entry, ok := s.catalogEntryFor(upstreamModel); ok {
		opt := parseRequestOptions(payload)
		if over, reason := contextCheck(entry, usageTokensForCheck(payload), opt); over {
			writeOpenAIError(w, http.StatusBadRequest, reason, "context_length_exceeded")
			return
		}
	}

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

// rejectIfPoolLimited 在池级限流期内直接返回 429，避免依次试满所有账号。
//
// 返回 true 表示已写出响应、调用方应立即返回。
func (s *Service) rejectIfPoolLimited(w http.ResponseWriter) bool {
	limited, until, msg := s.poolLimitStatus(time.Now())
	if !limited {
		return false
	}
	detail := "该模型的每日调用次数已达上游上限（账号池级限制，换号无效）"
	if msg != "" {
		detail += "：" + msg
	}
	detail += "。预计 " + until.In(cstZone).Format("2006-01-02 15:04") + " (UTC+8) 后恢复（该时间由上游每日重置规则推断，非上游下发）"
	writeOpenAIError(w, http.StatusTooManyRequests, detail, "insufficient_quota")
	return true
}

// proxyNonStream 非流式转发：上游只支持流式，这里聚合成整包；失败换号重试。
func (s *Service) proxyNonStream(ctx context.Context, w http.ResponseWriter, model string, body []byte) {
	if s.rejectIfPoolLimited(w) {
		return
	}
	st := s.Settings()
	exclude := map[string]bool{}
	var lastErr error
	allQuota := true
	tried := 0
	for attempt := 0; attempt < len(st.Accounts)+1; attempt++ {
		acc, ok := s.pickAccount(exclude, model)
		if !ok {
			break
		}
		exclude[acc.ID] = true
		tried++
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			allQuota = false
			continue
		}
		rc, status, lastBody, err := s.ChatStream(ctx, acc, body)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			allQuota = false
			continue
		}
		if status >= 400 {
			lastErr = s.noteUpstreamFailure(acc, status, string(lastBody))
			allQuota = false
			continue
		}
		resp, aerr := aggregateSSE(rc)
		_ = rc.Close()
		if aerr != nil {
			var envErr *upstreamEnvelopeError
			if errors.As(aerr, &envErr) && envErr.quotaExhausted() {
				// 聚合过程中发现额度错误：同样按池级候选处理。
				s.setCooldown(acc.ID, quotaCooldownDuration)
				lastErr = aerr
				continue
			}
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = aerr
			allQuota = false
			continue
		}
		s.clearCooldown(acc.ID)
		s.clearPoolLimit()
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
	if allQuota && tried > 0 {
		until := inferPoolResetAt(time.Now())
		s.markPoolLimited(lastErr.Error(), until)
		writeOpenAIError(w, http.StatusTooManyRequests,
			"所有可用账号的额度均已用尽（账号池级限制，换号无效）："+lastErr.Error()+
				"。预计 "+until.In(cstZone).Format("2006-01-02 15:04")+" (UTC+8) 后恢复（该时间由上游每日重置规则推断，非上游下发）",
			"insufficient_quota")
		return
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// proxyStream 流式转发。
//
// 换号重试的关键：上游把「额度用尽」这类业务错误塞在 HTTP 200 的 SSE 首行，
// 此时响应头还是干净的。因此这里**先预读首事件**：
//   - 首事件是错误 → 该账号设冷却、换下一个账号（客户端完全无感，不会看到 200）；
//   - 首事件正常   → 才写出响应头并转发（含已预读的那段）。
//
// 旧实现直接 pipeStreamCounted（先写 200 再读），错误一旦出现在首行就无法换号，
// 只能补一条 error 事件，表现为「看似成功但内容为空」。
func (s *Service) proxyStream(ctx context.Context, w http.ResponseWriter, model string, body []byte) {
	if s.rejectIfPoolLimited(w) {
		return
	}
	st := s.Settings()
	exclude := map[string]bool{}
	var lastErr error
	// 记录是否所有候选账号都因额度用尽而失败，用于给出更准确的提示。
	allQuota := true
	tried := 0
	for attempt := 0; attempt < len(st.Accounts)+1; attempt++ {
		acc, ok := s.pickAccount(exclude, model)
		if !ok {
			break
		}
		exclude[acc.ID] = true
		tried++
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			allQuota = false
			continue
		}
		rc, status, lastBody, err := s.ChatStream(ctx, acc, body)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			allQuota = false
			continue
		}
		if status >= 400 {
			lastErr = s.noteUpstreamFailure(acc, status, string(lastBody))
			allQuota = false
			continue
		}

		// 预读首事件：在写出任何响应之前判断这次到底能不能用。
		replay, first, perr := peekFirstEvent(rc)
		if perr != nil {
			_ = rc.Close()
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = perr
			allQuota = false
			continue
		}
		if env, isEnv := parseLegacyEnvelope(first); isEnv {
			if msg, isErr := envelopeError(env); isErr {
				_ = rc.Close()
				envErr := &upstreamEnvelopeError{message: msg, statusCode: env.StatusCodeValue}
				// 额度/次数用尽：用较长冷却，避免 1 分钟后再次选中这个已耗尽的账号。
				if envErr.quotaExhausted() {
					s.setCooldown(acc.ID, quotaCooldownDuration)
				} else {
					s.setCooldown(acc.ID, cooldownDuration)
					allQuota = false
				}
				lastErr = envErr
				continue // 换下一个账号
			}
		}

		// 首事件正常：写出响应头并转发（replay 会把首事件那段一并吐出）。
		s.clearCooldown(acc.ID)
		// 任一账号成功即说明池子恢复，清掉池级限流状态。
		s.clearPoolLimit()
		s.recordCall(acc.ID)
		usage, envErr := s.pipeStreamCounted(ctx, w, rc, replay, acc.ID, model)
		if usage != nil {
			s.recordUsageFromOpenAI(acc.ID, model, usage)
		}
		if envErr != nil {
			// 首事件之后才出现的错误（流中途）：此时 200 与部分数据已写出，
			// 无法换号，只能把错误明确传给调用方（而不是静默结束或干等超时）。
			msg := envErr.Error()
			if e, ok := envErr.(*upstreamEnvelopeError); ok && e.quotaExhausted() {
				s.setCooldown(acc.ID, quotaCooldownDuration)
				msg += "（该账号额度已用尽，可换用其它账号或等待额度重置）"
			}
			writeStreamError(w, msg)
		}
		return
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	// 所有尝试都因额度用尽失败：标记池级限流（后续请求直接短路），并返回 429。
	if allQuota && tried > 0 {
		until := inferPoolResetAt(time.Now())
		s.markPoolLimited(lastErr.Error(), until)
		writeOpenAIError(w, http.StatusTooManyRequests,
			"所有可用账号的额度均已用尽（账号池级限制，换号无效）："+lastErr.Error()+
				"。预计 "+until.In(cstZone).Format("2006-01-02 15:04")+" (UTC+8) 后恢复（该时间由上游每日重置规则推断，非上游下发）",
			"insufficient_quota")
		return
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// writeStreamError 在已经写出 200 SSE 响应头之后，用一条 error 事件告知调用方失败。
// 不能改 HTTP 状态码，但可以让客户端立即拿到原因而不是等超时。
func writeStreamError(w http.ResponseWriter, message string) {
	payload, err := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "upstream_error",
		},
	})
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// noteUpstreamFailure 记录一次上游非 2xx 失败：可重试错误（429/5xx）给账号设冷却。
func (s *Service) noteUpstreamFailure(acc Account, status int, body string) error {
	retryable := isRetryableHTTP(status)
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

// pipeStreamCounted 把上游 SSE 透传给下游，同时提取 usage（最后一个非空 usage 为准）。
//
// src 可以是「预读后回放的流」（见 peekFirstEvent）；rc 是原始响应体，由本函数负责关闭。
//
// 返回 (usage, 信封错误)。信封错误表示上游在 HTTP 200 的流里报了业务错误
// （如 code=110 额度用尽）——调用方据此决定「换号重试」还是「告诉用户」。
func (s *Service) pipeStreamCounted(ctx context.Context, w http.ResponseWriter, rc io.ReadCloser, src io.Reader, accountID, model string) (map[string]interface{}, error) {
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
	err := streamAndCapture(src, w, flusher, func(chunk map[string]interface{}) {
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			lastUsage = u
		}
	})
	if err != nil {
		var envErr *upstreamEnvelopeError
		if errors.As(err, &envErr) {
			// 信封错误：不是读失败，是上游明确报错（额度用尽等）。
			applog.Warn(ctx, "qoder", "upstream envelope error", "account", accountID, "model", model, "error", envErr.Error())
			return lastUsage, envErr
		}
		applog.Warn(ctx, "qoder", "read upstream stream failed", "error", err.Error())
	}
	return lastUsage, nil
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

// StartAutoRefresh 启动 access token 自动刷新：定期把即将过期/已过期的账号
// 提前换成新 token，避免转发时才发现失效。ctx 取消时退出。
func (s *Service) StartAutoRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(autoRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RefreshStaleAccounts(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// RefreshStaleAccounts 刷新所有「未停用、有 refresh token、剩余寿命低于提前量」的账号。
// 单个账号失败不影响其它账号，失败原因写回 LastError 便于前端排障。
func (s *Service) RefreshStaleAccounts(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if a.Disabled || a.RefreshToken == "" {
			continue
		}
		if state := tokenState(a); state == "valid" {
			continue
		}
		acc := a
		before := acc.AccessToken
		if err := s.refreshAccessToken(ctx, &acc); err != nil {
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
			continue
		}
		if acc.AccessToken == before && acc.LastRefreshAt == a.LastRefreshAt {
			continue
		}
		_ = s.upsertAccount(ctx, acc)
	}
}
