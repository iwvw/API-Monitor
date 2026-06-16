package filebox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultMaxFileSize    = 100 * 1024 * 1024
	defaultExpiryHours    = 24
	defaultCodeLength     = 5
	codeAlphabet          = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	maxAccessLogLimit     = 500
	multipartMemoryBudget = 32 << 20
)

type Authenticator interface {
	IsAuthenticated(context.Context, *http.Request) (bool, error)
}

type Service struct {
	cfg          config.Config
	store        *database.Store
	auth         Authenticator
	dataDir      string
	uploadsDir   string
	metadataFile string
}

type Settings struct {
	MaxFileSize         int64    `json:"max_file_size"`
	AllowedMIMETypes    []string `json:"allowed_mime_types"`
	DefaultExpiryHours  int      `json:"default_expiry_hours"`
	PublicUploadEnabled bool     `json:"public_upload_enabled"`
	UpdatedAt           *string  `json:"updated_at"`
}

type Entry struct {
	Code               string                 `json:"code"`
	Type               string                 `json:"type"`
	Content            *string                `json:"content,omitempty"`
	OriginalName       *string                `json:"originalName,omitempty"`
	Filename           string                 `json:"filename"`
	Path               *string                `json:"path,omitempty"`
	MIMEType           *string                `json:"mimetype,omitempty"`
	Size               int64                  `json:"size"`
	CreatedAt          int64                  `json:"createdAt"`
	Expiry             int64                  `json:"expiry"`
	BurnAfterReading   bool                   `json:"burnAfterReading"`
	Downloads          int64                  `json:"downloads"`
	MaxDownloads       int64                  `json:"maxDownloads"`
	AccessPasswordHash *string                `json:"accessPasswordHash,omitempty"`
	RequiresPassword   bool                   `json:"requiresPassword"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

type PublicEntry struct {
	Code             string  `json:"code"`
	Type             string  `json:"type"`
	OriginalName     *string `json:"originalName,omitempty"`
	Filename         string  `json:"filename"`
	MIMEType         *string `json:"mimetype,omitempty"`
	Size             int64   `json:"size"`
	CreatedAt        int64   `json:"createdAt"`
	Expiry           int64   `json:"expiry"`
	BurnAfterReading bool    `json:"burnAfterReading"`
	Downloads        int64   `json:"downloads"`
	MaxDownloads     int64   `json:"maxDownloads"`
	RequiresPassword bool    `json:"requiresPassword"`
	Preview          string  `json:"preview,omitempty"`
}

type AccessLog struct {
	ID        int64   `json:"id"`
	Code      string  `json:"code"`
	Action    string  `json:"action"`
	IPAddress *string `json:"ipAddress"`
	UserAgent *string `json:"userAgent"`
	CreatedAt string  `json:"createdAt"`
}

type requestMeta struct {
	ip        string
	userAgent string
}

type sharePayload struct {
	Type             string `json:"type"`
	Text             string `json:"text"`
	Expiry           string `json:"expiry"`
	BurnAfterReading any    `json:"burn_after_reading"`
	MaxDownloads     string `json:"max_downloads"`
	AccessPassword   string `json:"access_password"`
	Password         string `json:"password"`
}

func New(cfg config.Config, authenticator Authenticator) *Service {
	dataDir := filepath.Join(cfg.DataDir, "filebox")
	service := &Service{
		cfg:          cfg,
		store:        database.New(cfg),
		auth:         authenticator,
		dataDir:      dataDir,
		uploadsDir:   filepath.Join(dataDir, "uploads"),
		metadataFile: filepath.Join(dataDir, "metadata.json"),
	}
	_ = service.ensureDirs()
	_ = service.migrateJSONMetadata(context.Background())
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/filebox")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 2 && parts[0] == "retrieve" && r.Method == http.MethodGet:
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "public" && r.Method == http.MethodGet:
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "download" && r.Method == http.MethodGet:
		s.downloadEntry(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[2] == "download" && r.Method == http.MethodGet:
		s.downloadEntry(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyPublicShare(w, r, parts[1])
	case len(parts) == 1 && (parts[0] == "share" || parts[0] == "shares") && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.createShare(w, r)
	case len(parts) == 1 && parts[0] == "access-logs" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listAccessLogs(w, r)
	case len(parts) == 1 && parts[0] == "settings":
		if !s.requireAuth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r)
		case http.MethodPut:
			s.updateSettings(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "jobs" && parts[1] == "cleanup" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.cleanupJob(w, r)
	case len(parts) == 1 && (parts[0] == "history" || parts[0] == "shares") && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listShares(w, r)
	case len(parts) == 2 && parts[0] == "shares" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "shares" && r.Method == http.MethodDelete:
		if !s.requireAuth(w, r) {
			return
		}
		s.deleteShare(w, r, parts[1])
	case len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete:
		if !s.requireAuth(w, r) {
			return
		}
		s.deleteShare(w, r, parts[0])
	default:
		response.Error(w, http.StatusNotFound, "filebox route not implemented")
	}
}

func (s *Service) sendEntryMetadata(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "File not found or expired"})
		return
	}
	_ = s.LogAccess(r.Context(), entry.Code, "retrieve", metaFromRequest(r))
	response.OK(w, publicEntry(entry))
}

func (s *Service) downloadEntry(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	if !verifyAccessPassword(entry, accessPasswordFromRequest(r)) {
		http.Error(w, "Password required or invalid", http.StatusForbidden)
		return
	}

	if entry.Type == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if entry.Content != nil {
			_, _ = io.WriteString(w, *entry.Content)
		}
		_ = s.AccessEntry(context.Background(), entry.Code, metaFromRequest(r))
		return
	}

	if entry.Path == nil || *entry.Path == "" {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	file, err := os.Open(*entry.Path)
	if err != nil {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	name := entry.Filename
	if entry.OriginalName != nil && *entry.OriginalName != "" {
		name = *entry.OriginalName
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if entry.MIMEType != nil && *entry.MIMEType != "" {
		w.Header().Set("Content-Type", *entry.MIMEType)
	}
	http.ServeContent(w, r, name, stat.ModTime(), file)
	_ = file.Close()
	_ = s.AccessEntry(context.Background(), entry.Code, metaFromRequest(r))
}

func (s *Service) verifyPublicShare(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "File not found or expired"})
		return
	}
	verified := verifyAccessPassword(entry, accessPasswordFromRequest(r))
	status := http.StatusOK
	if !verified {
		status = http.StatusForbidden
	}
	response.JSON(w, status, map[string]interface{}{"success": verified, "requiresPassword": entry.RequiresPassword})
}

func (s *Service) createShare(w http.ResponseWriter, r *http.Request) {
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload, fileHeader, err := parseShareRequest(r, settings.MaxFileSize)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	expiryHours := parseExpiryHours(payload.Expiry, settings.DefaultExpiryHours)
	burn := parseBool(payload.BurnAfterReading)
	maxDownloads := parseNonNegativeInt64(payload.MaxDownloads)
	accessPassword := payload.AccessPassword
	if accessPassword == "" {
		accessPassword = payload.Password
	}

	var entry *Entry
	if strings.EqualFold(payload.Type, "text") {
		if strings.TrimSpace(payload.Text) == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Text content missing"})
			return
		}
		entry, err = s.AddText(r.Context(), payload.Text, expiryHours, burn, maxDownloads, accessPassword)
	} else {
		if fileHeader == nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "No file uploaded"})
			return
		}
		entry, err = s.AddFile(r.Context(), fileHeader, expiryHours, burn, maxDownloads, accessPassword, settings)
	}
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"code":    entry.Code,
		"data":    publicEntry(entry),
		"expiry":  entry.Expiry,
	})
}

func (s *Service) listShares(w http.ResponseWriter, r *http.Request) {
	entries, err := s.GetAll(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, entries)
}

func (s *Service) deleteShare(w http.ResponseWriter, r *http.Request, code string) {
	if _, err := s.DeleteEntry(r.Context(), code); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) listAccessLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.GetAccessLogs(r.Context(), r.URL.Query().Get("code"), r.URL.Query().Get("limit"))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, logs)
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	settings, err := s.UpdateSettings(r.Context(), payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) cleanupJob(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.CleanupExpired(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]int{"deleted": deleted})
}

func (s *Service) LoadSettings(ctx context.Context) (Settings, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer db.Close()
	return loadSettings(ctx, db)
}

func (s *Service) UpdateSettings(ctx context.Context, input map[string]interface{}) (Settings, error) {
	current, err := s.LoadSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	next := Settings{
		MaxFileSize:         positiveInt64(input["max_file_size"], current.MaxFileSize),
		AllowedMIMETypes:    stringSlice(input["allowed_mime_types"], current.AllowedMIMETypes),
		DefaultExpiryHours:  int(positiveInt64(input["default_expiry_hours"], int64(current.DefaultExpiryHours))),
		PublicUploadEnabled: boolValue(input["public_upload_enabled"], current.PublicUploadEnabled),
	}
	if next.MaxFileSize < 1 {
		next.MaxFileSize = defaultMaxFileSize
	}
	if next.DefaultExpiryHours < 1 {
		next.DefaultExpiryHours = defaultExpiryHours
	}

	db, err := s.open(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer db.Close()
	encodedMimeTypes, _ := json.Marshal(next.AllowedMIMETypes)
	_, err = db.ExecContext(ctx, `
		UPDATE filebox_settings
		SET max_file_size = ?,
			allowed_mime_types = ?,
			default_expiry_hours = ?,
			public_upload_enabled = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, next.MaxFileSize, string(encodedMimeTypes), next.DefaultExpiryHours, boolInt(next.PublicUploadEnabled))
	if err != nil {
		return Settings{}, fmt.Errorf("update filebox settings: %w", err)
	}
	return loadSettings(ctx, db)
}

