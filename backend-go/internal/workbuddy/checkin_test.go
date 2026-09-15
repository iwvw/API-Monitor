package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// withMockUpstream 把计费域与成长域指向同一 mock，返回还原函数。
func withMockUpstream(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prevBilling, prevWeb := billingBaseOverride, webBaseOverride
	billingBaseOverride, webBaseOverride = srv.URL, srv.URL
	t.Cleanup(func() { billingBaseOverride, webBaseOverride = prevBilling, prevWeb })
}

// newCheckinTestService 构造一个只带内存设置的服务（不落库、不读站点时区缓存）。
func newCheckinTestService(accounts ...Account) *Service {
	return &Service{
		settings: Settings{
			Enabled:  true,
			Accounts: accounts,
		},
		callBase:      map[string]int64{},
		callPending:   map[string]int64{},
		creditDayUsed: map[string]float64{},
		cooldownUntil: map[string]time.Time{},
	}
}

func TestCheckinSupportedOnlyCN(t *testing.T) {
	if !checkinSupported(Account{Region: regionCN}) {
		t.Error("国内版应支持签到")
	}
	// 空 Region 按国内版处理（存量账号兼容）。
	if !checkinSupported(Account{}) {
		t.Error("空区域应按国内版处理")
	}
	if checkinSupported(Account{Region: regionIntl}) {
		t.Error("国际版无签到体系，不应参与")
	}
}

func TestIsAlreadyCheckinError(t *testing.T) {
	// 上游业务码 10001「今天已签到」是幂等成功。
	if !isAlreadyCheckinError(errUpstreamCode(10001, "今天已签到，请明天再来")) {
		t.Error("业务码 10001 今天已签到应判定为幂等")
	}
	if !isAlreadyCheckinError(errUpstreamCode(14001, "already checked in")) {
		t.Error("14001 应判定为幂等")
	}
	// 10001 但文案是「活动未开启」不算已签到（国际版语义）。
	if isAlreadyCheckinError(errUpstreamCode(10001, "签到活动未开启或已过期")) {
		t.Error("活动未开启不应判定为已签到")
	}
	// 网络层错误不算幂等，否则抖动会被误记为签到成功。
	if isAlreadyCheckinError(io.ErrUnexpectedEOF) {
		t.Error("网络错误不应判定为已签到")
	}
	if isAlreadyCheckinError(nil) {
		t.Error("nil 不应判定为已签到")
	}
}

// errUpstreamCode 构造与 doJSON 同形状的业务码错误。
func errUpstreamCode(code int, msg string) error {
	return &fmtError{"上游业务码 " + itoa(code) + ": " + msg}
}

type fmtError struct{ s string }

func (e *fmtError) Error() string { return e.s }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestIsLockedError(t *testing.T) {
	if !isLockedError(&fmtError{"上游业务码 403: 连续登录天数不足，请继续打卡或使用补签卡"}) {
		t.Error("连登未解锁应判定为 locked")
	}
	if !isLockedError(&fmtError{"上游业务码 403: no makeup card balance"}) {
		t.Error("无补签卡应判定为 locked")
	}
	if isLockedError(&fmtError{"上游业务码 500: internal server error"}) {
		t.Error("服务端错误不应判定为 locked")
	}
}

func TestEndpointCheckinPerRegion(t *testing.T) {
	if got := endpointCheckin(regionCN); got != "https://www.codebuddy.cn/v2/billing/meter/daily-checkin" {
		t.Errorf("国内签到 URL = %q", got)
	}
	if got := endpointCheckin(regionIntl); got != "https://www.workbuddy.ai/v2/billing/meter/daily-checkin" {
		t.Errorf("国际签到 URL = %q", got)
	}
	if got := endpointGrowth(regionCN, "/activity/growth/streak"); got != "https://www.workbuddy.cn/activity/growth/streak" {
		t.Errorf("国内连登 URL = %q", got)
	}
}

