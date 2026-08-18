package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusHubSubscribeBroadcast(t *testing.T) {
	hub := newStatusHub()
	ch, unsubscribe := hub.subscribe()
	defer unsubscribe()

	hub.broadcast()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not reach subscriber")
	}

	unsubscribe()
	hub.broadcast()
	select {
	case <-ch:
		t.Fatal("unsubscribed channel still received broadcast")
	default:
	}
}

func TestStatusStreamWritesHeartbeats(t *testing.T) {
	hub := newStatusHub()
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/system/status/stream", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		(&Service{statusHub: hub}).serveStatusStream(rec, req)
		close(done)
	}()

	waitFor := func(substr string) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(rec.Body.String(), substr) {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}

	deadlineHeader := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadlineHeader) {
		if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatal("missing event-stream content type")
	}
	if !waitFor("event: heartbeat") {
		t.Fatal("initial heartbeat not written")
	}

	hub.broadcast()
	if !waitFor(`"status":"ok"`) {
		t.Fatal("broadcast heartbeat payload not written")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not stop after context cancel")
	}
}