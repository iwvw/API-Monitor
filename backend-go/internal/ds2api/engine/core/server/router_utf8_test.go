package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONRequestsRejectInvalidUTF8BeforeDecode(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["managed-key"],"accounts":[{"email":"u@example.com","password":"p"}]}`)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}

	body := append([]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"`), 0xff)
	body = append(body, []byte(`"}]}`)...)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-api-key", "direct-token")

	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid utf-8 request body, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid json") {
		t.Fatalf("expected invalid json error, got %q", rec.Body.String())
	}
}

func TestKnownJSONRequestsRejectInvalidUTF8WithoutJSONContentType(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["managed-key"],"accounts":[{"email":"u@example.com","password":"p"}]}`)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}

	body := append([]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"`), 0xff)
	body = append(body, []byte(`"}]}`)...)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("x-api-key", "direct-token")

	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid utf-8 request body, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid json") {
		t.Fatalf("expected invalid json error, got %q", rec.Body.String())
	}
}

func TestJSONRequestsRejectTrailingInvalidUTF8AfterCompleteJSON(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["managed-key"],"accounts":[{"email":"u@example.com","password":"p"}]}`)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}

	body := append([]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"ok"}]}`), 0xff)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "direct-token")

	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing invalid utf-8, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid json") {
		t.Fatalf("expected invalid json error, got %q", rec.Body.String())
	}
}
