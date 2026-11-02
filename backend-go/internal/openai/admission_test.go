package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func createAdmissionEndpoint(t *testing.T, service *Service, name, rawURL, model string) string {
	t.Helper()
	payload := fmt.Sprintf(`{"name":%q,"baseUrl":"%s","apiKey":"k","skipVerify":true}`, name, rawURL)
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(payload))
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create endpoint %s status=%d body=%s", name, rec.Code, rec.Body.String())
	}
	var res struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, rec.Body.String(), &res)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, fmt.Sprintf(`[%q]`, model), res.Endpoint.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()
	return res.Endpoint.ID
}

func createAdmissionGatewayKey(t *testing.T, service *Service, payload string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/openai/keys", strings.NewReader(payload))
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create gateway key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	mustDecode(t, rec.Body.String(), &res)
	if res.Key.ID == "" {
		t.Fatalf("gateway key id missing: %s", rec.Body.String())
	}
	return res.Key.ID
}

func admissionKeyTokensUsed(t *testing.T, service *Service, keyID string) int64 {
	t.Helper()
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var used int64
	if err := db.QueryRow(`SELECT COALESCE(total_tokens_used,0) FROM openai_gateway_keys WHERE id = ?`, keyID).Scan(&used); err != nil {
		t.Fatalf("read gateway key tokens: %v", err)
	}
	return used
}

func admissionRequestWithIdentity(service *Service, path, body string, identity gatewayKeyIdentity) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), gatewayKeyContextKey{}, identity))
	service.ServeHTTP(w, r)
	return w
}

// TestMessagesTokenQuotaSettled /v1/messages 必须结算网关密钥 token 配额。
// 回归：该入口此前从不调用 consumeGatewayKeyTokens，带配额的密钥可无限使用。
func TestMessagesTokenQuotaSettled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	createAdmissionEndpoint(t, service, "Quota EP", upstream.URL, "gpt-4")
	keyID := createAdmissionGatewayKey(t, service, `{"name":"quota-key","maxTokensQuota":100,"allowedModels":["gpt-4"]}`)

	body := `{"model":"gpt-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	w := admissionRequestWithIdentity(service, "/v1/messages", body, gatewayKeyIdentity{ID: keyID, Name: "quota-key"})
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/messages should succeed, got %d body=%s", w.Code, w.Body.String())
	}
	if used := admissionKeyTokensUsed(t, service, keyID); used != 15 {
		t.Fatalf("expected 15 tokens settled via /v1/messages, got %d", used)
	}
}

// TestMessagesStreamTokenQuotaSettled 流式 /v1/messages 同样必须结算 token 配额。
func TestMessagesStreamTokenQuotaSettled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"id":"c1","choices":[{"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
			`{"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	createAdmissionEndpoint(t, service, "Stream Quota EP", upstream.URL, "gpt-4")
	keyID := createAdmissionGatewayKey(t, service, `{"name":"stream-quota-key","maxTokensQuota":100,"allowedModels":["gpt-4"]}`)

	body := `{"model":"gpt-4","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	w := admissionRequestWithIdentity(service, "/v1/messages", body, gatewayKeyIdentity{ID: keyID, Name: "stream-quota-key"})
	if w.Code != http.StatusOK {
		t.Fatalf("stream /v1/messages should succeed, got %d body=%s", w.Code, w.Body.String())
	}
	if used := admissionKeyTokensUsed(t, service, keyID); used != 10 {
		t.Fatalf("expected 10 tokens settled via stream /v1/messages, got %d", used)
	}
}

// TestAdmissionWhitelistRejectedAcrossEntrypoints 三个转发入口共享同一准入 seam：
// 端点白名单排除全部候选时，三者都必须 403 且不触达任何上游。
func TestAdmissionWhitelistRejectedAcrossEntrypoints(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		okHandler(w, r)
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	createAdmissionEndpoint(t, service, "Only EP", upstream.URL, "gpt-4")

	identity := gatewayKeyIdentity{ID: "gk-reject", Name: "k", AllowedEndpoints: []string{"oai_missing"}}
	cases := []struct {
		name string
		path string
		body string
	}{
		{"chat", "/v1/chat/completions", `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`},
		{"responses", "/v1/responses", `{"model":"gpt-4","input":"hi"}`},
		{"messages", "/v1/messages", `{"model":"gpt-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`},
	}
	for _, tc := range cases {
		w := admissionRequestWithIdentity(service, tc.path, tc.body, identity)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s should be 403 when whitelist excludes all candidates, got %d body=%s", tc.name, w.Code, w.Body.String())
		}
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("rejected requests must not reach upstream, hits=%d", hits)
	}
}

// TestAdmissionWhitelistAllowedResponses responses 入口此前无准入测试覆盖：
// 白名单包含候选端点时必须正常放行。
func TestAdmissionWhitelistAllowedResponses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		okHandler(w, r)
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	idA := createAdmissionEndpoint(t, service, "Allowed EP", upstream.URL, "gpt-4")

	identity := gatewayKeyIdentity{ID: "gk-allow", Name: "k", AllowedEndpoints: []string{idA}}
	w := admissionRequestWithIdentity(service, "/v1/responses", `{"model":"gpt-4","input":"hi"}`, identity)
	if w.Code != http.StatusOK {
		t.Fatalf("responses with whitelisted endpoint should succeed, got %d body=%s", w.Code, w.Body.String())
	}
}
