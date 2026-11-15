package qoder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// timeUnixForToday 返回今天 10:00（UTC+8）的时间戳，用于构造「今日窗口」测试数据。
func timeUnixForToday() int64 {
	now := time.Now().In(cstZone)
	return time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, cstZone).Unix()
}

// itoa64 把 int64 转字符串（测试构造 JSON 用）。
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// TestDailyCheckinRejectedClaimIsFailure 守住「上游拒绝不能伪装成已签到」：
// 返回 CLAIMED 以外的状态且非 replayed 时，必须报 failed 而不是 already。
func TestDailyCheckinRejectedClaimIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sash/api/v1/me/campaigns":
			_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE","startAt":1761261600}]}`))
		case strings.HasSuffix(r.URL.Path, "/claim"):
			_, _ = w.Write([]byte(`{"status":"FAILED"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	res, err := s.DailyCheckin(context.Background(), Account{ID: "u1", AccessToken: "tok", Region: regionCN})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed", res.Status)
	}
	if strings.Contains(res.Message, "签到成功") {
		t.Fatalf("message should not claim success: %q", res.Message)
	}
}

// TestDailyCheckinRecordsWindowOnAlready 验证「今日已领取」也会补记窗口日期，
// 使连签天数不会因为积分在别处领取而断链。
func TestDailyCheckinRecordsWindowOnAlready(t *testing.T) {
	windowStart := timeUnixForToday()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED","startAt":` +
			itoa64(windowStart) + `}]}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	res, err := s.DailyCheckin(context.Background(), Account{ID: "u1", AccessToken: "tok", Region: regionCN})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "already" {
		t.Fatalf("status = %q", res.Status)
	}
	recs := s.loadCheckinHistory(context.Background(), "u1")
	if len(recs) != 1 {
		t.Fatalf("already-claimed window should be recorded, got %+v", recs)
	}
	if recs[0].Amount != 0 {
		t.Fatalf("amount should stay 0 for externally claimed window, got %g", recs[0].Amount)
	}
}
