package modelhub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUpstreamAuthFailureRecorded 验证上游 401/403 会写入 relay-errors。
//
// 背景:该分支此前只做 key 轮询重试,既不写 analytics 也不写 relay-errors,
// 单 key 端点被上游 403 时会在 relay_chat 里被当作该端点最终响应直接透传,
// 结果在网关上完全无痕,无法定位是哪个端点/模型返回的。本测试锁定「补记观测」行为。
func TestUpstreamAuthFailureRecorded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	t.Cleanup(upstream.Close)

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{
		"name": "Forbidden EP",
		"baseUrl": "%s",
		"apiKey": "k1",
		"keyRetryRounds": 1,
		"skipVerify": true
	}`, upstream.URL)
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE modelhub_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// 单端点且唯一 key 全部 403:端点耗尽后按「所有端点均返回 HTTP 403」聚合写回
	// (JSON 错误体,不再是被关闭 body 导致的空 403 / 502)。
	w := chatRequest(t, service, createRes.Endpoint.ID, true, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("all-endpoints-403 should surface as 403, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "403") {
		t.Fatalf("expected aggregated 403 message, got body=%s", w.Body.String())
	}

	wRelay := httptest.NewRecorder()
	rRelay, _ := http.NewRequest("GET", "/api/modelhub/relay-errors?limit=50", nil)
	service.ServeHTTP(wRelay, rRelay)
	if wRelay.Code != http.StatusOK {
		t.Fatalf("relay-errors status = %d body=%s", wRelay.Code, wRelay.Body.String())
	}
	var relayResp struct {
		Records []RelayErrorRecord `json:"records"`
	}
	mustDecode(t, wRelay.Body.String(), &relayResp)

	var found *RelayErrorRecord
	for i := range relayResp.Records {
		rec := relayResp.Records[i]
		if rec.Kind == "upstream_auth" && rec.StatusCode == http.StatusForbidden {
			found = &relayResp.Records[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an upstream_auth relay error with 403, got records=%+v", relayResp.Records)
	}
	if found.Endpoint != "Forbidden EP" {
		t.Fatalf("expected endpoint name recorded, got %q", found.Endpoint)
	}
	if found.Model != "gpt-4" {
		t.Fatalf("expected model recorded, got %q", found.Model)
	}
}

// TestUpstream403FailsOverToNextEndpoint 验证端点级 403 会 failover 到下一个候选端点,
// 而不是把 403 直接透传给客户端(修复「opencode 偶发空 body 403」的根因)。
func TestUpstream403FailsOverToNextEndpoint(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	t.Cleanup(forbidden.Close)

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(ok.Close)

	service := newOpenAIService(t)

	// 端点 A:autoSwitch=false,返回 403(对齐 Token Harbor 配置)。
	createA := fmt.Sprintf(`{"name":"Forbidden A","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":false}`, forbidden.URL)
	wA := httptest.NewRecorder()
	rA, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createA))
	service.ServeHTTP(wA, rA)
	if wA.Code != http.StatusOK {
		t.Fatalf("create A status = %d body=%s", wA.Code, wA.Body.String())
	}
	var resA struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wA.Body.String(), &resA)

	// 端点 B:正常返回 SSE。
	createB := fmt.Sprintf(`{"name":"Good B","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, ok.URL)
	wB := httptest.NewRecorder()
	rB, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createB))
	service.ServeHTTP(wB, rB)
	if wB.Code != http.StatusOK {
		t.Fatalf("create B status = %d body=%s", wB.Code, wB.Body.String())
	}
	var resB struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wB.Body.String(), &resB)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{resA.Endpoint.ID, resB.Endpoint.ID} {
		if _, err := db.Exec(`UPDATE modelhub_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	// 确定性选路:首个候选固定为 403 端点 A;流式请求应 failover 到 B 并成功返回 SSE。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("expected failover to healthy endpoint on 403, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if !strings.Contains(wChat.Body.String(), "[DONE]") {
		t.Fatalf("expected SSE [DONE] from failover endpoint, got body=%s", wChat.Body.String())
	}
}
