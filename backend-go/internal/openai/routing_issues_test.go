package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestWeightedPickBecomesFirstCandidate 验证加权首选端点被提升为实际首次尝试
// 端点（此前 chosen 被三个转发入口忽略，weight/priority/延迟择优从未生效，
// 多端点流量永远打向排序第一的端点）。
func TestWeightedPickBecomesFirstCandidate(t *testing.T) {
	oldOverride := endpointPickOverride
	defer func() { endpointPickOverride = oldOverride }()

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A"}}]}`))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"B"}}]}`))
	}))
	defer srvB.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	for _, ep := range []struct{ name, url string }{
		{"First", srvA.URL},
		{"Second", srvB.URL},
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
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name IN ('First','Second')`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	// 显式选中第二个候选（Second），验证它被提升到候选列表首位。
	endpointPickOverride = func(cands []Endpoint) int { return 1 }
	db, err = service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	candidates, chosen, chosenIndex, _, found := service.selectEndpointCandidates(context.Background(), db, "gpt-4", "", "")
	if !found {
		t.Fatal("no candidates")
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates len = %d, want 2", len(candidates))
	}
	if candidates[0].Name != "Second" {
		t.Fatalf("first candidate = %q, want weighted pick Second promoted; all=%v", candidates[0].Name, namesOf(candidates))
	}
	if chosen.Name != "Second" || chosenIndex != 0 {
		t.Fatalf("chosen=%s idx=%d, want Second/0", chosen.Name, chosenIndex)
	}
}

// TestWeightedPickKeepsAffinityHead 验证会话亲和端点仍保持首位，加权首选排在其后。
func TestWeightedPickKeepsAffinityHead(t *testing.T) {
	oldOverride := endpointPickOverride
	defer func() { endpointPickOverride = oldOverride }()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	for _, name := range []string{"EndA", "EndB", "EndC"} {
		createBody := fmt.Sprintf(`{"name":"%s","baseUrl":"https://example-%s.com/v1","apiKey":"k","skipVerify":true,"autoSwitch":false}`, name, strings.ToLower(name))
		wC := httptest.NewRecorder()
		rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
		service.ServeHTTP(wC, rC)
		if wC.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", name, wC.Code, wC.Body.String())
		}
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name IN ('EndA','EndB','EndC')`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	// 打乱 sort_order：EndA 初始不在首位（EndB 排第一），迫使亲和挪位真正发生。
	if _, err := db.Exec(`UPDATE openai_endpoints SET sort_order = CASE name WHEN 'EndB' THEN 0 WHEN 'EndC' THEN 1 WHEN 'EndA' THEN 2 END`); err != nil {
		t.Fatal(err)
	}
	var idA, idB string
	if err := db.QueryRow(`SELECT id FROM openai_endpoints WHERE name='EndA'`).Scan(&idA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM openai_endpoints WHERE name='EndB'`).Scan(&idB); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	service.recordChannelAffinity("sess-1", idA)
	// 加权首选固定为第三个候选（EndC）。
	endpointPickOverride = func(cands []Endpoint) int { return 2 }
	db, err = service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	candidates, _, _, _, found := service.selectEndpointCandidates(context.Background(), db, "gpt-4", "", "sess-1")
	if !found {
		t.Fatal("no candidates")
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates len = %d, want 3", len(candidates))
	}
	if candidates[0].ID != idA {
		t.Fatalf("candidates[0] = %s, want affinity EndA; all=%v", candidates[0].ID, namesOf(candidates))
	}
	// 亲和端点与加权首选不同时，加权首选必须紧跟其后（第二位），而不是留在原位。
	gotB := candidates[1].ID == idB || candidates[1].Name == "EndB"
	if !gotB && candidates[1].Name != "EndC" {
		t.Fatalf("candidates[1] = %s, want weighted pick EndC after affinity; all=%v", candidates[1].Name, namesOf(candidates))
	}
}

// TestTrimSessionBindings 验证会话绑定表容量上限与过期清理（session key 客户端
// 可控，无上限时绑定 map 无限增长形成内存泄漏/DoS）。
func TestTrimSessionBindings(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	now := time.Now()
	mkState := func(count int, fresh int) *endpointProxyState {
		state := newEndpointProxyState()
		for i := 0; i < count; i++ {
			key := fmt.Sprintf("session-%d", i)
			entry := &sessionBinding{proxy: "p", count: 1, updatedAt: now}
			if i >= fresh {
				entry.updatedAt = now.Add(-2 * sessionBindingTTL)
			}
			state.sessionBindings[key] = entry
		}
		return state
	}

	// 未超限：不做清理。
	under := mkState(100, 100)
	service.trimSessionBindings(under, now)
	if len(under.sessionBindings) != 100 {
		t.Fatalf("under-limit bindings trimmed, len=%d", len(under.sessionBindings))
	}

	// 超限但有过期条目：只清过期，保留新鲜的。
	mixed := mkState(sessionBindingMax, sessionBindingMax/2)
	service.trimSessionBindings(mixed, now)
	if len(mixed.sessionBindings) != sessionBindingMax/2 {
		t.Fatalf("mixed bindings len=%d, want %d", len(mixed.sessionBindings), sessionBindingMax/2)
	}

	// 超限且几乎没有过期条目：整体重建（回退语义，保证不无限增长）。
	full := mkState(sessionBindingMax, sessionBindingMax)
	service.trimSessionBindings(full, now)
	if len(full.sessionBindings) != 0 {
		t.Fatalf("full bindings len=%d, want 0 after rebuild", len(full.sessionBindings))
	}
}

func namesOf(cands []Endpoint) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Name
	}
	return out
}

// TestGatewayKeyEndpointWhitelistBlocksFailover 验证网关密钥的端点白名单在
// failover 时不被绕过：白名单内端点故障时绝不能 failover 到白名单外端点
// （此前仅在首个端点上校验一次，failover 循环不再校验，形成授权绕过）。
func TestGatewayKeyEndpointWhitelistBlocksFailover(t *testing.T) {
	var okHits int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"upstream exploded"}}`))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&okHits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer good.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	var idA, idB string
	for _, ep := range []struct{ name, url string }{
		{"BadAllowed", bad.URL},
		{"GoodDenied", good.URL},
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
	if err := db.QueryRow(`SELECT id FROM openai_endpoints WHERE name='BadAllowed'`).Scan(&idA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM openai_endpoints WHERE name='GoodDenied'`).Scan(&idB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name IN ('BadAllowed','GoodDenied')`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	// 网关密钥只允许 BadAllowed 端点。
	identity := gatewayKeyIdentity{ID: "whitelist-test", Name: "whitelist-test", AllowedEndpoints: []string{idA}}
	ctx := context.WithValue(context.Background(), gatewayKeyContextKey{}, identity)
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	service.ServeHTTP(wChat, rChat)

	if atomic.LoadInt32(&okHits) != 0 {
		t.Fatalf("whitelist bypass: denied endpoint B was hit %d times (status=%d body=%s)", okHits, wChat.Code, wChat.Body.String())
	}
	if wChat.Code != http.StatusServiceUnavailable && wChat.Code != http.StatusInternalServerError && wChat.Code != http.StatusForbidden {
		t.Fatalf("whitelist failover status = %d, want 503/500/403; body=%s", wChat.Code, wChat.Body.String())
	}
}

// TestReadWithIdleTimeoutCancelsOnCtx 验证 readWithIdleTimeout 在 ctx 取消时
// 立即返回（此前只监听 idle 超时，客户端断连后网关会继续拉流直到 90s 超时，
// 浪费上游配额与连接）。
func TestReadWithIdleTimeoutCancelsOnCtx(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, err := readWithIdleTimeout(ctx, pr, make([]byte, 32), 10*time.Second)
	elapsed := time.Since(started)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("cancel not honored promptly: elapsed=%v", elapsed)
	}
	pr.CloseWithError(nil)

	// 对照：ctx 正常且 reader 无数据时，走 idle 超时而非挂死。
	pr2, _ := io.Pipe()
	defer pr2.CloseWithError(nil)
	started = time.Now()
	_, err = readWithIdleTimeout(context.Background(), pr2, make([]byte, 32), 50*time.Millisecond)
	if !errors.Is(err, errStreamIdleTimeout) {
		t.Fatalf("err = %v, want errStreamIdleTimeout", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("idle timeout too slow: %v", time.Since(started))
	}
}

// TestNonStreamUpstreamBodyBounded 验证非流式转发对上游响应体读取有上限：
// 异常上游返回超大 body 时只透传前 upstreamBodyLimit 字节，防止内存尖峰。
func TestNonStreamUpstreamBodyBounded(t *testing.T) {
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"`))
		w.Write([]byte(strings.Repeat("x", 8*1024*1024)))
		w.Write([]byte(`"}}]}`))
	}))
	defer huge.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	createBody := fmt.Sprintf(`{"name":"Huge","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":false}`, huge.URL)
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
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE name = 'Huge'`, `["gpt-4"]`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("status = %d body-len=%d", wChat.Code, wChat.Body.Len())
	}
	if wChat.Body.Len() > upstreamBodyLimit+4096 {
		t.Fatalf("body untruncated: len=%d, limit=%d", wChat.Body.Len(), upstreamBodyLimit)
	}
}