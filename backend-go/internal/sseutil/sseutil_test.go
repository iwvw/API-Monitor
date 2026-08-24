package sseutil

import (
	"net/http/httptest"
	"testing"
)

func TestRenewWriteDeadlineToleratesUnsupportedWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := RenewWriteDeadline(rec, 0); err != nil {
		t.Fatalf("httptest.Recorder has no ResponseController, but RenewWriteDeadline returned %v", err)
	}
	rec.WriteHeader(200)
}

func TestRenewWriteDeadlineDefaultKeepAlive(t *testing.T) {
	if DefaultKeepAlive <= 0 {
		t.Fatal("DefaultKeepAlive must be positive")
	}
}