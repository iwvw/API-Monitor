package settings

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg            config.Config
	store          *database.Store
	pendingImports map[string]pendingDatabaseImport
	importsMu      sync.Mutex
}

type userSettingsRow struct {
	CustomCSS             sql.NullString
	ThemeMode             sql.NullString
	PageWidthMode         sql.NullString
	SidebarCollapsed      sql.NullInt64
	KoyebRefreshInterval  sql.NullInt64
	FlyRefreshInterval    sql.NullInt64
	ModuleVisibility      sql.NullString
	ChannelEnabled        sql.NullString
	ChannelModelPrefix    sql.NullString
	ModuleOrder           sql.NullString
	LoadBalancingStrategy sql.NullString
	ServerIPDisplayMode   sql.NullString
	VibrationEnabled      sql.NullInt64
	MainTabsLayout        sql.NullString
	TOTPSettings          sql.NullString
	AgentDownloadURL      sql.NullString
	PublicAPIURL          sql.NullString
}

type tableAnalysis struct {
	Table              string `json:"table"`
	Rows               int64  `json:"rows"`
	EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
	EstimatedSizeMB    string `json:"estimatedSizeMB"`
	AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
	Error              string `json:"error,omitempty"`
}

type pendingDatabaseImport struct {
	Path         string
	OriginalName string
	CreatedAt    time.Time
	Analysis     databaseImportAnalysis
}

type databaseImportAnalysis struct {
	Integrity  string                `json:"integrity"`
	Tables     []databaseImportTable `json:"tables"`
	TableCount int                   `json:"tableCount"`
	SizeBytes  int64                 `json:"sizeBytes"`
}

type databaseImportTable struct {
	Name string `json:"name"`
	Rows *int64 `json:"rows"`
}

const (
	maxDatabaseImportBytes = int64(2 << 30)
	databaseImportTTL      = 30 * time.Minute
)

func New(cfg config.Config) *Service {
	return &Service{
		cfg:            cfg,
		store:          database.New(cfg),
		pendingImports: map[string]pendingDatabaseImport{},
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/settings":
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r)
		case http.MethodPost:
			s.saveSettings(w, r, false)
		case http.MethodPatch:
			s.saveSettings(w, r, true)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "/api/settings/database-stats":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getDatabaseStats(w, r)
	case "/api/settings/migration-self-check":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getMigrationSelfCheck(w, r)
	case "/api/settings/database-analysis":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getDatabaseAnalysis(w, r)
	case "/api/settings/export-database":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.exportDatabase(w, r)
	case "/api/settings/database/import/preview", "/api/settings/import-database/preview":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.previewDatabaseImport(w, r)
	case "/api/settings/database/import/commit", "/api/settings/import-database/commit":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.commitDatabaseImport(w, r)
	case "/api/settings/import-database":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.importDatabaseLegacy(w, r)
	case "/api/settings/operation-logs":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getOperationLogs(w, r)
	case "/api/settings/sys-logs":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getSystemLogs(w, r)
	case "/api/settings/app-log-file":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getAppLogFile(w, r)
	case "/api/settings/log-settings":
		switch r.Method {
		case http.MethodGet:
			s.getLogSettings(w, r)
		case http.MethodPost:
			s.saveLogSettings(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "/api/settings/clear-app-logs":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.clearAppLogs(w, r)
	case "/api/settings/vacuum-database":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.vacuumDatabase(w, r)
	case "/api/settings/clear-logs":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.clearLogs(w, r)
	case "/api/settings/enforce-log-limits":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.enforceLogLimits(w, r)
	case "/api/settings/clear-chat-messages":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.clearChatMessages(w, r)
	default:
		response.Error(w, http.StatusNotFound, "settings route not implemented")
	}
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	settings, err := loadUserSettings(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) saveSettings(w http.ResponseWriter, r *http.Request, mergeCurrent bool) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}

	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if mergeCurrent {
		current, err := loadUserSettings(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		for key, value := range payload {
			current[key] = value
		}
		payload = current
	}

	if err := saveUserSettings(r.Context(), db, payload); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	message := "设置已保存"
	if mergeCurrent {
		message = "设置已更新"
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": message})
}

func (s *Service) getDatabaseStats(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	tables, err := listUserTables(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats := make(map[string]int64, len(tables))
	for _, table := range tables {
		count, err := countTableRows(r.Context(), db, table)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		stats[table] = count
	}

	dbSize, err := fileSize(s.store.DatabasePath())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"dbPath": s.store.DatabasePath(),
		"dbSize": dbSize,
		"tables": stats,
	})
}

func (s *Service) getMigrationSelfCheck(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	result := map[string]interface{}{}
	allOK := true
	for table, requiredColumns := range migrationRequiredTables() {
		exists, err := tableExists(r.Context(), db, table)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !exists {
			result[table] = map[string]interface{}{
				"ok":             false,
				"exists":         false,
				"missingColumns": requiredColumns,
			}
			allOK = false
			continue
		}

		actualColumns, err := tableColumns(r.Context(), db, table)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		missingColumns := missingStrings(requiredColumns, actualColumns)
		ok := len(missingColumns) == 0
		if !ok {
			allOK = false
		}
		result[table] = map[string]interface{}{
			"ok":             ok,
			"exists":         true,
			"missingColumns": missingColumns,
		}
	}

	response.OK(w, map[string]interface{}{
		"ok":        allOK,
		"checkedAt": nowRFC3339(),
		"tables":    result,
	})
}

func (s *Service) getDatabaseAnalysis(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	tables, err := listUserTables(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	analysis := make([]tableAnalysis, 0, len(tables))
	for _, table := range tables {
		item, err := analyzeTable(r.Context(), db, table)
		if err != nil {
			item = tableAnalysis{
				Table: table,
				Rows:  0,
				Error: err.Error(),
			}
		}
		analysis = append(analysis, item)
	}

	sort.SliceStable(analysis, func(i, j int) bool {
		if analysis[i].EstimatedSizeBytes == analysis[j].EstimatedSizeBytes {
			return analysis[i].Table < analysis[j].Table
		}
		return analysis[i].EstimatedSizeBytes > analysis[j].EstimatedSizeBytes
	})

	dbSize, err := fileSize(s.store.DatabasePath())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"dbFileSizeMB": sizeMBString(dbSize),
		"tables":       analysis,
	})
}

