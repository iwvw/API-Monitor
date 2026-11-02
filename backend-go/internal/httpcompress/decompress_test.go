package httpcompress

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func gzipBytes(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func brotliBytes(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := brotli.NewWriter(&buf)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatalf("brotli write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("brotli close: %v", err)
	}
	return buf.Bytes()
}

func zstdBytes(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	return buf.Bytes()
}

// echoBody 读取解压后的请求体原样返回，并回传看到的请求头。
func echoBody(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("X-Saw-Content-Encoding", r.Header.Get("Content-Encoding"))
		w.Header().Set("X-Saw-Content-Length", r.Header.Get("Content-Length"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

func TestDecompressRequestEncodings(t *testing.T) {
	payload := `{"model":"x","messages":[{"role":"user","content":"` +
		strings.Repeat("大上下文上下文 ", 200) + `"}]}`

	cases := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"gzip", "gzip", gzipBytes(t, payload)},
		{"x-gzip", "x-gzip", gzipBytes(t, payload)},
		{"brotli", "br", brotliBytes(t, payload)},
		{"zstd", "zstd", zstdBytes(t, payload)},
		{"uppercase", "GZip", gzipBytes(t, payload)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(tc.body))
			req.Header.Set("Content-Encoding", tc.encoding)
			rec := httptest.NewRecorder()

			DecompressRequest(0)(echoBody(t)).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != payload {
				t.Fatalf("body mismatch: got %d bytes want %d", len(got), len(payload))
			}
			if got := rec.Header().Get("X-Saw-Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding not cleared: %q", got)
			}
			if got := rec.Header().Get("X-Saw-Content-Length"); got != "" {
				t.Fatalf("Content-Length not cleared: %q", got)
			}
		})
	}
}

func TestDecompressRequestChainedEncodings(t *testing.T) {
	const payload = `{"ok":true}`
	// HTTP 规范：后声明的编码先解。这里先 gzip 再 brotli，头写 "gzip, br"。
	body := brotliBytes(t, string(gzipBytes(t, payload)))

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "gzip, br")
	rec := httptest.NewRecorder()

	DecompressRequest(0)(echoBody(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != payload {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestDecompressRequestPassesThroughPlainBody(t *testing.T) {
	const payload = `{"plain":true}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	DecompressRequest(0)(echoBody(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != payload {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDecompressRequestIdentityPassesThrough(t *testing.T) {
	const payload = `{"plain":true}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.Header.Set("Content-Encoding", "identity")
	rec := httptest.NewRecorder()

	DecompressRequest(0)(echoBody(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != payload {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDecompressRequestSkipsMethodsWithoutBody(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", strings.NewReader("not-really-compressed"))
			req.Header.Set("Content-Encoding", "gzip")
			rec := httptest.NewRecorder()

			DecompressRequest(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.ReadAll(r.Body); err != nil {
					t.Errorf("body should pass through untouched: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
}

func TestDecompressRequestSkipsWebSocketUpgrade(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ws/ssh", strings.NewReader("x"))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	called := false
	DecompressRequest(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("upgrade request should bypass decompression, called=%v status=%d", called, rec.Code)
	}
}

func TestDecompressRequestUnsupportedEncodingReturns415(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("whatever"))
	req.Header.Set("Content-Encoding", "deflate")
	rec := httptest.NewRecorder()

	called := false
	DecompressRequest(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not be reached for unsupported encoding")
	}
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestDecompressRequestCorruptBodyReturns400(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("definitely not gzip"))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	called := false
	DecompressRequest(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not be reached for corrupt body")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDecompressRequestEnforcesPostDecompressLimit(t *testing.T) {
	// 压缩后很小、解压后远超上限：必须被 MaxBytesReader 挡住（防解压炸弹）。
	payload := strings.Repeat("A", 1<<20)
	body := gzipBytes(t, payload)
	if len(body) >= 1<<20 {
		t.Fatalf("compressed body unexpectedly large: %d", len(body))
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	DecompressRequest(64<<10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var maxErr *http.MaxBytesError
			if !asMaxBytesError(err, &maxErr) {
				t.Errorf("unexpected error type: %T %v", err, err)
			}
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		t.Error("expected read to fail on oversized body")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func asMaxBytesError(err error, target **http.MaxBytesError) bool {
	for err != nil {
		if e, ok := err.(*http.MaxBytesError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestDecompressRequestEmptyBodyWithEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	DecompressRequest(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if len(data) != 0 {
			t.Errorf("body = %q, want empty", data)
		}
		if got := r.Header.Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Encoding not cleared: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestParseContentEncodings(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"identity", nil},
		{"gzip", []string{"gzip"}},
		{"GZip, BR", []string{"gzip", "br"}},
		{" gzip ,, br ", []string{"gzip", "br"}},
		{"gzip, identity", []string{"gzip"}},
	}
	for _, tc := range cases {
		got := parseContentEncodings(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("parseContentEncodings(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parseContentEncodings(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		}
	}
}

// 命中 skipPaths 的接口（如 2GB 数据库导入）必须完全跳过解压，
// 否则会被统一的 32MB 解压上限截断。
func TestDecompressRequestSkipsConfiguredPaths(t *testing.T) {
	const payload = `{"plain":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/database/import/preview", strings.NewReader(payload))
	// 故意声明 gzip 头但发明文：若被解压会 400，能证明确实跳过了。
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	DecompressRequest(0, "/api/settings/database/import/preview")(echoBody(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("skip path should bypass decompression, status = %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != payload {
		t.Fatalf("body = %q, want unchanged", rec.Body.String())
	}
	if got := rec.Header().Get("X-Saw-Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding should be left untouched on skip path, got %q", got)
	}
}
