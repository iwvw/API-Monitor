package opencode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// upstreamTimeout 是到 Zen 上游的单次请求总超时（长输出按 100 tok/s 约需 300s）。
const upstreamTimeout = 5 * time.Minute

// upstreamRetryAttempts 是上游 403/429/5xx/网络错误时经新出口重试的最大次数。
// 代理池 slot 质量不均（区域封禁/限流），失败后换出口重试可显著降低偶发失败率。
const upstreamRetryAttempts = 3

// upstreamBodyLimit 是非流式组装时上游响应体读取上限（对齐网关 relay_loop 的 64MB）。
const upstreamBodyLimit int64 = 64 << 20

// serveRelay 处理 /api/opencode/v1/* 的中继请求。
// 网关把本插件当作一个 OpenAI 兼容上游端点，因此这里只说 OpenAI 协议。
// stripped 是剥掉 /api/opencode 前缀后的路径。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "OpenCode 插件未启用", "service_unavailable")
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
//
// 免费层只接受流式且带 OpenCode 身份的请求，因此无论客户端是否要求流式，
// 上行一律强制 stream，非流式时由本插件重排成单个 chat.completion 对象。
func (s *Service) serveChatCompletions(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxChatBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
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
	payload["model"] = upstreamModel
	clientWantsStream, _ := payload["stream"].(bool)

	st := s.Settings()
	if strings.TrimSpace(st.Session.XSessionID) == "" {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"未配置 OpenCode 会话身份：请在插件「OpenCode Zen」页面粘贴 session.json 或填写 xSessionId",
			"service_unavailable")
		return
	}

	// muse-spark 系列只在 /zen/v1/responses 上游存在，chat 请求需要翻译。
	var outbound map[string]interface{}
	upstreamPath := "/zen/v1/chat/completions"
	translate := false
	if strings.HasPrefix(upstreamModel, "muse-spark-") {
		outbound = translateChatToResponses(payload)
		upstreamPath = "/zen/v1/responses"
		translate = true
	} else {
		outbound = cloneMap(payload)
		ensureChatTools(outbound)
	}
	reqBody, err := json.Marshal(outbound)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "请求体序列化失败", "proxy_error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()

	// 经代理池出口时，失败（403/429/5xx/网络错）回写池冻结该出口并经新出口重试：
	// 池内 slot 出口质量不均（区域封禁/限流），换出口可显著降低偶发失败与慢尾。
	poolID := strings.TrimSpace(st.ProxyPoolID)
	maxAttempts := 1
	if poolID != "" && s.externalPool != nil {
		maxAttempts = upstreamRetryAttempts
	}
	var resp *http.Response
	endStatus := http.StatusBadGateway
	endErrBody := ""
	for attempt := 0; attempt < maxAttempts; attempt++ {
		proxy, client := s.selectProxyAndClient(ctx, poolID, "")
		up, upErr := s.zenUpstream(ctx, client, upstreamPath, st.APIKey, st.Session, reqBody)
		if upErr != nil {
			s.reportPoolResult(poolID, proxy, false, false, nil)
			endStatus = http.StatusBadGateway
			endErrBody = upErr.Error()
			continue
		}
		if up.StatusCode >= 200 && up.StatusCode < 300 {
			s.reportPoolResult(poolID, proxy, true, false, nil)
			resp = up
			break
		}
		errBody, _ := io.ReadAll(io.LimitReader(up.Body, 2<<20))
		up.Body.Close()
		is429 := up.StatusCode == http.StatusTooManyRequests || up.StatusCode == 439
		s.reportPoolResult(poolID, proxy, false, is429, responseRetryAfter(up))
		endStatus = up.StatusCode
		endErrBody = string(errBody)
	}
	if resp == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(endStatus)
		_ = json.NewEncoder(w).Encode(normalizeUpstreamError(endStatus, endErrBody))
		return
	}
	defer resp.Body.Close()

	if translate {
		if clientWantsStream {
			s.pipeResponsesToChatStream(r.Context(), w, resp.Body, upstreamModel)
			return
		}
		s.assembleResponsesNonStream(w, resp.Body, upstreamModel)
		return
	}

	if clientWantsStream {
		s.pipeChatPassthrough(w, resp.Body)
		return
	}
	s.assembleChatNonStream(w, resp.Body, upstreamModel)
}