func (s *Service) exportDatabase(w http.ResponseWriter, r *http.Request) {
	tempDir := filepath.Join(s.cfg.DataDir, "temp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "create backup dir: " + err.Error()})
		return
	}

	backupFileName := "api-monitor-backup-" + timestampForFile(time.Now()) + ".db"
	backupPath := filepath.Join(tempDir, backupFileName)
	if err := s.backupCurrentDatabase(r.Context(), backupPath); err != nil {
		_ = os.Remove(backupPath)
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "数据库导出失败: " + err.Error()})
		return
	}
	defer os.Remove(backupPath)

	file, err := os.Open(backupPath)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "open database backup: " + err.Error()})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "stat database backup: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+backupFileName+`"`)
	http.ServeContent(w, r, backupFileName, stat.ModTime(), file)
}

func (s *Service) previewDatabaseImport(w http.ResponseWriter, r *http.Request) {
	uploaded, err := s.saveUploadedDatabase(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errDatabaseImportTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		response.JSON(w, status, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	analysis, err := analyzeDatabaseFile(r.Context(), uploaded.Path)
	if err != nil {
		_ = os.Remove(uploaded.Path)
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "数据库完整性检查失败: " + err.Error()})
		return
	}
	if analysis.Integrity != "ok" {
		_ = os.Remove(uploaded.Path)
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "数据库完整性检查失败: " + analysis.Integrity})
		return
	}

	token, err := randomToken()
	if err != nil {
		_ = os.Remove(uploaded.Path)
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "create import token: " + err.Error()})
		return
	}

	s.importsMu.Lock()
	s.cleanupExpiredImportsLocked(time.Now())
	s.pendingImports[token] = pendingDatabaseImport{
		Path:         uploaded.Path,
		OriginalName: uploaded.OriginalName,
		CreatedAt:    time.Now(),
		Analysis:     analysis,
	}
	s.importsMu.Unlock()

	response.OK(w, map[string]interface{}{
		"token":        token,
		"originalName": uploaded.OriginalName,
		"expiresAt":    time.Now().Add(databaseImportTTL).UTC().Format(time.RFC3339),
		"analysis":     analysis,
		"warnings":     databaseImportWarnings(analysis),
	})
}

func (s *Service) commitDatabaseImport(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Token   string `json:"token"`
		Confirm bool   `json:"confirm"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Token) == "" || !payload.Confirm {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "必须提供 preview token 并显式 confirm=true"})
		return
	}

	pending, ok := s.takePendingImport(payload.Token)
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "导入预览不存在或已过期"})
		return
	}
	defer os.Remove(pending.Path)

	backupPath, err := s.replaceDatabase(r.Context(), pending.Path)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"message":    "数据库导入成功，原数据库已备份",
		"backupPath": backupPath,
		"analysis":   pending.Analysis,
	})
}

func (s *Service) importDatabaseLegacy(w http.ResponseWriter, r *http.Request) {
	uploaded, err := s.saveUploadedDatabase(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errDatabaseImportTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		response.JSON(w, status, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer os.Remove(uploaded.Path)

	analysis, err := analyzeDatabaseFile(r.Context(), uploaded.Path)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "数据库完整性检查失败: " + err.Error()})
		return
	}
	if analysis.Integrity != "ok" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "数据库完整性检查失败: " + analysis.Integrity})
		return
	}

	backupPath, err := s.replaceDatabase(r.Context(), uploaded.Path)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "数据库导入失败: " + err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"message":    "数据库导入成功，原数据库已备份",
		"backupPath": backupPath,
	})
}