// TestDailyCheckinSuccessAndIdempotent 验证签到成功解析与重复签到幂等。
func TestDailyCheckinSuccessAndIdempotent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/billing/meter/daily-checkin" {
			t.Errorf("意外路径 %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		n := calls.Add(1)
		if n == 1 {
			_, _ = w.Write([]byte(`{"code":0,"msg":"OK","data":{"credit":100,"streak_days":4}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":10001,"msg":"今天已签到，请明天再来"}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv)

	s := newCheckinTestService()
	acc := Account{ID: "u1", UID: "u1", Region: regionCN, AccessToken: "tok"}

	first, err := s.dailyCheckin(context.Background(), acc)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "success" || first.Gained != 100 || first.StreakDays != 4 {
		t.Errorf("首次签到结果 = %+v", first)
	}

	second, err := s.dailyCheckin(context.Background(), acc)
	if err != nil {
		t.Fatalf("重复签到不应返回错误: %v", err)
	}
	if second.Status != "already" {
		t.Errorf("重复签到状态 = %q, want already", second.Status)
	}
}

// TestDailyCheckinSkipsIntl 验证国际版账号不发起上游请求。
func TestDailyCheckinSkipsIntl(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv)

	s := newCheckinTestService()
	result, err := s.dailyCheckin(context.Background(), Account{ID: "intl-u", Region: regionIntl, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" {
		t.Errorf("国际版状态 = %q, want skipped", result.Status)
	}
	if hits.Load() != 0 {
		t.Errorf("国际版不应打上游，实际 %d 次", hits.Load())
	}
}

// TestReportChatActivityShape 验证活跃上报的事件形状（userId 必填、单元素数组、chat_request_send）。
func TestReportChatActivityShape(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/report" {
			t.Errorf("意外路径 %s", r.URL.Path)
		}
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"code":0,"msg":"OK"}`))
	}))
	defer srv.Close()
	withMockUpstream(t, srv)

	s := newCheckinTestService()
	acc := Account{ID: "u1", UID: "uid-abc", Region: regionCN, AccessToken: "tok"}
	if err := s.reportChatActivity(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("上报体应为数组: %v (%s)", err, body)
	}
	if len(events) != 1 {
		t.Fatalf("事件数 = %d, want 1", len(events))
	}
	if events[0]["eventCode"] != "chat_request_send" {
		t.Errorf("eventCode = %v", events[0]["eventCode"])
	}
	// userId 缺失会让上游 200 但静默丢弃，是这条链路最容易出错的地方。
	if events[0]["userId"] != "uid-abc" {
		t.Errorf("userId = %v, want uid-abc", events[0]["userId"])
	}
}

