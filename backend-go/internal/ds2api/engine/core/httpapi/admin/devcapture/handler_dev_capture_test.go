package devcapture

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDevCapturesShape(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/dev/captures", nil)
	h.getDevCaptures(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := out["enabled"]; !ok {
		t.Fatalf("expected enabled field, got %#v", out)
	}
	if _, ok := out["items"]; !ok {
		t.Fatalf("expected items field, got %#v", out)
	}
}

func TestClearDevCapturesShape(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/dev/captures", nil)
	h.clearDevCaptures(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("expected success=true, got %#v", out)
	}
}