func (s *Service) getOperationLogs(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(r.Context(), `
		SELECT id, operation_type, table_name, record_id, details, ip_address, user_agent, trace_id, created_at
		FROM operation_logs
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Errorf("load operation logs: %w", err).Error())
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var operationType, tableName string
		var recordID, details, ipAddress, userAgent, traceID, createdAt sql.NullString
		if err := rows.Scan(&id, &operationType, &tableName, &recordID, &details, &ipAddress, &userAgent, &traceID, &createdAt); err != nil {
			response.Error(w, http.StatusInternalServerError, fmt.Errorf("scan operation log: %w", err).Error())
			return
		}
		logs = append(logs, map[string]interface{}{
			"id":             id,
			"operation_type": operationType,
			"table_name":     tableName,
			"record_id":      nullableString(recordID),
			"details":        nullableString(details),
			"ip_address":     nullableString(ipAddress),
			"user_agent":     nullableString(userAgent),
			"trace_id":       nullableString(traceID),
			"created_at":     nullableString(createdAt),
		})
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Errorf("iterate operation logs: %w", err).Error())
		return
	}

	response.OK(w, logs)
}

func (s *Service) getSystemLogs(w http.ResponseWriter, r *http.Request) {
	entries, err := s.readFormattedLogEntries(200)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	fileInfo := s.logFileInfo(10)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"data":     entries,
		"fileSize": fmt.Sprintf("%s MB", fileInfo["sizeMB"]),
		"fileInfo": fileInfo,
	})
}

func (s *Service) getAppLogFile(w http.ResponseWriter, r *http.Request) {
	path := s.logFilePath()
	content, err := readLastLines(path, 500)
	if errors.Is(err, os.ErrNotExist) {
		response.OK(w, "Log file not found at: "+path)
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	sizeKB := "0.00 KB"
	if stat, err := os.Stat(path); err == nil {
		sizeKB = fmt.Sprintf("%.2f KB", float64(stat.Size())/1024)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    content,
		"size":    sizeKB,
	})
}

func (s *Service) getLogSettings(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	days := getConfigInt(r.Context(), db, "log_retention_days", 0)
	count := getConfigInt(r.Context(), db, "log_max_count", 0)
	dbSizeMB := getConfigInt(r.Context(), db, "log_max_db_size_mb", 0)
	logFileSizeMB := getConfigInt(r.Context(), db, "log_file_max_size_mb", 10)
	if logFileSizeMB < 1 {
		logFileSizeMB = 10
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]int{
			"days":          days,
			"count":         count,
			"dbSizeMB":      dbSizeMB,
			"logFileSizeMB": logFileSizeMB,
		},
		"logConfig": map[string]int{
			"maxFileSizeMB": logFileSizeMB,
		},
		"fileInfo": s.logFileInfo(logFileSizeMB),
	})
}

func (s *Service) saveLogSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}

	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	days, _ := toInt(payload["days"])
	count, _ := toInt(payload["count"])
	dbSizeMB, _ := toInt(payload["dbSizeMB"])
	logFileSizeMB, ok := toInt(payload["logFileSizeMB"])
	if !ok {
		logFileSizeMB = 10
	}
	if days < 0 {
		days = 0
	}
	if count < 0 {
		count = 0
	}
	if dbSizeMB < 0 {
		dbSizeMB = 0
	}
	if logFileSizeMB < 1 {
		logFileSizeMB = 10
	}

	configs := []struct {
		key         string
		value       int
		description string
	}{
		{"log_retention_days", days, "log retention days"},
		{"log_max_count", count, "max log rows per table"},
		{"log_max_db_size_mb", dbSizeMB, "max database size in MB"},
		{"log_file_max_size_mb", logFileSizeMB, "max app.log size in MB"},
	}
	for _, item := range configs {
		if err := setSystemConfig(r.Context(), db, item.key, strconv.Itoa(item.value), item.description); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "log settings saved",
		"fileInfo": s.logFileInfo(logFileSizeMB),
	})
}

func (s *Service) clearAppLogs(w http.ResponseWriter, r *http.Request) {
	path := s.logFilePath()
	if handled, err := applog.ClearFileIfPath(path); handled || err != nil {
		if err != nil {
			response.Error(w, http.StatusInternalServerError, fmt.Errorf("clear log file: %w", err).Error())
			return
		}
		applog.Info(r.Context(), "settings", "app log file cleared")
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Logs cleared"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Errorf("create log dir: %w", err).Error())
		return
	}
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Errorf("clear log file: %w", err).Error())
		return
	}
	applog.Info(r.Context(), "settings", "app log file cleared")
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Logs cleared"})
}

func (s *Service) vacuumDatabase(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	beforeSize, _ := fileSize(s.store.DatabasePath())
	_, _ = db.ExecContext(r.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`)
	if _, err := db.ExecContext(r.Context(), `VACUUM`); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "database vacuum failed: " + err.Error()})
		return
	}
	_, _ = db.ExecContext(r.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`)
	afterSize, _ := fileSize(s.store.DatabasePath())

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "database vacuum completed",
		"data": map[string]string{
			"beforeSizeMB": sizeMBString(beforeSize),
			"afterSizeMB":  sizeMBString(afterSize),
			"savedMB":      sizeMBString(beforeSize - afterSize),
		},
	})
}

func (s *Service) clearLogs(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	deleted, err := clearLogTables(r.Context(), db)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "log cleanup failed: " + err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("log cleanup completed, removed %d records", deleted),
		"count":   deleted,
	})
}

func (s *Service) enforceLogLimits(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeOptionalObject(w, r)
	if !ok {
		return
	}

	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	days := intFromPayloadOrConfig(r.Context(), db, payload, "days", "log_retention_days", 0)
	count := intFromPayloadOrConfig(r.Context(), db, payload, "count", "log_max_count", 0)
	dbSizeMB := intFromPayloadOrConfig(r.Context(), db, payload, "dbSizeMB", "log_max_db_size_mb", 0)
	if days < 0 {
		days = 0
	}
	if count < 0 {
		count = 0
	}
	if dbSizeMB < 0 {
		dbSizeMB = 0
	}

	result, err := enforceLogTableLimits(r.Context(), db, s.store.DatabasePath(), days, count, dbSizeMB)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("log cleanup completed, removed %d records", result["deleted"]),
		"data":    result,
	})
}

func (s *Service) clearChatMessages(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeOptionalObject(w, r)
	if !ok {
		return
	}
	keepDays, ok := toInt(payload["keepDays"])
	if !ok {
		keepDays = 7
	}
	keepSessions, ok := toInt(payload["keepSessions"])
	if !ok {
		keepSessions = 10
	}
	if keepDays < 0 {
		keepDays = 0
	}
	if keepSessions < 0 {
		keepSessions = 0
	}

	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	hasSessions, err := tableExists(r.Context(), db, "chat_sessions")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMessages, err := tableExists(r.Context(), db, "chat_messages")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !hasSessions || !hasMessages {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":         true,
			"message":         "旧聊天表不存在，无需清理",
			"deletedMessages": int64(0),
			"deletedSessions": int64(0),
			"newSizeMB":       0,
		})
		return
	}

	result, err := clearLegacyChatMessages(r.Context(), db, keepDays, keepSessions)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_, _ = db.ExecContext(r.Context(), `VACUUM`)
	newSize, _ := fileSize(s.store.DatabasePath())

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "清理完成",
		"data": map[string]interface{}{
			"deletedMessages": result["deletedMessages"],
			"deletedSessions": result["deletedSessions"],
			"newDbSizeMB":     sizeMBString(newSize),
		},
	})
}

func loadUserSettings(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	var row userSettingsRow
	err := db.QueryRowContext(ctx, `
		SELECT
			custom_css,
			theme_mode,
			page_width_mode,
			sidebar_collapsed,
			koyeb_refresh_interval,
			fly_refresh_interval,
			module_visibility,
			channel_enabled,
			channel_model_prefix,
			module_order,
			load_balancing_strategy,
			server_ip_display_mode,
			vibration_enabled,
			main_tabs_layout,
			totp_settings,
			agent_download_url,
			public_api_url
		FROM user_settings
		WHERE id = 1
	`).Scan(
		&row.CustomCSS,
		&row.ThemeMode,
		&row.PageWidthMode,
		&row.SidebarCollapsed,
		&row.KoyebRefreshInterval,
		&row.FlyRefreshInterval,
		&row.ModuleVisibility,
		&row.ChannelEnabled,
		&row.ChannelModelPrefix,
		&row.ModuleOrder,
		&row.LoadBalancingStrategy,
		&row.ServerIPDisplayMode,
		&row.VibrationEnabled,
		&row.MainTabsLayout,
		&row.TOTPSettings,
		&row.AgentDownloadURL,
		&row.PublicAPIURL,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if _, insertErr := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_settings (id, custom_css, module_visibility, module_order)
			VALUES (1, '', '{"openai":true,"gemini-cli":true,"dns":true,"server":true}', '["openai","gemini-cli","dns","server"]')
		`); insertErr != nil {
			return nil, fmt.Errorf("create default user settings: %w", insertErr)
		}
		return loadUserSettings(ctx, db)
	}
	if err != nil {
		return nil, fmt.Errorf("load user settings: %w", err)
	}

	visibility := parseObject(row.ModuleVisibility, map[string]interface{}{
		"dns":    true,
		"openai": true,
		"server": true,
	})
	delete(visibility, "antigravity")
	ensureBoolKey(visibility, "gemini-cli", true)
	ensureBoolKey(visibility, "self-h", false)
	ensureBoolKey(visibility, "qwen", false)

	channelEnabled := parseObject(row.ChannelEnabled, map[string]interface{}{"gemini-cli": true})
	delete(channelEnabled, "antigravity")
	ensureBoolKey(channelEnabled, "qwen", false)

	channelModelPrefix := parseObject(row.ChannelModelPrefix, map[string]interface{}{"gemini-cli": ""})
	delete(channelModelPrefix, "antigravity")
	ensureStringKey(channelModelPrefix, "qwen", "")

	order := parseStringSlice(row.ModuleOrder, []string{"dns", "openai", "server"})
	order = filterString(order, "antigravity")
	order = ensureAfter(order, "qwen", "gemini-cli")
	order = ensurePresent(order, "gemini-cli")
	order = ensureBefore(order, "self-h", "server")

	settings := map[string]interface{}{
		"customCss":               nullString(row.CustomCSS, ""),
		"sidebarCollapsed":        nullInt(row.SidebarCollapsed, 0) != 0,
		"koyebRefreshInterval":    nullInt(row.KoyebRefreshInterval, 30000),
		"flyRefreshInterval":      nullInt(row.FlyRefreshInterval, 30000),
		"moduleVisibility":        visibility,
		"channelEnabled":          channelEnabled,
		"channelModelPrefix":      channelModelPrefix,
		"moduleOrder":             order,
		"load_balancing_strategy": nullString(row.LoadBalancingStrategy, "random"),
		"serverIpDisplayMode":     nullString(row.ServerIPDisplayMode, "normal"),
		"vibrationEnabled":        nullInt(row.VibrationEnabled, 1) != 0,
		"navLayout":               nullString(row.MainTabsLayout, "top"),
		"totpSettings":            parseObject(row.TOTPSettings, map[string]interface{}{}),
		"agentDownloadUrl":        nullString(row.AgentDownloadURL, ""),
		"publicApiUrl":            nullString(row.PublicAPIURL, ""),
	}
	if value := nullString(row.ThemeMode, ""); value != "" {
		settings["themeMode"] = value
	}
	if value := nullString(row.PageWidthMode, ""); value != "" {
		settings["pageWidthMode"] = value
	}
	return settings, nil
}

