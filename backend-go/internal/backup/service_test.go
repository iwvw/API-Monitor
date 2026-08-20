package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestBackupRunRestoreAndS3Upload(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "data.db")
	if err := os.WriteFile(dbPath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "filebox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "filebox", "note.txt"), []byte("box"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()

	res := performBackupRequest(service, http.MethodPost, "/api/backup/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run backup status = %d body=%s", res.Code, res.Body.String())
	}
	record := decodeBackupData[Record](t, res)
	if record.ID == "" || record.Size == 0 {
		t.Fatalf("unexpected record: %#v", record)
	}
	if err := os.WriteFile(dbPath, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	res = performBackupRequest(service, http.MethodPost, "/api/backup/restore", `{"id":"`+record.ID+`","confirm":"RESTORE"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("restore status = %d body=%s", res.Code, res.Body.String())
	}
	restored, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "before" {
		t.Fatalf("expected restored database, got %q", restored)
	}

	signed := map[string]bool{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/bucket/api-monitor/") {
			t.Fatalf("unexpected upload request: %s %s", r.Method, r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		switch {
		case strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=key/"):
			signed["s3"] = true
		case strings.HasPrefix(auth, "OSS key:"):
			signed["oss"] = true
		case strings.Contains(auth, "q-sign-algorithm=sha1&q-ak=key"):
			signed["cos"] = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()

	for _, provider := range []string{"s3", "oss", "cos"} {
		res = performBackupRequest(service, http.MethodPost, "/api/backup/configs", `{"provider":"`+provider+`","local_dir":"`+jsonPath(filepath.Join(dataDir, "records"))+`","endpoint":"`+remote.URL+`","bucket":"bucket","access_key_id":"key","access_key_secret":"secret"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("save %s config status = %d body=%s", provider, res.Code, res.Body.String())
		}
		res = performBackupRequest(service, http.MethodPost, "/api/backup/run", "")
		if res.Code != http.StatusOK {
			t.Fatalf("run %s backup status = %d body=%s", provider, res.Code, res.Body.String())
		}
		uploaded := decodeBackupData[Record](t, res)
		if uploaded.RemoteURL == "" || !signed[provider] {
			t.Fatalf("expected signed %s upload, record=%#v signed=%v", provider, uploaded, signed)
		}
	}
}

func TestRestoreRejectsUnsafeZipPath(t *testing.T) {
	dataDir := t.TempDir()
	zipPath := filepath.Join(dataDir, "bad.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipw := zip.NewWriter(file)
	writer, err := zipw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("bad"))
	if err := zipw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()
	if err := service.restoreFromZip(zipPath); err == nil {
		t.Fatal("expected unsafe zip path to be rejected")
	}
}

