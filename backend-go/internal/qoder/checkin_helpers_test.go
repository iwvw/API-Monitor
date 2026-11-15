package qoder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdentityStablePerAccount(t *testing.T) {
	a := Account{ID: "uid-1", AccessToken: "tok"}
	b := Account{ID: "uid-1", AccessToken: "tok"}
	c := Account{ID: "uid-2", AccessToken: "tok"}

	ia, ib, ic := identityForAccount(a), identityForAccount(b), identityForAccount(c)
	if ia.MachineID != ib.MachineID || ia.MachineToken != ib.MachineToken || ia.MachineType != ib.MachineType {
		t.Fatalf("same account must derive same identity: %+v vs %+v", ia, ib)
	}
	if ia.MachineID == ic.MachineID || ia.MachineToken == ic.MachineToken {
		t.Fatalf("different accounts must not share identity")
	}
	if ia.modernProtocol() {
		t.Fatalf("derived identity must stay on legacy protocol")
	}
	if len(ia.MachineType) != 18 {
		t.Fatalf("machine type should be 18 hex chars, got %q", ia.MachineType)
	}
	if len(ia.MachineToken) != 43 {
		t.Fatalf("machine token should be 43 chars, got %d", len(ia.MachineToken))
	}
	if !strings.Contains(ia.MachineID, "-") {
		t.Fatalf("machine id should be UUID form, got %q", ia.MachineID)
	}
}

func TestIdentityRealFingerprintSwitchesProtocol(t *testing.T) {
	acc := Account{ID: "u1", CheckinFingerprint: &CheckinFingerprint{
		MachineID:    "mid",
		MachineToken: "mtok",
		MachineType:  "mtype",
	}}
	id := identityForAccount(acc)
	if !id.Real || !id.modernProtocol() {
		t.Fatalf("configured fingerprint must enable modern protocol: %+v", id)
	}
	if id.ClientType != newClientType {
		t.Fatalf("modern protocol client type wrong: %+v", id)
	}
	// 版本优先取该区域真实客户端版本（本机实测国际版 0.4.3、国内版 0.4.8）。
	if id.Version != regionConfigFor(regionCN).clientVersion {
		t.Fatalf("version should fall back to region client version, got %q", id.Version)
	}
	if id.DataPolicy != "agree" || id.Product != "app" || id.Scene != "app" {
		t.Fatalf("modern protocol policy defaults wrong: %+v", id)
	}

	// 指纹里显式给了 version 时必须优先使用它（抓包的客户端版本可能比默认值准）。
	acc.CheckinFingerprint.Version = "9.9.9"
	if got := identityForAccount(acc).Version; got != "9.9.9" {
		t.Fatalf("explicit fingerprint version should win, got %q", got)
	}
	// machine_os 也应尊重指纹给出的值。
	acc.CheckinFingerprint.MachineOS = "x86_64_win32"
	if got := identityForAccount(acc).MachineOS; got != "x86_64_win32" {
		t.Fatalf("explicit machine os should win, got %q", got)
	}
}

func TestCheckinHeadersDiffersByRegion(t *testing.T) {
	cn := Account{ID: "u1", AccessToken: "tok", Region: regionCN}
	headers := checkinExtraHeaders(&cn, identityForAccount(cn), true)
	if headers["cosy-machinetoken"] != "" || headers["cosy-machineid"] != "" {
		t.Fatalf("CN must not send machine headers, got %v", headers)
	}

	intl := Account{ID: "u2", AccessToken: "tok", Region: regionIntl}
	headers = checkinExtraHeaders(&intl, identityForAccount(intl), true)
	if headers["cosy-machinetoken"] != "" || headers["cosy-machineid"] != "" {
		t.Fatalf("intl without real fingerprint must not send machine headers, got %v", headers)
	}

	intl.CheckinFingerprint = &CheckinFingerprint{MachineID: "mid", MachineToken: "mtok", MachineType: "mt"}
	full := checkinExtraHeaders(&intl, identityForAccount(intl), true)
	if full["cosy-machinetoken"] != "mtok" || full["cosy-machineid"] != "mid" {
		t.Fatalf("intl with fingerprint must send machine headers, got %v", full)
	}
	partial := checkinExtraHeaders(&intl, identityForAccount(intl), false)
	if partial["cosy-machinetoken"] != "" {
		t.Fatalf("fullDevice=false must omit machine token, got %v", partial)
	}
	if partial["cosy-machineid"] != "mid" {
		t.Fatalf("fullDevice=false should still send machine id, got %v", partial)
	}
}

