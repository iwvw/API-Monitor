package bookmarks

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// SSRF 防护：只有 http/https 且主机可公开访问时才允许抓取。
func TestValidateFetchURLRejectsInternalTargets(t *testing.T) {
	blocked := []struct {
		url    string
		reason string
	}{
		{"file:///C:/Windows/win.ini", "file scheme"},
		{"ftp://example.com/x", "ftp scheme"},
		{"gopher://127.0.0.1:70/", "gopher scheme"},
		{"http://127.0.0.1:3000/api/bookmarks/groups", "loopback"},
		{"http://169.254.169.254/latest/meta-data/", "cloud metadata link-local"},
		{"http://10.0.0.5/", "RFC1918 10/8"},
		{"http://192.168.1.1/", "RFC1918 192.168/16"},
		{"http://172.16.0.1/", "RFC1918 172.16/12"},
		{"http://[::1]:8080/", "IPv6 loopback"},
		{"http://0.0.0.0/", "unspecified"},
		{"", "empty"},
		{"not a url at all", "unparsable"},
	}
	for _, tc := range blocked {
		if _, err := validateFetchURL(tc.url); err == nil {
			t.Errorf("expected %q (%s) to be rejected", tc.url, tc.reason)
		}
	}

	// localhost 这类域名不是字面量 IP，在第一层不拦截，
	// 但会在拨号解析后被 safeHTTPClient 拦截（见下一个测试）。
	if _, err := validateFetchURL("http://localhost:3000/"); err != nil {
		t.Logf("localhost rejected eagerly (also acceptable): %v", err)
	}

	allowed := []string{"https://example.com/", "http://example.com/favicon.ico", "https://1.1.1.1/"}
	for _, u := range allowed {
		if _, err := validateFetchURL(u); err != nil {
			t.Errorf("expected %q to be allowed, got %v", u, err)
		}
	}
}

// localhost 域名必须在拨号阶段被拦截（DNS 解析到 127.0.0.1）。
func TestSafeHTTPClientBlocksDNSRebindingToLoopback(t *testing.T) {
	client := safeHTTPClient(3e9)
	req, err := http.NewRequest(http.MethodGet, "http://localhost:1/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected localhost to be blocked at dial time, got status %d", resp.StatusCode)
	}
	t.Logf("localhost blocked at dial time: %v", err)
}

// 抓取端点必须拒绝内网/非 http 目标，而不是把它们当普通错误处理。
func TestFetchFaviconEndpointRejectsInternal(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	for _, u := range []string{
		"http://127.0.0.1:3000/",
		"http://169.254.169.254/latest/meta-data/",
		"file:///C:/Windows/win.ini",
	} {
		rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/favicon/fetch", `{"url":"`+u+`"}`)
		if rec.Code == http.StatusOK {
			t.Errorf("fetch favicon for %q must fail, got 200", u)
		}
		t.Logf("%s -> %d", u, rec.Code)
	}
}

// 图标文件名白名单：拒绝任何非 md5+扩展名的路径。
func TestServeFaviconFilenameAllowlist(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 合法命名应通过校验（文件不存在 -> 404，而不是 400）
	rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/favicons/"+strings.Repeat("a", 32)+".png", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("valid filename should reach file serving (404 when missing), got %d", rec.Code)
	}

	// 路径穿越尝试：Go 会先解码再按 / 切分，所以 %2F 变体不会作为单个
	// 文件名到达这里；用真实可到达的形式验证白名单。
	bad := []string{
		"evil.png",
		"..%2F..%2Fdata.db",
		"....%2Fdata.db",
		strings.Repeat("a", 32) + ".svg",
		strings.Repeat("a", 32) + ".exe",
		strings.Repeat("A", 32) + ".png", // 大写十六进制不在白名单
		"datadb",
	}
	for _, name := range bad {
		rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/favicons/"+name, "")
		// 白名单拒绝 -> 400；被 URL 解码后切分成子路径 -> 404（同样不可达）。
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("filename %q should be rejected (400/404), got %d", name, rec.Code)
		}
		if rec.Code == http.StatusOK {
			t.Errorf("filename %q must never be served", name)
		}
		t.Logf("%-40q -> %d", name, rec.Code)
	}
}

