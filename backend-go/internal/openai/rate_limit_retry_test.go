package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// createRelayEndpoint 创建一个可转发的端点（skipVerify 建端点后直接写入模型），
// 返回 service 与端点 ID。extra 可注入附加 JSON 字段（如开启 429 等待重试）。
func createRelayEndpoint(t *testing.T, upstream string, extra map[string]interface{}) (*Service, string) {
	t.Helper()
	service := New(configForTest(t))
	fields := []string{
		fmt.Sprintf(`"name":"RelayEndpoint"`),
		fmt.Sprintf(`"baseUrl":"%s"`, upstream),
		`"apiKey":"k"`,
		`"skipVerify":true`,
		`"autoSwitch":true`,
	}
	for k, v := range extra {
		fields = append(fields, fmt.Sprintf(`%q:%v`, k, v))
	}
	createBody := "{" + strings.Join(fields, ",") + "}"
	wC := httptest.NewRecorder()
	rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wC, rC)
	if wC.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wC.Code, wC.Body.String())
	}
	var created struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wC.Body.String(), &created)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, created.Endpoint.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()
	return service, created.Endpoint.ID
}

// configForTest 返回测试用最小 config（等价现有 setupTwoProxyEndpoint 的 New 参数）。
func configForTest(t *testing.T) config.Config {
	return config.Config{DataDir: t.TempDir(), DBName: "data.db"}
}

// requestChat 发起一次 /v1/chat/completions 请求。
func requestChat(t *testing.T, service *Service) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(w, r)
	return w
}

// TestRateLimitRetryRecoversAfterWait 开启 rateLimitRetryEnabled 后，端点先 429、
// 等待 Retry-After/配置秒数后再试成功：请求最终 200，且上游被调用两次。
func TestRateLimitRetryRecoversAfterWait(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls
		mu.Unlock()
		if first == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	// 缩短等待预算与缺省等待，让测试秒级完成。
	oldBudget := rateLimitRetryBudget
	defer func() { rateLimitRetryBudget = oldBudget }()
	rateLimitRetryBudget = 10 * time.Second

	service, _ := createRelayEndpoint(t, upstream.URL, map[string]interface{}{
		"rateLimitRetryEnabled":        true,
		"rateLimitRetryWaitSeconds":    1,
	})

	wChat := requestChat(t, service)
	if wChat.Code != http.StatusOK {
		t.Fatalf("expected eventual 200 after wait-retry, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("expected exactly 2 upstream calls (429 then retry-200), got %d", gotCalls)
	}
}

// TestRateLimitRetryStill429WhenBudgetExhausted 端点持续 429 时，等待重试最多消耗
// 预算（不无限循环），预算耗尽后仍以 429 收尾。
func TestRateLimitRetryStill429WhenBudgetExhausted(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
	}))
	defer upstream.Close()

	// 预算 1s、缺省等待 1s：第一轮 429 后等待 1s、第二轮 429 后发现预算耗尽停止。
	oldBudget := rateLimitRetryBudget
	defer func() { rateLimitRetryBudget = oldBudget }()
	rateLimitRetryBudget = time.Second

	service, _ := createRelayEndpoint(t, upstream.URL, map[string]interface{}{
		"rateLimitRetryEnabled":        true,
		"rateLimitRetryWaitSeconds":    1,
	})

	wChat := requestChat(t, service)
	if wChat.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after budget exhausted, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	// 初始 1 次 + 预算内等待后的 1 次重试 = 2；预算耗尽后不再重试。
	if gotCalls != 2 {
		t.Fatalf("expected 2 upstream calls before budget exhaustion, got %d", gotCalls)
	}
}

// TestRateLimitRetryDisabledKeepsFast429 关闭 rateLimitRetryEnabled 时保持原有行为：
// 全 429 一轮即快速收尾（不等待、不重试）。
func TestRateLimitRetryDisabledKeepsFast429(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
	}))
	defer upstream.Close()

	service, _ := createRelayEndpoint(t, upstream.URL, map[string]interface{}{
		"rateLimitRetryEnabled": false,
	})

	wChat := requestChat(t, service)
	if wChat.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 passthrough, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("disabled wait-retry should make exactly 1 upstream call, got %d", gotCalls)
	}
}

// TestRateLimitRetryPreferRetryAfter 端点响应带 Retry-After 头时，等待采用该值而非
// 端点配置秒数（通过 Retry-After=1s vs 端点配置 60s 验证优先使用响应头）。
func TestRateLimitRetryPreferRetryAfter(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls
		mu.Unlock()
		if first == 1 {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	// 端点配置等待 60s，但 Retry-After=1s 应被优先采用，请求秒级恢复。
	oldBudget := rateLimitRetryBudget
	defer func() { rateLimitRetryBudget = oldBudget }()
	rateLimitRetryBudget = 30 * time.Second

	service, _ := createRelayEndpoint(t, upstream.URL, map[string]interface{}{
		"rateLimitRetryEnabled":     true,
		"rateLimitRetryWaitSeconds": 60,
	})

	start := time.Now()
	wChat := requestChat(t, service)
	elapsed := time.Since(start)
	if wChat.Code != http.StatusOK {
		t.Fatalf("expected 200 via Retry-After wait, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Retry-After=1s should override 60s endpoint config, took %s", elapsed)
	}
}

// TestRateLimitRetryDefaultOn 端点未显式设置 rateLimitRetryEnabled 时默认开启，
// 429 会触发等待重试（与 TestRateLimitRetryRecoversAfterWait 相同的场景）。
func TestRateLimitRetryDefaultOn(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls
		mu.Unlock()
		if first == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	oldBudget := rateLimitRetryBudget
	defer func() { rateLimitRetryBudget = oldBudget }()
	rateLimitRetryBudget = 2 * time.Second

	service, _ := createRelayEndpoint(t, upstream.URL, nil)

	wChat := requestChat(t, service)
	if wChat.Code != http.StatusOK {
		t.Fatalf("default-on wait-retry should recover to 200, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("expected 2 upstream calls with default-on retry, got %d", gotCalls)
	}
}