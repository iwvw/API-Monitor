package serveragent

import (
	"testing"
	"time"
)

func TestPtyDataHubRoutesDataBySessionID(t *testing.T) {
	hub := newPtyDataHub()
	chA, cancelA := hub.Subscribe("pty-a")
	defer cancelA()
	chB, cancelB := hub.Subscribe("pty-b")
	defer cancelB()

	if !hub.Publish("pty-a", "hello") {
		t.Fatalf("expected publish to report delivery")
	}

	select {
	case got := <-chA:
		if got != "hello" {
			t.Fatalf("got %q, want hello", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for pty-a data")
	}

	select {
	case got := <-chB:
		t.Fatalf("pty-b received unrelated data: %q", got)
	default:
	}
}

func TestPtyDataHubUnsubscribeClosesChannel(t *testing.T) {
	hub := newPtyDataHub()
	ch, cancel := hub.Subscribe("pty-a")

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for channel close")
	}

	if hub.Publish("pty-a", "ignored") {
		t.Fatalf("expected publish after unsubscribe to report no delivery")
	}
}