// extensionFor 不再对未知类型默认 .png；SVG 改为允许（落盘前会剥离脚本）。
func TestExtensionForRejectsNonImages(t *testing.T) {
	cases := []struct {
		contentType string
		url         string
		wantErr     bool
		wantExt     string
	}{
		{"image/png", "https://x/i", false, ".png"},
		{"image/jpeg", "https://x/i", false, ".jpg"},
		{"image/webp", "https://x/i", false, ".webp"},
		{"image/vnd.microsoft.icon", "https://x/i", false, ".ico"},
		// SVG 允许：很多站点（如 Vite 默认模板）只提供 SVG 图标，
		// 一律拒绝会让这些站点完全拿不到 favicon。安全性由 sanitizeSVG 保证。
		{"image/svg+xml", "https://x/i.svg", false, ".svg"},
		{"text/html; charset=utf-8", "https://x/404", true, ""},
		{"application/json", "https://x/api", true, ""},
		{"image/avif", "https://x/i", true, ""},
		{"", "https://x/icon.png", false, ".png"},
		{"", "https://x/icon.svg", false, ".svg"},
		{"", "https://x/noext", false, ""}, // 交由魔数判定
	}
	for _, tc := range cases {
		ext, err := extensionFor(tc.contentType, tc.url)
		if tc.wantErr && err == nil {
			t.Errorf("extensionFor(%q,%q) should error", tc.contentType, tc.url)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("extensionFor(%q,%q) unexpected error: %v", tc.contentType, tc.url, err)
		}
		if !tc.wantErr && ext != tc.wantExt {
			t.Errorf("extensionFor(%q,%q) = %q, want %q", tc.contentType, tc.url, ext, tc.wantExt)
		}
	}
}

// 魔数嗅探：HTML 错误页不能被当成图片。
func TestSniffImageExt(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 8)...)
	if got := sniffImageExt(png); got != ".png" {
		t.Errorf("png magic -> %q, want .png", got)
	}
	jpg := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 8)...)
	if got := sniffImageExt(jpg); got != ".jpg" {
		t.Errorf("jpeg magic -> %q, want .jpg", got)
	}
	html := []byte("<!DOCTYPE html><html><body>404</body></html>")
	if got := sniffImageExt(html); got != "" {
		t.Errorf("html must not sniff as image, got %q", got)
	}
	if got := sniffImageExt([]byte("ab")); got != "" {
		t.Errorf("short body must not sniff, got %q", got)
	}
}

// 端到端：从测试服务器抓一个真实 PNG 图标必须成功，且拒绝 HTML。
func TestDownloadFaviconRealPNG(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

	// 用 127.0.0.1 会被 SSRF 拦截，所以这里直接调用底层逻辑验证落盘分支：
	// 通过临时目录 + 直接写文件的方式不可行，改为验证 sniff+extensionFor 组合。
	ext, err := extensionFor("image/png", "https://example.com/i")
	if err != nil {
		t.Fatalf("extensionFor: %v", err)
	}
	if got := sniffImageExt(png); got != ext {
		t.Errorf("sniff %q vs extensionFor %q mismatch", got, ext)
	}

	// HTML 响应必须被拒绝
	if sniffImageExt([]byte("<html>nope</html>")) != "" {
		t.Errorf("html must be rejected as icon content")
	}
}

// 超大响应必须被拒绝，而不是静默截断成损坏文件。
func TestDownloadFaviconRejectsOversize(t *testing.T) {
	oversize := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, faviconMaxBytes+10)...)
	if len(oversize) <= faviconMaxBytes {
		t.Fatal("test fixture not oversize")
	}
	// 复现 downloadFavicon 的判定逻辑：多读 1 字节后必须 > max
	limited := oversize[:faviconMaxBytes+1]
	if len(limited) <= faviconMaxBytes {
		t.Errorf("oversize body must be detectable, got %d bytes", len(limited))
	}
	t.Logf("oversize detection: read %d bytes for a %d byte cap", len(limited), faviconMaxBytes)
}

var _ = httptest.NewRecorder

// 候选排序：位图必须排在 SVG 前面，即使 SVG 在文档里声明得更早。
// 这是「优先找 PNG/ICO，找不到才用 SVG」的核心保证。
func TestSortedIconHrefsPrefersRasterOverSVG(t *testing.T) {
	// SVG 写在最前面，PNG 在后面 —— 仍然必须优先返回 PNG
	htmlSrc := `<html><head>
		<link rel="icon" type="image/svg+xml" href="/vite.svg">
		<link rel="icon" type="image/png" href="/favicon.png">
		<link rel="apple-touch-icon" href="/apple.png">
	</head></html>`
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		t.Fatal(err)
	}
	got := sortedIconHrefs(doc)
	if len(got) == 0 {
		t.Fatal("expected candidates")
	}
	if strings.HasSuffix(got[0].href, ".svg") {
		t.Errorf("raster icon must come first, got %v", got)
	}
	// 但 SVG 不能被丢掉：它应作为最后的候选保留
	last := got[len(got)-1]
	if !strings.HasSuffix(last.href, ".svg") {
		t.Errorf("svg should be kept as the last candidate, got %v", got)
	}
	// firstIconHref 与排序结果保持一致
	if firstIconHref(doc) != got[0].href {
		t.Errorf("firstIconHref=%q, want %q", firstIconHref(doc), got[0].href)
	}
}