// zenUpstream 向 https://opencode.ai/zen/v1 发起一次流式 POST。
// client 由调用方按本次出口选择（直连共享客户端或经代理池出口的客户端）。
func (s *Service) zenUpstream(ctx context.Context, client *http.Client, upstreamPath, apiKey string, session SessionIdentity, body []byte) (*http.Response, error) {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = freeKey
	}
	fullURL := upstreamBase + upstreamPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for k, vs := range identityHeaders(session) {
		if len(vs) > 0 {
			req.Header.Set(k, vs[0])
		}
	}
	return client.Do(req)
}

// responseRetryAfter 从响应头提取 Retry-After 秒数（供 429 冻结时长参考）。
func responseRetryAfter(resp *http.Response) *time.Duration {
	if resp == nil {
		return nil
	}
	ra := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if ra == "" {
		return nil
	}
	if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
		d := time.Duration(secs) * time.Second
		return &d
	}
	return nil
}

// identityHeaders 还原 OpenCode 客户端身份头（User-Agent + x-opencode-* / x-session-*）。
func identityHeaders(session SessionIdentity) http.Header {
	h := http.Header{}
	h.Set("User-Agent", firstNonEmptyStr(session.UserAgent, "opencode/latest/2.0.12/cli"))
	h.Set("x-opencode-client", firstNonEmptyStr(session.XOpencodeClient, "cli"))
	setIfNonEmpty(h, "x-opencode-org-id", session.XOpencodeOrgID)
	setIfNonEmpty(h, "x-opencode-project", session.XOpencodeProject)
	setIfNonEmpty(h, "x-opencode-session", session.XOpencodeSession)
	setIfNonEmpty(h, "x-session-id", session.XSessionID)
	if session.XOpencodeSession != "" {
		h.Set("x-opencode-session-id", session.XOpencodeSession)
	} else {
		setIfNonEmpty(h, "x-opencode-session-id", session.XSessionID)
	}
	setIfNonEmpty(h, "x-session-affinity", session.XSessionAffinity)
	h.Set("x-opencode-request", freshMsgID())
	return h
}

func setIfNonEmpty(h http.Header, k, v string) {
	if strings.TrimSpace(v) != "" {
		h.Set(k, v)
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// freshMsgID 生成与官方客户端一致格式的请求 ID（msg_ + 时间戳 + 随机 B62）。
func freshMsgID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	if len(ts) > 12 {
		ts = ts[len(ts)-12:]
	}
	for len(ts) < 12 {
		ts = "0" + ts
	}
	return "msg_" + ts + randB62(14)
}

const b62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randB62(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 兜底：直接退化为当前时间计数，几乎不会触发。
		for i := 0; i < n; i++ {
			buf[i] = b62Chars[(time.Now().UnixNano()+int64(i))%int64(len(b62Chars))]
		}
		return string(buf)
	}
	for i := range buf {
		buf[i] = b62Chars[int(buf[i])%len(b62Chars)]
	}
	return string(buf)
}

// normalizeUpstreamError 把上游错误统一成 OpenAI 标准信封 {error:{message,type,code}}：
//   - 已是标准形状的错误体提取 message 重包一层；
//   - 命中 FreeTierError（免费身份过期）时，message 追加续期提示；
//   - 非 JSON（HTML 错误页）截断包成 message。
func normalizeUpstreamError(status int, text string) map[string]interface{} {
	text = strings.TrimSpace(text)
	msg, typ := parseUpstreamErrorMsg(text)
	if freeTier := strings.Contains(strings.ToLower(text), "freetiererror"); freeTier {
		if msg == "" {
			msg = "OpenCode 免费层拒绝该请求（FreeTierError）"
		}
		msg = msg + "；免费身份可能已过期，请在插件页面重新抓取/粘贴 OpenCode 会话身份（session.json）"
		typ = "freetier_error"
	}
	if msg == "" {
		msg = text
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if msg == "" {
			msg = fmt.Sprintf("上游返回错误 %d", status)
		}
		typ = "upstream_error"
	}
	return map[string]interface{}{"error": map[string]interface{}{"message": msg, "type": typ, "code": status}}
}

// parseUpstreamErrorMsg 从上游错误体提取 message 与 type；提取不到返回空串。
func parseUpstreamErrorMsg(text string) (string, string) {
	var o map[string]interface{}
	if json.Unmarshal([]byte(text), &o) != nil {
		return "", ""
	}
	if e, ok := o["error"].(map[string]interface{}); ok {
		if m, ok := e["message"].(string); ok && m != "" {
			typ, _ := e["type"].(string)
			if typ == "" {
				typ = "upstream_error"
			}
			return m, typ
		}
	}
	if m, ok := o["error"].(string); ok && m != "" {
		return m, "upstream_error"
	}
	if m, ok := o["message"].(string); ok && m != "" {
		typ, _ := o["type"].(string)
		if typ == "" {
			typ = "upstream_error"
		}
		return m, typ
	}
	return "", ""
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// scanSSELines 用 bufio.Scanner 逐行读上游 SSE 流，回调非空 data 载荷。
// 行大小上限 4MB；流传输给 Translator 或原样透传。
func scanSSELines(ctx context.Context, r io.Reader, fn func(data []byte) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if !fn(data) {
			return nil
		}
	}
	return scanner.Err()
}

