package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func validAccount(id, token string) Account {
	return Account{
		ID:          id,
		UID:         id,
		AccessToken: token,
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}
}

// 选号：取「今天已消耗 credit 最少」的可用账号；相同消耗取列表序靠前者（确定性）。
func TestPickLeastConsumed(t *testing.T) {
	s := &Service{creditDayUsed: map[string]float64{}}
	a1 := validAccount("u1", "t1")
	a2 := validAccount("u2", "t2")

	// 都还没消耗 → 取列表序第一个。
	acc, ok := s.pickLeastConsumed([]Account{a1, a2})
	if !ok || acc.ID != "u1" {
		t.Fatalf("零消耗应取列表序首个，得到 %v/%v", acc.ID, ok)
	}

	// u1 消耗更多 → 选 u2。
	s.creditDayUsed["u1"] = 0.5
	s.creditDayUsed["u2"] = 0.1
	acc, ok = s.pickLeastConsumed([]Account{a1, a2})
	if !ok || acc.ID != "u2" {
		t.Fatalf("应选消耗更少的 u2，得到 %v", acc.ID)
	}

	// 相同消耗 → 回到列表序。
	s.creditDayUsed["u2"] = 0.5
	acc, _ = s.pickLeastConsumed([]Account{a1, a2})
	if acc.ID != "u1" {
		t.Fatalf("同消耗应取列表序首个 u1，得到 %v", acc.ID)
	}

	// 不可用账号（停用 / token 过期 / 无 token）不参与。
	disabled := validAccount("u3", "t3")
	disabled.Disabled = true
	expired := validAccount("u4", "t4")
	expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	noToken := Account{ID: "u5", UID: "u5"}
	acc, ok = s.pickLeastConsumed([]Account{disabled, expired, noToken, a2})
	if !ok || acc.ID != "u2" {
		t.Fatalf("应跳过不可用账号选 u2，得到 %v/%v", acc.ID, ok)
	}

	// 全不可用 → false。
	if _, ok := s.pickLeastConsumed([]Account{disabled, expired, noToken}); ok {
		t.Fatal("没有可用账号时应返回 false")
	}
}

// 权重是「今天」的：跨天要清零，不能把昨天的消耗算进今天。
func TestBumpCreditDayResetsOnNewDay(t *testing.T) {
	s := &Service{creditDayUsed: map[string]float64{}}
	s.bumpCreditDay("2026-09-10", "u1", 5)
	s.bumpCreditDay("2026-09-10", "u1", 1)
	if got := s.creditDayUsed["u1"]; got != 6 {
		t.Fatalf("同一天应累加，得到 %v", got)
	}
	s.bumpCreditDay("2026-09-11", "u2", 2)
	if got := s.creditDayUsed["u1"]; got != 0 {
		t.Errorf("跨天后昨日消耗应清零，u1 仍为 %v", got)
	}
	if got := s.creditDayUsed["u2"]; got != 2 {
		t.Errorf("新一天应重新计入，u2=%v", got)
	}
	if s.creditDay != "2026-09-11" {
		t.Errorf("creditDay 未更新: %s", s.creditDay)
	}
}

// 快照刷新不得把「还没落盘的在途增量」覆盖掉（取大合并）。
func TestRefreshCreditDaySnapshotKeepsInflight(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	day := usageDay(time.Now(), s.siteLocation(ctx))

	// 库里 u1 只有 0.1（已落盘），内存里 u1 已经有 0.9（含在途增量）。
	s.creditDayMu.Lock()
	s.creditDay = day
	s.creditDayUsed = map[string]float64{"u1": 0.9, "u2": 0.2}
	s.creditDayMu.Unlock()

	if err := s.SaveSettings(ctx, Settings{Accounts: []Account{}}); err != nil {
		t.Fatal(err)
	}
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workbuddy_usage_daily
		(day, account_id, model, requests, prompt_tokens, completion_tokens, cached_tokens, credit, updated_at)
		VALUES (?, 'u1', 'hy3', 1, 10, 1, 0, 0.1, '2026-09-10T00:00:00Z')`, day); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	s.refreshCreditDaySnapshot(ctx)

	if got := s.creditDayUsed["u1"]; got != 0.9 {
		t.Errorf("在途增量被覆盖：u1=%v want=0.9", got)
	}
	if got := s.creditDayUsed["u2"]; got != 0.2 {
		t.Errorf("库里没有的账号应保留内存值：u2=%v want=0.2", got)
	}
}

// 端到端：多账号会按消耗真的轮换。
// 首次请求两账号零消耗 → 取列表序 u1；u1 消耗后 → 第二次走 u2；再次拉平 → 又回 u1。
// 断言送到上游的 Bearer token（每个账号一个），这才是"真的换了账号"的证据。
func TestRelayBalancesAcrossAccountsByCredit(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		auths = append(auths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"c1","model":"hy3","choices":[{"delta":{"content":"ok"}}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"credit":0.03}}`,
			`data: [DONE]`,
			``,
		}, "\n"))
	}))
	defer srv.Close()
	old := upstreamBase
	upstreamBase = srv.URL
	defer func() { upstreamBase = old }()

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	call := func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/chat/completions",
			strings.NewReader(`{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`))
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("中继应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
		}
	}

	call() // 两账号零消耗 → u1
	call() // u1 已消耗 0.03 → 轮到 u2
	call() // 拉平（都 0.03）→ 回到列表序 u1

	want := []string{"Bearer t1", "Bearer t2", "Bearer t1"}
	if len(auths) != len(want) {
		t.Fatalf("上游收到 %d 次请求，want %d", len(auths), len(want))
	}
	for i := range want {
		if auths[i] != want[i] {
			t.Fatalf("第 %d 次选号错误：%q want %q（完整序列 %v）", i+1, auths[i], want[i], auths)
		}
	}

	// 消耗也确实记到了两个账号名下。
	urec := httptest.NewRecorder()
	s.ServeHTTP(urec, httptest.NewRequest(http.MethodGet, "/api/workbuddy/usage?days=1", nil))
	var payload struct {
		ByAccount []map[string]any `json:"byAccount"`
	}
	if err := json.Unmarshal(urec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.ByAccount) != 2 {
		t.Fatalf("应有两个账号的用量记录，得到 %+v", payload.ByAccount)
	}
}
