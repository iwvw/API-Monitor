package openaibeta

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/config"
	enginetransform "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/transform"
	enginevertex "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/vertex"
)

// ServeHTTP 是 Beta 插件的总入口，按路径前缀分派：
//   - /v1/chat/completions、/v1/models：OpenAI 兼容中继（需已启用）
//   - 其余：管理接口（settings / models / test）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/v1/chat/completions"):
		s.handleChatCompletions(w, r)
	case strings.HasSuffix(path, "/v1/models"):
		s.handleModels(w, r)
	case path == "/api/openaibeta/settings":
		s.handleSettings(w, r)
	case path == "/api/openaibeta/models":
		s.handleModelsAdmin(w, r)
	case path == "/api/openaibeta/test":
		s.handleTest(w, r)
	case path == "/api/openaibeta/status":
		s.handleStatus(w, r)
	case path == "/api/openaibeta/link":
		s.handleLink(w, r)
	default:
		response.Error(w, http.StatusNotFound, "openaibeta route not found")
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"enabled": st.Enabled,
		"proxyEndpointId": st.ProxyEndpointID,
		"modelCount": len(st.Models),
		"enabledModels": len(engineconfig.BaseModels()),
	})
}

func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": s.Settings()})
	case http.MethodPut, http.MethodPost:
		var body Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			response.Error(w, http.StatusBadRequest, "请求体解析失败")
			return
		}
		if err := s.SaveSettings(r.Context(), body); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": s.Settings()})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) handleModelsAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Models   []engineconfig.ModelEntry `json:"models"`
		AliasMap map[string]string         `json:"aliasMap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	st := s.Settings()
	if body.Models != nil {
		st.Models = body.Models
	}
	if body.AliasMap != nil {
		st.AliasMap = body.AliasMap
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleChatCompletions 实现 OpenAI 兼容 /v1/chat/completions（流式 + 非流式）。
func (s *Service) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.Settings().Enabled {
		oaiError(w, http.StatusNotFound, "Beta 插件未启用 (beta plugin disabled)", "invalid_request_error")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		oaiError(w, http.StatusBadRequest, "请求格式错误，JSON 解析失败 (invalid JSON)", "invalid_request_error")
		return
	}
	if body == nil {
		body = make(map[string]any)
	}

	rawModel, _ := body["model"].(string)
	if strings.TrimSpace(rawModel) == "" {
		oaiError(w, http.StatusBadRequest, "缺少必需字段 model (missing required field 'model')", "invalid_request_error")
		return
	}
	actualModel, modelOK := resolveConfiguredModel(rawModel, settingsProvider{s: s})
	if !modelOK {
		oaiModelNotFound(w, rawModel)
		return
	}
	body["model"] = actualModel

	stream, _ := body["stream"].(bool)
	aggregateStream := stream && s.Settings().AggregateStream

	model, geminiPayload, convErr := enginetransform.DefaultRequestConverter().Convert(body, settingsProvider{s: s})
	if convErr != nil {
		oaiError(w, http.StatusBadRequest, "请求参数有误: "+convErr.Error(), "invalid_request_error")
		return
	}
	n, nErr := resolveN(body["n"], s.Settings().MaxN)
	if nErr != "" {
		oaiError(w, http.StatusBadRequest, nErr, "invalid_request_error")
		return
	}
	if stream && n > 1 {
		oaiError(w, http.StatusBadRequest, "流式不支持 n>1", "invalid_request_error")
		return
	}

	enginetransform.ApplyImageConfig(geminiPayload, body, actualModel)
	enginetransform.ApplyImageDefaults(geminiPayload, actualModel, "1K", "图文")

	if aggregateStream {
		s.handleAggregateStream(w, r, model, geminiPayload)
		return
	}
	if stream {
		s.handleStreamChat(w, r, model, geminiPayload)
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	defer db.Close()

	proxyURI, _ := s.proxyForRequest(r.Context(), db)
	if n > 1 {
		responses, vErr := s.completeChatN(r.Context(), db, model, geminiPayload, n, proxyURI)
		if vErr != nil {
			s.writeVertexError(w, vErr, model)
			return
		}
		response.JSON(w, http.StatusOK, enginetransform.DefaultResponseConverter().AggregateN(responses, model))
		return
	}
	geminiResp, vErr := s.completeChat(r.Context(), db, model, geminiPayload, proxyURI)
	if vErr != nil {
		s.writeVertexError(w, vErr, model)
		return
	}
	response.JSON(w, http.StatusOK, enginetransform.DefaultResponseConverter().ToOAI(geminiResp, model))
}

// handleModels 返回 OAI 模型列表（只含启用的模型）。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if !s.Settings().Enabled {
		oaiError(w, http.StatusNotFound, "Beta 插件未启用 (beta plugin disabled)", "invalid_request_error")
		return
	}
	now := time.Now().Unix()
	models := engineconfig.BaseModels()
	data := make([]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{
			"id": m, "object": "model", "created": now, "owned_by": "google", "permission": []any{},
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	model := engineconfig.BaseModels()
	if len(model) == 0 {
		response.Error(w, http.StatusBadRequest, "无可用模型")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	proxyURI, _ := s.proxyForRequest(r.Context(), db)
	payload := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "ping"}}}},
	}
	start := time.Now()
	resp, vErr := s.completeChat(r.Context(), db, model[0], payload, proxyURI)
	ms := time.Since(start).Milliseconds()
	if vErr != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "model": model[0], "latencyMs": ms,
			"error": vErr.Error(), "proxy": proxyURI,
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "model": model[0], "latencyMs": ms, "proxy": proxyURI,
		"text": extractText(resp),
	})
}

// proxyForRequest 从当前设置解析出口代理；空表示直连。
func (s *Service) proxyForRequest(ctx context.Context, db *sql.DB) (string, error) {
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c == nil {
		return "", nil
	}
	st := s.Settings()
	return c.pickProxy(ctx, db, st.ProxyEndpointID, st.ManualProxies)
}

// writeVertexError 把引擎错误映射为 OpenAI 错误响应；安全拦截返回 content_filter。
func (s *Service) writeVertexError(w http.ResponseWriter, vErr error, model string) {
	ve := toVertexError(vErr)
	if isSafetyBlock(ve) {
		response.JSON(w, http.StatusOK, oaiSafetyResponse(model))
		return
	}
	oaiError(w, ve.Code, friendlyErrorMessage(ve), errorTypeFor(ve))
}

// handleStreamChat 流式转发：SSE 输出。
func (s *Service) handleStreamChat(w http.ResponseWriter, r *http.Request, model string, payload map[string]any) {
	db, err := s.open(r.Context())
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	defer db.Close()
	proxyURI, _ := s.proxyForRequest(r.Context(), db)
	requestID := reqID24()
	sw := newSSEWriter(w, "text/event-stream")
	isFirst := true
	hasFinish := false
	gotContent := false
	streamErrWritten := false
	tracker := enginetransform.NewStreamToolCallTracker()
	respConv := enginetransform.DefaultResponseConverter()

	s.streamChat(r.Context(), db, model, payload, proxyURI, func(ch enginevertex.StreamChunk) bool {
		if ch.Err != nil {
			if !sw.hasWritten() {
				s.writeVertexError(w, ch.Err, model)
			} else {
				ve := toVertexError(ch.Err)
				base := streamChunkBase(model, requestID)
				if isSafetyBlock(ve) {
					base["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "content_filter"}}
				} else {
					_ = sw.write(sseEvent(oaiErrorPayload(ve)))
				}
				_ = sw.write("data: [DONE]\n\n")
			}
			streamErrWritten = true
			return false
		}
		events := respConv.StreamToSSE(ch.Data, model, requestID, isFirst, tracker)
		isFirst = false
		for _, ev := range events {
			if strings.Contains(ev, `"finish_reason"`) && !strings.Contains(ev, `"finish_reason":null`) {
				hasFinish = true
			}
			if strings.Contains(ev, `"content":`) || strings.Contains(ev, `"tool_calls":`) || strings.Contains(ev, `"reasoning_content":`) {
				gotContent = true
			}
			if !sw.write(ev) {
				return false
			}
		}
		return true
	})

	if streamErrWritten {
		return
	}
	if !gotContent {
		ve := enginevertex.NewEmptyResponseError("Upstream returned empty response (no content)")
		if !sw.hasWritten() {
			oaiError(w, ve.Code, friendlyErrorMessage(ve), errorTypeFor(ve))
		} else {
			_ = sw.write(sseEvent(oaiErrorPayload(ve)))
		}
		return
	}
	if !hasFinish {
		base := streamChunkBase(model, requestID)
		base["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "length"}}
		_ = sw.write(sseEvent(base))
	}
	_ = sw.write("data: [DONE]\n\n")
}

// handleAggregateStream 聚合流式：等完整响应后按流式协议吐回。
func (s *Service) handleAggregateStream(w http.ResponseWriter, r *http.Request, model string, payload map[string]any) {
	db, err := s.open(r.Context())
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	defer db.Close()
	proxyURI, _ := s.proxyForRequest(r.Context(), db)
	requestID := reqID24()
	sw := newSSEWriter(w, "text/event-stream")
	resp, vErr := s.completeChat(r.Context(), db, model, payload, proxyURI)
	if vErr != nil {
		ve := toVertexError(vErr)
		if isSafetyBlock(ve) {
			base := streamChunkBase(model, requestID)
			base["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "content_filter"}}
			_ = sw.write(sseEvent(base))
		} else {
			_ = sw.write(sseEvent(oaiErrorPayload(ve)))
		}
		_ = sw.write("data: [DONE]\n\n")
		return
	}
	oai := enginetransform.DefaultResponseConverter().ToOAI(resp, model)
	_ = sw.write(sseEvent(oai))
	_ = sw.write("data: [DONE]\n\n")
}

// extractText 从非流式 Gemini 响应中抽取首段文本。
func extractText(resp map[string]any) string {
	cands, _ := resp["candidates"].([]any)
	for _, c := range cands {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		content, _ := cm["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := pm["text"].(string); t != "" {
				return t
			}
		}
	}
	return ""
}

// resolveConfiguredModel 别名 + 启用校验（对齐上游语义）。
func resolveConfiguredModel(rawModel string, cfg engineconfig.ConfigProvider) (string, bool) {
	actualModel := cfg.ResolveModelName(strings.TrimSpace(rawModel))
	entry, exists := cfg.LookupModel(actualModel)
	if !exists || !entry.Enabled {
		return actualModel, false
	}
	return actualModel, true
}

func resolveN(raw any, maxN int) (int, string) {
	if maxN <= 0 {
		maxN = 8
	}
	if raw == nil {
		return 1, ""
	}
	var n int
	switch v := raw.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, "n 必须是整数"
		}
		n = int(v)
	case int:
		n = v
	default:
		return 0, "n 必须是整数"
	}
	if n < 1 {
		return 0, "n 必须 >= 1"
	}
	if n > maxN {
		return 0, "n 超过上限 " + strconv.Itoa(maxN)
	}
	return n, ""
}

func oaiErrorPayload(e *enginevertex.VertexError) map[string]any {
	return map[string]any{"error": map[string]any{
		"message": friendlyErrorMessage(e), "type": errorTypeFor(e), "code": e.Code,
	}}
}

func friendlyErrorMessage(e *enginevertex.VertexError) string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.UpstreamResponse)
	}
	if msg == "" {
		msg = e.Status
	}
	if msg == "" {
		msg = "upstream error"
	}
	return msg
}

func errorTypeFor(e *enginevertex.VertexError) string {
	switch e.Kind {
	case "invalid":
		return "invalid_request_error"
	case "ratelimit":
		return "rate_limit_error"
	case "auth":
		return "server_error"
	case "notfound", "permission":
		return "invalid_request_error"
	default:
		return "server_error"
	}
}

func oaiSafetyResponse(model string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-" + reqID24(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index": 0, "message": map[string]any{"role": "assistant", "content": nil}, "finish_reason": "content_filter",
		}},
	}
}

func oaiError(w http.ResponseWriter, status int, msg, errType string) {
	response.JSON(w, status, map[string]any{"error": map[string]any{
		"message": msg, "type": errType, "code": status,
	}})
}

func oaiModelNotFound(w http.ResponseWriter, model string) {
	oaiError(w, http.StatusNotFound, "Model '"+model+"' not found.", "invalid_request_error")
}

func toVertexError(err error) *enginevertex.VertexError {
	if ve, ok := err.(*enginevertex.VertexError); ok {
		return ve
	}
	return enginevertex.NewInternalError(err.Error(), err)
}

func isSafetyBlock(e *enginevertex.VertexError) bool {
	if e == nil {
		return false
	}
	if e.Kind == "safety" {
		return true
	}
	msg := strings.ToLower(e.Message)
	status := strings.ToLower(e.Status)
	for _, k := range []string{"safety", "block_reason", "content_filter", "finish_reason_safety"} {
		if strings.Contains(msg, k) || strings.Contains(status, k) {
			return true
		}
	}
	return false
}

// ---- SSE 工具 ----

type sseWriter struct {
	w           http.ResponseWriter
	contentType string
	wroteHeader bool
	flush       func()
}

func newSSEWriter(w http.ResponseWriter, contentType string) *sseWriter {
	flusher, _ := w.(http.Flusher)
	sw := &sseWriter{w: w, contentType: contentType}
	if flusher != nil {
		sw.flush = flusher.Flush
	}
	return sw
}

func (sw *sseWriter) ensureHeader() {
	if sw.wroteHeader {
		return
	}
	sw.wroteHeader = true
	ct := sw.contentType
	if ct == "" {
		ct = "text/event-stream"
	}
	sw.w.Header().Set("Content-Type", ct)
	sw.w.Header().Set("Cache-Control", "no-cache")
	sw.w.Header().Set("Connection", "keep-alive")
	sw.w.Header().Set("X-Accel-Buffering", "no")
	sw.w.WriteHeader(http.StatusOK)
}

func (sw *sseWriter) hasWritten() bool { return sw.wroteHeader }

func (sw *sseWriter) write(line string) bool {
	sw.ensureHeader()
	if _, err := sw.w.Write([]byte(line)); err != nil {
		return false
	}
	if sw.flush != nil {
		sw.flush()
	}
	return true
}

func sseEvent(obj map[string]any) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "data: {}\n\n"
	}
	return "data: " + string(data) + "\n\n"
}

func streamChunkBase(model, requestID string) map[string]any {
	return map[string]any{
		"id": "chatcmpl-" + requestID, "object": "chat.completion.chunk",
		"created": time.Now().Unix(), "model": model,
	}
}

var reqCounter atomic.Uint64

func reqID24() string {
	b := make([]byte, 12)
	if _, err := cryptorand.Read(b[:]); err != nil {
		return strconv.FormatUint(uint64(time.Now().UnixNano()), 16) + strconv.FormatUint(reqCounter.Add(1), 16)
	}
	return hex.EncodeToString(b[:])
}

// completeChatN 并行 n 路（复用引擎 via 方法，无竞速池）。
func (s *Service) completeChatN(ctx context.Context, db *sql.DB, model string, payload map[string]any, n int, proxyURI string) ([]map[string]any, error) {
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c == nil {
		return nil, enginevertex.NewInternalError("Beta 插件客户端未初始化")
	}
	return c.vc.CompleteChatNViaProxy(ctx, model, payload, n, proxyURI)
}