// TestRunStreakBonusRedeemsAndDraws 验证连登管家：跳过 locked、兑换 unlocked、抽完次数。
func TestRunStreakBonusRedeemsAndDraws(t *testing.T) {
	var redeemed, drawn atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/activity/growth/streak"):
			_, _ = w.Write([]byte(`{"code":0,"data":{
				"streak":{"days":7},
				"makeup_cards":{"balance":0,"max":4},
				"redemption_status":{
					"tier_7d_status":"available","tier_14d_status":"locked","tier_28d_status":"claimed",
					"tiers":[
						{"tier":"7d","days":7,"credit":0,"energy":2,"cards":1,"chances":1},
						{"tier":"14d","days":14,"credit":50,"energy":3,"cards":1,"chances":1},
						{"tier":"28d","days":28,"credit":150,"energy":5,"cards":1,"chances":1}
					]}}}`))
		case strings.HasSuffix(r.URL.Path, "/activity/growth/redeem"):
			redeemed.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"msg":"OK"}`))
		case strings.HasSuffix(r.URL.Path, "/activity/growth/lottery/summary"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"chances":2,"module":{"enabled":true}}}`))
		case strings.HasSuffix(r.URL.Path, "/activity/growth/lottery/draw"):
			drawn.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"data":{"prize":"+10c"}}`))
		default:
			t.Errorf("意外路径 %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withMockUpstream(t, srv)

	s := newCheckinTestService()
	out, err := s.runStreakBonus(context.Background(), Account{ID: "u1", UID: "u1", Region: regionCN, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	// 只有 7d 处于 available，14d locked、28d claimed 都应跳过。
	if redeemed.Load() != 1 {
		t.Errorf("兑换次数 = %d, want 1", redeemed.Load())
	}
	if len(out.RedeemedTiers) != 1 || out.RedeemedTiers[0] != "7d" {
		t.Errorf("兑换档位 = %v, want [7d]", out.RedeemedTiers)
	}
	if drawn.Load() != 2 {
		t.Errorf("抽奖次数 = %d, want 2", drawn.Load())
	}
	if out.DrawCount != 2 {
		t.Errorf("DrawCount = %d, want 2", out.DrawCount)
	}
	if out.Days != 7 {
		t.Errorf("Days = %d, want 7", out.Days)
	}
}

// TestRunStreakBonusRedemptionToleratesLocked 验证兑换被上游拒绝时静默跳过（不中断抽奖）。
func TestRunStreakBonusRedemptionToleratesLocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/activity/growth/streak"):
			_, _ = w.Write([]byte(`{"code":0,"data":{
				"streak":{"days":3},"makeup_cards":{"balance":0},
				"redemption_status":{"tier_7d_status":"available","tiers":[{"tier":"7d","days":7}]}}}`))
		case strings.HasSuffix(r.URL.Path, "/activity/growth/redeem"):
			// 上游对未解锁档位返回 403（即使列表标 available）。
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":403,"msg":"连续登录天数不足，请继续打卡或使用补签卡"}`))
		case strings.HasSuffix(r.URL.Path, "/activity/growth/lottery/summary"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"chances":0}}`))
		default:
			t.Errorf("意外路径 %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withMockUpstream(t, srv)

	s := newCheckinTestService()
	out, err := s.runStreakBonus(context.Background(), Account{ID: "u1", UID: "u1", Region: regionCN, AccessToken: "tok"})
	if err != nil {
		t.Fatalf("已解锁档位被拒不应升级为错误: %v", err)
	}
	if len(out.RedeemedTiers) != 0 {
		t.Errorf("未成功兑换不应记录档位: %v", out.RedeemedTiers)
	}
}

// TestRebuildSchedulerReplacesAndStopsOld 重建调度器后 s.scheduler 必须指向新时区，
// 且旧调度器被停止。回归「watchTimezone 拿启动时的 rt 当基准」导致的泄漏：
// 基准不更新会让时区变更一次后每分钟都重建，旧调度器越积越多、任务重复触发。
func TestRebuildSchedulerReplacesAndStopsOld(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("tzdata 不可用: %v", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata 不可用: %v", err)
	}

	first := &cronRuntime{scheduler: cron.New(cron.WithLocation(shanghai)), loc: shanghai}
	s.armScheduler(ctx, first)
	first.scheduler.Start()
	s.schedMu.Lock()
	s.scheduler = first
	s.schedMu.Unlock()

	// 时区变更 -> 重建。
	s.rebuildScheduler(ctx, tokyo)

	s.schedMu.Lock()
	got := s.scheduler
	s.schedMu.Unlock()
	if got == first {
		t.Fatal("重建后 s.scheduler 应指向新运行时")
	}
	if got.location() != tokyo {
		t.Fatalf("新调度器时区 = %v, want %v", got.location(), tokyo)
	}
	// 新调度器的任务必须已挂载（漏挂会丢签到/活跃上报）。
	if n := len(got.scheduler.Entries()); n != 2 {
		t.Fatalf("新调度器任务数 = %d, want 2（签到 + 活跃上报）", n)
	}
	// 旧调度器应已停止：其 Stop 返回的 ctx 应已完成。
	stopCtx := first.scheduler.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("旧调度器未停止")
	}

	// 关键回归点：时区不变时不得再次重建。
	// 若 watchTimezone 拿的是启动时的固定基准（旧实现），这里会反复判定「时区又变了」，
	// 每轮都新建调度器、旧调度器泄漏且任务重复触发。
	s.rebuildScheduler(ctx, tokyo)
	s.schedMu.Lock()
	again := s.scheduler
	s.schedMu.Unlock()
	if again.location() != tokyo {
		t.Fatalf("重建基准确认失败：新调度器时区 = %v, want %v", again.location(), tokyo)
	}
	if n := len(again.scheduler.Entries()); n != 2 {
		t.Fatalf("重建后任务数 = %d, want 2", n)
	}
}
