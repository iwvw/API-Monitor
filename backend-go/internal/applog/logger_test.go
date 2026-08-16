package applog

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPruneRotatedLogsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "app.log")
	previous := logPath
	logPath = current
	defer func() { logPath = previous }()

	for i := 0; i < 15; i++ {
		name := filepath.Join(dir, fmt.Sprintf("app-2026080%d-%02d0000.log", 1+i/10, i%10))
		if err := os.WriteFile(name, []byte("line\n"), 0o644); err != nil {
			t.Fatalf("write rotated file %s: %v", name, err)
		}
		mt := time.Date(2026, 8, 1+i, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(name, mt, mt); err != nil {
			t.Fatalf("set mtime %s: %v", name, err)
		}
	}
	// 当前 app.log 不应被误删
	if err := os.WriteFile(current, []byte("current\n"), 0o644); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	pruneRotatedLogs()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	rotated := 0
	for _, entry := range entries {
		if entry.Name() != "app.log" && strings.HasPrefix(entry.Name(), "app-") {
			rotated++
		}
	}
	if rotated != maxRotatedLogFiles {
		t.Fatalf("rotated files after prune = %d, want %d", rotated, maxRotatedLogFiles)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current app.log must be preserved, stat error: %v", err)
	}
	// 剩余文件应是最新的（时间最大的）那一批
	var newest string
	for i := 0; i < 15; i++ {
		name := filepath.Join(dir, fmt.Sprintf("app-2026080%d-%02d0000.log", 1+i/10, i%10))
		if _, err := os.Stat(name); err == nil {
			newest = name
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app-20260801-000000.log")); err == nil {
		t.Fatalf("oldest rotated file should have been pruned")
	}
	if newest == "" {
		t.Fatalf("expected at least one newest rotated file to survive")
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
