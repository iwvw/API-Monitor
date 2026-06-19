package applog

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestMiddlewarePreservesWebSocketHijacker(t *testing.T) {
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
