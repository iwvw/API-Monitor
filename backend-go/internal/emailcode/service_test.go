package emailcode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

func TestIngestAndList(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Ingest(ctx, Message{Mailbox: "Probe1@DSUKHUB.com", Code: "123456", Subject: "Verify", Sender: "hey@x.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, Message{Mailbox: "probe1@dsukhub.com", Code: "654321", Subject: "Verify2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, Message{Mailbox: "other@dsukhub.com", Code: "999999"}); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.List(ctx, "PROBE1@dsukhub.com", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("应返回 2 封，得到 %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Code != "654321" {
		t.Fatalf("应按时间倒序，首条 code = %q", msgs[0].Code)
	}
	if msgs[0].ReceivedAt == "" {
		t.Fatal("receivedAt 应自动填充")
	}
	if msgs[0].FromDomain != "x.com" && msgs[1].FromDomain != "x.com" {
		t.Fatalf("from_domain 应自动从 sender 解析: %+v", msgs)
	}
}

func TestIngestDeduplicatesByMessageID(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	first, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111", MessageID: "<dup@x>"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111", MessageID: "<dup@x>"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("同 Message-ID 应复用记录: %d vs %d", first, second)
	}
	msgs, _ := s.List(ctx, "a@x.com", false, 10)
	if len(msgs) != 1 {
		t.Fatalf("去重后应只有 1 封，得到 %d", len(msgs))
	}
}

func TestIngestRejectsEmptyMailbox(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Ingest(context.Background(), Message{Code: "1"}); err == nil {
		t.Fatal("空收件人应报错")
	}
}

func TestConsumeHidesFromDefaultList(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	id, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Consume(ctx, id, "test"); err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.List(ctx, "a@x.com", false, 10)
	if len(fresh) != 0 {
		t.Fatalf("已消费邮件默认不应出现，得到 %d", len(fresh))
	}
	all, _ := s.List(ctx, "a@x.com", true, 10)
	if len(all) != 1 || all[0].ConsumedAt == "" || all[0].ConsumedBy != "test" {
		t.Fatalf("includeConsumed 应返回已消费邮件且带 consumedAt/consumedBy: %+v", all)
	}
}

func TestClearOnlyConsumed(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	keep, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111"})
	drop, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "222222"})
	if err := s.Consume(ctx, drop, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clear(ctx, true); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.List(ctx, "a@x.com", true, 10)
	if len(msgs) != 1 || msgs[0].ID != keep {
		t.Fatalf("只清已消费时应保留未消费邮件: %+v", msgs)
	}
}

func TestPurgeBeforeRemovesOldMessages(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	old, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111", ReceivedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)})
	fresh, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "222222"})
	n, err := s.PurgeBefore(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应删除 1 封过期邮件，得到 %d", n)
	}
	msgs, _ := s.List(ctx, "a@x.com", true, 10)
	if len(msgs) != 1 || msgs[0].ID != fresh {
		t.Fatalf("应保留未过期邮件: %+v (old=%d)", msgs, old)
	}
}

