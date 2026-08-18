package serveragent

import (
	"testing"
	"time"
)

func testPresencePolicyManager() *agentPresenceManager {
	return &agentPresenceManager{
		startedAt: time.Now().Add(-time.Hour),
		records:   make(map[string]*agentPresenceRecord),
		cfg: agentPresenceConfig{
			offlineAfter: 180 * time.Second,
			startupGrace: 0,
		},
	}
}

func TestOfflineAfterForUsesConfiguredDefaultWithoutSampleInterval(t *testing.T) {
	p := testPresencePolicyManager()
	for _, interval := range []int64{0, -1} {
		rec := &agentPresenceRecord{SampleIntervalMs: interval}
		if got := p.offlineAfterFor(rec); got != p.cfg.offlineAfter {
			t.Fatalf("sample=%d offlineAfter=%v, want %v", interval, got, p.cfg.offlineAfter)
		}
	}
}

func TestOfflineAfterForKeepsConfiguredWhenDynamicIsSmaller(t *testing.T) {
	p := testPresencePolicyManager()
	rec := &agentPresenceRecord{SampleIntervalMs: 10_000}
	if got := p.offlineAfterFor(rec); got != p.cfg.offlineAfter {
		t.Fatalf("offlineAfter=%v, want configured %v", got, p.cfg.offlineAfter)
	}
}

func TestOfflineAfterForScalesWithSampleInterval(t *testing.T) {
	p := testPresencePolicyManager()
	rec := &agentPresenceRecord{SampleIntervalMs: 60_000}
	if got := p.offlineAfterFor(rec); got != 6*time.Minute {
		t.Fatalf("offlineAfter=%v, want 6 minutes", got)
	}
}

func TestOfflineAfterForCapsSampleInterval(t *testing.T) {
	p := testPresencePolicyManager()
	rec := &agentPresenceRecord{SampleIntervalMs: maxAcceptableAgentSampleIntervalMs}
	if got := p.offlineAfterFor(rec); got != 30*time.Minute {
		t.Fatalf("offlineAfter=%v, want 30 minutes", got)
	}
	rec.SampleIntervalMs = maxAcceptableAgentSampleIntervalMs * 100
	if got := p.offlineAfterFor(rec); got != 30*time.Minute {
		t.Fatalf("overflow sample offlineAfter=%v, want capped 30 minutes", got)
	}
}

func TestNotificationsSuppressedWithinStartupGrace(t *testing.T) {
	p := testPresencePolicyManager()
	p.startedAt = time.Now()
	p.cfg.startupGrace = 5 * time.Minute
	if !p.notificationsSuppressed("any-server", time.Now()) {
		t.Fatal("notifications should be suppressed during startup grace")
	}
}

func TestNotificationsSuppressedOnlyInsideSuppressWindow(t *testing.T) {
	p := testPresencePolicyManager()
	p.mu.Lock()
	p.records["suppressed"] = &agentPresenceRecord{
		ServerID:      "suppressed",
		SuppressUntil: time.Now().Add(time.Minute),
	}
	p.records["expired"] = &agentPresenceRecord{
		ServerID:      "expired",
		SuppressUntil: time.Now().Add(-time.Minute),
	}
	p.records["never"] = &agentPresenceRecord{ServerID: "never"}
	p.mu.Unlock()
	if !p.notificationsSuppressed("suppressed", time.Now()) {
		t.Fatal("record inside suppress window should be suppressed")
	}
	if p.notificationsSuppressed("expired", time.Now()) {
		t.Fatal("record with expired suppress window should notify")
	}
	if p.notificationsSuppressed("never", time.Now()) {
		t.Fatal("record without suppress window should notify")
	}
}

func TestNotificationsSuppressedWithoutRecord(t *testing.T) {
	p := testPresencePolicyManager()
	if p.notificationsSuppressed("missing-server", time.Now()) {
		t.Fatal("unknown server should not be suppressed")
	}
}
