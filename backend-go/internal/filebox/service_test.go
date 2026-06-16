package filebox

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

type fakeAuth struct {
	ok bool
}

func (f fakeAuth) IsAuthenticated(context.Context, *http.Request) (bool, error) {
	return f.ok, nil
}

func TestTextShareRetrieveVerifyDownloadAndExpire(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})

	body, contentType := multipartBody(t, map[string]string{
		"type":               "text",
		"text":               "hello filebox",
		"expiry":             "1",
		"max_downloads":      "1",
		"access_password":    "secret",
		"burn_after_reading": "false",
	}, nil)
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/share", body, contentType)
	if res.Code != http.StatusOK {
		t.Fatalf("create text status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    struct {
			RequiresPassword bool   `json:"requiresPassword"`
			Preview          string `json:"preview"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)
	if !createPayload.Success || createPayload.Code == "" || !createPayload.Data.RequiresPassword || createPayload.Data.Preview != "hello filebox" {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/retrieve/"+createPayload.Code, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("retrieve status = %d body=%s", res.Code, res.Body.String())
	}
	var metadata struct {
		Success bool `json:"success"`
		Data    struct {
			Code             string `json:"code"`
			RequiresPassword bool   `json:"requiresPassword"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &metadata)
	if !metadata.Success || metadata.Data.Code != createPayload.Code || !metadata.Data.RequiresPassword {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/public/"+createPayload.Code+"/verify", strings.NewReader(`{"password":"bad"}`), "application/json")
	if res.Code != http.StatusForbidden {
		t.Fatalf("bad verify status = %d body=%s", res.Code, res.Body.String())
	}
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/public/"+createPayload.Code+"/verify", strings.NewReader(`{"password":"secret"}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("good verify status = %d body=%s", res.Code, res.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/filebox/download/"+createPayload.Code, nil)
	req.Header.Set("X-Filebox-Password", "secret")
	res = httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "hello filebox" {
		t.Fatalf("download status=%d body=%q", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/retrieve/"+createPayload.Code, nil, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("retrieve after max downloads status = %d body=%s", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/access-logs", nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("access logs status = %d body=%s", res.Code, res.Body.String())
	}
	var logs struct {
		Success bool        `json:"success"`
		Data    []AccessLog `json:"data"`
	}
	mustDecodeFilebox(t, res, &logs)
	if !logs.Success || len(logs.Data) < 2 {
		t.Fatalf("expected access logs, got %#v", logs)
	}
}

func TestSettingsRequireAuthAndCleanupExpired(t *testing.T) {
	unauthenticated := newTestService(t, fakeAuth{ok: false})
	res := performFileboxRequest(unauthenticated, http.MethodGet, "/api/filebox/settings", nil, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated settings status = %d body=%s", res.Code, res.Body.String())
	}

	service := newTestService(t, fakeAuth{ok: true})
	res = performFileboxRequest(service, http.MethodPut, "/api/filebox/settings", strings.NewReader(`{"max_file_size":1024,"allowed_mime_types":["text/*"],"default_expiry_hours":2,"public_upload_enabled":true}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("update settings status = %d body=%s", res.Code, res.Body.String())
	}
	var settingsPayload struct {
		Success bool     `json:"success"`
		Data    Settings `json:"data"`
	}
	mustDecodeFilebox(t, res, &settingsPayload)
	if !settingsPayload.Success || settingsPayload.Data.MaxFileSize != 1024 || !settingsPayload.Data.PublicUploadEnabled || len(settingsPayload.Data.AllowedMIMETypes) != 1 {
		t.Fatalf("unexpected settings payload: %#v", settingsPayload)
	}

	entry, err := service.AddText(context.Background(), "expired", -1, false, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.Code == "" {
		t.Fatalf("expected expired entry, got %#v", entry)
	}
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/jobs/cleanup", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("cleanup status = %d body=%s", res.Code, res.Body.String())
	}
	var cleanup struct {
		Success bool `json:"success"`
		Data    struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &cleanup)
	if !cleanup.Success || cleanup.Data.Deleted != 1 {
		t.Fatalf("unexpected cleanup payload: %#v", cleanup)
	}
}

func newTestService(t *testing.T, auth Authenticator) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	}, auth)
}

func performFileboxRequest(service *Service, method, path string, body ioReader, contentType string) *httptest.ResponseRecorder {
	var reader ioReader = body
	if reader == nil {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

type ioReader interface {
	Read([]byte) (int, error)
}

func multipartBody(t *testing.T, fields map[string]string, file *multipartFile) (*bytes.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes()), writer.FormDataContentType()
}

type multipartFile struct {
	name    string
	content string
}

func mustDecodeFilebox(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
}