// -----------------------------------------------------------------------------
// Responses SSE → OpenAI chat SSE（增量流式翻译）
// -----------------------------------------------------------------------------

type chatStreamChunk struct {
	Base    map[string]interface{}
	Choices []map[string]interface{}
	Usage   *openAIUsage
}

// responsesChatStream 把 /zen/v1/responses 的 SSE 逐块翻译成 OpenAI chat 流式 chunk。
type responsesChatStream struct {
	model        string
	id           string
	created      int64
	toolIndexes  map[int]int
	toolCount    int
	hasTools     bool
	done         bool
	cachedTokens int
	usage        *openAIUsage
}

func newResponsesChatStream(model string) *responsesChatStream {
	return &responsesChatStream{
		model:       model,
		id:          "chatcmpl-opencode-" + strconv.FormatInt(time.Now().UnixMilli(), 10),
		created:     time.Now().Unix(),
		toolIndexes: map[int]int{},
	}
}

func (t *responsesChatStream) chunk(choices []map[string]interface{}) []byte {
	obj := map[string]interface{}{"id": t.id, "object": "chat.completion.chunk", "created": t.created, "model": t.model}
	if choices != nil {
		obj["choices"] = choices
	}
	out, _ := json.Marshal(obj)
	return append(append([]byte("data: "), out...), '\n', '\n')
}

func (t *responsesChatStream) usageChunk() []byte {
	if t.usage == nil {
		return nil
	}
	u := *t.usage
	if t.cachedTokens > 0 && u.PromptTokensDetails == nil {
		u.PromptTokensDetails = &promptTokensDetailObj{CachedTokens: t.cachedTokens}
	}
	out, _ := json.Marshal(map[string]interface{}{"id": t.id, "object": "chat.completion.chunk", "created": t.created, "model": t.model, "choices": []interface{}{}, "usage": u})
	return append(append([]byte("data: "), out...), '\n', '\n')
}

func (t *responsesChatStream) finish(reason, maxOut string) []byte {
	if t.done {
		return nil
	}
	t.done = true
	final := reason
	if maxOut != "" {
		final = "length"
	} else if reason == "" {
		final = "stop"
	}
	if t.hasTools {
		final = "tool_calls"
	}
	buf := bytes.Buffer{}
	buf.Write(t.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{}, "finish_reason": final}}))
	if u := t.usageChunk(); u != nil {
		buf.Write(u)
	}
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes()
}

// Consume 处理一行 SSE 载荷，返回要写出的完整 SSE 数据块。
func (t *responsesChatStream) Consume(data []byte) [][]byte {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	if nu, ok := obj["normalizedUsage"].(map[string]interface{}); ok {
		if n := intOf(nu["cacheReadTokens"]); n > t.cachedTokens {
			t.cachedTokens = n
		}
	}
	typ, _ := obj["type"].(string)
	switch typ {
	case "response.output_text.delta":
		if delta, ok := obj["delta"].(string); ok {
			return [][]byte{t.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": delta}, "finish_reason": nil}})}
		}
	case "response.reasoning_summary_text.delta":
		if delta, ok := obj["delta"].(string); ok {
			return [][]byte{t.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"reasoning_content": delta}, "finish_reason": nil}})}
		}
	case "response.output_item.added":
		item, _ := obj["item"].(map[string]interface{})
		if item["type"] == "function_call" {
			name, _ := item["name"].(string)
			idx := t.toolCount
			t.toolIndexes[intOf(obj["output_index"])] = idx
			t.toolCount++
			t.hasTools = true
			callID, _ := item["call_id"].(string)
			return [][]byte{t.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{
				"tool_calls": []map[string]interface{}{{
					"index":    idx,
					"id":       callID,
					"type":     "function",
					"function": map[string]interface{}{"name": unprefixToolName(name, nil), "arguments": ""},
				}},
			}, "finish_reason": nil}})}
		}
	case "response.function_call_arguments.delta":
		if delta, ok := obj["delta"].(string); ok {
			if idx, ok := t.toolIndexes[intOf(obj["output_index"])]; ok {
				return [][]byte{t.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{{"index": idx, "function": map[string]interface{}{"arguments": delta}}},
				}, "finish_reason": nil}})}
			}
		}
	case "response.completed", "response.incomplete":
		resp, _ := obj["response"].(map[string]interface{})
		if resp != nil {
			if u, ok := resp["usage"].(map[string]interface{}); ok {
				t.usage = usageToOpenAI(u)
			}
		}
		reason := ""
		maxOut := ""
		if typ == "response.incomplete" {
			details, _ := resp["incomplete_details"].(map[string]interface{})
			if r, ok := details["reason"].(string); ok && r == "max_output_tokens" {
				maxOut = r
			}
		}
		if out := t.finish(reason, maxOut); out != nil {
			return [][]byte{out}
		}
	}
	return nil
}

