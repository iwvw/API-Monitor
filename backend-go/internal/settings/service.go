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
	cleanupCancel  context.CancelFunc
	cleanupWG      sync.WaitGroup
	vacuumMu       sync.Mutex
	vacuumRunning  bool
	vacuumMode     string
	vacuumStarted  time.Time
	vacuumDone     time.Time
	vacuumError    string
	vacuumBefore   int64
	vacuumAfter    int64
}

// 数据库压缩执行模式：migrate = 首次全量 VACUUM（建立 auto_vacuum
// pointer-map，1 核机器需数分钟）；incremental = 分批增量回收（秒级）。
const (
	vacuumModeMigrate     = "migrate"
	vacuumModeIncremental = "incremental"
)

type userSettingsRow struct {
	CustomCSS             sql.NullString
	ThemeMode             sql.NullString
	PageWidthMode         sql.NullString
	SidebarCollapsed      sql.NullInt64
	SiteBrandIconID       sql.NullString
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
	TimeZone              sql.NullString
	UIFont                sql.NullString
}

type tableAnalysis struct {
	Table              string `json:"table"`
	Rows               int64  `json:"rows"`
	EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
	EstimatedSizeMB    string `json:"estimatedSizeMB"`
	TableSizeBytes     int64  `json:"tableSizeBytes,omitempty"`
	IndexSizeBytes     int64  `json:"indexSizeBytes,omitempty"`
	AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
	SizeSource         string `json:"sizeSource,omitempty"`
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

const (
	defaultAutoCleanupHours = 24
	initialAutoCleanupDelay = time.Minute
	autoCleanupTimeout      = 2 * time.Minute
)

// StartBackgroundCleanup launches the periodic log-retention enforcement loop.
// It is idempotent and safe to call once at server startup.
func (s *Service) StartBackgroundCleanup() {
	if s.cleanupCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cleanupCancel = cancel
	s.cleanupWG.Add(1)
	go func() {
		defer s.cleanupWG.Done()
		s.runBackgroundCleanup(ctx)
	}()
}

// Stop cancels the background cleanup loop and waits for it to exit.
func (s *Service) Stop() {
	if s.cleanupCancel == nil {
		return
	}
	s.cleanupCancel()
	s.cleanupWG.Wait()
}

func (s *Service) runBackgroundCleanup(ctx context.Context) {
	timer := time.NewTimer(initialAutoCleanupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.autoCleanupOnce(ctx)
			timer.Reset(time.Duration(s.autoCleanupInterval(ctx)) * time.Hour)
		}
	}
}

func (s *Service) autoCleanupInterval(ctx context.Context) int {
	db, err := s.store.Open(ctx)
	if err != nil {
		return defaultAutoCleanupHours
	}
	defer db.Close()
	hours := getConfigInt(ctx, db, "log_auto_cleanup_hours", defaultAutoCleanupHours)
	if hours < 1 {
		hours = defaultAutoCleanupHours
	}
	return hours
}

