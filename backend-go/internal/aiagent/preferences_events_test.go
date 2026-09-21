package aiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseRecorder 是一个可并发读写的 SSE 响应记录器：httptest.Recorder 不是
// 并发安全的，而 SSE handler 在写入的同时测试方要读取已产出的帧。
type sseRecorder struct {
	mu     sync.Mutex
	header http.Header
	status int
	body   strings.Builder
	flush  chan struct{}
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{header: make(http.Header), flush: make(chan struct{}, 64)}
}

func (r *sseRecorder) Header() http.Header { return r.header }

func (r *sseRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = status
	}
}

func (r *sseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.body.Write(p)
	r.mu.Unlock()
	return len(p), nil
}

func (r *sseRecorder) Flush() {
	select {
	case r.flush <- struct{}{}:
	default:
	}
}

func (r *sseRecorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

// waitFor 等待条件成立，避免依赖固定 sleep 造成不稳定。
func (r *sseRecorder) waitFor(t *testing.T, needle string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body := r.snapshot()
		if strings.Contains(body, needle) {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	return r.snapshot()
}

func issueTokenFor(t *testing.T, service *Service, username string) string {
	t.Helper()
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	user, err := service.createUser(ctx, db, username, "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "test-device", "")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	return plain
}

// TestPreferenceEventsDeliversToSameUser 验证写入偏好后，同用户的订阅者
// 收到变更通知，且事件只带键名与时间戳（不带值）。
func TestPreferenceEventsDeliversToSameUser(t *testing.T) {
	service := newTestService(t)
	plain := issueTokenFor(t, service, "evt-alice")

	rec := newSSERecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences/events", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	req = req.WithContext(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.ServeHTTP(rec, req)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
		}
	})

	if body := rec.waitFor(t, "event: hello"); !strings.Contains(body, "connected") {
		t.Fatalf("SSE 未发出 hello 帧: %q", body)
	}

	// 通过 HTTP 写一次偏好，触发 publish。
	put := httptest.NewRequest(http.MethodPut, "/api/aiagent/preferences?lastWriteWins=1",
		strings.NewReader(`{"values":{"theme-preset":"\"ocean\""},"updatedAt":"2030-01-01T00:00:00.000Z"}`))
	put.Header.Set("Content-Type", "application/json")
	put.Header.Set("Authorization", "Bearer "+plain)
	res := httptest.NewRecorder()
	service.ServeHTTP(res, put)
	if res.Code != http.StatusOK {
		t.Fatalf("put preferences status=%d body=%s", res.Code, res.Body.String())
	}

	body := rec.waitFor(t, "event: preferences")
	if !strings.Contains(body, "theme-preset") {
		t.Fatalf("变更事件未包含写入的键: %q", body)
	}
	if strings.Contains(body, "ocean") {
		t.Fatalf("事件不应携带偏好值（避免大流量与隐私面）: %q", body)
	}
}

// TestPreferenceEventsIsolatedPerUser 验证偏好事件按用户隔离：另一用户的
// 订阅者不应收到事件（键名含实例 ID 与路径，跨用户泄露属隐私问题）。
func TestPreferenceEventsIsolatedPerUser(t *testing.T) {
	service := newTestService(t)
	alice := issueTokenFor(t, service, "evt-owner")
	bob := issueTokenFor(t, service, "evt-other")

	rec := newSSERecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences/events", nil)
	req.Header.Set("Authorization", "Bearer "+bob)
	go func() { service.ServeHTTP(rec, req) }()
	rec.waitFor(t, "event: hello")

	put := httptest.NewRequest(http.MethodPut, "/api/aiagent/preferences?lastWriteWins=1",
		strings.NewReader(`{"values":{"font-scale":"1.1"},"updatedAt":"2030-01-01T00:00:00.000Z"}`))
	put.Header.Set("Content-Type", "application/json")
	put.Header.Set("Authorization", "Bearer "+alice)
	res := httptest.NewRecorder()
	service.ServeHTTP(res, put)
	if res.Code != http.StatusOK {
		t.Fatalf("put preferences status=%d body=%s", res.Code, res.Body.String())
	}

	// 给事件一点传播时间，然后确认 bob 没收到。
	time.Sleep(150 * time.Millisecond)
	if body := rec.snapshot(); strings.Contains(body, "font-scale") {
		t.Fatalf("跨用户泄露了偏好事件: %q", body)
	}
}

// TestPreferenceEventsRequiresAuth 验证未带凭据时返回 401，不建立流。
func TestPreferenceEventsRequiresAuth(t *testing.T) {
	service := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences/events", nil)
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without credentials, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestPreferenceEventsUnsubscribe 验证客户端断开后订阅表被清理，
// 否则长期运行会泄漏订阅者。
func TestPreferenceEventsUnsubscribe(t *testing.T) {
	service := newTestService(t)
	plain := issueTokenFor(t, service, "evt-drop")

	ctx, cancel := context.WithCancel(context.Background())
	rec := newSSERecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences/events", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	req = req.WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.ServeHTTP(rec, req)
	}()
	rec.waitFor(t, "event: hello")

	if got := service.preferenceEvents.subscriberCount(); got != 1 {
		t.Fatalf("expected 1 subscriber, got %d", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE handler 未在上下文取消后退出")
	}
	if got := service.preferenceEvents.subscriberCount(); got != 0 {
		t.Fatalf("断开后订阅者未清理，got %d", got)
	}
}

// TestPublishPreferenceChangeSkipsEmptyKeys 验证空键不发事件，
// 避免客户端被无意义事件唤醒后空跑一次拉取。
func TestPublishPreferenceChangeSkipsEmptyKeys(t *testing.T) {
	service := newTestService(t)
	ch, unsubscribe := service.preferenceEvents.subscribe("u1")
	defer unsubscribe()

	service.publishPreferenceChange("u1", nil, "")
	service.publishPreferenceChange("u1", []string{"  "}, "")

	select {
	case event := <-ch:
		t.Fatalf("空键不应产生事件: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}

	service.publishPreferenceChange("u1", []string{"theme-preset"}, "2030-01-01T00:00:00Z")
	select {
	case event := <-ch:
		if event.Type != "preferences.changed" || len(event.Keys) != 1 || event.Keys[0] != "theme-preset" {
			t.Fatalf("事件内容不符: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("应收到非空键的事件")
	}
}

// TestSanitizePreferenceKeys 覆盖去空白、去空串与数量上限。
func TestSanitizePreferenceKeys(t *testing.T) {
	keys := sanitizePreferenceKeys([]string{" a ", "", "   ", "b"})
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("sanitize 结果不符: %v", keys)
	}

	many := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		many = append(many, "k"+string(rune('a'+i%26)))
	}
	if got := len(sanitizePreferenceKeys(many)); got != 64 {
		t.Fatalf("键数量上限应为 64，got %d", got)
	}
}

// TestPreferenceEventJSONShape 固定事件载荷结构，客户端按此解析。
func TestPreferenceEventJSONShape(t *testing.T) {
	raw, err := json.Marshal(preferenceEvent{Type: "preferences.changed", Keys: []string{"k"}, UpdatedAt: "t"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"preferences.changed","keys":["k"],"updatedAt":"t"}`
	if string(raw) != want {
		t.Fatalf("事件载荷结构变化，客户端契约会失效:\n got %s\nwant %s", raw, want)
	}
}
