package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestKeyExhausted401NotLeakedToClient 验证端点的 API Key 失效（上游 401/403）时，
// 401 响应绝不以「最终响应」透传回客户端：此前单 key 场景下 relayLoop 会把
// 最后一次 401 当作成功选中结果，流式请求将以 text/event-stream 头写出 HTTP 401
// 并补发 "data: [DONE]"，客户端只会看到 "Unauthorized: data: [DONE]" 畸形错误。
// 修复后应改为 key 耗尽语义：返回规范 JSON 错误（而非 SSE 流）。
func TestKeyExhausted401NotLeakedToClient(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`Unauthorized`))
	}))
	defer bad.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	createBody := fmt.Sprintf(`{"name":"BadKey","baseUrl":"%s","apiKey":"expired-key","skipVerify":true,"autoSwitch":false}`, bad.URL)
	wC := httptest.NewRecorder()
	rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wC, rC)
	if wC.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wC.Code, wC.Body.String())
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name = 'BadKey'`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	// 流式请求：必须返回规范 JSON 错误，绝不能是 SSE Content-Type + [DONE]。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusUnauthorized && wChat.Code != http.StatusServiceUnavailable {
		t.Fatalf("stream status = %d, want 401/503; body=%s", wChat.Code, wChat.Body.String())
	}
	if ct := wChat.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("stream content-type = %q, want application/json", ct)
	}
	if strings.Contains(wChat.Body.String(), "data: [DONE]") || strings.Contains(wChat.Body.String(), "text/event-stream") {
		t.Fatalf("stream body must not contain SSE artifacts, got=%s", wChat.Body.String())
	}

	// 非流式请求：同样是规范 JSON 错误。
	wChat2 := httptest.NewRecorder()
	rChat2, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat2, rChat2)
	if wChat2.Code != http.StatusUnauthorized && wChat2.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-stream status = %d, want 401/503; body=%s", wChat2.Code, wChat2.Body.String())
	}
}

// TestKeyExhaustedFailsOverToNextEndpoint 验证 key 失效端点不吞掉整个请求：
// 401 的候选端点应被标记不可用，failover 继续尝试下一个正常端点，客户端拿到 200。
func TestKeyExhaustedFailsOverToNextEndpoint(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`Unauthorized`))
	}))
	defer bad.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer ok.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	for _, ep := range []struct{ name, url string }{
		{"BadKeyFirst", bad.URL},
		{"GoodSecond", ok.URL},
	} {
		createBody := fmt.Sprintf(`{"name":"%s","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":false}`, ep.name, ep.url)
		wC := httptest.NewRecorder()
		rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
		service.ServeHTTP(wC, rC)
		if wC.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", ep.name, wC.Code, wC.Body.String())
		}
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name IN ('BadKeyFirst','GoodSecond')`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET sort_order = 0 WHERE name = 'BadKeyFirst'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET sort_order = 1 WHERE name = 'GoodSecond'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("failover status = %d, want 200; body=%s", wChat.Code, wChat.Body.String())
	}
	if !strings.Contains(wChat.Body.String(), "data: [DONE]") {
		t.Fatalf("failover body should be a normal SSE stream, got=%s", wChat.Body.String())
	}
}