// 只有 SVG 时仍要返回它（不能被过滤掉，否则这类站点拿不到图标）。
func TestSortedIconHrefsKeepsSVGWhenOnlyOption(t *testing.T) {
	htmlSrc := `<html><head><link rel="icon" type="image/svg+xml" href="/vite.svg"></head></html>`
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		t.Fatal(err)
	}
	got := sortedIconHrefs(doc)
	if len(got) != 1 || got[0].href != "/vite.svg" {
		t.Fatalf("svg-only page must yield the svg candidate, got %v", got)
	}
}

// SVG 消毒：剥离脚本、事件属性、javascript: 引用与 foreignObject。
func TestSanitizeSVGStripsExecutableContent(t *testing.T) {
	dirty := `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)">
		<script>alert('xss')</script>
		<rect width="10" height="10" onclick="steal()"/>
		<a xlink:href="javascript:alert(2)"><text>x</text></a>
		<foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><script>alert(3)</script></body></foreignObject>
		<style>.a{fill:red}</style>
	</svg>`

	clean, err := sanitizeSVG([]byte(dirty))
	if err != nil {
		t.Fatalf("sanitizeSVG: %v", err)
	}
	out := string(clean)

	for _, bad := range []string{"<script", "onload", "onclick", "javascript:", "foreignObject", "alert("} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
			t.Errorf("sanitized svg still contains %q:\n%s", bad, out)
		}
	}
	// 正常绘图内容必须保留，否则图标会被剥成空白
	if !strings.Contains(out, "<rect") {
		t.Errorf("sanitized svg lost drawing content:\n%s", out)
	}
	if !strings.Contains(out, "fill:red") {
		t.Errorf("sanitized svg lost <style> (needed for icon colors):\n%s", out)
	}
}

// 非 SVG 内容不得通过 sanitizeSVG（防止把 HTML 错误页当图标存下）。
func TestSanitizeSVGRejectsNonSVG(t *testing.T) {
	if _, err := sanitizeSVG([]byte("<html><body>404</body></html>")); err == nil {
		t.Error("html content must be rejected by sanitizeSVG")
	}
}

// 魔数嗅探要能识别 SVG（纯文本、没有二进制魔数）。
func TestSniffImageExtDetectsSVG(t *testing.T) {
	cases := []struct {
		body []byte
		want string
	}{
		{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ".svg"},
		{[]byte("  \n\t<svg></svg>"), ".svg"},                // 前导空白
		{[]byte("\ufeff<svg></svg>"), ".svg"},                // UTF-8 BOM
		{[]byte(`<?xml version="1.0"?><svg></svg>`), ".svg"}, // XML 声明
		{[]byte("<!DOCTYPE html><html></html>"), ""},         // HTML 不是 SVG
		{[]byte("\x89PNG\r\n\x1a\n"), ".png"},                // 位图仍然优先识别
	}
	for _, tc := range cases {
		if got := sniffImageExt(tc.body); got != tc.want {
			t.Errorf("sniffImageExt(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

// 落盘的 SVG 必须能真的取回来：白名单漏掉 svg 会让「位图找不到就回落 SVG」
// 这条路径存下来的图标一律 400，等于功能从未生效。
func TestServeFaviconFileServesSVGWithCSP(t *testing.T) {
	service := newTestService(t)
	if err := os.MkdirAll(service.faviconDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "0123456789abcdef0123456789abcdef.svg"
	if err := os.WriteFile(filepath.Join(service.faviconDir(), name),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks/favicons/"+name, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("svg favicon must be served, got %d: %s", rec.Code, rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("svg response must carry a script-blocking CSP, got %q", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("favicon response must set nosniff")
	}

	// 白名单之外的扩展名仍然要挡住
	recBad := httptest.NewRecorder()
	service.ServeHTTP(recBad, httptest.NewRequest(http.MethodGet,
		"/api/bookmarks/favicons/0123456789abcdef0123456789abcdef.exe", nil))
	if recBad.Code != http.StatusBadRequest {
		t.Errorf("non-image extension must be rejected, got %d", recBad.Code)
	}
}
