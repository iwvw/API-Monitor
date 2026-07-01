package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/robfig/cron/v3"
)

type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

type Service struct {
	cfg       config.Config
	scheduler *cron.Cron
	entry     cron.EntryID
	mu        sync.Mutex
	client    *http.Client
	notifier  Notifier
}

type Config struct {
	Provider        string `json:"provider"`
	LocalDir        string `json:"local_dir"`
	Cron            string `json:"cron"`
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret,omitempty"`
}

type Record struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"`
	Location  string `json:"location"`
	RemoteURL string `json:"remote_url,omitempty"`
}

func New(cfg config.Config) *Service {
	s := &Service{cfg: cfg, scheduler: cron.New(), client: &http.Client{Timeout: 10 * time.Minute}}
	s.scheduler.Start()
	_ = s.reloadSchedule(context.Background())
	return s
}

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/backup"), "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	switch {
	case len(parts) == 1 && parts[0] == "configs":
		if r.Method == http.MethodGet {
			s.getConfig(w, r)
			return
		}
		if r.Method == http.MethodPost {
			s.saveConfig(w, r)
			return
		}
	case len(parts) == 1 && parts[0] == "records" && r.Method == http.MethodGet:
		s.listRecords(w, r)
		return
	case len(parts) == 1 && parts[0] == "run" && r.Method == http.MethodPost:
		s.runBackup(w, r)
		return
	case len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost:
		s.restoreBackup(w, r)
		return
	case len(parts) == 2 && parts[0] == "records" && r.Method == http.MethodDelete:
		s.deleteRecord(w, r, parts[1])
		return
	case len(parts) == 3 && parts[0] == "records" && parts[2] == "download" && r.Method == http.MethodGet:
		s.downloadRecord(w, r, parts[1])
		return
	}
	response.Error(w, http.StatusNotFound, "backup route not implemented")
}

func (s *Service) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadConfig(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg.AccessKeySecret = ""
	response.OK(w, cfg)
}

func (s *Service) saveConfig(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var cfg Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid backup config")
		return
	}
	cfg.Provider = first(cfg.Provider, "local")
	cfg.LocalDir = first(cfg.LocalDir, s.recordsDir())
	if strings.TrimSpace(cfg.Cron) != "" {
		if _, err := cron.ParseStandard(cfg.Cron); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid backup cron")
			return
		}
	}
	if cfg.Provider != "local" && cfg.Provider != "oss" && cfg.Provider != "cos" && cfg.Provider != "s3" {
		response.Error(w, http.StatusBadRequest, "unsupported backup provider")
		return
	}
	if strings.TrimSpace(cfg.AccessKeySecret) == "" {
		if existing, err := s.loadConfig(r.Context()); err == nil {
			cfg.AccessKeySecret = existing.AccessKeySecret
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath()), 0o755); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(s.configPath(), data, 0o600); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.reloadSchedule(context.Background())
	cfg.AccessKeySecret = ""
	response.OK(w, cfg)
}

func (s *Service) listRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.records(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, records)
}

func (s *Service) runBackup(w http.ResponseWriter, r *http.Request) {
	record, err := s.createBackup(r.Context())
	s.triggerBackupNotify(r.Context(), record, err)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, record)
}