func saveUserSettings(ctx context.Context, db *sql.DB, settings map[string]interface{}) error {
	updates := map[string]interface{}{}
	assignString(updates, "custom_css", settings, "customCss", "custom_css")
	assignString(updates, "theme_mode", settings, "themeMode", "theme_mode")
	assignString(updates, "page_width_mode", settings, "pageWidthMode", "page_width_mode")
	assignBoolInt(updates, "sidebar_collapsed", settings, "sidebarCollapsed", "sidebar_collapsed")
	assignInt(updates, "koyeb_refresh_interval", settings, "koyebRefreshInterval", "koyeb_refresh_interval")
	assignInt(updates, "fly_refresh_interval", settings, "flyRefreshInterval", "fly_refresh_interval")
	assignJSON(updates, "module_visibility", settings, "moduleVisibility", "module_visibility")
	assignJSON(updates, "channel_enabled", settings, "channelEnabled", "channel_enabled")
	assignJSON(updates, "channel_model_prefix", settings, "channelModelPrefix", "channel_model_prefix")
	assignJSON(updates, "module_order", settings, "moduleOrder", "module_order")
	assignString(updates, "load_balancing_strategy", settings, "load_balancing_strategy", "load_balancing_strategy_form")
	assignString(updates, "server_ip_display_mode", settings, "serverIpDisplayMode", "server_ip_display_mode")
	assignBoolInt(updates, "vibration_enabled", settings, "vibrationEnabled", "vibration_enabled")
	assignString(updates, "main_tabs_layout", settings, "navLayout", "mainTabsLayout", "main_tabs_layout")
	assignJSON(updates, "totp_settings", settings, "totpSettings", "totp_settings")
	assignString(updates, "agent_download_url", settings, "agentDownloadUrl", "agent_download_url")
	assignString(updates, "public_api_url", settings, "publicApiUrl", "public_api_url")

	if len(updates) == 0 {
		return nil
	}

	columns := []string{
		"custom_css",
		"theme_mode",
		"page_width_mode",
		"sidebar_collapsed",
		"koyeb_refresh_interval",
		"fly_refresh_interval",
		"module_visibility",
		"channel_enabled",
		"channel_model_prefix",
		"module_order",
		"load_balancing_strategy",
		"server_ip_display_mode",
		"vibration_enabled",
		"main_tabs_layout",
		"totp_settings",
		"agent_download_url",
		"public_api_url",
	}

	setParts := make([]string, 0, len(updates)+1)
	args := make([]interface{}, 0, len(updates)+1)
	for _, column := range columns {
		value, ok := updates[column]
		if !ok {
			continue
		}
		setParts = append(setParts, column+" = ?")
		args = append(args, value)
	}
	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, 1)

	_, err := db.ExecContext(ctx, "UPDATE user_settings SET "+strings.Join(setParts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return fmt.Errorf("save user settings: %w", err)
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	if strings.TrimSpace(buf.String()) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	if err := json.Unmarshal(buf.Bytes(), target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求参数验证失败"})
		return false
	}
	return true
}

func parseObject(value sql.NullString, fallback map[string]interface{}) map[string]interface{} {
	result := copyMap(fallback)
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return result
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return result
	}
	return parsed
}

