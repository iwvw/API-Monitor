package systemlogs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestStreamLevelAllReturnsLogLines(t *testing.T) {
	dataDir := t.TempDir()
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "app.log")
	content := `{"time":"2026-06-30T08:00:00Z","level":"INFO","msg":"server started","module":"core"}` + "\n" +
		`{"timestamp":"2026-06-30T08:00:01Z","level":"WARN","message":"disk high","module":"ops"}` + "\n" +
		`plain fallback line` + "\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs/stream?level=all&limit=10", nil)
	rec := httptest.NewRecorder()
	New(config.Config{DataDir: dataDir}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Lines []LogLine `json:"lines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success {
		t.Fatalf("success=false body=%s", rec.Body.String())
	}
	if len(body.Data.Lines) != 3 {
		t.Fatalf("lines = %d, want 3; body=%s", len(body.Data.Lines), rec.Body.String())
	}
	if body.Data.Lines[1].Message != "disk high" || body.Data.Lines[1].Time == "" {
		t.Fatalf("message/timestamp compatibility failed: %+v", body.Data.Lines[1])
	}
	if body.Data.Lines[2].Raw != "plain fallback line" {
		t.Fatalf("raw fallback failed: %+v", body.Data.Lines[2])
	}
}
