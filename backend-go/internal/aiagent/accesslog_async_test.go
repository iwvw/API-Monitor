package aiagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 异步访问日志的行为回归：
//   1. 投递后能在合理时间内批量落库（不是永久丢失）。
//   2. StopAccessLog 排空已入队条目。
//   3. StopAccessLog 幂等，可重复调用。
//   4. 并发投递不丢条（队列未满时）。

func newAccessLogTestService(t *testing.T) *Service {
	t.Helper()
	service := New(testConfig(t))
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return service
}

func countAccessLogs(t *testing.T, service *Service, instanceID string) int {
	t.Helper()
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	logs, err := service.listAccessLogs(ctx, db, 500, instanceID)
	if err != nil {
		t.Fatalf("listAccessLogs: %v", err)
	}
	return len(logs)
}

func TestAccessLogAsyncPersist(t *testing.T) {
	service := newAccessLogTestService(t)
	defer service.StopAccessLog()

	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/gw/x/session", nil)
	service.writeAccessLog(req.Context(), AccessLog{InstanceID: "inst-a", Action: "gw GET /session", Result: "ok", StatusCode: 200})

	// 批量落库有 500ms 抖动窗口，给足余量。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countAccessLogs(t, service, "inst-a") >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("access log was not persisted within the flush window")
}

func TestAccessLogStopDrainsQueue(t *testing.T) {
	service := newAccessLogTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/gw/x/session", nil)
	const n = 100
	for i := 0; i < n; i++ {
		service.writeAccessLog(req.Context(), AccessLog{InstanceID: "inst-b", Action: "gw", Result: "ok", StatusCode: 200})
	}
	// Stop 必须排空已入队条目。
	service.StopAccessLog()
	if got := countAccessLogs(t, service, "inst-b"); got != n {
		t.Fatalf("expected %d drained logs, got %d", n, got)
	}
	// 幂等：重复调用不 panic。
	service.StopAccessLog()
}

func TestAccessLogConcurrentEnqueue(t *testing.T) {
	service := newAccessLogTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/gw/x/session", nil)
	const workers = 8
	const perWorker = 50
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				service.writeAccessLog(req.Context(), AccessLog{InstanceID: "inst-c", Action: "gw", Result: "ok", StatusCode: 200})
			}
		}()
	}
	wg.Wait()
	service.StopAccessLog()
	if got := countAccessLogs(t, service, "inst-c"); got != workers*perWorker {
		t.Fatalf("expected %d logs, got %d", workers*perWorker, got)
	}
}
