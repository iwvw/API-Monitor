package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

// writeMarkerDB 创建带 restore_marker 标记行的真实 SQLite 库；恢复流程
// 会对库文件做 integrity_check，不能用任意字节伪造。
func writeMarkerDB(t *testing.T, path, marker string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS restore_marker (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM restore_marker`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO restore_marker (value) VALUES (?)`, marker); err != nil {
		t.Fatal(err)
	}
}

func readMarkerDB(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(`SELECT value FROM restore_marker LIMIT 1`).Scan(&value); err != nil {
		t.Fatalf("read marker from %s: %v", path, err)
	}
	return value
}

func TestBackupRunRestoreAndS3Upload(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "data.db")
	writeMarkerDB(t, dbPath, "before")
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
	writeMarkerDB(t, dbPath, "after")
	// 恢复前遗留旧 sidecar：恢复流程必须清理，否则新库会做错误的 WAL 回放。
	if err := os.WriteFile(dbPath+"-wal", []byte("stale-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("stale-shm"), 0o600); err != nil {
		t.Fatal(err)
	}
	res = performBackupRequest(service, http.MethodPost, "/api/backup/restore", `{"id":"`+record.ID+`","confirm":"RESTORE"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("restore status = %d body=%s", res.Code, res.Body.String())
	}
	if got := readMarkerDB(t, dbPath); got != "before" {
		t.Fatalf("expected restored database marker, got %q", got)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar %s must be removed after restore", sidecar)
		}
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

// 多渠道同时备份：一次备份应同时上传到所有配置渠道，并在记录上带出各渠道远端地址。
func TestBackupMultiChannelUpload(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "data.db")
	writeMarkerDB(t, dbPath, "before")

	s3Done := false
	webdavDone := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/bucket/api-monitor/"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=key/") {
				t.Fatalf("unexpected s3 auth: %q", r.Header.Get("Authorization"))
			}
			s3Done = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/backup/backups/api-monitor-backup-") && strings.HasSuffix(r.URL.Path, ".zip"):
			if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("dav-user:dav-pass")) {
				t.Fatalf("unexpected webdav auth: %q", r.Header.Get("Authorization"))
			}
			webdavDone = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer remote.Close()

	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()

	payload := `{"local_dir":"` + jsonPath(filepath.Join(dataDir, "records")) + `","channels":[
		{"provider":"s3","endpoint":"` + remote.URL + `","bucket":"bucket","access_key_id":"key","access_key_secret":"secret"},
		{"provider":"webdav","name":"私有WebDAV","endpoint":"` + remote.URL + `/backup","username":"dav-user","password":"dav-pass","remote_path":"backups"}
	]}`
	res := performBackupRequest(service, http.MethodPost, "/api/backup/configs", payload)
	if res.Code != http.StatusOK {
		t.Fatalf("save multi-channel config status = %d body=%s", res.Code, res.Body.String())
	}

	res = performBackupRequest(service, http.MethodPost, "/api/backup/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run multi-channel backup status = %d body=%s", res.Code, res.Body.String())
	}
	record := decodeBackupData[Record](t, res)
	if !s3Done || !webdavDone {
		t.Fatalf("expected uploads to all channels, s3=%v webdav=%v", s3Done, webdavDone)
	}
	if len(record.RemoteURLs) != 2 {
		t.Fatalf("expected 2 remote uploads, got %#v", record.RemoteURLs)
	}
	if record.RemoteURL == "" {
		t.Fatal("expected record.RemoteURL to be populated")
	}

	// 渠道校验：secret 不出现在保存响应中。
	var payload2 struct {
		Success bool   `json:"success"`
		Data    Config `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload2); err != nil {
		t.Fatal(err)
	}
	for _, ch := range payload2.Data.Channels {
		if ch.AccessKeySecret != "" || ch.Password != "" {
			t.Fatalf("save response must not leak secrets: %#v", ch)
		}
	}
}

