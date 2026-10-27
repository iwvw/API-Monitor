package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/httpcompress"
)

// 构造 dist：源文件 + 预压缩变体。压缩内容用真实 gzip/brotli 以便端到端校验。
func writePrecompressedAsset(t *testing.T, dir, name, body string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full+".gz", gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// brotli 变体只需存在且内容可区分；断言只校验 Content-Encoding 与体积。
	if err := os.WriteFile(full+".br", []byte("BROTLI:"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func newStaticTestServer(t *testing.T, distDir string) *Server {
	t.Helper()
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DistDir: distDir,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

func doStaticRequest(t *testing.T, handler http.Handler, target, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPrecompressedBrotliPreferredOverGzip(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "gzip, deflate, br")

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if got := res.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := res.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
}

func TestPrecompressedGzipChosenWhenBrotliAbsent(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "gzip")

	if got := res.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	// 必须发的是 .gz 内容（真实可解压），而不是原始文件。
	if body := res.Body.Bytes(); !bytes.HasPrefix(body, []byte{0x1f, 0x8b}) {
		t.Fatalf("body does not look like gzip payload: %q", body)
	}
}

func TestPrecompressedContentTypeUsesOriginalExtension(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "br")

	ct := res.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") && !strings.Contains(ct, "ecmascript") {
		t.Fatalf("Content-Type = %q, want JS mime inferred from original extension", ct)
	}
}

func TestPrecompressedKeepsImmutableCachePolicy(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.css", "body{}")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.css", "br")

	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable assets policy", got)
	}
}

func TestPrecompressedServesRawWhenNoAcceptEncoding(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "")

	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := res.Body.String(); got != "console.log('hi')" {
		t.Fatalf("body = %q, want raw source", got)
	}
}

func TestPrecompressedFallsBackWhenVariantMissing(t *testing.T) {
	distDir := t.TempDir()
	// 只有源文件，没有 .gz/.br（模拟开发环境）。
	full := filepath.Join(distDir, "assets", "app-abc123.js")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("console.log('raw')"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "gzip, br")

	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty fallback", got)
	}
	if got := res.Body.String(); got != "console.log('raw')" {
		t.Fatalf("body = %q, want raw source", got)
	}
}

func TestPrecompressedSkipsAlreadyCompressedTypes(t *testing.T) {
	distDir := t.TempDir()
	// 二进制类型即使存在 .gz 变体也不应该被使用。
	writePrecompressedAsset(t, distDir, "assets/font-abc123.woff2", "FONTDATA")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/font-abc123.woff2", "gzip, br")

	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for already-compressed type", got)
	}
}

func TestPrecompressedPreservesLastModified(t *testing.T) {
	distDir := t.TempDir()
	full := writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	sourceTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(full, sourceTime, sourceTime); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".gz", ".br"} {
		if err := os.Chtimes(full+suffix, sourceTime, sourceTime); err != nil {
			t.Fatal(err)
		}
	}
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "br")

	if got := res.Header().Get("Last-Modified"); !strings.Contains(got, "01 Mar 2026") {
		t.Fatalf("Last-Modified = %q, want source mtime", got)
	}
	// If-Modified-Since 命中时应 304，证明预压缩产物与源文件 mtime 一致。
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("If-Modified-Since", sourceTime.Format(http.TimeFormat))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional request status = %d, want 304", rec.Code)
	}
}

func TestPrecompressedDoesNotDoubleCompressThroughMiddleware(t *testing.T) {
	// 这条守的是跨层契约：中间件以 Content-Encoding 非空判定已编码。
	// 预压缩路径必须设置该头，否则会被实时 gzip 覆盖成双层编码。
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", strings.Repeat("x", 2000))
	handler := httpcompress.Middleware(newStaticTestServer(t, distDir))

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "gzip, br")

	values := res.Header().Values("Content-Encoding")
	if len(values) != 1 || values[0] != "br" {
		t.Fatalf("Content-Encoding = %v, want single br", values)
	}
	// 未被中间件重新压缩，因此 Content-Length 仍在且等于 .br 文件大小。
	brInfo, err := os.Stat(filepath.Join(distDir, "assets", "app-abc123.js.br"))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Content-Length"); got != strconv.FormatInt(brInfo.Size(), 10) {
		t.Fatalf("Content-Length = %q, want %d", got, brInfo.Size())
	}
}

func TestAcceptedEncodingsParsing(t *testing.T) {
	cases := []struct {
		header string
		br     bool
		gzip   bool
	}{
		{"", false, false},
		{"gzip", false, true},
		{"br", true, false},
		{"gzip, deflate, br", true, true},
		{"gzip;q=0", false, false},
		{"br;q=0, gzip", false, true},
		{"br;q=0, *", false, true},
		{"*", true, true},
		{"identity", false, false},
		{"GZIP", false, true},
		{"x-gzip", false, true},
	}
	for _, tc := range cases {
		br, gzip := acceptedEncodings(tc.header)
		if br != tc.br || gzip != tc.gzip {
			t.Fatalf("acceptedEncodings(%q) = (%v,%v), want (%v,%v)", tc.header, br, gzip, tc.br, tc.gzip)
		}
	}
}

func TestPrecompressedDirectoryIndex(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "vendor/drawio/index.html", "<title>Draw.io</title>")
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/vendor/drawio/", "br")

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if got := res.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache for index.html", got)
	}
}

func TestPrecompressedIgnoresPost(t *testing.T) {
	distDir := t.TempDir()
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", "console.log('hi')")
	handler := newStaticTestServer(t, distDir)

	req := httptest.NewRequest(http.MethodPost, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// 保证 gzip 变体内容确实能被标准库解开（防止发错文件）。
func TestPrecompressedGzipBodyDecodesToSource(t *testing.T) {
	distDir := t.TempDir()
	const body = "const a = 1;"
	writePrecompressedAsset(t, distDir, "assets/app-abc123.js", body)
	handler := newStaticTestServer(t, distDir)

	res := doStaticRequest(t, handler, "/assets/app-abc123.js", "gzip")

	zr, err := gzip.NewReader(bytes.NewReader(res.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded = %q, want %q", decoded, body)
	}
}
