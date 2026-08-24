package cronjobs

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// fakeCronAIServer 模拟 /api/admin-ai/cron/task-run 内部接口，统计收到的执行请求次数，
// 用于验证重试循环是否整体重放了 AI 任务。
func fakeCronAIServer(t *testing.T, status int, body string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin-ai/cron/task-run" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return srv, &calls
}

// newAITaskService 构造指向 fake server 的 cron Service（Port 指向测试服务器端口）。
func newAITaskService(port int) *Service {
	return &Service{
		cfg:    config.Config{Port: port},
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// aiTask 构造一个带重试配置的 AI 定时任务。
func aiTask(retryCount int) SchedulerTask {
	return SchedulerTask{
		Task:                 Task{Name: "AI 任务", Command: "执行一条写入操作", Type: "ai"},
		TimeoutSeconds:       5,
		RetryCount:           retryCount,
		RetryIntervalSeconds: 1,
	}
}

// TestAITaskFailureDoesNotRetryReplay 核心回归：AI 任务执行失败后不得整体重试，
// 否则会重放已产生的副作用（会话/写操作/频道推送），模拟请求必须只发出一次。
func TestAITaskFailureDoesNotRetryReplay(t *testing.T) {
	srv, calls := fakeCronAIServer(t, http.StatusInternalServerError, `{"error":"AI 执行失败"}`)
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	service := newAITaskService(port)

	_, err := service.executeSchedulerTaskCommand(context.Background(), aiTask(2))
	if err == nil {
		t.Fatal("expected AI task failure")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("AI 任务失败后不应重试重放副作用，期望请求 1 次，实际 %d 次", got)
	}
	if !strings.Contains(err.Error(), "第 1 次尝试失败") {
		t.Fatalf("失败历史应保留首次尝试记录，实际: %v", err)
	}
}

// TestAITaskSuccessSingleCall 验证 AI 任务成功路径只执行一次（无多余调用）。
func TestAITaskSuccessSingleCall(t *testing.T) {
	srv, calls := fakeCronAIServer(t, http.StatusOK, `{"output":"执行完成"}`)
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	service := newAITaskService(port)

	output, err := service.executeSchedulerTaskCommand(context.Background(), aiTask(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "执行完成" {
		t.Fatalf("unexpected output: %q", output)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("AI 任务成功只应执行 1 次，实际 %d 次", got)
	}
}

// TestNonAITaskKeepsRetrySemantics 守护非 AI 任务（shell）原有重试语义不变。
func TestNonAITaskKeepsRetrySemantics(t *testing.T) {
	service := &Service{cfg: config.Config{AllowLocalShellTasks: true}}
	task := SchedulerTask{
		Task:           Task{Name: "Shell 任务", Command: "exit 1", Type: "shell"},
		TimeoutSeconds: 5,
		RetryCount:     1,
	}
	_, err := service.executeSchedulerTaskCommand(context.Background(), task)
	if err == nil {
		t.Fatal("expected shell task failure")
	}
	if !strings.Contains(err.Error(), "第 2 次尝试失败") {
		t.Fatalf("非 AI 任务应保留重试语义，实际错误: %v", err)
	}
}
