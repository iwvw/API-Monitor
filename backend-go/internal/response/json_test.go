package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"message": "hello"}
	JSON(rec, http.StatusAccepted, payload)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", ct)
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if res["message"] != "hello" {
		t.Fatalf("expected hello, got %s", res["message"])
	}
}

func TestOK(t *testing.T) {
	rec := httptest.NewRecorder()
	OK(rec, map[string]int{"count": 42})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if !env.Success {
		t.Fatalf("expected success true")
	}
	if env.Error != "" {
		t.Fatalf("expected empty error, got %s", env.Error)
	}
}

func TestError(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "req-12345")
	Error(rec, http.StatusBadRequest, "invalid parameter")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if env.Success {
		t.Fatalf("expected success false")
	}
	if env.Error != "invalid parameter" {
		t.Fatalf("expected error message 'invalid parameter', got %s", env.Error)
	}
	if env.RequestID != "req-12345" {
		t.Fatalf("expected RequestID req-12345, got %s", env.RequestID)
	}
}
