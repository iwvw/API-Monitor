package requestbody

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type singleByteReadCloser struct {
	data []byte
	pos  int
}

func (r *singleByteReadCloser) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func (r *singleByteReadCloser) Close() error {
	return nil
}

func TestValidateJSONUTF8AllowsSplitMultibyteRunes(t *testing.T) {
	body := []byte(`{"text":"你好"}`)
	handler := ValidateJSONUTF8(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", &singleByteReadCloser{data: body})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for valid utf-8 json, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestValidateJSONUTF8RejectsInvalidBytesBeforeJSONDecode(t *testing.T) {
	body := append([]byte(`{"text":"`), 0xff)
	body = append(body, []byte(`"}`)...)
	handler := ValidateJSONUTF8(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid utf-8 json, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid utf-8") {
		t.Fatalf("expected utf-8 validation error, got %q", rec.Body.String())
	}
}

func TestValidateJSONUTF8RejectsInvalidBytesWithoutJSONContentTypeOnKnownPath(t *testing.T) {
	body := append([]byte(`{"text":"`), 0xff)
	body = append(body, []byte(`"}`)...)
	handler := ValidateJSONUTF8(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid utf-8 json, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid utf-8") {
		t.Fatalf("expected utf-8 validation error, got %q", rec.Body.String())
	}
}

func TestValidateJSONUTF8RejectsTrailingInvalidBytesAfterJSONValue(t *testing.T) {
	body := append([]byte(`{"text":"ok"}`), 0xff)
	handler := ValidateJSONUTF8(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing invalid utf-8, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "invalid utf-8") {
		t.Fatalf("expected utf-8 validation error, got %q", rec.Body.String())
	}
}

func TestIsJSONContentType(t *testing.T) {
	for _, raw := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/problem+json",
		"application/vnd.api+json",
	} {
		if !isJSONContentType(raw) {
			t.Fatalf("expected %q to be recognized as json", raw)
		}
	}
	for _, raw := range []string{
		"multipart/form-data; boundary=abc",
		"text/plain",
		"application/octet-stream",
	} {
		if isJSONContentType(raw) {
			t.Fatalf("expected %q not to be recognized as json", raw)
		}
	}
}

func TestIsKnownJSONRequestPathIncludesGeminiStream(t *testing.T) {
	if !isKnownJSONRequestPath(http.MethodPost, "/v1beta/models/gemini-pro:streamGenerateContent") {
		t.Fatal("expected Gemini stream generate path to be recognized as json")
	}
}
