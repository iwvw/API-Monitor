package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServiceWithAccount(t *testing.T) (*Service, Account) {
	t.Helper()
	s := newTestService(t)
	acc := Account{ID: "u1", Nickname: "tester", AccessToken: "tok", Region: regionCN}
	if err := s.upsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	return s, acc
}

func doJSON(t *testing.T, s *Service, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestSettingsPutNormalizesCheckinTimes(t *testing.T) {
	s := newTestService(t)
	body := `{"enabled":true,"accountStrategy":"round-robin","autoCheckin":true,` +
		`"autoCheckinTimes":["9:15"," 17:00 ","25:00",""],"autoRefreshToken":false}`
	rec := doJSON(t, s, http.MethodPut, "/api/qoder/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	st := s.Settings()
	if len(st.AutoCheckinTimes) != 2 || st.AutoCheckinTimes[0] != "09:15" || st.AutoCheckinTimes[1] != "17:00" {
		t.Fatalf("times = %v", st.AutoCheckinTimes)
	}
	if st.autoRefreshEnabled() {
		t.Fatalf("autoRefreshToken=false must be persisted")
	}
	if st.AccountStrategy != "round-robin" {
		t.Fatalf("strategy = %q", st.AccountStrategy)
	}

	// 下发视图要把规范化后的时刻与开关一并给出，前端才不会再写坏。
	var payload struct {
		Success  bool `json:"success"`
		Settings struct {
			AutoCheckinTimes   []string `json:"autoCheckinTimes"`
			AutoRefreshToken   bool     `json:"autoRefreshToken"`
			CheckinStagger     bool     `json:"checkinStagger"`
			CheckinStaggerSec  int      `json:"checkinStaggerSec"`
			CheckinGapMs       int      `json:"checkinGapMs"`
			CheckinFullMachine bool     `json:"checkinFullDeviceHeaders"`
		} `json:"settings"`
	}
	rec = doJSON(t, s, http.MethodGet, "/api/qoder/settings", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Settings.CheckinStagger || payload.Settings.CheckinStaggerSec != 60 {
		t.Fatalf("stagger defaults missing: %+v", payload.Settings)
	}
	if payload.Settings.CheckinGapMs != 1000 {
		t.Fatalf("gap default = %d", payload.Settings.CheckinGapMs)
	}
	if !payload.Settings.CheckinFullMachine {
		t.Fatalf("full device headers should default to true")
	}
}

func TestUpdateAccountFingerprint(t *testing.T) {
	s, _ := newTestServiceWithAccount(t)

	body := `{"nickname":"改过的","region":"intl","checkinFingerprint":` +
		`{"machine_id":" mid ","machine_token":" mtok ","machine_type":" mtype "}}`
	rec := doJSON(t, s, http.MethodPut, "/api/qoder/accounts/u1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	acc, ok := s.findAccount("u1")
	if !ok {
		t.Fatal("account missing")
	}
	if acc.Nickname != "改过的" || acc.Region != regionIntl {
		t.Fatalf("account = %+v", acc)
	}
	if acc.CheckinFingerprint == nil || acc.CheckinFingerprint.MachineToken != "mtok" {
		t.Fatalf("fingerprint = %+v", acc.CheckinFingerprint)
	}
	if acc.CheckinFingerprint.MachineID != "mid" {
		t.Fatalf("fingerprint fields should be trimmed: %+v", acc.CheckinFingerprint)
	}

	// 视图只下发布尔标记，不回显指纹内容。
	list := doJSON(t, s, http.MethodGet, "/api/qoder/accounts", "")
	if strings.Contains(list.Body.String(), "mtok") {
		t.Fatalf("account view must not leak fingerprint: %s", list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"fingerprint":true`) {
		t.Fatalf("view should flag fingerprint: %s", list.Body.String())
	}

	// 缺 machine_token 直接拒绝，避免存下无效指纹（国际版签到仍会失败）。
	rec = doJSON(t, s, http.MethodPut, "/api/qoder/accounts/u1", `{"checkinFingerprint":{"machine_id":"x"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing machine_token should be rejected, got %d", rec.Code)
	}

	// clearFingerprint 显式清空。
	rec = doJSON(t, s, http.MethodPut, "/api/qoder/accounts/u1", `{"clearFingerprint":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	acc, _ = s.findAccount("u1")
	if acc.CheckinFingerprint != nil {
		t.Fatalf("fingerprint should be cleared: %+v", acc.CheckinFingerprint)
	}
}

func TestCheckinHistoryEndpoint(t *testing.T) {
	s, _ := newTestServiceWithAccount(t)
	ctx := context.Background()
	today := todayIn(cstZone)
	s.recordCheckin(ctx, "u1", shiftDay(today, -1), 100)
	s.recordCheckin(ctx, "u1", today, 100)

	rec := doJSON(t, s, http.MethodGet, "/api/qoder/accounts/u1/checkin-history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Success      bool `json:"success"`
		StreakDays   int  `json:"streakDays"`
		TotalDays    int  `json:"totalDays"`
		TotalCredits int  `json:"totalCredits"`
		Records      []struct {
			Date   string  `json:"date"`
			Amount float64 `json:"amount"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Success || out.StreakDays != 2 || out.TotalDays != 2 || out.TotalCredits != 200 {
		t.Fatalf("history = %+v", out)
	}
	if len(out.Records) != 2 || out.Records[0].Date != today {
		t.Fatalf("records should be newest-first: %+v", out.Records)
	}
}

func TestCheckinHistoryEndpointUnknownAccount(t *testing.T) {
	s := newTestService(t)
	rec := doJSON(t, s, http.MethodGet, "/api/qoder/accounts/nope/checkin-history", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
