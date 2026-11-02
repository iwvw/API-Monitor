package httpcompress

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// SSE 响应应被识别为流并尝试续期写超时（ResponseRecorder 不支持
// SetWriteDeadline 时静默忽略，不影响写入）。
func TestSSEDeadlineWriterDetectsStream(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &sseDeadlineWriter{ResponseWriter: rec, rc: http.NewResponseController(rec)}
	sw.Header().Set("Content-Type", "text/event-stream")
	if n, err := sw.Write([]byte("data: hello\n\n")); err != nil || n != 13 {
		t.Fatalf("SSE write = %d, %v; want 13, nil", n, err)
	}
	if !sw.isStream {
		t.Fatal("text/event-stream 应被识别为流式响应")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
}

// 非 SSE 响应原样透传，不触发续期。
func TestSSEDeadlineWriterNonStream(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &sseDeadlineWriter{ResponseWriter: rec, rc: http.NewResponseController(rec)}
	sw.Header().Set("Content-Type", "application/json")
	if _, err := sw.Write([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if sw.isStream {
		t.Fatal("application/json 不应被识别为流式响应")
	}
}

// Hijack 必须转发到底层：WebSocket/Engine.IO 等长连接都经过本中间件链，
// 若 sseDeadlineWriter 挡住 Hijack 会导致连接升级失败。
func TestSSEDeadlineWriterHijack(t *testing.T) {
	wrapped := &hijackableWriter{header: http.Header{}}
	sw := &sseDeadlineWriter{ResponseWriter: wrapped, rc: http.NewResponseController(wrapped)}
	conn, rw, err := sw.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	if !wrapped.hijacked || conn != nil || rw != nil {
		t.Fatalf("hijack = %v/%v/%v", wrapped.hijacked, conn, rw)
	}
}

func TestSSEDeadlineWriterHijackNotSupported(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &sseDeadlineWriter{ResponseWriter: rec, rc: http.NewResponseController(rec)}
	if _, _, err := sw.Hijack(); err != http.ErrNotSupported {
		t.Fatalf("hijack err = %v, want ErrNotSupported", err)
	}
}
