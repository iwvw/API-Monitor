package responsehistory

import (
	"testing"
	"time"
)

func TestProgressPersistDueCoalescesFrequentUpdates(t *testing.T) {
	base := time.Unix(100, 0)
	if progressPersistDue(base.Add(500*time.Millisecond), base) {
		t.Fatal("expected frequent progress update to be coalesced")
	}
	if !progressPersistDue(base.Add(time.Second), base) {
		t.Fatal("expected progress update after one second")
	}
}

func TestProgressPersistDueAllowsInitialUpdate(t *testing.T) {
	if !progressPersistDue(time.Unix(100, 0), time.Time{}) {
		t.Fatal("expected initial progress update")
	}
}
