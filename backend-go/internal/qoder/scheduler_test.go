package qoder

import (
	"testing"
	"time"
)

func TestCheckedInToday(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Now()
	if checkedInToday(Account{}, loc) {
		t.Fatalf("account without lastCheckinAt should not count as checked in")
	}
	if !checkedInToday(Account{LastCheckinAt: now.UTC().Format(time.RFC3339)}, loc) {
		t.Fatalf("today (UTC storage) should count as checked in")
	}
	old := now.Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	if checkedInToday(Account{LastCheckinAt: old}, loc) {
		t.Fatalf("3 days ago should not count as checked in today")
	}
	if checkedInToday(Account{LastCheckinAt: "not-a-time"}, loc) {
		t.Fatalf("unparsable timestamp should not count as checked in")
	}
}

func TestSlotAccountDoneMemo(t *testing.T) {
	s := newTestService(t)
	key := doneSlot{date: "2026-11-15", time: "10:00"}
	if s.slotAccountDone(key, "a") {
		t.Fatalf("fresh slot should have no finished account")
	}
	s.markSlotAccountDone(key, "a")
	if !s.slotAccountDone(key, "a") {
		t.Fatalf("marked account should be reported done")
	}
	if s.slotAccountDone(key, "b") {
		t.Fatalf("other account should not be done")
	}
	// 时段完成后清掉逐账号记录，避免内存随时间累积。
	s.markSlotDone(key)
	if s.slotAccountDone(key, "a") {
		t.Fatalf("slot completion should clear per-account memo")
	}
}

func TestSettingsDefaults(t *testing.T) {
	st := Settings{}
	if !st.autoCheckinEnabled() || !st.autoRefreshEnabled() || !st.checkinStaggerEnabled() {
		t.Fatalf("switches should default to enabled")
	}
	if st.checkinGap() != time.Second || st.checkinJitter() != 500*time.Millisecond {
		t.Fatalf("gap/jitter defaults = %v / %v", st.checkinGap(), st.checkinJitter())
	}
	if st.checkinStaggerWindow() != 60*time.Second {
		t.Fatalf("stagger default = %v", st.checkinStaggerWindow())
	}
	off := false
	st.AutoCheckin = &off
	st.AutoRefreshToken = &off
	st.CheckinStagger = &off
	if st.autoCheckinEnabled() || st.autoRefreshEnabled() || st.checkinStaggerEnabled() {
		t.Fatalf("explicit false must win over defaults")
	}
	st.CheckinGapMS = 250
	st.CheckinJitterMS = 100
	st.CheckinStaggerS = 30
	if st.checkinGap() != 250*time.Millisecond || st.checkinJitter() != 100*time.Millisecond || st.checkinStaggerWindow() != 30*time.Second {
		t.Fatalf("configured values not applied")
	}
}

func TestFullDeviceHeadersSwitch(t *testing.T) {
	st := Settings{}
	if !st.checkinFullDeviceHeaders() {
		t.Fatalf("full device headers should default to true (intl needs them)")
	}
	off := false
	st.CheckinFullDeviceHeaders = &off
	if st.checkinFullDeviceHeaders() {
		t.Fatalf("explicit false must disable full device headers")
	}
}