func (s *Service) AddText(ctx context.Context, content string, expiryHours float64, burnAfterReading bool, maxDownloads int64, accessPassword string) (*Entry, error) {
	code, err := s.GenerateCode(ctx, defaultCodeLength)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	expiry := now + int64(expiryHours*float64(time.Hour/time.Millisecond))
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		return nil, err
	}

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_entries (
			code, type, content, filename, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash
		) VALUES (?, 'text', ?, ?, ?, ?, ?, ?, ?)
	`, code, content, "text_"+code+".txt", now, expiry, boolInt(burnAfterReading), maxDownloads, passwordHash)
	if err != nil {
		return nil, fmt.Errorf("create filebox text share: %w", err)
	}
	return s.GetEntry(ctx, code, true)
}

func (s *Service) AddFile(ctx context.Context, fileHeader *multipart.FileHeader, expiryHours float64, burnAfterReading bool, maxDownloads int64, accessPassword string, settings Settings) (*Entry, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	if fileHeader.Size > settings.MaxFileSize {
		return nil, fmt.Errorf("file too large, max %d bytes", settings.MaxFileSize)
	}
	mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if !isMIMEAllowed(mimeType, settings.AllowedMIMETypes) {
		return nil, fmt.Errorf("file type not allowed: %s", emptyDefault(mimeType, "unknown"))
	}
	code, err := s.GenerateCode(ctx, defaultCodeLength)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	expiry := now + int64(expiryHours*float64(time.Hour/time.Millisecond))
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		return nil, err
	}
	safeName := sanitizeFilename(fileHeader.Filename)
	saveFilename := fmt.Sprintf("%d-%s-%s", now, code, safeName)
	savePath := filepath.Join(s.uploadsDir, saveFilename)
	if !isPathInside(s.uploadsDir, savePath) {
		return nil, errors.New("invalid upload path")
	}
	if err := saveUploadedFile(fileHeader, savePath); err != nil {
		return nil, err
	}

	db, err := s.open(ctx)
	if err != nil {
		_ = os.Remove(savePath)
		return nil, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_entries (
			code, type, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash
		) VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, code, fileHeader.Filename, saveFilename, savePath, nullString(mimeType), fileHeader.Size, now, expiry, boolInt(burnAfterReading), maxDownloads, passwordHash)
	if err != nil {
		_ = os.Remove(savePath)
		return nil, fmt.Errorf("create filebox file share: %w", err)
	}
	return s.GetEntry(ctx, code, true)
}

func (s *Service) GenerateCode(ctx context.Context, length int) (string, error) {
	if length < 1 {
		length = defaultCodeLength
	}
	for i := 0; i < 32; i++ {
		code, err := randomCode(length)
		if err != nil {
			return "", err
		}
		exists, err := s.Exists(ctx, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique filebox code")
}

func (s *Service) Exists(ctx context.Context, code string) (bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var found string
	err = db.QueryRowContext(ctx, `SELECT code FROM filebox_entries WHERE code = ? AND deleted_at IS NULL`, normalizeCode(code)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check filebox code: %w", err)
	}
	return true, nil
}

func (s *Service) GetEntry(ctx context.Context, code string, includeExpired bool) (*Entry, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	entry, err := findEntry(ctx, db, normalizeCode(code))
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	if !includeExpired && time.Now().UnixMilli() > entry.Expiry {
		_, _ = s.DeleteEntry(context.Background(), entry.Code)
		return nil, nil
	}
	if !includeExpired && entry.MaxDownloads > 0 && entry.Downloads >= entry.MaxDownloads {
		_, _ = s.DeleteEntry(context.Background(), entry.Code)
		return nil, nil
	}
	return entry, nil
}

func (s *Service) AccessEntry(ctx context.Context, code string, meta requestMeta) error {
	entry, err := s.GetEntry(ctx, code, false)
	if err != nil || entry == nil {
		return err
	}
	nextDownloads := entry.Downloads + 1
	if entry.BurnAfterReading || (entry.MaxDownloads > 0 && nextDownloads >= entry.MaxDownloads) {
		_, err = s.DeleteEntry(ctx, entry.Code)
	} else {
		db, openErr := s.open(ctx)
		if openErr != nil {
			return openErr
		}
		_, err = db.ExecContext(ctx, `UPDATE filebox_entries SET downloads = downloads + 1 WHERE code = ?`, entry.Code)
		_ = db.Close()
	}
	if err != nil {
		return err
	}
	return s.LogAccess(ctx, entry.Code, "download", meta)
}

func (s *Service) DeleteEntry(ctx context.Context, code string) (bool, error) {
	entry, err := s.GetEntry(ctx, code, true)
	if err != nil || entry == nil {
		return false, err
	}
	if entry.Type == "file" && entry.Path != nil && isPathInside(s.uploadsDir, *entry.Path) {
		_ = os.Remove(*entry.Path)
	}
	db, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM filebox_entries WHERE code = ?`, entry.Code)
	if err != nil {
		return false, fmt.Errorf("delete filebox entry: %w", err)
	}
	_ = s.LogAccess(context.Background(), entry.Code, "delete", requestMeta{})
	return true, nil
}

