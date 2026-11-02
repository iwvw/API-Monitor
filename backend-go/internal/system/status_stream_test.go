package system

import (
	"bufio"
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
	service := &Service{statusHub: hub}

	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		service.serveStatusStream(w, r)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("open status stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}

	// 用真实 HTTP 连接读流：httptest.ResponseRecorder 不是并发安全的，
	// 在 handler goroutine 仍在写的时候从外部读 Body/Header 会被 -race 判为数据竞争。
	lines := make(chan string, 128)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	heartbeats := 0
	sawOKPayload := false
	waitHeartbeats := func(want int) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for heartbeats < want {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed after %d heartbeat events, want %d", heartbeats, want)
				}
				if line == "event: heartbeat" {
					heartbeats++
				}
				if strings.Contains(line, `"status":"ok"`) {
					sawOKPayload = true
				}
			case <-deadline:
				t.Fatalf("timed out waiting for heartbeat #%d, got %d", want, heartbeats)
			}
		}
	}

	waitHeartbeats(1)
	hub.broadcast()
	waitHeartbeats(2)

	if !sawOKPayload {
		t.Fatal(`heartbeat payload missing "status":"ok"`)
	}

	resp.Body.Close()
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not stop after client disconnect")
	}
}