func (s *Service) autoCleanupOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, autoCleanupTimeout)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	// 总开关未显式关闭（system_config 中无该键）时默认启用自动清理。
	// 显式写入 0 的用户仍可整体关闭。
	if getConfigInt(ctx, db, "log_auto_cleanup", 1) == 0 {
		return
	}
	// 未显式配置时启用内置保护默认：日志表 30 天截断、库文件 500MB 空间红线
	// （仅增量回收 freelist，绝不触发行删除）防生产库无界膨胀；统计表独立
	// 长保留（statisticsRetentionDays）不受 days 影响。显式配置（含 0=不限）
	// 优先。
	days := getConfigInt(ctx, db, "log_retention_days", 30)
	count := getConfigInt(ctx, db, "log_max_count", 0)
	dbSizeMB := getConfigInt(ctx, db, "log_max_db_size_mb", 500)
	if days == 0 && count == 0 && dbSizeMB == 0 {
		return
	}
	result, err := enforceLogTableLimits(ctx, db, s.store.DatabasePath(), days, count, dbSizeMB, false)
	if err != nil {
		applog.Error(ctx, "settings", fmt.Sprintf("auto log cleanup failed: %v", err))
		return
	}
	if deleted, ok := result["deleted"].(int64); ok && deleted > 0 {
		applog.Info(ctx, "settings", fmt.Sprintf("auto log cleanup removed %d records", deleted))
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/settings/site-brand/icons" {
		switch r.Method {
		case http.MethodGet:
			s.listSiteBrandIcons(w, r)
		case http.MethodPost:
			s.uploadSiteBrandIcon(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/settings/site-brand/icons/") {
		switch r.Method {
		case http.MethodGet:
			s.serveSiteBrandIcon(w, r, strings.TrimPrefix(r.URL.Path, "/api/settings/site-brand/icons/"))
		case http.MethodDelete:
			s.deleteSiteBrandIcon(w, r, strings.TrimPrefix(r.URL.Path, "/api/settings/site-brand/icons/"))
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

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
	case "/api/settings/deprecated-tables":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getDeprecatedTables(w, r)
	case "/api/settings/cleanup-deprecated-tables":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.cleanupDeprecatedTables(w, r)
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
		switch r.Method {
		case http.MethodGet:
			s.vacuumStatus(w, r)
		case http.MethodPost:
			s.vacuumDatabase(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
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
	settings, err := loadUserSettings(r.Context(), db)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	message := "设置已保存"
	if mergeCurrent {
		message = "设置已更新"
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": message, "data": settings})
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

	deep := r.URL.Query().Get("deep") == "1"
	stats := make(map[string]int64, len(tables))
	for _, table := range tables {
		stats[table] = -1
		if deep {
			count, err := countTableRows(r.Context(), db, table)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			stats[table] = count
		}
	}

	storage := databaseStorageStats(r.Context(), db, s.store.DatabasePath())

	response.OK(w, map[string]interface{}{
		"dbPath":        s.store.DatabasePath(),
		"dbSize":        storage.MainSizeBytes,
		"mainDbSize":    storage.MainSizeBytes,
		"totalSize":     storage.TotalSizeBytes,
		"walSize":       storage.WALSizeBytes,
		"shmSize":       storage.SHMSizeBytes,
		"journalSize":   storage.JournalSizeBytes,
		"pageSize":      storage.PageSize,
		"pageCount":     storage.PageCount,
		"freelistCount": storage.FreelistCount,
		"usedPageBytes": storage.UsedPageBytes,
		"freePageBytes": storage.FreePageBytes,
		"storage":       storage,
		"tables":        stats,
		"countsExact":   deep,
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

	deep := r.URL.Query().Get("deep") == "1"
	pageSizes, sizeSource := tablePageSizes(r.Context(), db)
	analysis := make([]tableAnalysis, 0, len(tables))
	for _, table := range tables {
		item := tableAnalysis{
			Table: table,
			Rows:  -1,
		}
		if deep {
			var err error
			item, err = analyzeTable(r.Context(), db, table)
			if err != nil {
				item = tableAnalysis{
					Table: table,
					Rows:  -1,
					Error: err.Error(),
				}
			}
		}
		if pageSize, ok := pageSizes[table]; ok {
			item.EstimatedSizeBytes = pageSize.TableBytes + pageSize.IndexBytes
			item.EstimatedSizeMB = sizeMBString(item.EstimatedSizeBytes)
			item.TableSizeBytes = pageSize.TableBytes
			item.IndexSizeBytes = pageSize.IndexBytes
			item.SizeSource = sizeSource
			if item.Rows > 0 && item.EstimatedSizeBytes > 0 {
				item.AvgRowSizeBytes = (item.EstimatedSizeBytes + item.Rows/2) / item.Rows
			}
		} else if item.EstimatedSizeBytes > 0 {
			item.SizeSource = "payload"
		}
		analysis = append(analysis, item)
	}

	sort.SliceStable(analysis, func(i, j int) bool {
		if analysis[i].EstimatedSizeBytes == analysis[j].EstimatedSizeBytes {
			return analysis[i].Table < analysis[j].Table
		}
		return analysis[i].EstimatedSizeBytes > analysis[j].EstimatedSizeBytes
	})

	storage := databaseStorageStats(r.Context(), db, s.store.DatabasePath())

	response.OK(w, map[string]interface{}{
		"dbFileSizeMB": sizeMBString(storage.MainSizeBytes),
		"storage":      storage,
		"tables":       analysis,
		"countsExact":  deep,
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
	// Auto-cleanup toggles default OFF so existing deployments do not change behavior silently.
	autoCleanup := getConfigInt(r.Context(), db, "log_auto_cleanup", 0)
	autoCleanupHours := getConfigInt(r.Context(), db, "log_auto_cleanup_hours", defaultAutoCleanupHours)
	if autoCleanupHours < 1 {
		autoCleanupHours = defaultAutoCleanupHours
	}
	logFileSizeMB := getConfigInt(r.Context(), db, "log_file_max_size_mb", 10)
	if logFileSizeMB < 1 {
		logFileSizeMB = 10
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]int{
			"days":             days,
			"count":            count,
			"dbSizeMB":         dbSizeMB,
			"logFileSizeMB":    logFileSizeMB,
			"autoCleanup":      autoCleanup,
			"autoCleanupHours": autoCleanupHours,
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
	autoCleanup := 0
	if value, ok := toBool(payload["autoCleanup"]); ok && value {
		autoCleanup = 1
	}
	autoCleanupHours, _ := toInt(payload["autoCleanupHours"])
	if autoCleanupHours < 1 {
		autoCleanupHours = defaultAutoCleanupHours
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
		{"log_auto_cleanup", autoCleanup, "auto log cleanup enabled"},
		{"log_auto_cleanup_hours", autoCleanupHours, "auto log cleanup interval hours"},
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

// vacuumDatabase 启动数据库压缩（异步执行）。
// VACUUM 会重写整个数据库文件并持有排他写锁，在 1 核小容器上可能耗时数分钟，
// 同步执行会阻塞单连接池（SetMaxOpenConns(1)）导致整个面板无响应，因此改为
// 后台任务执行，请求立即返回，前端通过 GET 同路径轮询任务状态。
func (s *Service) vacuumDatabase(w http.ResponseWriter, r *http.Request) {
	queued, err := s.enqueueVacuumTask(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "读取数据库压缩模式失败: " + err.Error(),
		})
		return
	}
	if !queued {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"message": "数据库压缩已在运行中",
			"data":    s.vacuumSnapshot(),
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "数据库压缩已开始，将在后台执行",
		"data":    s.vacuumSnapshot(),
	})
}

// enqueueVacuumTask 探测 auto_vacuum 模式并排队后台压缩任务（互斥单例）。
// 返回 queued=false 表示已有压缩任务在运行（非错误）。
func (s *Service) enqueueVacuumTask(ctx context.Context) (bool, error) {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	if s.vacuumRunning {
		return false, nil
	}
	s.vacuumRunning = true
	s.vacuumStarted = time.Now()
	s.vacuumDone = time.Time{}
	s.vacuumError = ""
	s.vacuumBefore = 0
	s.vacuumAfter = 0

	// 探测 auto_vacuum 模式决定压缩路径：NONE 需首次全量迁移（建立
	// pointer-map，耗时数分钟），INCREMENTAL/FULL 走增量回收（秒级）。
	mode, err := func() (int, error) {
		db, openErr := s.store.Open(ctx)
		if openErr != nil {
			return 0, openErr
		}
		defer db.Close()
		return autoVacuumMode(ctx, db)
	}()
	if err != nil {
		s.vacuumRunning = false
		return false, err
	}
	s.vacuumMode = vacuumModeIncremental
	if mode == 0 {
		s.vacuumMode = vacuumModeMigrate
	}

	s.cleanupWG.Add(1)
	go s.runVacuum(context.Background())
	return true, nil
}

// vacuumStatus 查询数据库压缩任务状态（GET /api/settings/vacuum-database）
func (s *Service) vacuumStatus(w http.ResponseWriter, r *http.Request) {
	response.OK(w, s.vacuumSnapshot())
}

func (s *Service) vacuumSnapshot() map[string]interface{} {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	return map[string]interface{}{
		"running":      s.vacuumRunning,
		"mode":         s.vacuumMode,
		"started":      s.vacuumStarted,
		"done":         s.vacuumDone,
		"error":        s.vacuumError,
		"beforeSizeMB": sizeMBString(s.vacuumBefore),
		"afterSizeMB":  sizeMBString(s.vacuumAfter),
		"savedMB":      sizeMBString(s.vacuumBefore - s.vacuumAfter),
	}
}

func (s *Service) runVacuum(ctx context.Context) {
	defer s.cleanupWG.Done()
	defer func() {
		s.vacuumMu.Lock()
		s.vacuumRunning = false
		s.vacuumDone = time.Now()
		s.vacuumMu.Unlock()
	}()

	db, err := s.store.Open(ctx)
	if err != nil {
		s.vacuumMu.Lock()
		s.vacuumError = err.Error()
		s.vacuumMu.Unlock()
		return
	}
	defer db.Close()

	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	before := databaseStorageStats(ctx, db, s.store.DatabasePath())
	s.vacuumMu.Lock()
	s.vacuumBefore = before.TotalSizeBytes
	s.vacuumMu.Unlock()

	s.vacuumMu.Lock()
	mode := s.vacuumMode
	s.vacuumMu.Unlock()

	if mode == vacuumModeMigrate {
		// 首次迁移：全量 VACUUM 建立 auto_vacuum pointer-map，之后才能
		// 增量回收。256MB 小容器上 temp_store=MEMORY + 16MB 页缓存容易
		// OOM，migrateAutoVacuumIncremental 内会临时切 FILE 并把页缓存
		// 减半，结束后恢复连接级 PRAGMA。
		if err := migrateAutoVacuumIncremental(ctx, db); err != nil {
			s.vacuumMu.Lock()
			s.vacuumError = err.Error()
			s.vacuumMu.Unlock()
			return
		}
	} else {
		// 增量回收：分批回收 freelist，每批 4096 页（约 16MB），
		// 每次持锁仅几十毫秒，1 核机器上对在线请求几乎无感。
		for rounds := 0; rounds < 64; rounds++ {
			if ctx.Err() != nil {
				break
			}
			freePages, err := freelistPageCount(ctx, db)
			if err != nil || freePages == 0 {
				break
			}
			_, _ = db.ExecContext(ctx, `PRAGMA incremental_vacuum(4096)`)
		}
	}
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	after := databaseStorageStats(ctx, db, s.store.DatabasePath())
	s.vacuumMu.Lock()
	s.vacuumAfter = after.TotalSizeBytes
	s.vacuumMu.Unlock()
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
	if deleted > 0 {
		if _, err := db.ExecContext(r.Context(), `VACUUM`); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "database vacuum failed: " + err.Error()})
			return
		}
	}
	_, _ = db.ExecContext(r.Context(), `PRAGMA wal_checkpoint(PASSIVE)`)

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

	preview := false
	if value, ok := toBool(payload["preview"]); ok {
		preview = value
	}
	if preview {
		plans, err := planLogTableLimits(r.Context(), db, days, count)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		var totalDeleted int64
		for _, plan := range plans {
			totalDeleted += plan.Deleted
		}
		currentSize, _ := fileSize(s.store.DatabasePath())
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"preview":       true,
			"totalDeleted":  totalDeleted,
			"tables":        plans,
			"days":          days,
			"count":         count,
			"dbSizeMB":      dbSizeMB,
			"sizeOverLimit": dbSizeMB > 0 && float64(currentSize)/1024/1024 > float64(dbSizeMB),
			"currentSizeMB": sizeMBString(currentSize),
		})
		return
	}

	result, err := enforceLogTableLimits(r.Context(), db, s.store.DatabasePath(), days, count, dbSizeMB, true)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_, _ = db.ExecContext(r.Context(), `PRAGMA wal_checkpoint(PASSIVE)`)

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
			site_brand_icon_id,
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
			public_api_url,
			time_zone,
			ui_font
		FROM user_settings
		WHERE id = 1
	`).Scan(
		&row.CustomCSS,
		&row.ThemeMode,
		&row.PageWidthMode,
		&row.SidebarCollapsed,
		&row.SiteBrandIconID,
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
		&row.TimeZone,
		&row.UIFont,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if _, insertErr := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_settings (id, custom_css, module_visibility, module_order)
			VALUES (1, '', '{"openai":true,"dns":true,"server":true}', '["openai","dns","server"]')
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
	delete(visibility, "gemini-cli")
	delete(visibility, "qwen")
	if legacyValue, ok := visibility["self-h"]; ok {
		if _, exists := visibility["scheduler"]; !exists {
			visibility["scheduler"] = legacyValue
		}
		delete(visibility, "self-h")
	}
	ensureBoolKey(visibility, "scheduler", false)

	channelEnabled := parseObject(row.ChannelEnabled, map[string]interface{}{})
	delete(channelEnabled, "antigravity")
	delete(channelEnabled, "gemini-cli")
	delete(channelEnabled, "qwen")

	channelModelPrefix := parseObject(row.ChannelModelPrefix, map[string]interface{}{})
	delete(channelModelPrefix, "antigravity")
	delete(channelModelPrefix, "gemini-cli")
	delete(channelModelPrefix, "qwen")

	order := parseStringSlice(row.ModuleOrder, []string{"dns", "openai", "server"})
	order = filterString(order, "antigravity")
	order = filterString(order, "gemini-cli")
	order = filterString(order, "qwen")
	for i, item := range order {
		if item == "self-h" {
			order[i] = "scheduler"
		}
	}
	order = uniqueStrings(order)
	order = ensureBefore(order, "scheduler", "server")

	settings := map[string]interface{}{
		"customCss":               nullString(row.CustomCSS, ""),
		"sidebarCollapsed":        nullInt(row.SidebarCollapsed, 0) != 0,
		"siteBrandIconId":         nullString(row.SiteBrandIconID, ""),
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
		"timezone":                nullString(row.TimeZone, "system"),
		"uiFont":                  nullString(row.UIFont, "default"),
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
	assignString(updates, "site_brand_icon_id", settings, "siteBrandIconId", "site_brand_icon_id")
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
	assignString(updates, "time_zone", settings, "timezone", "timeZone", "time_zone")
	assignString(updates, "ui_font", settings, "uiFont", "ui_font")

	if len(updates) == 0 {
		return nil
	}

	columns := []string{
		"custom_css",
		"theme_mode",
		"page_width_mode",
		"sidebar_collapsed",
		"site_brand_icon_id",
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
		"time_zone",
		"ui_font",
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

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
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

// freelistPageCount 返回数据库 freelist 上空闲页数量。
// 空闲页是可回收空间；为 0 说明无需再做空间回收。
func freelistPageCount(ctx context.Context, db *sql.DB) (int64, error) {
	var count int64
	err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read freelist count: %w", err)
	}
	return count, nil
}

// autoVacuumMode 返回数据库 auto_vacuum 模式：0=NONE 1=FULL 2=INCREMENTAL。
func autoVacuumMode(ctx context.Context, db *sql.DB) (int, error) {
	var mode int
	err := db.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode)
	if err != nil {
		return 0, fmt.Errorf("read auto_vacuum: %w", err)
	}
	return mode, nil
}

// migrateAutoVacuumIncremental 将数据库迁移到 auto_vacuum=INCREMENTAL：
// 一次性全量 VACUUM 建立 pointer-map，此后 incremental_vacuum 可长期无痛回收空间。
// 仅在模式为 NONE 时执行（FULL 已自动截断，无需迁移）。
// 注意：全量 VACUUM 需要独占锁，应在低峰期/维护窗口执行一次；执行期间临时
// 收紧内存参数（temp_store=FILE、页缓存减半）避免 256MB 小容器 OOM，完成后
// 恢复连接级 PRAGMA（物理连接会被池化复用，必须还原以免污染后续连接）。
func migrateAutoVacuumIncremental(ctx context.Context, db *sql.DB) error {
	mode, err := autoVacuumMode(ctx, db)
	if err != nil {
		return err
	}
	if mode != 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, `PRAGMA temp_store = FILE`); err != nil {
		return fmt.Errorf("set temp_store file: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA cache_size = -8192`); err != nil {
		return fmt.Errorf("set cache_size: %w", err)
	}
	defer func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `PRAGMA cache_size = -16000`)
		_, _ = db.ExecContext(bg, `PRAGMA temp_store = MEMORY`)
	}()
	if _, err := db.ExecContext(ctx, `PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		return fmt.Errorf("enable auto_vacuum incremental: %w", err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum for auto_vacuum migration: %w", err)
	}
	return nil
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
		"user_settings":         {"id", "theme_mode", "page_width_mode", "sidebar_collapsed", "site_brand_icon_id", "module_visibility", "module_order", "time_zone"},
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

// statisticsTables 是统计/趋势类表：记录使用量、趋势与聚合指标。
// 这些数据是仪表盘与报表的数据源，删除会丢失全部历史统计，
// 因此不参与日志清理（只按独立长保留策略清理，见 enforceLogTableLimits）。
var statisticsTables = map[string]bool{
	"openai_gateway_analytics":    true,
	"system_api_stats":            true,
	"uptime_daily_stats":          true,
	"github_repository_snapshots": true,
	"github_traffic_samples":      true,
	"github_contributors":         true,
}

func listLogTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND (
			name LIKE '%_logs'
			OR name LIKE '%_history'
			OR name LIKE '%_audit'
			OR name IN (
				'uptime_heartbeats', 'server_network_quality_samples',
				'github_action_runs', 'github_events', 'github_webhook_deliveries'
			)
		)
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
		if statisticsTables[name] {
			continue
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

	var deleted int64
	for _, table := range tables {
		if ctx.Err() != nil {
			break
		}
		quotedTable := quoteIdentifier(table)
		// 分批清空：每次只删 cleanupBatchSize 行并自动提交，
		// 避免单条大 DELETE 长时间持有写锁导致系统卡死。
		for ctx.Err() == nil {
			result, err := db.ExecContext(ctx, `
				DELETE FROM `+quotedTable+`
				WHERE rowid IN (
					SELECT rowid FROM `+quotedTable+` LIMIT ?
				)
			`, cleanupBatchSize)
			if err != nil {
				return 0, fmt.Errorf("clear %s: %w", table, err)
			}
			changes, _ := result.RowsAffected()
			deleted += changes
			if changes < cleanupBatchSize {
				break
			}
		}
	}
	return deleted, nil
}

// cleanupBatchSize 是日志清理单批删除的行数上限。
// 分批删除避免单条大 DELETE 长时间持有写锁导致系统卡死。
const cleanupBatchSize = 1000

// statisticsRetentionDays 是统计/趋势表的独立长保留天数。
// 统计表不参与日志清理（避免丢失历史统计），但为避免无限增长，
// 仍按此较长窗口清理明细——默认 180 天，足够仪表盘完整展示历史趋势。
const statisticsRetentionDays = 180

func enforceLogTableLimits(ctx context.Context, db *sql.DB, dbPath string, days, count, dbSizeMB int, allowVacuum bool) (map[string]interface{}, error) {
	if days == 0 && count == 0 && dbSizeMB == 0 {
		return map[string]interface{}{"deleted": int64(0), "reason": "no_limits"}, nil
	}

	tables, err := listLogTables(ctx, db)
	if err != nil {
		return nil, err
	}

	var totalDeleted int64

	// 统计表独立长保留：仅按天数清理超期明细，保持历史统计可用。
	for table := range statisticsTables {
		if ctx.Err() != nil {
			break
		}
		timeColumn, err := logTimeColumn(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if timeColumn == "" {
			continue
		}
		quotedTable := quoteIdentifier(table)
		quotedTime := quoteIdentifier(timeColumn)
		cutoff := time.Now().UTC().AddDate(0, 0, -statisticsRetentionDays).Format(time.RFC3339)
		for ctx.Err() == nil {
			result, err := db.ExecContext(ctx, `
				DELETE FROM `+quotedTable+`
				WHERE `+quotedTime+` < ?
				AND rowid IN (
					SELECT rowid FROM `+quotedTable+`
					WHERE `+quotedTime+` < ?
					LIMIT ?)
			`, cutoff, cutoff, cleanupBatchSize)
			if err != nil {
				return nil, fmt.Errorf("delete old stats from %s: %w", table, err)
			}
			changes, _ := result.RowsAffected()
			totalDeleted += changes
			if changes < cleanupBatchSize {
				break
			}
		}
	}

	if len(tables) == 0 {
		return map[string]interface{}{"deleted": totalDeleted}, nil
	}

	for _, table := range tables {
		if ctx.Err() != nil {
			break
		}
		timeColumn, err := logTimeColumn(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if timeColumn == "" {
			continue
		}
		quotedTable := quoteIdentifier(table)
		quotedTime := quoteIdentifier(timeColumn)

		if days > 0 {
			cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
			// 分批删除旧数据：每次只删 cleanupBatchSize 行，避免长事务锁库。
			for ctx.Err() == nil {
				result, err := db.ExecContext(ctx, `
					DELETE FROM `+quotedTable+`
					WHERE `+quotedTime+` < ?
					AND rowid IN (
						SELECT rowid FROM `+quotedTable+`
						WHERE `+quotedTime+` < ?
						LIMIT ?)
				`, cutoff, cutoff, cleanupBatchSize)
				if err != nil {
					return nil, fmt.Errorf("delete old rows from %s: %w", table, err)
				}
				changes, _ := result.RowsAffected()
				totalDeleted += changes
				if changes < cleanupBatchSize {
					break
				}
			}
		}
		if count > 0 {
			effective := int64(count)
			if value, ok, err := logTableCountFloor(ctx, db, table); err != nil {
				return nil, err
			} else if ok && value > effective {
				effective = value
			}
			// 分批删除最旧行直到表行数 ≤ effective：
			// 每次只删 cleanupBatchSize 行（利用时间索引走 rowid IN），避免全表 NOT IN 扫描。
			for ctx.Err() == nil {
				rowCount, err := countTableRows(ctx, db, table)
				if err != nil {
					return nil, err
				}
				if rowCount <= effective {
					break
				}
				batch := int64(cleanupBatchSize)
				if rowCount-effective < batch {
					batch = rowCount - effective
				}
				result, err := db.ExecContext(ctx, `
					DELETE FROM `+quotedTable+`
					WHERE rowid IN (
						SELECT rowid FROM `+quotedTable+`
						ORDER BY `+quotedTime+` ASC
						LIMIT ?)
				`, batch)
				if err != nil {
					return nil, fmt.Errorf("trim rows from %s: %w", table, err)
				}
				changes, _ := result.RowsAffected()
				totalDeleted += changes
				if changes == 0 {
					break
				}
			}
		}
	}

	cleanupRounds := 0
	if dbSizeMB > 0 && ctx.Err() == nil {
		// 空间回收：优先用有界的 incremental_vacuum 分批回收（不阻塞读写）。
		// 数据库未启用 auto_vacuum 时 incremental_vacuum 是 no-op（安全无副作用）。
		// 全量 VACUUM（含迁移到 INCREMENTAL）仅在手动执行时允许（allowVacuum），
		// 自动清理绝不触发全量 VACUUM——它在生产大库上会独占锁导致系统卡死。
		if allowVacuum {
			if err := migrateAutoVacuumIncremental(ctx, db); err != nil {
				applog.Warn(ctx, "settings", "auto_vacuum migration skipped", "error", err.Error())
			}
		}
		for cleanupRounds < 3 {
			currentSize, err := fileSize(dbPath)
			if err != nil {
				return nil, err
			}
			if float64(currentSize)/1024/1024 <= float64(dbSizeMB) {
				break
			}
			cleanupRounds++
			if ctx.Err() != nil {
				break
			}
			// 有界回收：每次最多回收 4096 页（约 16MB @4KB 页），
			// 避免一次性回收整个 freelist 长时间持有写锁。
			_, _ = db.ExecContext(ctx, `PRAGMA incremental_vacuum(4096)`)
			freePages, _ := freelistPageCount(ctx, db)
			if freePages == 0 {
				break
			}
		}
	}

	return map[string]interface{}{
		"deleted":       totalDeleted,
		"cleanupRounds": cleanupRounds,
	}, nil
}

type logTablePlan struct {
	Table   string `json:"table"`
	Current int64  `json:"current"`
	Kept    int64  `json:"kept"`
	Deleted int64  `json:"deleted"`
	Floor   int64  `json:"floor,omitempty"`
}

// logTableGroupColumns maps a cleanable table to the column identifying the
// tracked entity, so count-based trimming always keeps at least the newest
// record per entity (entity floor).
var logTableGroupColumns = map[string]string{
	"uptime_heartbeats":           "monitor_id",
	"server_metrics_history":      "server_id",
	"server_monitor_logs":         "server_id",
	"openai_health_history":       "endpoint_id",
	"openai_gateway_analytics":    "endpoint_id",
	"github_repository_snapshots": "repository_id",
	"github_traffic_samples":      "repository_id",
	"github_contributors":         "repository_id",
	"github_action_runs":          "repository_id",
	"github_events":               "repository_id",
	"github_webhook_deliveries":   "repository_id",
	"github_operation_audit":      "repository_id",
	"cron_logs":                   "task_id",
	"subscription_access_logs":    "subscription_id",
	"uptime_daily_stats":          "monitor_id",
}

// logTableMinRows is a fixed per-table floor for aggregate tables keyed by a
// growing time bucket where a distinct-entity count would not be bounded.
var logTableMinRows = map[string]int64{
	"system_api_stats": 1,
}

func logTableCountFloor(ctx context.Context, db *sql.DB, table string) (int64, bool, error) {
	if column, ok := logTableGroupColumns[table]; ok {
		var floor int64
		err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT `+quoteIdentifier(column)+`) FROM `+quoteIdentifier(table)).Scan(&floor)
		if err != nil {
			return 0, false, fmt.Errorf("count distinct %s in %s: %w", column, table, err)
		}
		if floor < 1 {
			floor = 1
		}
		return floor, true, nil
	}
	if minRows, ok := logTableMinRows[table]; ok {
		return minRows, true, nil
	}
	return 0, false, nil
}

// planLogTableLimits dry-runs the days/count limits and reports per-table
// deletion counts without modifying any data. The size-based cleanup is not
// included here because it depends on runtime vacuum behavior; callers should
// surface dbSizeMB separately (see sizeOverLimit).
func planLogTableLimits(ctx context.Context, db *sql.DB, days, count int) ([]logTablePlan, error) {
	tables, err := listLogTables(ctx, db)
	if err != nil {
		return nil, err
	}
	cutoff := ""
	if days > 0 {
		cutoff = time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	}
	plans := make([]logTablePlan, 0, len(tables))
	for _, table := range tables {
		timeColumn, err := logTimeColumn(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if timeColumn == "" {
			continue
		}
		total, err := countTableRows(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if total == 0 {
			continue
		}
		var daysDeleted int64
		if cutoff != "" {
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoteIdentifier(table)+` WHERE `+quoteIdentifier(timeColumn)+` < ?`, cutoff).Scan(&daysDeleted); err != nil {
				return nil, fmt.Errorf("count old rows in %s: %w", table, err)
			}
		}
		remaining := total - daysDeleted
		kept := remaining
		var floor int64
		if count > 0 {
			effective := int64(count)
			if value, ok, err := logTableCountFloor(ctx, db, table); err != nil {
				return nil, err
			} else if ok && value > effective {
				effective = value
			}
			floor = effective
			if remaining > effective {
				kept = effective
			}
		}
		deleted := daysDeleted + (remaining - kept)
		if deleted > 0 {
			plans = append(plans, logTablePlan{Table: table, Current: total, Kept: kept, Deleted: deleted, Floor: floor})
		}
	}
	return plans, nil
}

func logTimeColumn(ctx context.Context, db *sql.DB, table string) (string, error) {
	columns, err := tableColumns(ctx, db, table)
	if err != nil {
		return "", err
	}
	for _, column := range []string{"created_at", "checked_at", "timestamp", "recorded_at", "start_time", "collected_at", "date"} {
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

	// 与后台数据库优化互斥：真空执行期间替换文件会相互破坏
	s.vacuumMu.Lock()
	vacuumRunning := s.vacuumRunning
	s.vacuumMu.Unlock()
	if vacuumRunning {
		return "", fmt.Errorf("数据库正在执行优化任务（VACUUM），请稍后重试导入")
	}

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

	// 失效连接池：释放对旧文件的空闲句柄，替换后新连接的读写落到新文件。
	// 注意：进程常驻句柄仍指向旧文件，导入成功后应尽快重启后端。
	database.ResetPool(dbPath)

	cleanupSQLiteSidecars(dbPath)
	replaceErr := func() error {
		if err := os.Remove(dbPath); err == nil || errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(tempTarget, dbPath); err == nil {
				return nil
			}
		}
		return copyFile(tempTarget, dbPath, 0o600)
	}()
	if replaceErr != nil {
		database.ResetPool(dbPath)
		_ = copyFile(backupPath, dbPath, 0o600)
		return "", fmt.Errorf("替换数据库失败: %w", replaceErr)
	}
	cleanupSQLiteSidecars(dbPath)

	// 替换完成后再失效一次，把替换间隙内可能打开旧文件的新连接也排除掉
	database.ResetPool(dbPath)

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
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
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