func TestWaitClaimsAndFilters(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	// 非目标发件人的邮件不应被认领。
	if _, err := s.Ingest(ctx, Message{Mailbox: "u@x.com", Code: "111111", Sender: "noise@other.com"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Wait(ctx, Selector{Mailbox: "u@x.com", FromDomain: "posthog.com", RequireCode: true}, "t", 300*time.Millisecond)
	if err == nil {
		t.Fatal("发件人不匹配时不应认领")
	}

	target, err := s.Ingest(ctx, Message{Mailbox: "u@x.com", Code: "654321", Sender: "hey@posthog.com"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := s.Wait(ctx, Selector{Mailbox: "u@x.com", FromDomain: "posthog.com"}, "consumer-a", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != "654321" || msg.ID != target {
		t.Fatalf("应认领目标邮件: %+v", msg)
	}
	// 认领后不可再次取用（认领语义）。
	if _, err := s.Wait(ctx, Selector{Mailbox: "u@x.com", FromDomain: "posthog.com"}, "consumer-b", 300*time.Millisecond); err == nil {
		t.Fatal("已被认领的邮件不应再被取用")
	}
}

// TestWaitFromDomainMatchesSubdomain 覆盖真实场景：站点从 Mailgun 子域发信
// （如 cioeu80164.posthog.com），选择器写 posthog.com 也必须能命中。
func TestWaitFromDomainMatchesSubdomain(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Ingest(ctx, Message{Mailbox: "u@x.com", Code: "112233", Sender: "postmaster@cioeu80164.posthog.com"}); err != nil {
		t.Fatal(err)
	}
	msg, err := s.Wait(ctx, Selector{Mailbox: "u@x.com", FromDomain: "posthog.com"}, "t", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != "112233" {
		t.Fatalf("子域发件人应命中主域选择器，得到 %q", msg.Code)
	}
}

// TestWaitFromDomainDoesNotMatchLookalike 后缀匹配不能误伤形似域名。
func TestWaitFromDomainDoesNotMatchLookalike(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Ingest(ctx, Message{Mailbox: "u@x.com", Code: "445566", Sender: "evil@notposthog.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(ctx, Selector{Mailbox: "u@x.com", FromDomain: "posthog.com"}, "t", 300*time.Millisecond); err == nil {
		t.Fatal("notposthog.com 不应命中 posthog.com")
	}
}

// TestWaitAcceptsMessageOlderThanSince 覆盖真实故障场景：
// PostHog 短时间内重复登录会复用同一封验证邮件、不重发，
// 用户先点登录页、稍后才发起面板自动登录时，邮件会早于会话开始时间。
// 调用方（posthogcode）为此把 since 放宽 15 分钟；此处验证放宽后确实能取到旧邮件。
func TestWaitAcceptsMessageOlderThanSince(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	// 邮件到达于 7 分钟前。
	if _, err := s.Ingest(ctx, Message{
		Mailbox:    "u@x.com",
		Code:       "150628",
		Sender:     "postmaster@cioeu80164.posthog.com",
		Subject:    "Verify your PostHog login",
		ReceivedAt: time.Now().Add(-7 * time.Minute).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	// 会话 1 分钟前才开始，按 posthogcode 的策略放宽 15 分钟。
	msg, err := s.Wait(ctx, Selector{
		Mailbox:         "u@x.com",
		FromDomain:      "posthog.com",
		SubjectContains: "login",
		Since:           time.Now().Add(-1 * time.Minute).Add(-15 * time.Minute),
		RequireCode:     true,
	}, "t", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != "150628" {
		t.Fatalf("应取到早于会话开始的邮件，得到 %q", msg.Code)
	}
}

func TestWaitReceivesLateMessage(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	since := time.Now()
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = s.Ingest(context.Background(), Message{Mailbox: "late@x.com", Code: "777777"})
	}()
	msg, err := s.Wait(ctx, Selector{Mailbox: "late@x.com", Since: since, RequireCode: true}, "t", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != "777777" {
		t.Fatalf("应收到后到的验证码，得到 %q", msg.Code)
	}
}

func TestWaitTimesOut(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Wait(context.Background(), Selector{Mailbox: "never@x.com"}, "t", 200*time.Millisecond); err == nil {
		t.Fatal("应超时")
	}
}

func TestMailboxKeyNormalization(t *testing.T) {
	if got := mailboxKey("  Probe1@X.COM "); got != "probe1@x.com" {
		t.Fatalf("mailboxKey = %q", got)
	}
}

func TestWorkerSecretStableAndEnvOverride(t *testing.T) {
	t.Setenv(workerSecretEnv, "")
	a := WorkerSecret()
	if a == "" || a != WorkerSecret() {
		t.Fatal("派生密钥应稳定非空")
	}
	t.Setenv(workerSecretEnv, "custom-secret")
	if WorkerSecret() != "custom-secret" {
		t.Fatal("环境变量应覆盖派生值")
	}
}

func TestStatsAggregation(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Domain: "x.com", Sender: "hey@posthog.com", FromDomain: "posthog.com", Code: "123456"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Domain: "x.com", Sender: "hey@posthog.com", FromDomain: "posthog.com", Code: ""}); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats["received"].(int) != 2 || stats["extracted"].(int) != 1 {
		t.Fatalf("统计不正确: %+v", stats)
	}
	rate, _ := stats["rate"].(float64)
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("提取率应约 0.5，得到 %v", rate)
	}
	rows, _ := stats["rows"].([]StatsRow)
	if len(rows) != 1 || rows[0].FromDomain != "posthog.com" {
		t.Fatalf("应按发件人域名聚合: %+v", rows)
	}
}

func itoa(v int64) string { return fmt.Sprintf("%d", v) }

func TestIngestHandlerSecretAndParsing(t *testing.T) {
	s := newTestService(t)
	t.Setenv(workerSecretEnv, "sekret")

	raw := strings.Join([]string{
		"From: PostHog <hey@posthog.com>",
		"To: u@x.com",
		"Subject: =?UTF-8?B?" + b64("Let's verify your email") + "?=",
		"Message-ID: <msg-1@posthog.com>",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Enter this code to verify your email address:",
		"",
		"919941",
		"",
		"If you did not create a PostHog account, ignore this.",
	}, "\r\n")
	body, _ := json.Marshal(map[string]string{"mailbox": "u@x.com", "raw": raw})

	req := httptest.NewRequest(http.MethodPost, "/api/emailcode/ingest", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无密钥应 401，得到 %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/emailcode/ingest", strings.NewReader(string(body)))
	req.Header.Set("X-Worker-Secret", "sekret")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("正确密钥应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	msgs, _ := s.List(context.Background(), "u@x.com", false, 10)
	if len(msgs) != 1 {
		t.Fatalf("邮件应已入库: %+v", msgs)
	}
	if msgs[0].Code != "919941" {
		t.Fatalf("面板侧应提取出验证码，得到 %q", msgs[0].Code)
	}
	if msgs[0].ExtractStatus != ExtractOK {
		t.Fatalf("extract_status 应为 ok，得到 %q", msgs[0].ExtractStatus)
	}
	if msgs[0].Subject != "Let's verify your email" {
		t.Fatalf("MIME 头应解码，得到 %q", msgs[0].Subject)
	}
	if msgs[0].FromDomain != "posthog.com" {
		t.Fatalf("from_domain 应解析为 posthog.com，得到 %q", msgs[0].FromDomain)
	}
}

func TestIngestHandlerAcceptsToAlias(t *testing.T) {
	s := newTestService(t)
	t.Setenv(workerSecretEnv, "sekret")
	req := httptest.NewRequest(http.MethodPost, "/api/emailcode/ingest", strings.NewReader(`{"to":"alias@x.com","code":"121212"}`))
	req.Header.Set("X-Worker-Secret", "sekret")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("to 应作为 mailbox 兜底，得到 %d: %s", rec.Code, rec.Body.String())
	}
	msgs, _ := s.List(context.Background(), "alias@x.com", false, 10)
	if len(msgs) != 1 {
		t.Fatalf("应以 to 入库: %+v", msgs)
	}
}

func TestDomainsEndpoint(t *testing.T) {
	s := newTestService(t)
	s.SetDomainProvider(stubDomains{"a.com", "b.com"})
	req := httptest.NewRequest(http.MethodGet, "/api/emailcode/domains", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", rec.Code)
	}
	var out struct {
		Data struct {
			Domains []string `json:"domains"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Data.Domains) != 2 {
		t.Fatalf("应返回 2 个域名: %s", rec.Body.String())
	}
}

func TestMessageItemDeleteAndConsumeRoutes(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	id, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "1"})

	req := httptest.NewRequest(http.MethodGet, "/api/emailcode/messages/"+itoa(id), nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情应 200，得到 %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/emailcode/messages/"+itoa(id)+"/consume", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("consume 应 200，得到 %d", rec.Code)
	}
	all, _ := s.List(ctx, "a@x.com", true, 10)
	if all[0].ConsumedAt == "" {
		t.Fatal("consume 应写入 consumedAt")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/emailcode/messages/"+itoa(id), nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete 应 200，得到 %d", rec.Code)
	}
	all, _ = s.List(ctx, "a@x.com", true, 10)
	if len(all) != 0 {
		t.Fatalf("删除后应无记录: %+v", all)
	}
}

func TestUnknownRouteNotFound(t *testing.T) {
	s := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/emailcode/nope", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知路由应 404，得到 %d", rec.Code)
	}
}

type stubDomains []string

func (s stubDomains) InboxDomains(context.Context) ([]string, error) { return s, nil }

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