// WebDAV 渠道上传：验证 Basic Auth、MKCOL 建目录、PUT 落点。
func TestBackupWebDAVUpload(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := []string{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == "MKCOL" {
			if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")) {
				t.Fatalf("unexpected MKCOL auth: %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method == http.MethodPut {
			if !strings.HasSuffix(r.URL.Path, "/dav/backups/api-monitor-backup-test.zip") {
				t.Fatalf("unexpected put path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer remote.Close()

	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()
	dummy := filepath.Join(dataDir, "dummy.zip")
	if err := os.WriteFile(dummy, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := Channel{Provider: "webdav", Endpoint: remote.URL + "/dav", Username: "u", Password: "p", RemotePath: "backups"}
	target, err := service.uploadWebDAV(context.Background(), ch, dummy, "api-monitor-backup-test.zip")
	if err != nil {
		t.Fatalf("uploadWebDAV: %v", err)
	}
	if !strings.Contains(target, "/dav/backups/api-monitor-backup-test.zip") {
		t.Fatalf("target = %q", target)
	}
	mu.Lock()
	defer mu.Unlock()
	foundMKCOL := false
	foundPUT := false
	for _, req := range requests {
		if req == "MKCOL /dav/backups" {
			foundMKCOL = true
		}
		if req == "PUT /dav/backups/api-monitor-backup-test.zip" {
			foundPUT = true
		}
	}
	if !foundMKCOL || !foundPUT {
		t.Fatalf("expected MKCOL + PUT, got %v", requests)
	}
}

// 旧版单渠道配置迁移：provider/endpoint/bucket 应被读成一条渠道。
func TestBackupLegacyConfigMigration(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := filepath.Join(dataDir, "backup")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"provider":"cos","local_dir":"` + jsonPath(filepath.Join(dataDir, "records")) + `","cron":"","endpoint":"https://cos.example.com","bucket":"mybucket","access_key_id":"ak","access_key_secret":"sk","max_records":5}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()
	cfg, err := service.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Provider != "cos" || cfg.Channels[0].AccessKeySecret != "sk" {
		t.Fatalf("expected migrated cos channel, got %#v", cfg.Channels)
	}
}

// 多渠道裁剪：本地与多个远端对象应随 max_records 一起清理。
func TestBackupMultiChannelPruneRemovesRemotes(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	deleted := []string{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MKCOL" {
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodDelete {
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
	payload := `{"local_dir":"` + jsonPath(filepath.Join(dataDir, "records")) + `","max_records":2,"channels":[
		{"provider":"s3","endpoint":"` + remote.URL + `","bucket":"bucket","access_key_id":"key","access_key_secret":"secret"},
		{"provider":"webdav","endpoint":"` + remote.URL + `/dav","username":"u","password":"p","remote_path":"backups"}
	]}`
	res := performBackupRequest(service, http.MethodPost, "/api/backup/configs", payload)
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
	if deletedCount != 4 {
		t.Fatalf("expected 4 remote deletes (2 per record x 2 channels), got %d: %v", deletedCount, deleted)
	}
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

// 恢复必须与导入/压缩共用换库互斥：互斥被持有时恢复阻塞等待，
// 释放后才执行替换。
func TestRestoreWaitsForSwapMutex(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "data.db")
	writeMarkerDB(t, dbPath, "current")

	sourceDB := filepath.Join(dataDir, "source.db")
	writeMarkerDB(t, sourceDB, "restored")
	zipPath := filepath.Join(dataDir, "restore-me.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipw := zip.NewWriter(file)
	entry, err := zipw.Create("data/data.db")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(entry, src); err != nil {
		t.Fatal(err)
	}
	src.Close()
	if err := zipw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	defer service.scheduler.Stop()

	swapMu := database.SwapMutex()
	swapMu.Lock()
	done := make(chan error, 1)
	go func() { done <- service.restoreFromZip(zipPath) }()
	select {
	case err := <-done:
		swapMu.Unlock()
		t.Fatalf("restore must block while swap mutex is held, got err=%v", err)
	case <-time.After(150 * time.Millisecond):
	}
	swapMu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restore after releasing swap mutex: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not finish after swap mutex was released")
	}
	if got := readMarkerDB(t, dbPath); got != "restored" {
		t.Fatalf("restored marker = %q, want restored", got)
	}
}

// 上传必须流式发送文件：ContentLength 来自 Stat、请求体逐块传输、
// S3 签名的 payload SHA-256 与文件内容一致；失败（非 2xx）必须报错。
func TestUploadObjectStreamsLargeFile(t *testing.T) {
	dir := t.TempDir()
	uploadPath := filepath.Join(dir, "big-backup.zip")
	payload := make([]byte, 8<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(payload))

	var mu sync.Mutex
	sawChunked := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		for _, enc := range r.TransferEncoding {
			if enc == "chunked" {
				mu.Lock()
				sawChunked = true
				mu.Unlock()
			}
		}
		if r.ContentLength != int64(len(payload)) {
			t.Errorf("ContentLength = %d, want %d", r.ContentLength, len(payload))
		}
		if got := r.Header.Get("X-Amz-Content-Sha256"); got != wantHash {
			t.Errorf("X-Amz-Content-Sha256 = %q, want %q", got, wantHash)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("uploaded body mismatch: got %d bytes, want %d", len(got), len(payload))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	defer service.scheduler.Stop()
	ch := Channel{Provider: "s3", Endpoint: remote.URL, Bucket: "bucket", AccessKeyID: "key", AccessKeySecret: "secret"}
	target, err := service.uploadObject(context.Background(), ch, uploadPath, "test.zip")
	if err != nil {
		t.Fatalf("uploadObject: %v", err)
	}
	if !strings.Contains(target, "/bucket/api-monitor/test.zip") {
		t.Fatalf("upload target = %q", target)
	}
	mu.Lock()
	chunked := sawChunked
	mu.Unlock()
	if chunked {
		t.Fatal("upload must send a fixed ContentLength, not chunked encoding")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()
	ch.Endpoint = failing.URL
	if _, err := service.uploadObject(context.Background(), ch, uploadPath, "test.zip"); err == nil {
		t.Fatal("upload against 5xx endpoint must fail loudly")
	}
}