// Finish 在流结束但未收到 completed/incomplete 事件时优雅收尾。
func (t *responsesChatStream) Finish() []byte {
	return t.finish("stop", "")
}

// startChunk 是流开始先发的一条 role 占位块（见 pipeResponsesToChatStream）。

// pipeResponsesToChatStream 把 Responses SSE 流式翻译成 OpenAI chat SSE 写出。
// ctx 派生自请求上下文：客户端断开时及时中止上游读取，避免空转读完整个流。
func (s *Service) pipeResponsesToChatStream(ctx context.Context, w http.ResponseWriter, body io.Reader, model string) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	tr := newResponsesChatStream(model)
	_, _ = w.Write(tr.chunk([]map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"role": "assistant"}, "finish_reason": nil}}))
	if flusher != nil {
		flusher.Flush()
	}

	_ = scanSSELines(ctx, body, func(data []byte) bool {
		for _, chunk := range tr.Consume(data) {
			_, _ = w.Write(chunk)
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	})
	if out := tr.Finish(); out != nil {
		_, _ = w.Write(out)
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// assembleResponsesNonStream 汇总 Responses SSE 为单个 chat.completion 返回。
func (s *Service) assembleResponsesNonStream(w http.ResponseWriter, body io.Reader, fallbackModel string) {
	sseText, err := io.ReadAll(io.LimitReader(body, upstreamBodyLimit+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "读取上游响应失败: "+err.Error(), "upstream_error")
		return
	}
	if int64(len(sseText)) > upstreamBodyLimit {
		writeOpenAIError(w, http.StatusBadGateway, "上游响应体超过 64MB 上限", "upstream_error")
		return
	}
	out := responsesSSEToChatCompletion(string(sseText), fallbackModel)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// pipeChatPassthrough 直连 chat 模型：上游已是 OpenAI chat SSE，原样透传。
func (s *Service) pipeChatPassthrough(w http.ResponseWriter, body io.Reader) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

// assembleChatNonStream 汇总 chat SSE 为单个 chat.completion 返回。
func (s *Service) assembleChatNonStream(w http.ResponseWriter, body io.Reader, fallbackModel string) {
	sseText, err := io.ReadAll(io.LimitReader(body, upstreamBodyLimit+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "读取上游响应失败: "+err.Error(), "upstream_error")
		return
	}
	if int64(len(sseText)) > upstreamBodyLimit {
		writeOpenAIError(w, http.StatusBadGateway, "上游响应体超过 64MB 上限", "upstream_error")
		return
	}
	out := chatSSEToChatCompletion(string(sseText), fallbackModel)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// responsesSSEToChatCompletion 把 Responses SSE 文本组装成 chat.completion 对象。
func responsesSSEToChatCompletion(sseText, fallbackModel string) map[string]interface{} {
	model := fallbackModel
	id := "chatcmpl-opencode-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	var content, reasoning strings.Builder
	var toolCalls []map[string]interface{}
	var usage map[string]interface{}
	incomplete := false
	incompleteReason := ""
	hasTools := false

	forEachSSEData(sseText, func(obj map[string]interface{}) {
		if m, ok := obj["model"].(string); ok && m != "" {
			model = m
		}
		if r, ok := obj["response"].(map[string]interface{}); ok {
			if rID, ok := r["id"].(string); ok && rID != "" {
				id = rID
			}
			if rModel, ok := r["model"].(string); ok && rModel != "" {
				model = rModel
			}
		}
		typ, _ := obj["type"].(string)
		switch typ {
		case "response.output_text.delta":
			if d, ok := obj["delta"].(string); ok {
				content.WriteString(d)
			}
		case "response.reasoning_summary_text.delta":
			if d, ok := obj["delta"].(string); ok {
				reasoning.WriteString(d)
			}
		case "response.completed", "response.incomplete":
			if typ == "response.incomplete" {
				incomplete = true
			}
			resp, _ := obj["response"].(map[string]interface{})
			if resp != nil {
				if r, ok := resp["incomplete_details"].(map[string]interface{}); ok {
					if re, ok := r["reason"].(string); ok {
						incompleteReason = re
					}
				}
				if u, ok := resp["usage"].(map[string]interface{}); ok {
					usage = u
				}
				if items, ok := resp["output"].([]interface{}); ok {
					for _, raw := range items {
						item, _ := raw.(map[string]interface{})
						if item["type"] == "function_call" {
							name, _ := item["name"].(string)
							arguments := stringifyArgs(item["arguments"])
							callID, _ := item["call_id"].(string)
							toolCalls = append(toolCalls, map[string]interface{}{
								"id": callID, "type": "function",
								"function": map[string]interface{}{"name": unprefixToolName(name, nil), "arguments": arguments},
							})
							hasTools = true
						}
					}
				}
			}
		}
	})

	message := map[string]interface{}{"role": "assistant", "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	finishReason := "stop"
	if hasTools {
		finishReason = "tool_calls"
	} else if incomplete && incompleteReason == "max_output_tokens" {
		finishReason = "length"
	}
	out := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": nowUnix(),
		"model":   model,
		"choices": []map[string]interface{}{{"index": 0, "message": message, "finish_reason": finishReason}},
	}
	if u := usageToOpenAI(usage); u != nil {
		out["usage"] = u
	}
	return out
}

// chatSSEToChatCompletion 把 chat SSE 文本组装成 chat.completion 对象。
func chatSSEToChatCompletion(sseText, fallbackModel string) map[string]interface{} {
	model := fallbackModel
	id := "chatcmpl-opencode-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	var content, reasoning strings.Builder
	var toolCalls []map[string]interface{}
	var usage map[string]interface{}
	finishReason := "stop"
	created := nowUnix()

	forEachSSEData(sseText, func(obj map[string]interface{}) {
		if mID, ok := obj["id"].(string); ok && mID != "" {
			id = mID
		}
		if m, ok := obj["model"].(string); ok && m != "" {
			model = m
		}
		if c, ok := obj["created"].(float64); ok && c > 0 {
			created = int64(c)
		}
		if u, ok := obj["usage"].(map[string]interface{}); ok {
			usage = u
		}
		choices, _ := obj["choices"].([]interface{})
		if len(choices) == 0 {
			return
		}
		choice, _ := choices[0].(map[string]interface{})
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}
		delta, _ := choice["delta"].(map[string]interface{})
		if delta == nil {
			delta, _ = choice["message"].(map[string]interface{})
		}
		if c, ok := delta["content"].(string); ok {
			content.WriteString(c)
		}
		if r, ok := delta["reasoning_content"].(string); ok {
			reasoning.WriteString(r)
		}
		if tcs, ok := delta["tool_calls"].([]interface{}); ok {
			for _, raw := range tcs {
				tc, _ := raw.(map[string]interface{})
				idx := intOf(tc["index"])
				for len(toolCalls) <= idx {
					toolCalls = append(toolCalls, map[string]interface{}{"id": "", "type": "function", "function": map[string]interface{}{"name": "", "arguments": ""}})
				}
				entry := toolCalls[idx]
				if callID, ok := tc["id"].(string); ok && callID != "" {
					entry["id"] = callID
				}
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					ef, _ := entry["function"].(map[string]interface{})
					if n, ok := fn["name"].(string); ok && n != "" {
						cur, _ := ef["name"].(string)
						ef["name"] = cur + n
					}
					if a, ok := fn["arguments"].(string); ok {
						cur, _ := ef["arguments"].(string)
						ef["arguments"] = cur + a
					}
				}
				toolCalls[idx] = entry
			}
		}
	})

	var message map[string]interface{}
	if len(toolCalls) > 0 {
		message = map[string]interface{}{"role": "assistant", "content": content.String(), "tool_calls": toolCalls}
	} else {
		message = map[string]interface{}{"role": "assistant", "content": content.String()}
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	out := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{{"index": 0, "message": message, "finish_reason": finishReason}},
	}
	if usage != nil && len(usage) > 0 {
		out["usage"] = usage
	}
	return out
}

// forEachSSEData 逐条解析 SSE 的 data: 载荷并回调（解析失败静默跳过）。
func forEachSSEData(sseText string, fn func(map[string]interface{})) {
	for _, line := range strings.Split(sseText, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "data:") {
			continue
		}
		payload := strings.TrimSpace(t[len("data:"):])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]interface{}
		if json.Unmarshal([]byte(payload), &obj) == nil {
			fn(obj)
		}
	}
}