func (s *Service) GetAll(ctx context.Context) ([]PublicEntry, error) {
	if _, err := s.CleanupExpired(ctx); err != nil {
		return nil, err
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, max_downloads, access_password_hash, metadata_json
		FROM filebox_entries
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list filebox entries: %w", err)
	}
	defer rows.Close()
	entries := []PublicEntry{}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, publicEntry(entry))
	}
	return entries, rows.Err()
}

func (s *Service) CleanupExpired(ctx context.Context) (int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT code FROM filebox_entries WHERE expiry < ? AND deleted_at IS NULL`, time.Now().UnixMilli())
	if err != nil {
		_ = db.Close()
		return 0, fmt.Errorf("load expired filebox entries: %w", err)
	}
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			_ = rows.Close()
			_ = db.Close()
			return 0, err
		}
		codes = append(codes, code)
	}
	if err := rows.Close(); err != nil {
		_ = db.Close()
		return 0, err
	}
	_ = db.Close()
	for _, code := range codes {
		_, _ = s.DeleteEntry(ctx, code)
	}
	return len(codes), nil
}

func (s *Service) GetAccessLogs(ctx context.Context, code string, limitText string) ([]AccessLog, error) {
	limit := 100
	if parsed, err := strconv.Atoi(strings.TrimSpace(limitText)); err == nil && parsed > 0 {
		limit = parsed
	}
	if limit > maxAccessLogLimit {
		limit = maxAccessLogLimit
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := []interface{}{}
	where := ""
	if strings.TrimSpace(code) != "" {
		where = "WHERE code = ?"
		args = append(args, normalizeCode(code))
	}
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, code, action, ip_address, user_agent, created_at
		FROM filebox_access_logs
		`+where+`
		ORDER BY created_at DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("load filebox access logs: %w", err)
	}
	defer rows.Close()
	logs := []AccessLog{}
	for rows.Next() {
		var log AccessLog
		var ip, userAgent sql.NullString
		if err := rows.Scan(&log.ID, &log.Code, &log.Action, &ip, &userAgent, &log.CreatedAt); err != nil {
			return nil, err
		}
		log.IPAddress = nullableStringPtr(ip)
		log.UserAgent = nullableStringPtr(userAgent)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (s *Service) LogAccess(ctx context.Context, code string, action string, meta requestMeta) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_access_logs (code, action, ip_address, user_agent)
		VALUES (?, ?, ?, ?)
	`, normalizeCode(code), action, nullString(meta.ip), nullString(meta.userAgent))
	if err != nil {
		return fmt.Errorf("log filebox access: %w", err)
	}
	return nil
}

