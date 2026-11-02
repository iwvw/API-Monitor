package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestGeminiUpstreamNonStream 验证 Gemini 上游完整转发链路（非流式）：
// OpenAI chat 请求 → /v1beta/interactions（x-goog-api-key + snake_case 请求体）
// → Gemini 响应 → OpenAI chat.completions 格式返回。
func TestGeminiUpstreamNonStream(t *testing.T) {
	var hitInteractions atomic.Int32
	var gotAPIKey, gotBody string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1beta/models":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[
				{"name":"models/gemini-3.7-flash","supportedGenerationMethods":["generateContent","countTokens"]},
				{"name":"models/gemini-embedding-2","supportedGenerationMethods":["embedContent","countTokens"]}
			]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1beta/interactions":
			hitInteractions.Add(1)
			gotAPIKey = r.Header.Get("X-Goog-Api-Key")
			bodyBytes := make([]byte, 4096)
			n, _ := r.Body.Read(bodyBytes)
			gotBody = string(bodyBytes[:n])
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id":"v1_itest","status":"completed","model":"gemini-3.7-flash",
				"created":"2026-09-02T10:00:00Z",
				"usage":{"total_tokens":50,"total_input_tokens":20,"total_output_tokens":30,"total_cached_tokens":5},
				"steps":[
					{"type":"thought","signature":"s1"},
					{"type":"model_output","content":[{"type":"text","text":"Hello from Gemini"}]}
				],
				"object":"interaction"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":"unexpected %s %s"}}`, r.Method, r.URL.Path)))
		}
	}))
	defer mock.Close()

	service := newOpenAIService(t)

	// 创建 Gemini 端点（自动验证 + 拉模型）。
	createPayload := fmt.Sprintf(`{
		"name": "Gemini Upstream",
		"baseUrl": "%s",
		"apiKey": "GEMINI_TEST_KEY",
		"upstreamType": "gemini"
	}`, mock.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if !createRes.Success || createRes.Endpoint.UpstreamType != "gemini" {
		t.Fatalf("create res = %#v", createRes)
	}
	if len(createRes.Endpoint.Models) != 1 || createRes.Endpoint.Models[0] != "gemini-3.7-flash" {
		t.Fatalf("gemini models not parsed (embedding should be filtered): %#v", createRes.Endpoint.Models)
	}

	// 非流式 chat。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gemini-3.7-flash",
		"messages": [
			{"role":"system","content":"Be terse"},
			{"role":"user","content":"hello"}
		],
		"temperature": 0.5
	}`))
	rChat.Header.Set("x-endpoint-id", createRes.Endpoint.ID)
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("chat status = %d body=%s", wChat.Code, wChat.Body.String())
	}
	if hitInteractions.Load() != 1 {
		t.Fatalf("interactions hit = %d; want 1", hitInteractions.Load())
	}
	if gotAPIKey != "GEMINI_TEST_KEY" {
		t.Errorf("x-goog-api-key = %q", gotAPIKey)
	}
	// 断言上游收到 Gemini 格式请求体（snake_case + input step_list + system_instruction）。
	if !strings.Contains(gotBody, `"input"`) || !strings.Contains(gotBody, `"system_instruction"`) || !strings.Contains(gotBody, `"generation_config"`) {
		t.Errorf("gemini request body missing fields: %s", gotBody)
	}
	if strings.Contains(gotBody, `"messages"`) {
		t.Errorf("request body should be Gemini format, got OpenAI messages: %s", gotBody)
	}

	// 断言响应转回 OpenAI 格式。
	var oai struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(wChat.Body.Bytes(), &oai); err != nil {
		t.Fatalf("decode response: %v; raw=%s", err, wChat.Body.String())
	}
	if oai.Object != "chat.completion" {
		t.Errorf("object = %q", oai.Object)
	}
	if len(oai.Choices) != 1 || oai.Choices[0].Message.Content != "Hello from Gemini" {
		t.Errorf("choices = %+v", oai.Choices)
	}
	if oai.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q", oai.Choices[0].FinishReason)
	}
	if oai.Usage.PromptTokens != 20 || oai.Usage.CompletionTokens != 30 || oai.Usage.TotalTokens != 50 {
		t.Errorf("usage = %+v", oai.Usage)
	}
}

// TestGeminiUpstreamStream 验证 Gemini 上游流式链路：Interactions SSE →
// OpenAI chat.completions chunk（含 usage 收尾与 [DONE]）。
func TestGeminiUpstreamStream(t *testing.T) {
	var hitInteractions atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1beta/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[
				{"name":"models/gemini-3.6-flash","supportedGenerationMethods":["generateContent","countTokens"]},
				{"name":"models/gemini-3.7-flash","supportedGenerationMethods":["generateContent","countTokens"]}
			]}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1beta/interactions" {
			hitInteractions.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`event: interaction.created
