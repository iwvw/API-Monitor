package cronjobs

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// 时区热更新：改站点时区后 ReloadAll 重建调度器，存量任务按新时区重新挂载，
// 不再需要重启进程。
func TestReloadAllRebuildsSchedulerOnTimezoneChange(t *testing.T) {
	service := newCronService(t)

	res := performCronRequest(service, http.MethodPost, "/api/cron/tasks",
		`{"name":"TZ","schedule":"0 8 * * *","command":"echo hi","type":"shell","enabled":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", res.Code, res.Body.String())
	}

	if got := service.scheduler.Location(); got != time.Local {
		t.Fatalf("initial scheduler location = %v, want local", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE user_settings SET time_zone='Asia/Shanghai' WHERE id=1`); err != nil {
		t.Fatalf("update timezone: %v", err)
	}
	db.Close()

	if err := service.ReloadAll(context.Background()); err != nil {
		t.Fatalf("reload all: %v", err)
	}

	if got := service.scheduler.Location(); got.String() != shanghai.String() {
		t.Fatalf("scheduler location after reload = %v, want Asia/Shanghai", got)
	}
	service.mu.Lock()
	entryCount := len(service.entries)
	service.mu.Unlock()
	if entryCount != 1 {
		t.Fatalf("entries after rebuild = %d, want 1", entryCount)
	}
}