func (s *Service) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	ok, err := s.auth.IsAuthenticated(r.Context(), r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "please login first"})
		return false
	}
	return true
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) ensureDirs() error {
	if err := os.MkdirAll(s.uploadsDir, 0o755); err != nil {
		return fmt.Errorf("create filebox upload dir: %w", err)
	}
	return nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS filebox_entries (
			code TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			content TEXT,
			original_name TEXT,
			filename TEXT NOT NULL,
			path TEXT,
			mimetype TEXT,
			size INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			expiry INTEGER NOT NULL,
			burn_after_reading INTEGER DEFAULT 0,
			downloads INTEGER DEFAULT 0,
			max_downloads INTEGER DEFAULT 0,
			access_password_hash TEXT,
			metadata_json TEXT,
			deleted_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS filebox_access_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL,
			action TEXT NOT NULL,
			ip_address TEXT,
			user_agent TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS filebox_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			max_file_size INTEGER NOT NULL DEFAULT 104857600,
			allowed_mime_types TEXT NOT NULL DEFAULT '[]',
			default_expiry_hours INTEGER NOT NULL DEFAULT 24,
			public_upload_enabled INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_entries_expiry ON filebox_entries(expiry)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_entries_created ON filebox_entries(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_access_code ON filebox_access_logs(code, created_at)`,
		`INSERT OR IGNORE INTO filebox_settings (
			id, max_file_size, allowed_mime_types, default_expiry_hours, public_upload_enabled
		) VALUES (1, 104857600, '[]', 24, 0)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure filebox schema: %w", err)
		}
	}
	return nil
}

func loadSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	var row struct {
		maxFileSize      sql.NullInt64
		allowedMIMETypes sql.NullString
		defaultExpiry    sql.NullInt64
		publicUpload     sql.NullInt64
		updatedAt        sql.NullString
	}
	err := db.QueryRowContext(ctx, `
		SELECT max_file_size, allowed_mime_types, default_expiry_hours, public_upload_enabled, updated_at
		FROM filebox_settings
		WHERE id = 1
	`).Scan(&row.maxFileSize, &row.allowedMIMETypes, &row.defaultExpiry, &row.publicUpload, &row.updatedAt)
	if err != nil {
		return Settings{}, fmt.Errorf("load filebox settings: %w", err)
	}
	return Settings{
		MaxFileSize:         int64Default(row.maxFileSize, defaultMaxFileSize),
		AllowedMIMETypes:    parseStringArray(row.allowedMIMETypes.String),
		DefaultExpiryHours:  int(int64Default(row.defaultExpiry, defaultExpiryHours)),
		PublicUploadEnabled: row.publicUpload.Valid && row.publicUpload.Int64 == 1,
		UpdatedAt:           nullableStringPtr(row.updatedAt),
	}, nil
}

func findEntry(ctx context.Context, db *sql.DB, code string) (*Entry, error) {
	row := db.QueryRowContext(ctx, `
		SELECT code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, max_downloads, access_password_hash, metadata_json
		FROM filebox_entries
		WHERE code = ? AND deleted_at IS NULL
	`, normalizeCode(code))
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

type entryScanner interface {
	Scan(dest ...interface{}) error
}

func scanEntry(scanner entryScanner) (*Entry, error) {
	var entry Entry
	var content, originalName, pathValue, mimeType, passwordHash, metadataJSON sql.NullString
	var size, createdAt, expiry, burnAfterReading, downloads, maxDownloads sql.NullInt64
	if err := scanner.Scan(
		&entry.Code,
		&entry.Type,
		&content,
		&originalName,
		&entry.Filename,
		&pathValue,
		&mimeType,
		&size,
		&createdAt,
		&expiry,
		&burnAfterReading,
		&downloads,
		&maxDownloads,
		&passwordHash,
		&metadataJSON,
	); err != nil {
		return nil, err
	}
	entry.Content = nullableStringPtr(content)
	entry.OriginalName = nullableStringPtr(originalName)
	entry.Path = nullableStringPtr(pathValue)
	entry.MIMEType = nullableStringPtr(mimeType)
	entry.Size = int64Default(size, 0)
	entry.CreatedAt = int64Default(createdAt, 0)
	entry.Expiry = int64Default(expiry, 0)
	entry.BurnAfterReading = burnAfterReading.Valid && burnAfterReading.Int64 == 1
	entry.Downloads = int64Default(downloads, 0)
	entry.MaxDownloads = int64Default(maxDownloads, 0)
	entry.AccessPasswordHash = nullableStringPtr(passwordHash)
	entry.RequiresPassword = passwordHash.Valid && passwordHash.String != ""
	entry.Metadata = parseObject(metadataJSON.String)
	return &entry, nil
}

func publicEntry(entry *Entry) PublicEntry {
	result := PublicEntry{
		Code:             entry.Code,
		Type:             entry.Type,
		OriginalName:     entry.OriginalName,
		Filename:         entry.Filename,
		MIMEType:         entry.MIMEType,
		Size:             entry.Size,
		CreatedAt:        entry.CreatedAt,
		Expiry:           entry.Expiry,
		BurnAfterReading: entry.BurnAfterReading,
		Downloads:        entry.Downloads,
		MaxDownloads:     entry.MaxDownloads,
		RequiresPassword: entry.RequiresPassword,
	}
	if entry.Type == "text" && entry.Content != nil {
		runes := []rune(*entry.Content)
		if len(runes) > 80 {
			runes = runes[:80]
		}
		result.Preview = string(runes)
	}
	return result
}

func (s *Service) migrateJSONMetadata(ctx context.Context) error {
	if _, err := os.Stat(s.metadataFile); err != nil {
		return nil
	}
	bytes, err := os.ReadFile(s.metadataFile)
	if err != nil {
		return err
	}
	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO filebox_entries (
			code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, item := range raw {
		code := normalizeCode(fmt.Sprint(item["code"]))
		if code == "" {
			continue
		}
		encoded, _ := json.Marshal(item)
		_, err = stmt.ExecContext(
			ctx,
			code,
			emptyDefault(fmt.Sprint(item["type"]), "file"),
			nullStringFromAny(item["content"]),
			nullStringFromAny(item["originalName"]),
			emptyDefault(fmt.Sprint(item["filename"]), "file_"+code),
			nullStringFromAny(item["path"]),
			nullStringFromAny(item["mimetype"]),
			numberFromAny(item["size"]),
			numberFromAny(item["createdAt"]),
			numberFromAny(item["expiry"]),
			boolInt(parseBool(item["burnAfterReading"])),
			numberFromAny(item["downloads"]),
			string(encoded),
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func parseShareRequest(r *http.Request, maxFileSize int64) (sharePayload, *multipart.FileHeader, error) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		defer r.Body.Close()
		var payload sharePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return sharePayload{}, nil, errors.New("request parameter validation failed")
		}
		return payload, nil, nil
	}
	if err := r.ParseMultipartForm(multipartMemoryBudget); err != nil {
		return sharePayload{}, nil, err
	}
	payload := sharePayload{
		Type:           r.FormValue("type"),
		Text:           r.FormValue("text"),
		Expiry:         r.FormValue("expiry"),
		MaxDownloads:   r.FormValue("max_downloads"),
		AccessPassword: r.FormValue("access_password"),
		Password:       r.FormValue("password"),
	}
	if values, ok := r.MultipartForm.Value["burn_after_reading"]; ok && len(values) > 0 {
		payload.BurnAfterReading = values[0]
	}
	file, header, err := r.FormFile("file")
	if err == nil {
		_ = file.Close()
		if header.Size > maxFileSize {
			return sharePayload{}, nil, fmt.Errorf("file too large, max %d bytes", maxFileSize)
		}
		return payload, header, nil
	}
	if errors.Is(err, http.ErrMissingFile) {
		return payload, nil, nil
	}
	return sharePayload{}, nil, err
}

func accessPasswordFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get("password")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.Header.Get("X-Filebox-Password")); value != "" {
		return value
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") && r.Body != nil {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return body.Password
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		return false
	}
	return true
}