func TestBackupRecordsPersistRemoteURL(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/bucket/api-monitor/") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()

	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()
	res := performBackupRequest(service, http.MethodPost, "/api/backup/configs", `{"provider":"s3","endpoint":"`+remote.URL+`","bucket":"bucket","access_key_id":"key","access_key_secret":"secret"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("save config status = %d body=%s", res.Code, res.Body.String())
	}
	res = performBackupRequest(service, http.MethodPost, "/api/backup/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run backup status = %d body=%s", res.Code, res.Body.String())
	}
	created := decodeBackupData[Record](t, res)
	if created.RemoteURL == "" {
		t.Fatal("expected uploaded record to carry remote_url")
	}

	res = performBackupRequest(service, http.MethodGet, "/api/backup/records", "")
	if res.Code != http.StatusOK {
		t.Fatalf("records status = %d body=%s", res.Code, res.Body.String())
	}
	listed := decodeBackupData[[]Record](t, res)
	if len(listed) != 1 || listed[0].RemoteURL != created.RemoteURL {
		t.Fatalf("expected listed record with remote_url=%q, got %#v", created.RemoteURL, listed)
	}

	restarted := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer restarted.scheduler.Stop()
	res = performBackupRequest(restarted, http.MethodGet, "/api/backup/records", "")
	listed = decodeBackupData[[]Record](t, res)
	if len(listed) != 1 || listed[0].RemoteURL == "" {
		t.Fatalf("expected remote_url to survive restart, got %#v", listed)
	}

	res = performBackupRequest(service, http.MethodDelete, "/api/backup/records/"+listed[0].ID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", res.Code, res.Body.String())
	}
	res = performBackupRequest(service, http.MethodGet, "/api/backup/records", "")
	listed = decodeBackupData[[]Record](t, res)
	if len(listed) != 0 {
		t.Fatalf("expected empty records after delete, got %#v", listed)
	}
}

func TestBackupMaxRecordsPrune(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()

	res := performBackupRequest(service, http.MethodPost, "/api/backup/configs", `{"provider":"local","max_records":3}`)
	if res.Code != http.StatusOK {
		t.Fatalf("save config status = %d body=%s", res.Code, res.Body.String())
	}
	recordsDir := service.recordsDir()
	if err := os.MkdirAll(recordsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("api-monitor-backup-20260820-060001.%06d.zip", i)
		if err := os.WriteFile(filepath.Join(recordsDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := service.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service.pruneRecords(context.Background(), cfg, cfg.MaxRecords)

	res = performBackupRequest(service, http.MethodGet, "/api/backup/records", "")
	if res.Code != http.StatusOK {
		t.Fatalf("records status = %d body=%s", res.Code, res.Body.String())
	}
	listed := decodeBackupData[[]Record](t, res)
	kept := map[string]bool{}
	for _, record := range listed {
		kept[record.ID] = true
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 records after pruning, got %d: %#v", len(listed), listed)
	}
	for _, drop := range []int{0, 1} {
		if kept[fmt.Sprintf("api-monitor-backup-20260820-060001.%06d.zip", drop)] {
			t.Fatalf("oldest record should have been pruned, kept=%v", kept)
		}
	}
	for _, keep := range []int{2, 3, 4} {
		if !kept[fmt.Sprintf("api-monitor-backup-20260820-060001.%06d.zip", keep)] {
			t.Fatalf("newest record should have been kept, kept=%v", kept)
		}
	}
}

func TestBackupMaxRecordsPruneRemovesRemote(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	deleted := []string{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if strings.Contains(r.URL.Path, "/bucket/api-monitor/") {
				w.WriteHeader(http.StatusOK)
				return
			}
		case http.MethodDelete:
			mu.Lock()
			deleted = append(deleted, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer remote.Close()

	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()
	res := performBackupRequest(service, http.MethodPost, "/api/backup/configs", `{"provider":"s3","max_records":2,"endpoint":"`+remote.URL+`","bucket":"bucket","access_key_id":"key","access_key_secret":"secret"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("save config status = %d body=%s", res.Code, res.Body.String())
	}
	for i := 0; i < 4; i++ {
		res = performBackupRequest(service, http.MethodPost, "/api/backup/run", "")
		if res.Code != http.StatusOK {
			t.Fatalf("run %d status = %d body=%s", i, res.Code, res.Body.String())
		}
	}
	mu.Lock()
	deletedCount := len(deleted)
	mu.Unlock()
	if deletedCount != 2 {
		t.Fatalf("expected 2 remote deletes, got %d: %v", deletedCount, deleted)
	}
	res = performBackupRequest(service, http.MethodGet, "/api/backup/records", "")
	if res.Code != http.StatusOK {
		t.Fatalf("records status = %d body=%s", res.Code, res.Body.String())
	}
	listed := decodeBackupData[[]Record](t, res)
	if len(listed) != 2 {
		t.Fatalf("expected 2 records after pruning, got %d: %#v", len(listed), listed)
	}
}

func performBackupRequest(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func decodeBackupData[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	var payload struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
	if !payload.Success {
		t.Fatalf("expected success payload, got error=%q body=%s", payload.Error, res.Body.String())
	}
	var data T
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		t.Fatalf("decode data %q: %v", string(payload.Data), err)
	}
	return data
}

func jsonPath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}
