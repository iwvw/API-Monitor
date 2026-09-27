package bookmarks

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 构造一个最小但合法的 PNG（魔数正确即可通过格式检测）。
func pngBytes(size int) []byte {
	head := []byte("\x89PNG\r\n\x1a\n")
	out := make([]byte, 0, len(head)+size)
	out = append(out, head...)
	return append(out, make([]byte, size)...)
}

func jpegBytes(size int) []byte {
	head := []byte("\xff\xd8\xff\xe0")
	out := make([]byte, 0, len(head)+size)
	out = append(out, head...)
	return append(out, make([]byte, size)...)
}

// 通过 multipart 上传背景图，走真实 ServeHTTP 路由。
func uploadBackground(t *testing.T, service *Service, fileName string, data []byte) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/bookmarks/backgrounds/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec, payload
}

func TestUploadBackgroundPNG(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	rec, payload := uploadBackground(t, service, "wallpaper.png", pngBytes(1024))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if data["ext"] != ".png" {
		t.Errorf("ext=%v want .png", data["ext"])
	}
	url, _ := data["url"].(string)
	if url == "" {
		t.Fatalf("missing url in response: %v", data)
	}
	t.Logf("uploaded background: %s", url)

	// 列表应包含刚上传的
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/backgrounds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	items, _ := payload["data"].([]interface{})
	if len(items) != 1 {
		t.Errorf("expected 1 background, got %d", len(items))
	}
}

// 上传后的资源必须能公开读取（公开页免登录要用）。
func TestBackgroundAssetIsPubliclyReadable(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	rec, payload := uploadBackground(t, service, "bg.png", pngBytes(512))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	url := payload["data"].(map[string]interface{})["url"].(string)

	// 用裸 ServeHTTP（无会话）请求该资源。
	// 资源路径不在 /api/bookmarks 前缀下，由 server 层分发，因此这里
	// 直接用一个新的 recorder 调用公开资产处理函数验证其行为。
	assetReq := httptest.NewRequest(http.MethodGet, url, nil)
	assetRec := httptest.NewRecorder()

	if !service.ServePublicBackgroundAsset(assetRec, assetReq) {
		t.Fatalf("ServePublicBackgroundAsset returned false for %s", url)
	}
	if assetRec.Code != http.StatusOK {
		t.Errorf("asset status=%d want 200", assetRec.Code)
	}
	if got := assetRec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content-type=%q want image/png", got)
	}
	if cc := assetRec.Header().Get("Cache-Control"); cc == "" {
		t.Errorf("expected cache-control header")
	}
	// 内容必须与上传的一致（不是空文件或错误页）
	if body := assetRec.Body.Bytes(); len(body) != 512+8 {
		t.Errorf("served %d bytes, want %d", len(body), 512+8)
	}
}

// 背景图拒绝 SVG（可携带脚本），并拒绝非图片文件。
func TestUploadBackgroundRejectsUnsupported(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"svg", "evil.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"html", "page.html", []byte(`<html><body>hi</body></html>`)},
		{"plain text", "note.txt", []byte(`just some text content here`)},
		{"empty", "empty.png", []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := uploadBackground(t, service, tc.file, tc.data)
			if rec.Code == http.StatusOK {
				t.Errorf("%s must be rejected, got 200", tc.name)
			}
		})
	}
}

// 超过体积上限的图片必须被拒绝，而不是静默截断写入。
func TestUploadBackgroundRejectsOversize(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	rec, _ := uploadBackground(t, service, "huge.png", pngBytes(maxBackgroundImageBytes+1024))
	if rec.Code == http.StatusOK {
		t.Errorf("oversize upload must fail, got 200")
	}
	t.Logf("oversize rejected with %d", rec.Code)
}

func TestUploadBackgroundJPEG(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	rec, payload := uploadBackground(t, service, "photo.jpg", jpegBytes(2048))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ext := payload["data"].(map[string]interface{})["ext"]; ext != ".jpg" {
		t.Errorf("ext=%v want .jpg", ext)
	}
}

func TestDeleteBackground(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := uploadBackground(t, service, "a.png", pngBytes(256))
	id := payload["data"].(map[string]interface{})["id"].(string)

	rec, _ := doJSON(service, http.MethodDelete, "/api/bookmarks/backgrounds/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/backgrounds", "")
	if items, _ := payload["data"].([]interface{}); len(items) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(items))
	}

	// 重复删除应 404，而不是静默成功
	rec, _ = doJSON(service, http.MethodDelete, "/api/bookmarks/backgrounds/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("second delete status=%d want 404", rec.Code)
	}
}

// 路径穿越尝试不得读到目录外文件。
func TestBackgroundAssetRejectsTraversal(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	uploadBackground(t, service, "a.png", pngBytes(128))

	for _, path := range []string{
		"/site-brand-backgrounds/..%2F..%2Fdata.db",
		"/site-brand-backgrounds/../backgrounds.json",
		"/site-brand-backgrounds/..\\..\\data.db",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		served := service.ServePublicBackgroundAsset(rec, req)
		if served && rec.Code == http.StatusOK {
			t.Errorf("traversal %q must not be served", path)
		}
	}
}

// 同一文件的多次上传各自独立（不共用 id），避免删除一个影响另一个。
func TestBackgroundUploadsAreIndependent(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, p1 := uploadBackground(t, service, "same.png", pngBytes(128))
	_, p2 := uploadBackground(t, service, "same.png", pngBytes(128))
	id1 := p1["data"].(map[string]interface{})["id"].(string)
	id2 := p2["data"].(map[string]interface{})["id"].(string)
	if id1 == id2 {
		t.Fatalf("two uploads must not share an id: %s", id1)
	}

	doJSON(service, http.MethodDelete, "/api/bookmarks/backgrounds/"+id1, "")
	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/backgrounds", "")
	if items, _ := payload["data"].([]interface{}); len(items) != 1 {
		t.Errorf("deleting one should leave the other, got %d", len(items))
	}
	if rec.Code != http.StatusOK {
		t.Errorf("list status=%d", rec.Code)
	}
}

// 背景配置能通过分组 config 持久化并读回（公开页据此渲染）。
func TestBackgroundConfigPersistsOnGroup(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
		`{"title":"公开组","public":true,"slug":"pub","config":{"bgImage":"https://cdn.example/bg.jpg","bgBlur":8,"bgDim":0.5,"bgColor":"#112233"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	gid := int64Value(payload["group"].(map[string]interface{})["id"])

	// 通过公开端点读回
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/pub", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public read status=%d body=%s", rec.Code, rec.Body.String())
	}
	config := payload["data"].(map[string]interface{})["group"].(map[string]interface{})["config"].(map[string]interface{})
	if config["bgImage"] != "https://cdn.example/bg.jpg" {
		t.Errorf("bgImage=%v", config["bgImage"])
	}
	if config["bgBlur"].(float64) != 8 {
		t.Errorf("bgBlur=%v", config["bgBlur"])
	}

	// 聚合端点同样要带上背景配置
	_, payload = doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	groups := payload["data"].(map[string]interface{})["groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	aggConfig := groups[0].(map[string]interface{})["config"].(map[string]interface{})
	if aggConfig["bgColor"] != "#112233" {
		t.Errorf("aggregate config missing bgColor: %v", aggConfig)
	}
	t.Logf("background config round-trips via public API for group %d", gid)
}