func parseStringSlice(value sql.NullString, fallback []string) []string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return append([]string(nil), fallback...)
	}
	var parsed []string
	if err := json.Unmarshal([]byte(value.String), &parsed); err == nil {
		return parsed
	}
	var anyParsed []interface{}
	if err := json.Unmarshal([]byte(value.String), &anyParsed); err != nil {
		return append([]string(nil), fallback...)
	}
	result := make([]string, 0, len(anyParsed))
	for _, item := range anyParsed {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func assignString(updates map[string]interface{}, column string, settings map[string]interface{}, keys ...string) {
	value, ok := firstValue(settings, keys...)
	if !ok || value == nil {
		return
	}
	if text, ok := value.(string); ok {
		updates[column] = text
		return
	}
	updates[column] = fmt.Sprint(value)
}

func assignInt(updates map[string]interface{}, column string, settings map[string]interface{}, keys ...string) {
	value, ok := firstValue(settings, keys...)
	if !ok || value == nil {
		return
	}
	if parsed, ok := toInt(value); ok {
		updates[column] = parsed
	}
}

func assignBoolInt(updates map[string]interface{}, column string, settings map[string]interface{}, keys ...string) {
	value, ok := firstValue(settings, keys...)
	if !ok || value == nil {
		return
	}
	if parsed, ok := toBool(value); ok {
		if parsed {
			updates[column] = 1
		} else {
			updates[column] = 0
		}
	}
}

func assignJSON(updates map[string]interface{}, column string, settings map[string]interface{}, keys ...string) {
	value, ok := firstValue(settings, keys...)
	if !ok || value == nil {
		return
	}
	if text, ok := value.(string); ok {
		updates[column] = text
		return
	}
	encoded, err := json.Marshal(value)
	if err == nil {
		updates[column] = string(encoded)
	}
}

func firstValue(settings map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, key := range keys {
		value, ok := settings[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func toInt(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func toBool(value interface{}) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return false, false
	}
}

func nullString(value sql.NullString, fallback string) string {
	if value.Valid {
		return value.String
	}
	return fallback
}

func nullInt(value sql.NullInt64, fallback int) int {
	if value.Valid {
		return int(value.Int64)
	}
	return fallback
}

func copyMap(input map[string]interface{}) map[string]interface{} {
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func ensureBoolKey(target map[string]interface{}, key string, value bool) {
	if _, ok := target[key]; !ok {
		target[key] = value
	}
}

func ensureStringKey(target map[string]interface{}, key string, value string) {
	if _, ok := target[key]; !ok {
		target[key] = value
	}
}

func filterString(values []string, forbidden string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value != forbidden {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func ensurePresent(values []string, item string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

func ensureAfter(values []string, item, after string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	for i, value := range values {
		if value == after {
			values = append(values, "")
			copy(values[i+2:], values[i+1:])
			values[i+1] = item
			return values
		}
	}
	return append(values, item)
}

func ensureBefore(values []string, item, before string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	for i, value := range values {
		if value == before {
			values = append(values, "")
			copy(values[i+1:], values[i:])
			values[i] = item
			return values
		}
	}
	return append(values, item)
}

func listUserTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list database tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scan database table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database tables: %w", err)
	}
	return tables, nil
}

func countTableRows(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var count int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdentifier(table)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count rows in %s: %w", table, err)
	}
	return count, nil
}

func analyzeTable(ctx context.Context, db *sql.DB, table string) (tableAnalysis, error) {
	count, err := countTableRows(ctx, db, table)
	if err != nil {
		return tableAnalysis{}, err
	}

	sizeEstimate, err := estimateTableSize(ctx, db, table)
	if err != nil {
		return tableAnalysis{}, err
	}

	var avgRowSize int64
	if count > 0 && sizeEstimate > 0 {
		avgRowSize = (sizeEstimate + count/2) / count
	}

	return tableAnalysis{
		Table:              table,
		Rows:               count,
		EstimatedSizeBytes: sizeEstimate,
		EstimatedSizeMB:    sizeMBString(sizeEstimate),
		AvgRowSizeBytes:    avgRowSize,
	}, nil
}

func estimateTableSize(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var query string
	switch table {
	case "chat_messages":
		query = `SELECT COALESCE(SUM(LENGTH(content)), 0) + COALESCE(SUM(LENGTH(reasoning)), 0) FROM chat_messages`
	case "api_logs", "operation_logs":
		query = "SELECT COALESCE(SUM(LENGTH(COALESCE(details, ''))), 0) + COALESCE(SUM(LENGTH(COALESCE(user_agent, ''))), 0) FROM " + quoteIdentifier(table)
	default:
		return 0, nil
	}

	var size sql.NullInt64
	if err := db.QueryRowContext(ctx, query).Scan(&size); err != nil {
		return 0, fmt.Errorf("estimate %s size: %w", table, err)
	}
	if !size.Valid {
		return 0, nil
	}
	return size.Int64, nil
}

func migrationRequiredTables() map[string][]string {
	return map[string][]string{
		"user_settings":         {"id", "theme_mode", "page_width_mode", "sidebar_collapsed", "module_visibility", "module_order"},
		"operation_logs":        {"id", "operation_type", "table_name", "trace_id"},
		"totp_accounts":         {"id", "secret", "secret_encrypted_at", "last_revealed_at"},
		"filebox_entries":       {"code", "type", "expiry", "downloads"},
		"filebox_settings":      {"id", "max_file_size", "allowed_mime_types", "default_expiry_hours"},
		"uptime_monitors":       {"id", "type", "keyword", "dns_resolve_type", "config_json", "push_token", "push_grace_seconds"},
		"uptime_monitor_states": {"monitor_id", "state", "fail_count", "recover_count"},
		"notification_channels": {"id", "type", "config"},
		"settings_registry":     {"domain", "defaults_json", "mask_fields_json"},
	}
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return true, nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s columns: %w", table, err)
	}
	return columns, nil
}

func missingStrings(required []string, actual map[string]bool) []string {
	missing := make([]string, 0)
	for _, value := range required {
		if !actual[value] {
			missing = append(missing, value)
		}
	}
	return missing
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func fileSize(path string) (int64, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat database file: %w", err)
	}
	return stat.Size(), nil
}

func sizeMBString(bytes int64) string {
	return fmt.Sprintf("%.2f", float64(bytes)/1024/1024)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *Service) logFilePath() string {
	if path := applog.LogPath(); path != "" && filepath.Clean(path) == filepath.Clean(filepath.Join(s.cfg.DataDir, "logs", "app.log")) {
		return path
	}
	return filepath.Join(s.cfg.DataDir, "logs", "app.log")
}

func (s *Service) logFileInfo(maxSizeMB int) map[string]interface{} {
	if maxSizeMB < 1 {
		maxSizeMB = 10
	}
	path := s.logFilePath()
	info := map[string]interface{}{
		"size":         int64(0),
		"sizeMB":       "0.00",
		"maxSizeMB":    maxSizeMB,
		"usagePercent": "0.0",
		"path":         path,
	}
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return info
	}
	if err != nil {
		info["error"] = err.Error()
		return info
	}
	sizeMB := float64(stat.Size()) / 1024 / 1024
	info["size"] = stat.Size()
	info["sizeMB"] = fmt.Sprintf("%.2f", sizeMB)
	info["usagePercent"] = fmt.Sprintf("%.1f", sizeMB/float64(maxSizeMB)*100)
	info["modifiedAt"] = stat.ModTime().UTC().Format(time.RFC3339)
	return info
}

func (s *Service) readFormattedLogEntries(limit int) ([]map[string]interface{}, error) {
	if limit < 1 {
		limit = 200
	}
	content, err := readLastLines(s.logFilePath(), limit)
	if errors.Is(err, os.ErrNotExist) {
		return []map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	entries := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			entries = append(entries, map[string]interface{}{
				"time":    "00:00:00",
				"level":   "INFO",
				"module":  "core",
				"message": line,
			})
			continue
		}
		timestamp := defaultString(entry["timestamp"], defaultString(entry["time"], ""))
		displayTime := "00:00:00"
		if parsed, err := parseLogTime(timestamp); err == nil {
			displayTime = parsed.Format("15:04:05")
		}
		message := defaultString(entry["message"], defaultString(entry["msg"], ""))
		if message == "" {
			message = line
		}
		if _, ok := entry["data"]; ok {
			message += " [DATA]"
		}
		entries = append(entries, map[string]interface{}{
			"time":    displayTime,
			"level":   defaultString(entry["level"], "INFO"),
			"module":  defaultString(entry["module"], "core"),
			"message": message,
		})
	}
	return entries, nil
}

func readLastLines(path string, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read log file: %w", err)
	}
	text := string(content)
	lines := strings.Split(text, "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n"), nil
}

func parseLogTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported log time: %s", value)
}

func defaultString(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func nullableString(value sql.NullString) interface{} {
	if value.Valid {
		return value.String
	}
	return nil
}

func getConfigInt(ctx context.Context, db *sql.DB, key string, fallback int) int {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func setSystemConfig(ctx context.Context, db *sql.DB, key, value, description string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO system_config (key, value, description, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			description = excluded.description,
			updated_at = CURRENT_TIMESTAMP
	`, key, value, description)
	if err != nil {
		return fmt.Errorf("set config %s: %w", key, err)
	}
	return nil
}

func decodeOptionalObject(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	defer r.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		return nil, false
	}
	if strings.TrimSpace(buf.String()) == "" {
		return map[string]interface{}{}, true
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		return nil, false
	}
	return payload, true
}

func intFromPayloadOrConfig(ctx context.Context, db *sql.DB, payload map[string]interface{}, payloadKey, configKey string, fallback int) int {
	if value, ok := payload[payloadKey]; ok {
		if parsed, ok := toInt(value); ok {
			return parsed
		}
	}
	return getConfigInt(ctx, db, configKey, fallback)
}

func listLogTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND (name LIKE '%_logs' OR name LIKE '%_history')
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list log tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan log table: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate log tables: %w", err)
	}
	return tables, nil
}

func clearLogTables(ctx context.Context, db *sql.DB) (int64, error) {
	tables, err := listLogTables(ctx, db)
	if err != nil {
		return 0, err
	}
	if len(tables) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin log cleanup: %w", err)
	}
	defer tx.Rollback()

	var deleted int64
	for _, table := range tables {
		result, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table))
		if err != nil {
			return 0, fmt.Errorf("clear %s: %w", table, err)
		}
		changes, _ := result.RowsAffected()
		deleted += changes
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit log cleanup: %w", err)
	}
	return deleted, nil
}

func enforceLogTableLimits(ctx context.Context, db *sql.DB, dbPath string, days, count, dbSizeMB int) (map[string]interface{}, error) {
	if days == 0 && count == 0 && dbSizeMB == 0 {
		return map[string]interface{}{"deleted": int64(0), "reason": "no_limits"}, nil
	}

	tables, err := listLogTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return map[string]interface{}{"deleted": int64(0)}, nil
	}

	var totalDeleted int64
	for _, table := range tables {
		timeColumn, err := logTimeColumn(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if timeColumn == "" {
			continue
		}
		if days > 0 {
			cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
			result, err := db.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table)+" WHERE "+quoteIdentifier(timeColumn)+" < ?", cutoff)
			if err != nil {
				return nil, fmt.Errorf("delete old rows from %s: %w", table, err)
			}
			changes, _ := result.RowsAffected()
			totalDeleted += changes
		}
		if count > 0 {
			result, err := db.ExecContext(ctx, `
				DELETE FROM `+quoteIdentifier(table)+`
				WHERE rowid NOT IN (
					SELECT rowid FROM `+quoteIdentifier(table)+`
					ORDER BY `+quoteIdentifier(timeColumn)+` DESC
					LIMIT ?
				)
			`, count)
			if err != nil {
				return nil, fmt.Errorf("trim rows from %s: %w", table, err)
			}
			changes, _ := result.RowsAffected()
			totalDeleted += changes
		}
	}

	cleanupRounds := 0
	if dbSizeMB > 0 {
		for cleanupRounds < 10 {
			currentSize, err := fileSize(dbPath)
			if err != nil {
				return nil, err
			}
			if float64(currentSize)/1024/1024 <= float64(dbSizeMB) {
				break
			}
			cleanupRounds++
			var roundDeleted int64
			for _, table := range tables {
				timeColumn, err := logTimeColumn(ctx, db, table)
				if err != nil {
					return nil, err
				}
				if timeColumn == "" {
					continue
				}
				tableCount, err := countTableRows(ctx, db, table)
				if err != nil {
					return nil, err
				}
				if tableCount <= 10 {
					continue
				}
				deleteCount := tableCount / 5
				if deleteCount < 1 {
					deleteCount = 1
				}
				result, err := db.ExecContext(ctx, `
					DELETE FROM `+quoteIdentifier(table)+`
					WHERE rowid IN (
						SELECT rowid FROM `+quoteIdentifier(table)+`
						ORDER BY `+quoteIdentifier(timeColumn)+` ASC
						LIMIT ?
					)
				`, deleteCount)
				if err != nil {
					return nil, fmt.Errorf("delete oldest rows from %s: %w", table, err)
				}
				changes, _ := result.RowsAffected()
				roundDeleted += changes
				totalDeleted += changes
			}
			if roundDeleted == 0 {
				break
			}
			_, _ = db.ExecContext(ctx, `VACUUM`)
		}
	}

	return map[string]interface{}{
		"deleted":       totalDeleted,
		"cleanupRounds": cleanupRounds,
	}, nil
}

func logTimeColumn(ctx context.Context, db *sql.DB, table string) (string, error) {
	columns, err := tableColumns(ctx, db, table)
	if err != nil {
		return "", err
	}
	for _, column := range []string{"created_at", "checked_at", "timestamp", "recorded_at", "start_time"} {
		if columns[column] {
			return column, nil
		}
	}
	return "", nil
}

type uploadedDatabase struct {
	Path         string
	OriginalName string
}

var errDatabaseImportTooLarge = errors.New("上传的数据库文件过大")

func (s *Service) saveUploadedDatabase(r *http.Request) (uploadedDatabase, error) {
	if r.ContentLength > maxDatabaseImportBytes {
		return uploadedDatabase{}, errDatabaseImportTooLarge
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return uploadedDatabase{}, fmt.Errorf("解析上传文件失败: %w", err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("database")
	if err != nil {
		return uploadedDatabase{}, errors.New("未找到上传的数据库文件")
	}
	defer file.Close()

	originalName := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(originalName), ".db") {
		return uploadedDatabase{}, errors.New("无效的文件类型，请上传 .db 文件")
	}

	tempDir := filepath.Join(s.cfg.DataDir, "temp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return uploadedDatabase{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		return uploadedDatabase{}, err
	}
	tempPath := filepath.Join(tempDir, "database-import-"+token+".db")
	if err := saveMultipartFile(file, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return uploadedDatabase{}, err
	}
	return uploadedDatabase{Path: tempPath, OriginalName: originalName}, nil
}

func saveMultipartFile(file multipart.File, target string) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("保存上传文件失败: %w", err)
	}
	defer out.Close()

	limited := io.LimitReader(file, maxDatabaseImportBytes+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		return fmt.Errorf("保存上传文件失败: %w", err)
	}
	if written > maxDatabaseImportBytes {
		return errDatabaseImportTooLarge
	}
	if written == 0 {
		return errors.New("上传的数据库文件为空")
	}
	return nil
}

func analyzeDatabaseFile(ctx context.Context, filePath string) (databaseImportAnalysis, error) {
	db, err := sql.Open("sqlite", filePath)
	if err != nil {
		return databaseImportAnalysis{}, fmt.Errorf("open uploaded sqlite: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return databaseImportAnalysis{}, fmt.Errorf("integrity check: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return databaseImportAnalysis{}, fmt.Errorf("list uploaded tables: %w", err)
	}
	defer rows.Close()

	tableNames := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return databaseImportAnalysis{}, fmt.Errorf("scan uploaded table: %w", err)
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Close(); err != nil {
		return databaseImportAnalysis{}, fmt.Errorf("close uploaded table cursor: %w", err)
	}
	if err := rows.Err(); err != nil {
		return databaseImportAnalysis{}, fmt.Errorf("iterate uploaded tables: %w", err)
	}

	tables := make([]databaseImportTable, 0, len(tableNames))
	for _, name := range tableNames {
		var count int64
		var countPtr *int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdentifier(name)).Scan(&count); err == nil {
			countPtr = &count
		}
		tables = append(tables, databaseImportTable{Name: name, Rows: countPtr})
	}

	size, err := fileSize(filePath)
	if err != nil {
		return databaseImportAnalysis{}, err
	}
	return databaseImportAnalysis{
		Integrity:  integrity,
		Tables:     tables,
		TableCount: len(tables),
		SizeBytes:  size,
	}, nil
}

func databaseImportWarnings(analysis databaseImportAnalysis) []string {
	seen := map[string]bool{}
	for _, table := range analysis.Tables {
		seen[table.Name] = true
	}
	warnings := make([]string, 0, 2)
	for _, table := range []string{"system_config", "user_settings"} {
		if !seen[table] {
			warnings = append(warnings, "缺少常见核心表: "+table)
		}
	}
	return warnings
}

func (s *Service) takePendingImport(token string) (pendingDatabaseImport, bool) {
	s.importsMu.Lock()
	defer s.importsMu.Unlock()
	s.cleanupExpiredImportsLocked(time.Now())
	pending, ok := s.pendingImports[token]
	if !ok {
		return pendingDatabaseImport{}, false
	}
	delete(s.pendingImports, token)
	if _, err := os.Stat(pending.Path); err != nil {
		return pendingDatabaseImport{}, false
	}
	return pending, true
}

func (s *Service) cleanupExpiredImportsLocked(now time.Time) {
	for token, pending := range s.pendingImports {
		if now.Sub(pending.CreatedAt) > databaseImportTTL {
			_ = os.Remove(pending.Path)
			delete(s.pendingImports, token)
		}
	}
}

func (s *Service) replaceDatabase(ctx context.Context, importPath string) (string, error) {
	s.importsMu.Lock()
	defer s.importsMu.Unlock()

	backupDir := s.backupDir()
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	backupPath := filepath.Join(backupDir, "api-monitor-before-import-"+timestampForFile(time.Now())+".db")
	if err := s.backupCurrentDatabase(ctx, backupPath); err != nil {
		return "", fmt.Errorf("备份当前数据库失败: %w", err)
	}

	dbPath := s.store.DatabasePath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return "", fmt.Errorf("创建数据库目录失败: %w", err)
	}
	tempTarget := dbPath + ".importing-" + timestampForFile(time.Now())
	if err := copyFile(importPath, tempTarget, 0o600); err != nil {
		return "", fmt.Errorf("准备导入数据库失败: %w", err)
	}
	defer os.Remove(tempTarget)

	cleanupSQLiteSidecars(dbPath)
	if err := os.Remove(dbPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("替换数据库前删除旧文件失败: %w", err)
	}
	if err := os.Rename(tempTarget, dbPath); err != nil {
		_ = copyFile(backupPath, dbPath, 0o600)
		return "", fmt.Errorf("替换数据库失败: %w", err)
	}
	cleanupSQLiteSidecars(dbPath)

	db, err := s.store.Open(ctx)
	if err != nil {
		_ = copyFile(backupPath, dbPath, 0o600)
		return "", fmt.Errorf("重新打开导入数据库失败: %w", err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		_ = db.Close()
		_ = copyFile(backupPath, dbPath, 0o600)
		if err != nil {
			return "", fmt.Errorf("导入后完整性检查失败: %w", err)
		}
		return "", fmt.Errorf("导入后完整性检查失败: %s", integrity)
	}
	return backupPath, nil
}

func (s *Service) backupCurrentDatabase(ctx context.Context, backupPath string) error {
	db, err := s.store.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(backupPath)
	if _, err := db.ExecContext(ctx, `VACUUM INTO `+quoteSQLiteString(backupPath)); err != nil {
		return err
	}
	return nil
}

func (s *Service) backupDir() string {
	dataDir := filepath.Clean(s.cfg.DataDir)
	if strings.EqualFold(filepath.Base(dataDir), "data") {
		return filepath.Join(filepath.Dir(dataDir), "backup")
	}
	return filepath.Join(dataDir, "backup")
}

func cleanupSQLiteSidecars(dbPath string) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(dbPath + suffix)
	}
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func clearLegacyChatMessages(ctx context.Context, db *sql.DB, keepDays, keepSessions int) (map[string]int64, error) {
	if keepSessions < 1 {
		return map[string]int64{"deletedMessages": 0, "deletedSessions": 0}, nil
	}
	orderColumn, err := firstExistingColumn(ctx, db, "chat_sessions", []string{"updated_at", "created_at", "id"})
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id FROM chat_sessions
		ORDER BY `+quoteIdentifier(orderColumn)+` DESC
		LIMIT ?
	`, keepSessions)
	if err != nil {
		return nil, fmt.Errorf("query recent chat sessions: %w", err)
	}
	defer rows.Close()

	ids := make([]interface{}, 0, keepSessions)
	for rows.Next() {
		var value interface{}
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan recent chat session: %w", err)
		}
		ids = append(ids, normalizeSQLValue(value))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent chat sessions: %w", err)
	}
	if len(ids) == 0 {
		return map[string]int64{"deletedMessages": 0, "deletedSessions": 0}, nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -keepDays).Format(time.RFC3339)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin chat cleanup: %w", err)
	}
	defer tx.Rollback()

	args := append([]interface{}{}, ids...)
	args = append(args, cutoff)
	msgResult, err := tx.ExecContext(ctx, `
		DELETE FROM chat_messages
		WHERE session_id NOT IN (`+placeholders(len(ids))+`)
		AND created_at < ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("delete old chat messages: %w", err)
	}
	deletedMessages, _ := msgResult.RowsAffected()

	args = append([]interface{}{}, ids...)
	args = append(args, cutoff)
	sessionResult, err := tx.ExecContext(ctx, `
		DELETE FROM chat_sessions
		WHERE id NOT IN (`+placeholders(len(ids))+`)
		AND created_at < ?
		AND id NOT IN (SELECT DISTINCT session_id FROM chat_messages)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("delete empty chat sessions: %w", err)
	}
	deletedSessions, _ := sessionResult.RowsAffected()
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit chat cleanup: %w", err)
	}
	return map[string]int64{"deletedMessages": deletedMessages, "deletedSessions": deletedSessions}, nil
}

func firstExistingColumn(ctx context.Context, db *sql.DB, table string, candidates []string) (string, error) {
	columns, err := tableColumns(ctx, db, table)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if columns[candidate] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s does not contain any supported ordering column", table)
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	values := make([]string, count)
	for i := range values {
		values[i] = "?"
	}
	return strings.Join(values, ",")
}

func normalizeSQLValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return typed
	}
}

func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func timestampForFile(value time.Time) string {
	return value.UTC().Format("2006-01-02T15-04-05")
}

func quoteSQLiteString(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