func metaFromRequest(r *http.Request) requestMeta {
	ip := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if idx := strings.Index(ip, ","); idx >= 0 {
		ip = strings.TrimSpace(ip[:idx])
	}
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}
	return requestMeta{ip: ip, userAgent: r.UserAgent()}
}

func hashAccessPassword(accessPassword string) (*string, error) {
	if strings.TrimSpace(accessPassword) == "" {
		return nil, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(accessPassword), 10)
	if err != nil {
		return nil, fmt.Errorf("hash filebox access password: %w", err)
	}
	value := string(hash)
	return &value, nil
}

func verifyAccessPassword(entry *Entry, accessPassword string) bool {
	if entry == nil || entry.AccessPasswordHash == nil || *entry.AccessPasswordHash == "" {
		return true
	}
	if accessPassword == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(*entry.AccessPasswordHash), []byte(accessPassword)) == nil
}

func randomCode(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate filebox code: %w", err)
	}
	var builder strings.Builder
	builder.Grow(length)
	for _, value := range bytes {
		builder.WriteByte(codeAlphabet[int(value)%len(codeAlphabet)])
	}
	return builder.String(), nil
}

func parseExpiryHours(value string, fallback int) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 {
		return float64(fallback)
	}
	return parsed
}

func parseNonNegativeInt64(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func parseBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "upload.bin"
	}
	replacer := strings.NewReplacer("<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_")
	base = replacer.Replace(base)
	if len(base) > 180 {
		base = base[:180]
	}
	return base
}

