package httpcompress

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipDecompress(t *testing.T, data []byte) string {
	t.Helper()
	reader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return string(decoded)
}

func compressTestHandler(body string, headers map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, body)
	})
}

func requestWithGzip(handler http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestCompressibleResponseIsGzipCompressed(t *testing.T) {
	body := strings.Repeat("可压缩的文本内容 compressible text ", 100)
	recorder := requestWithGzip(Middleware(compressTestHandler(body, map[string]string{"Content-Type": "text/plain; charset=utf-8"})))

	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", recorder.Header().Get("Content-Encoding"))
	}
	if recorder.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary = %q", recorder.Header().Get("Vary"))
	}
	if got := gzipDecompress(t, recorder.Body.Bytes()); got != body {
		t.Fatalf("decoded body mismatch, got %d bytes want %d", len(got), len(body))
	}
}

func TestContentLengthRemovedWhenCompressed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	})
	recorder := requestWithGzip(Middleware(handler))
	if recorder.Header().Get("Content-Length") != "" {
		t.Fatalf("Content-Length = %q, want removed", recorder.Header().Get("Content-Length"))
	}
	if got := gzipDecompress(t, recorder.Body.Bytes()); got != `{"ok":true}` {
		t.Fatalf("decoded body = %q", got)
	}
}

func TestCompressibleContentTypes(t *testing.T) {
	cases := []struct {
		contentType string
		compress    bool
	}{
		{"text/plain", true},
		{"text/html; charset=utf-8", true},
		{"application/json", true},
		{"application/javascript", true},
		{"application/xml; charset=utf-8", true},
		{"application/manifest+json", true},
		{"image/svg+xml", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"font/woff2", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			recorder := requestWithGzip(Middleware(compressTestHandler("data", map[string]string{"Content-Type": tc.contentType})))
			if tc.compress {
				if recorder.Header().Get("Content-Encoding") != "gzip" {
					t.Fatalf("expected gzip for %q", tc.contentType)
				}
			} else if recorder.Header().Get("Content-Encoding") != "" {
				t.Fatalf("unexpected gzip for %q", tc.contentType)
			}
		})
	}
}

func TestAcceptEncodingWithoutGzipSkipsCompression(t *testing.T) {
	for _, accept := range []string{"br", "deflate", ""} {
		handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := w.(*gzipResponseWriter); ok {
				t.Errorf("handler wrapped for Accept-Encoding %q", accept)
			}
			io.WriteString(w, "body")
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", accept)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Header().Get("Content-Encoding") != "" {
			t.Fatalf("unexpected Content-Encoding for %q", accept)
		}
	}
}

func TestSSEStreamIsNotCompressed(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			io.WriteString(w, "data: hello\n\n")
			flusher.Flush()
		}
	}))
	recorder := requestWithGzip(handler)
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatalf("SSE must not be compressed, got %q", recorder.Header().Get("Content-Encoding"))
	}
	if got := recorder.Body.String(); got != "data: hello\n\n" {
		t.Fatalf("SSE body = %q", got)
	}
}

func TestAlreadyEncodedResponseNotDoubleCompressed(t *testing.T) {
	recorder := requestWithGzip(Middleware(compressTestHandler("bytes", map[string]string{
		"Content-Type":     "text/plain",
		"Content-Encoding": "br",
	})))
	if recorder.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding = %q, want br", recorder.Header().Get("Content-Encoding"))
	}
	if recorder.Body.String() != "bytes" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestBodyAllowedForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{100, false}, {101, false}, {199, false},
		{200, true}, {204, false}, {304, false},
		{404, true}, {500, true},
	}
	for _, tc := range cases {
		if got := bodyAllowedForStatus(tc.status); got != tc.want {
			t.Fatalf("bodyAllowedForStatus(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestNoBodyStatusesNotCompressed(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(status)
		}))
		recorder := requestWithGzip(handler)
		if recorder.Header().Get("Content-Encoding") != "" {
			t.Fatalf("status %d unexpectedly compressed", status)
		}
	}
}

func TestContentTypeSniffedOnImplicitWriteHeader(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "plain text without explicit header")
	}))
	recorder := requestWithGzip(handler)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip via sniffed type, got %q", recorder.Header().Get("Content-Encoding"))
	}
	decoded := gzipDecompress(t, recorder.Body.Bytes())
	if !strings.HasPrefix(decoded, "plain text") {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestHijackPassthroughOnUpgrade(t *testing.T) {
	converted := false
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(*gzipResponseWriter); ok {
			t.Error("websocket request must not be wrapped")
		}
		if _, ok := w.(http.Hijacker); ok {
			converted = true
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	handler.ServeHTTP(&hijackableWriter{header: http.Header{}}, req)
	if !converted {
		t.Fatal("handler did not see a hijackable writer")
	}
}

type hijackableWriter struct {
	header    http.Header
	status    int
	hijacked  bool
	conn      net.Conn
	readwrite *bufio.ReadWriter
}

func (w *hijackableWriter) Header() http.Header { return w.header }
func (w *hijackableWriter) WriteHeader(status int) {
	w.status = status
}
func (w *hijackableWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return w.conn, w.readwrite, nil
}

func TestGzipWriterHijack(t *testing.T) {
	wrapped := &hijackableWriter{header: http.Header{}}
	gw := &gzipResponseWriter{ResponseWriter: wrapped}
	conn, rw, err := gw.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	if !wrapped.hijacked || conn != nil || rw != nil {
		t.Fatalf("hijack = %v/%v/%v", wrapped.hijacked, conn, rw)
	}
}

func TestGzipWriterHijackNotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	gw := &gzipResponseWriter{ResponseWriter: recorder}
	if _, _, err := gw.Hijack(); err != http.ErrNotSupported {
		t.Fatalf("hijack err = %v, want ErrNotSupported", err)
	}
}

func TestGzipWriterFlushPropagates(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "chunk1")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("writer does not support Flush")
		}
		flusher.Flush()
		io.WriteString(w, "chunk2")
	}))
	recorder := requestWithGzip(handler)
	decoded := gzipDecompress(t, recorder.Body.Bytes())
	if decoded != "chunk1chunk2" {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestUnwrapReachesUnderlyingWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	gw := &gzipResponseWriter{ResponseWriter: recorder}
	if got := gw.Unwrap(); got != recorder {
		t.Fatal("Unwrap did not return underlying writer")
	}
	if rc := http.NewResponseController(gw); rc == nil {
		t.Fatal("NewResponseController failed")
	}
}