data: {"interaction":{"id":"v1_abc","status":"in_progress","model":"gemini-3.6-flash"},"event_type":"interaction.created"}

event: interaction.status_update
data: {"interaction_id":"v1_abc","status":"in_progress","event_type":"interaction.status_update"}

event: step.start
data: {"index":0,"step":{"type":"model_output"},"event_type":"step.start"}

event: step.delta
data: {"index":0,"delta":{"type":"text","text":"Hello"},"event_type":"step.delta"}

event: step.delta
data: {"index":0,"delta":{"type":"text","text":" world"},"event_type":"step.delta"}

event: interaction.completed
data: {"interaction":{"id":"v1_abc","status":"incomplete","usage":{"total_tokens":30,"total_input_tokens":12,"total_output_tokens":18},"model":"gemini-3.6-flash"},"event_type":"interaction.completed"}

event: done
data: [DONE]

`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mock.Close()

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{"name":"G","baseUrl":"%s","apiKey":"K","upstreamType":"gemini"}`, mock.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model":"gemini-3.6-flash",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`))
	rChat.Header.Set("x-endpoint-id", createRes.Endpoint.ID)
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("stream status = %d body=%s", wChat.Code, wChat.Body.String())
	}
	body := wChat.Body.String()
	if hitInteractions.Load() != 1 {
		t.Fatalf("interactions hit = %d", hitInteractions.Load())
	}
	// 文本增量 + usage + [DONE]。
	if !strings.Contains(body, `"content":"Hello"`) || !strings.Contains(body, `"content":" world"`) {
		t.Errorf("missing text deltas: %s", body)
	}
	if !strings.Contains(body, `"prompt_tokens":12`) || !strings.Contains(body, `"completion_tokens":18`) {
		t.Errorf("missing usage: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", body)
	}
}

// TestGeminiCreateEndpointNormalizeBaseURL 验证 Gemini 端点 baseURL 不追加 /v1。
func TestGeminiCreateEndpointNormalizeBaseURL(t *testing.T) {
	service := newOpenAIService(t)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(`{
		"name":"G2","baseUrl":"https://generativelanguage.googleapis.com","apiKey":"K",
		"upstreamType":"gemini","skipVerify":true
	}`))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if createRes.Endpoint.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("gemini baseURL normalized wrong: %q", createRes.Endpoint.BaseURL)
	}
	if got := geminiInteractionsURL(createRes.Endpoint.BaseURL); got != "https://generativelanguage.googleapis.com/v1beta/interactions" {
		t.Errorf("interactions url = %q", got)
	}
}

// TestGeminiUpstreamHealthCheck 验证 Gemini 端点的健康检测走 Interactions API：
// x-goog-api-key + /v1beta/interactions + 转换后的请求体，2xx 判定 operational。
func TestGeminiUpstreamHealthCheck(t *testing.T) {
	var hitInteractions atomic.Int32
	var gotAPIKey, gotPath string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1beta/interactions" {
			hitInteractions.Add(1)
			gotAPIKey = r.Header.Get("X-Goog-Api-Key")
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"v1_h","status":"completed","model":"gemini-3.7-flash","steps":[{"type":"model_output","content":[{"type":"text","text":"ok"}]}],"usage":{"total_tokens":5,"total_input_tokens":2,"total_output_tokens":3}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mock.Close()

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{"name":"GHC","baseUrl":"%s","apiKey":"K","upstreamType":"gemini"}`, mock.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	record := service.healthCheckSingleModel(context.Background(), createRes.Endpoint.ID, createRes.Endpoint.BaseURL, "K", "gemini-3.7-flash", 10*time.Second, nil, "gemini")
	if record.Status != "operational" {
		t.Fatalf("health status = %q err=%q; want operational", record.Status, record.Error)
	}
	if hitInteractions.Load() != 1 {
		t.Fatalf("interactions hit = %d; want 1", hitInteractions.Load())
	}
	if gotPath != "/v1beta/interactions" {
		t.Errorf("health check hit wrong path: %q", gotPath)
	}
	if gotAPIKey != "K" {
		t.Errorf("x-goog-api-key = %q", gotAPIKey)
	}
}