func saveUploadedFile(header *multipart.FileHeader, target string) error {
	src, err := header.Open()
	if err != nil {
		return fmt.Errorf("open uploaded file: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create uploaded file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("save uploaded file: %w", err)
	}
	return nil
}

func isPathInside(root string, candidate string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func isMIMEAllowed(mimeType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	mimeValue := strings.ToLower(strings.TrimSpace(mimeType))
	for _, pattern := range allowed {
		rule := strings.ToLower(strings.TrimSpace(pattern))
		if rule == "" {
			continue
		}
		if strings.HasSuffix(rule, "/*") && strings.HasPrefix(mimeValue, strings.TrimSuffix(rule, "*")) {
			return true
		}
		if mimeValue == rule {
			return true
		}
	}
	return false
}

func positiveInt64(value interface{}, fallback int64) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func stringSlice(value interface{}, fallback []string) []string {
	raw, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return cleanStrings(typed)
		}
		return fallback
	}
	result := []string{}
	for _, item := range raw {
		result = append(result, fmt.Sprint(item))
	}
	return cleanStrings(result)
}

func cleanStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return parseBool(value)
}

func parseStringArray(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return []string{}
	}
	return result
}

func parseObject(value string) map[string]interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func int64Default(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}

func nullString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullStringFromAny(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	return text
}

func numberFromAny(value interface{}) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" || value == "<nil>" {
		return fallback
	}
	return value
}
