package applog

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestMiddlewarePreservesWebSocketHijacker(t *testing.T) {
	var buf lockedBuf
	previous := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer func() {
		SetLogger(previous)
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		if err := conn.WriteMessage(websocket.TextMessage, []byte("ok")); err != nil {
			t.Errorf("write message: %v", err)
		}
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket through middleware: %v", err)
	}
	defer conn.Close()

	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if string(message) != "ok" {
		t.Fatalf("message = %q, want ok", message)
	}

	// 服务端 handler 在独立 goroutine 返回，defer 日志与测试结束存在竞态；
	// 等待日志落盘到本测试的 buffer，避免其延迟写入后续测试的日志 buffer。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), `"msg":"http request"`) {
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMiddlewareSkipsCodexDiscoveryNoise(t *testing.T) {
	var buf bytes.Buffer
	previous := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer func() {
		SetLogger(previous)
	}()

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("User-Agent", "Codex local server discovery")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Fatalf("expected discovery request to be skipped, got log %q", got)
	}
}

func TestMiddlewareSkipsSuccessfulSystemLogStreamRequest(t *testing.T) {
	var buf bytes.Buffer
	previous := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer func() {
		SetLogger(previous)
	}()

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs/stream?limit=500", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Fatalf("expected system log stream request to be skipped, got log %q", got)
	}
}

func TestMiddlewareLogsFailedSystemLogStreamRequest(t *testing.T) {
	var buf bytes.Buffer
	previous := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer func() {
		SetLogger(previous)
	}()

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs/stream?limit=500", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	logLine := buf.String()
	if !strings.Contains(logLine, "\"path\":\"/api/system/logs/stream\"") {
		t.Fatalf("expected failed system log stream request log, got %q", logLine)
	}
}

func TestMiddlewareLogsNormalAPIRequest(t *testing.T) {
	var buf bytes.Buffer
	previous := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer func() {
		SetLogger(previous)
	}()

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/server/accounts", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	logLine := buf.String()
	if !strings.Contains(logLine, "\"path\":\"/api/server/accounts\"") {
		t.Fatalf("expected normal api request log, got %q", logLine)
	}
}
