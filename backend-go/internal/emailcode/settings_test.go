package emailcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetentionSettingsDefaultAndClamp(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	if got := s.RetentionDays(ctx); got != defaultRetentionDays {
		t.Fatalf("默认保留天数应为 %d,得到 %d", defaultRetentionDays, got)
	}

	days, err := s.SetRetentionDays(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if days != 3 || s.RetentionDays(ctx) != 3 {
		t.Fatalf("应保存为 3,得到 days=%d 读取=%d", days, s.RetentionDays(ctx))
	}

	if days, _ := s.SetRetentionDays(ctx, 0); days != 1 {
		t.Fatalf("下界应夹到 1,得到 %d", days)
	}
	if days, _ := s.SetRetentionDays(ctx, 999); days != maxRetentionDays {
		t.Fatalf("上界应夹到 %d,得到 %d", maxRetentionDays, days)
	}
}

func TestSetRetentionDaysPurgesExpired(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	expired, err := s.Ingest(ctx, Message{
		Mailbox:    "a@x.com",
		ReceivedAt: time.Now().Add(-5 * 24 * time.Hour).UTC().Format(time.RFC3339),
	})
	fresh, err := s.Ingest(ctx, Message{Mailbox: "a@x.com"})
	if err != nil {
		t.Fatal(err)
	}

	// 缩短到 2 天:5 天前的邮件应在保存时立即清掉,新邮件保留。
	if _, err := s.SetRetentionDays(ctx, 2); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.List(ctx, "a@x.com", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != fresh {
		t.Fatalf("应只保留新邮件: %+v (expired=%d fresh=%d)", msgs, expired, fresh)
	}
}

func TestSettingsEndpointReadWrite(t *testing.T) {
	s := newTestService(t)

	req := httptest.NewRequest(http.MethodGet, "/api/emailcode/settings", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"retentionDays":7`) {
		t.Fatalf("GET 应返回默认 7 天: status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/emailcode/settings", strings.NewReader(`{"retentionDays":14}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"retentionDays":14`) {
		t.Fatalf("PUT 应保存 14 天: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := s.RetentionDays(context.Background()); got != 14 {
		t.Fatalf("PUT 后读取应为 14,得到 %d", got)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/emailcode/settings", strings.NewReader(`{"retentionDays":0}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("越界应 400,得到 %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/emailcode/settings", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("不支持的方法应 405,得到 %d", rec.Code)
	}
}