func (s *Service) deleteRecord(w http.ResponseWriter, r *http.Request, id string) {
	path, ok := s.recordPath(r.Context(), id)
	if !ok {
		response.Error(w, http.StatusBadRequest, "invalid backup id")
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Service) downloadRecord(w http.ResponseWriter, r *http.Request, id string) {
	path, ok := s.recordPath(r.Context(), id)
	if !ok {
		response.Error(w, http.StatusBadRequest, "invalid backup id")
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Service) restoreBackup(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload struct {
		ID      string `json:"id"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid restore payload")
		return
	}
	if payload.Confirm != "RESTORE" {
		response.Error(w, http.StatusBadRequest, "restore requires confirm=RESTORE")
		return
	}
	path, ok := s.recordPath(r.Context(), payload.ID)
	if !ok {
		response.Error(w, http.StatusBadRequest, "invalid backup id")
		return
	}
	err := s.restoreFromZip(path)
	s.triggerRestoreNotify(r.Context(), payload.ID, err)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Service) createBackup(ctx context.Context) (Record, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(cfg.LocalDir, 0o755); err != nil {
		return Record{}, err
	}
	name := "api-monitor-backup-" + time.Now().Format("20060102-150405") + ".zip"
	target := filepath.Join(cfg.LocalDir, name)
	file, err := os.Create(target)
	if err != nil {
		return Record{}, err
	}
	zipw := zip.NewWriter(file)
	writeErr := s.addBackupFiles(zipw)
	closeErr := zipw.Close()
	fileErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(target)
		return Record{}, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return Record{}, closeErr
	}
	if fileErr != nil {
		_ = os.Remove(target)
		return Record{}, fileErr
	}
	info, err := os.Stat(target)
	if err != nil {
		return Record{}, err
	}
	record := recordFromInfo(info, cfg.LocalDir)
	if cfg.Provider != "local" {
		remoteURL, err := s.uploadBackup(ctx, cfg, target, name)
		if err != nil {
			return Record{}, err
		}
		record.RemoteURL = remoteURL
	}
	return record, nil
}

func (s *Service) addBackupFiles(zipw *zip.Writer) error {
	if err := addFile(zipw, s.cfg.DatabasePath(), "data/"+s.cfg.DBName); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, dir := range []string{"filebox", "files"} {
		root := filepath.Join(s.cfg.DataDir, dir)
		if err := addDir(zipw, root, "data/"+dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Service) records(ctx context.Context) ([]Record, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(cfg.LocalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Record{}, nil
		}
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			records = append(records, recordFromInfo(info, cfg.LocalDir))
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt > records[j].CreatedAt })
	return records, nil
}

func (s *Service) loadConfig(ctx context.Context) (Config, error) {
	cfg := Config{Provider: "local", LocalDir: s.recordsDir()}
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Provider = first(cfg.Provider, "local")
	cfg.LocalDir = first(cfg.LocalDir, s.recordsDir())
	return cfg, nil
}

func (s *Service) reloadSchedule(ctx context.Context) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entry != 0 {
		s.scheduler.Remove(s.entry)
		s.entry = 0
	}
	if strings.TrimSpace(cfg.Cron) == "" {
		return nil
	}
	entry, err := s.scheduler.AddFunc(cfg.Cron, func() {
		ctx := context.Background()
		record, err := s.createBackup(ctx)
		s.triggerBackupNotify(ctx, record, err)
	})
	if err != nil {
		return err
	}
	s.entry = entry
	return nil
}

func (s *Service) triggerBackupNotify(ctx context.Context, record Record, err error) {
	if s.notifier == nil {
		return
	}
	eventData := map[string]interface{}{}
	if err != nil {
		eventData["status"] = "failed"
		eventData["error"] = err.Error()
	} else {
		eventData["status"] = "success"
		eventData["backupId"] = record.ID
		eventData["fileName"] = record.FileName
		eventData["size"] = record.Size
		eventData["location"] = record.Location
		eventData["remoteUrl"] = record.RemoteURL
	}
	_ = s.notifier.Trigger(ctx, "system", "database.backup", eventData)
}

func (s *Service) triggerRestoreNotify(ctx context.Context, backupID string, err error) {
	if s.notifier == nil {
		return
	}
	eventData := map[string]interface{}{"backupId": backupID}
	if err != nil {
		eventData["status"] = "failed"
		eventData["error"] = err.Error()
	} else {
		eventData["status"] = "success"
	}
	_ = s.notifier.Trigger(ctx, "system", "database.import", eventData)
}

func (s *Service) uploadBackup(ctx context.Context, cfg Config, path, objectName string) (string, error) {
	switch cfg.Provider {
	case "s3", "oss", "cos":
		return s.uploadObject(ctx, cfg, path, objectName)
	default:
		return "", fmt.Errorf("unsupported backup provider: %s", cfg.Provider)
	}
}

func (s *Service) uploadObject(ctx context.Context, cfg Config, path, objectName string) (string, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return "", fmt.Errorf("cloud endpoint, bucket, access key and secret are required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	objectKey := "api-monitor/" + objectName
	target := endpoint + "/" + strings.Trim(cfg.Bucket, "/") + "/" + objectKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/zip")
	now := time.Now().UTC()
	switch cfg.Provider {
	case "oss":
		signOSSRequest(req, cfg, now)
	case "cos":
		signCOSRequest(req, cfg, now)
	default:
		req.Header.Set("X-Amz-Content-Sha256", sha256HexBytes(body))
		req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
		signS3Request(req, cfg, now, "auto")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("%s upload failed: status %d %s", strings.ToUpper(cfg.Provider), res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return target, nil
}

func (s *Service) restoreFromZip(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		clean := filepath.Clean(file.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe backup path: %s", file.Name)
		}
		target, ok := s.restoreTarget(clean)
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		tmp := target + ".restore-tmp"
		dst, err := os.Create(tmp)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}

func (s *Service) restoreTarget(name string) (string, bool) {
	name = filepath.ToSlash(name)
	if name == "data/"+s.cfg.DBName {
		return s.cfg.DatabasePath(), true
	}
	for _, dir := range []string{"filebox", "files"} {
		prefix := "data/" + dir + "/"
		if strings.HasPrefix(name, prefix) {
			rel := strings.TrimPrefix(name, prefix)
			if rel == "" || strings.HasPrefix(rel, "../") {
				return "", false
			}
			return filepath.Join(s.cfg.DataDir, dir, filepath.FromSlash(rel)), true
		}
	}
	return "", false
}

func (s *Service) recordPath(ctx context.Context, id string) (string, bool) {
	name := filepath.Base(id)
	if name != id || !strings.HasSuffix(name, ".zip") {
		return "", false
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return "", false
	}
	return filepath.Join(cfg.LocalDir, name), true
}

func (s *Service) configPath() string { return filepath.Join(s.cfg.DataDir, "backup", "config.json") }
func (s *Service) recordsDir() string { return filepath.Join(s.cfg.DataDir, "backup", "records") }

func recordFromInfo(info os.FileInfo, dir string) Record {
	return Record{ID: info.Name(), FileName: info.Name(), Size: info.Size(), CreatedAt: info.ModTime().Unix(), Location: filepath.Join(dir, info.Name())}
}

func addDir(zipw *zip.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return addFile(zipw, path, filepath.ToSlash(filepath.Join(prefix, rel)))
	})
}

func addFile(zipw *zip.Writer, path, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer, err := zipw.Create(filepath.ToSlash(name))
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func signS3Request(req *http.Request, cfg Config, now time.Time, region string) {
	host := req.URL.Host
	date := now.Format("20060102")
	scope := date + "/" + region + "/s3/aws4_request"
	payloadHash := req.Header.Get("X-Amz-Content-Sha256")
	canonicalURI := req.URL.EscapedPath()
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + req.Header.Get("X-Amz-Date") + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{req.Method, canonicalURI, "", canonicalHeaders, signedHeaders, payloadHash}, "\n")
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", req.Header.Get("X-Amz-Date"), scope, sha256Hex(canonicalRequest)}, "\n")
	signingKey := s3SigningKey(cfg.AccessKeySecret, date, region)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	req.Host = host
}

func signOSSRequest(req *http.Request, cfg Config, now time.Time) {
	date := now.Format(http.TimeFormat)
	req.Header.Set("Date", date)
	objectPath := req.URL.EscapedPath()
	if idx := strings.Index(objectPath, "/api-monitor/"); idx >= 0 {
		objectPath = objectPath[idx:]
	}
	resource := "/" + strings.Trim(cfg.Bucket, "/") + objectPath
	stringToSign := strings.Join([]string{req.Method, "", req.Header.Get("Content-Type"), date, resource}, "\n")
	mac := hmac.New(sha1.New, []byte(cfg.AccessKeySecret))
	mac.Write([]byte(stringToSign))
	req.Header.Set("Authorization", "OSS "+cfg.AccessKeyID+":"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func signCOSRequest(req *http.Request, cfg Config, now time.Time) {
	start := now.Unix()
	end := now.Add(15 * time.Minute).Unix()
	keyTime := fmt.Sprintf("%d;%d", start, end)
	headerList := "content-type;host"
	urlParamList := ""
	canonicalURI := req.URL.EscapedPath()
	canonicalHeaders := "content-type=" + url.QueryEscape(strings.ToLower(req.Header.Get("Content-Type"))) + "&host=" + url.QueryEscape(strings.ToLower(req.URL.Host))
	httpString := strings.Join([]string{strings.ToLower(req.Method), canonicalURI, "", canonicalHeaders, ""}, "\n")
	signKey := hmacSHA1([]byte(cfg.AccessKeySecret), keyTime)
	stringToSign := strings.Join([]string{"sha1", keyTime, sha1Hex(httpString), ""}, "\n")
	signature := hex.EncodeToString(hmacSHA1(signKey, stringToSign))
	req.Header.Set("Authorization", "q-sign-algorithm=sha1&q-ak="+cfg.AccessKeyID+"&q-sign-time="+keyTime+"&q-key-time="+keyTime+"&q-header-list="+headerList+"&q-url-param-list="+urlParamList+"&q-signature="+signature)
}

func s3SigningKey(secret, date, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func hmacSHA1(key []byte, value string) []byte {
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha1Hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256HexBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