func TestCheckinNeedsFingerprint(t *testing.T) {
	if checkinNeedsFingerprint(Account{Region: regionCN}, deviceIdentity{}) {
		t.Fatalf("CN does not require fingerprint")
	}
	if !checkinNeedsFingerprint(Account{Region: regionIntl}, deviceIdentity{}) {
		t.Fatalf("intl requires real fingerprint")
	}
	if checkinNeedsFingerprint(Account{Region: regionIntl}, deviceIdentity{Real: true}) {
		t.Fatalf("intl with real fingerprint must be allowed")
	}
}

// TestDailyCheckinIntlWithoutFingerprintIsExplicit 验证：国际版无指纹、且上游
// 确实没有下发 CLAIM_BENEFIT 时，返回 need_fingerprint 并给出可操作说明。
func TestDailyCheckinIntlWithoutFingerprintIsExplicit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只下发 VIEW_DETAILS，没有 CLAIM_BENEFIT：这是「缺指纹导致活动被隐藏」的形态。
		_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"VIEW_DETAILS","claimStatus":"CLAIMABLE"}]}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	res, err := s.DailyCheckin(context.Background(), Account{ID: "u1", AccessToken: "tok", Region: regionIntl})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "need_fingerprint" {
		t.Fatalf("status = %q, want need_fingerprint", res.Status)
	}
	if !strings.Contains(res.Message, "指纹") {
		t.Fatalf("message should explain fingerprint requirement: %q", res.Message)
	}
}

// TestDailyCheckinIntlLazyDeliveryWithoutFingerprint 守住「懒下发」：
// 国际版账号已被官方客户端领取过时，即使不带指纹也能查到 CLAIMED 活动，
// 此时必须报 already 而不是 need_fingerprint（实测 2026-11-15 确认）。
func TestDailyCheckinIntlLazyDeliveryWithoutFingerprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED","startAt":1761261600}]}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	res, err := s.DailyCheckin(context.Background(), Account{ID: "u1", AccessToken: "tok", Region: regionIntl})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "already" {
		t.Fatalf("status = %q, want already (lazy delivery)", res.Status)
	}
	if res.Fingerprint {
		t.Fatalf("fingerprint flag should stay false here")
	}
}

func TestDailyCheckinIntlWithFingerprintSendsMachineHeaders(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		switch {
		case r.URL.Path == "/sash/api/v1/me/campaigns":
			_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE","startAt":1761261600,"benefit":{"amount":100}}]}`))
		case strings.HasSuffix(r.URL.Path, "/claim"):
			_, _ = w.Write([]byte(`{"status":"CLAIMED","grantId":"g1","benefit":{"amount":100}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	acc := Account{ID: "u1", AccessToken: "tok", Region: regionIntl, CheckinFingerprint: &CheckinFingerprint{
		MachineID: "mid", MachineToken: "mtok", MachineType: "mtype",
	}}
	res, err := s.DailyCheckin(context.Background(), acc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "success" || res.Gained != 100 {
		t.Fatalf("result = %+v", res)
	}
	if seen.Get("cosy-machinetoken") != "mtok" || seen.Get("cosy-machineid") != "mid" {
		t.Fatalf("intl checkin must send machine headers, got %v", seen)
	}
	if res.WindowDate != unixToDate(1761261600) {
		t.Fatalf("window date = %q", res.WindowDate)
	}
}

func TestDailyCheckinCNOmitsMachineHeaders(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = w.Write([]byte(`{"campaigns":[{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED","startAt":1761261600}]}`))
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
	if seen.Get("cosy-machinetoken") != "" || seen.Get("cosy-machineid") != "" {
		t.Fatalf("CN checkin must not send machine headers, got %v", seen)
	}
}

func TestCalcLocalCheckinStats(t *testing.T) {
	today := todayIn(cstZone)
	yesterday := shiftDay(today, -1)
	twoDays := shiftDay(today, -2)
	recs := []checkinRecord{
		{Date: twoDays, Amount: 100},
		{Date: yesterday, Amount: 100},
		{Date: today, Amount: 50},
	}
	streak, total, credits := calcLocalCheckinStats(recs)
	if streak != 3 || total != 3 || credits != 250 {
		t.Fatalf("stats = %d %d %g", streak, total, credits)
	}
	// 今天未领：从昨天往前数，当天未领不算断链。
	streak, total, _ = calcLocalCheckinStats(recs[:2])
	if streak != 2 || total != 2 {
		t.Fatalf("streak should continue from yesterday, got %d %d", streak, total)
	}
	if s, _, _ := calcLocalCheckinStats(nil); s != 0 {
		t.Fatalf("empty history streak = %d", s)
	}
}

func TestRecordCheckinIdempotent(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	date := todayIn(cstZone)
	s.recordCheckin(ctx, "u1", date, 100)
	s.recordCheckin(ctx, "u1", date, 100)
	recs := s.loadCheckinHistory(ctx, "u1")
	if len(recs) != 1 || recs[0].Amount != 100 {
		t.Fatalf("records = %+v", recs)
	}
	// 金额 0 不覆盖已有非 0 记录。
	s.recordCheckin(ctx, "u1", date, 0)
	recs = s.loadCheckinHistory(ctx, "u1")
	if recs[0].Amount != 100 {
		t.Fatalf("zero amount must not overwrite: %+v", recs)
	}
}

func TestNormalizeCheckinTimes(t *testing.T) {
	got := normalizeCheckinTimes([]string{" 09:15 ", "17:00", "9:15", "25:00", "", "bad"})
	want := []string{"09:15", "17:00"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(normalizeCheckinTimes(nil)) != 0 {
		t.Fatalf("nil input should normalize to empty slice")
	}
	st := Settings{}
	if times := st.checkinTimes(); len(times) != 1 || times[0] != "10:00" {
		t.Fatalf("default checkin times = %v", times)
	}
}

func TestBuildCronSpec(t *testing.T) {
	if got := buildCronSpec(9, 15); got != "15 09 * * *" {
		t.Fatalf("cron spec = %q", got)
	}
	if got := buildCronSpec(0, 0); got != "00 00 * * *" {
		t.Fatalf("cron spec = %q", got)
	}
}

func TestStaggerOffsetStableAndBounded(t *testing.T) {
	window := 60 * time.Second
	a := staggerOffset("acct-1", window)
	b := staggerOffset("acct-1", window)
	if a != b {
		t.Fatalf("stagger offset must be stable, got %v vs %v", a, b)
	}
	if a < -window || a > window {
		t.Fatalf("stagger offset out of window: %v", a)
	}
	diff := false
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if staggerOffset(id, window) != staggerOffset("a", window) {
			diff = true
			break
		}
	}
	if !diff {
		t.Fatalf("different accounts should not all share the same offset")
	}
	if staggerOffset("acct-1", 0) != 0 {
		t.Fatalf("zero window must yield zero offset")
	}
}

func TestCampaignWindowHint(t *testing.T) {
	yesterday := shiftDay(todayIn(cstZone), -1)
	hint := campaignWindowHint(yesterday, "今天已签到")
	if !strings.Contains(hint, "窗口额度") || !strings.Contains(hint, "10:00") {
		t.Fatalf("hint = %q", hint)
	}
	if got := campaignWindowHint(todayIn(cstZone), "今天已签到"); got != "今天已签到" {
		t.Fatalf("same-day hint should pass through, got %q", got)
	}
	if got := campaignWindowHint("", "今天已签到"); got != "今天已签到" {
		t.Fatalf("empty window should pass through, got %q", got)
	}
}

func TestFetchQuotaParsesDedicatedPackages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userType":"personal_pro","userQuota":{"total":300,"used":28,"remaining":272,"unit":"credits"},
			"dedicatedResourcePackages":[{"id":"sota","name":"SOTA 专属积分","total":2000,"used":0,"remaining":2000,"unit":"credits","expiresAt":1761261600000,"available":true}]}`))
	}))
	defer srv.Close()
	withMockOpenAPI(t, srv)

	s := newTestService(t)
	q, err := s.FetchQuota(context.Background(), Account{ID: "u1", AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Remaining != 272 || len(q.Packages) != 1 {
		t.Fatalf("quota = %+v", q)
	}
	if q.Packages[0].Remaining != 2000 || q.Packages[0].ExpiresAt != 1761261600 {
		t.Fatalf("package = %+v", q.Packages[0])
	}
	// 专属包不并入通用额度合计，避免被当成通用余额。
	if q.Limit != 300 {
		t.Fatalf("limit should exclude dedicated packages, got %g", q.Limit)
	}
}

func TestPickClaimableCampaigns(t *testing.T) {
	targets, already := pickClaimableCampaigns([]campaign{
		{ActionType: "VIEW_DETAILS", ClaimStatus: "CLAIMABLE"},
		{ActionType: "CLAIM_BENEFIT", ClaimStatus: "claimable"},
		{ActionType: "CLAIM_BENEFIT", ClaimStatus: "CLAIMED"},
	})
	if len(targets) != 1 || !already {
		t.Fatalf("targets=%d already=%v", len(targets), already)
	}
}

func TestNeedsReauth(t *testing.T) {
	if !needsReauth(Account{}) {
		t.Fatalf("account without token needs reauth")
	}
	if needsReauth(Account{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour).Unix()}) {
		t.Fatalf("valid token should not need reauth")
	}
	if !needsReauth(Account{AccessToken: "tok", ExpiresAt: time.Now().Add(-time.Hour).Unix()}) {
		t.Fatalf("expired token without refresh needs reauth")
	}
	if needsReauth(Account{AccessToken: "tok", ExpiresAt: time.Now().Add(-time.Hour).Unix(), RefreshToken: "r"}) {
		t.Fatalf("expired token with refresh can be renewed")
	}
}
