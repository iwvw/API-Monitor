package settings

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type databaseStorageInfo struct {
	DBPath           string `json:"dbPath"`
	MainSizeBytes    int64  `json:"mainSizeBytes"`
	WALSizeBytes     int64  `json:"walSizeBytes"`
	SHMSizeBytes     int64  `json:"shmSizeBytes"`
	JournalSizeBytes int64  `json:"journalSizeBytes"`
	TotalSizeBytes   int64  `json:"totalSizeBytes"`
	PageSize         int64  `json:"pageSize"`
	PageCount        int64  `json:"pageCount"`
	FreelistCount    int64  `json:"freelistCount"`
	UsedPageBytes    int64  `json:"usedPageBytes"`
	FreePageBytes    int64  `json:"freePageBytes"`
}

type tablePageSize struct {
	TableBytes int64
	IndexBytes int64
}

type deprecatedTableCandidate struct {
	Table     string `json:"table"`
	Rows      int64  `json:"rows"`
	SizeBytes int64  `json:"sizeBytes"`
	SizeMB    string `json:"sizeMB"`
	Category  string `json:"category"`
	Reason    string `json:"reason"`
}

type deprecatedTableRule struct {
	Exact    string
	Prefix   string
	Category string
	Reason   string
}

var deprecatedTableRules = []deprecatedTableRule{
	{Prefix: "music_", Category: "retired_module", Reason: "Music module is retired"},
	{Prefix: "openlist_", Category: "retired_module", Reason: "OpenList module is retired"},
	{Prefix: "chat_", Category: "legacy_chat", Reason: "Legacy chat tables are no longer used by the current OpenAI chat UI"},
	{Exact: "nextchat_sessions", Category: "legacy_chat", Reason: "Legacy NextChat tables are no longer used by the current OpenAI chat UI"},
	{Exact: "nextchat_messages", Category: "legacy_chat", Reason: "Legacy NextChat tables are no longer used by the current OpenAI chat UI"},
	{Prefix: "ai_chat_", Category: "legacy_ai", Reason: "Legacy AI chat tables are no longer used by the current UI"},
	{Prefix: "ai_draw_", Category: "legacy_ai", Reason: "Legacy AI drawing tables are no longer used by the current UI"},
	{Prefix: "lobsterai_", Category: "retired_module", Reason: "LobsterAI plugin is removed"},
	{Prefix: "zcode_", Category: "retired_module", Reason: "ZCode plugin is removed"},
	{Prefix: "autoclaw_", Category: "retired_module", Reason: "AutoClaw plugin is removed"},
	{Prefix: "gemini_cli_", Category: "legacy_ai_gateway", Reason: "Legacy Gemini CLI gateway tables are not used by the current Go backend"},
	{Prefix: "qwen_", Category: "legacy_ai_gateway", Reason: "Legacy Qwen gateway tables are not used by the current Go backend"},
	{Exact: "ds_accounts", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_session_cache", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_settings", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_logs", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_file_cache", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_model_checks", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "ds_model_redirects", Category: "legacy_ai_gateway", Reason: "Legacy DS gateway tables are not used by the current Go backend"},
	{Exact: "nezha_config", Category: "retired_integration", Reason: "Nezha integration is not implemented in the current Go backend"},
	{Exact: "zeabur_accounts", Category: "retired_integration", Reason: "Zeabur integration is not implemented in the current Go backend"},
	{Exact: "zeabur_projects", Category: "retired_integration", Reason: "Zeabur integration is not implemented in the current Go backend"},
}

func databaseStorageStats(ctx context.Context, db *sql.DB, dbPath string) databaseStorageInfo {
	info := databaseStorageInfo{
		DBPath:           dbPath,
		MainSizeBytes:    fileSizeIfExists(dbPath),
		WALSizeBytes:     fileSizeIfExists(dbPath + "-wal"),
		SHMSizeBytes:     fileSizeIfExists(dbPath + "-shm"),
		JournalSizeBytes: fileSizeIfExists(dbPath + "-journal"),
		PageSize:         pragmaInt64(ctx, db, "page_size"),
		PageCount:        pragmaInt64(ctx, db, "page_count"),
		FreelistCount:    pragmaInt64(ctx, db, "freelist_count"),
	}
	info.TotalSizeBytes = info.MainSizeBytes + info.WALSizeBytes + info.SHMSizeBytes + info.JournalSizeBytes
	if info.PageSize > 0 {
		info.FreePageBytes = info.FreelistCount * info.PageSize
		info.UsedPageBytes = (info.PageCount - info.FreelistCount) * info.PageSize
		if info.UsedPageBytes < 0 {
			info.UsedPageBytes = 0
		}
	}
	return info
}

func fileSizeIfExists(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

func pragmaInt64(ctx context.Context, db *sql.DB, name string) int64 {
	var value int64
	_ = db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&value)
	return value
}

func tablePageSizes(ctx context.Context, db *sql.DB) (map[string]tablePageSize, string) {
	owners, objectTypes, err := sqliteObjectOwners(ctx, db)
	if err != nil {
		return map[string]tablePageSize{}, ""
	}

	rows, err := db.QueryContext(ctx, `
		SELECT name, COALESCE(SUM(pgsize), 0)
		FROM dbstat
		GROUP BY name
	`)
	if err != nil {
		return map[string]tablePageSize{}, ""
	}
	defer rows.Close()

	sizes := map[string]tablePageSize{}
	for rows.Next() {
		var objectName string
		var bytes int64
		if err := rows.Scan(&objectName, &bytes); err != nil {
			return map[string]tablePageSize{}, ""
		}
		owner := owners[objectName]
		if owner == "" {
			owner = autoIndexOwner(objectName)
		}
		if owner == "" || strings.HasPrefix(owner, "sqlite_") {
			continue
		}
		size := sizes[owner]
		if objectTypes[objectName] == "table" || objectName == owner {
			size.TableBytes += bytes
		} else {
			size.IndexBytes += bytes
		}
		sizes[owner] = size
	}
	if err := rows.Err(); err != nil {
		return map[string]tablePageSize{}, ""
	}
	return sizes, "dbstat"
}

func sqliteObjectOwners(ctx context.Context, db *sql.DB) (map[string]string, map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, tbl_name, type
		FROM sqlite_master
		WHERE type IN ('table', 'index')
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	owners := map[string]string{}
	objectTypes := map[string]string{}
	for rows.Next() {
		var name, tableName, objectType string
		if err := rows.Scan(&name, &tableName, &objectType); err != nil {
			return nil, nil, err
		}
		owners[name] = tableName
		objectTypes[name] = objectType
	}
	return owners, objectTypes, rows.Err()
}

func autoIndexOwner(name string) string {
	const prefix = "sqlite_autoindex_"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(name, prefix)
	index := strings.LastIndex(rest, "_")
	if index <= 0 {
		return ""
	}
	return rest[:index]
}

func (s *Service) getDeprecatedTables(w http.ResponseWriter, r *http.Request) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	candidates, err := deprecatedTableCandidates(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"tables":       candidates,
		"count":        len(candidates),
		"totalRows":    sumDeprecatedRows(candidates),
		"totalSize":    sumDeprecatedSize(candidates),
		"backupOnDrop": true,
	})
}

func (s *Service) cleanupDeprecatedTables(w http.ResponseWriter, r *http.Request) {
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

	candidates, err := deprecatedTableCandidates(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	rawTables, hasTableSelection := payload["tables"]
	targets, err := selectedDeprecatedTables(rawTables, hasTableSelection, candidates)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(targets) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "no deprecated tables to clean",
			"data": map[string]interface{}{
				"dropped":     []deprecatedTableCandidate{},
				"deletedRows": int64(0),
				"storage":     databaseStorageStats(r.Context(), db, s.store.DatabasePath()),
			},
		})
		return
	}

	backupPath := filepath.Join(s.backupDir(), "api-monitor-before-deprecated-cleanup-"+timestampForFile(time.Now())+".db")
	if err := s.backupCurrentDatabase(r.Context(), backupPath); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "backup current database failed: " + err.Error()})
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "begin cleanup failed: " + err.Error()})
		return
	}
	for _, table := range targets {
		if _, err := tx.ExecContext(r.Context(), "DROP TABLE IF EXISTS "+quoteIdentifier(table.Table)); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": fmt.Sprintf("drop %s failed: %v", table.Table, err)})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "commit cleanup failed: " + err.Error()})
		return
	}

	// 回收空间：INCREMENTAL 库按批增量回收（每次持锁仅几十毫秒，对在线请求
	// 几乎无感）；NONE 存量库无法增量回收，排队后台全量迁移压缩（复用既有
	// 异步任务机制，请求不阻塞）。
	s.reclaimDroppedTableSpace(r.Context(), db, true)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("deprecated table cleanup completed, dropped %d tables", len(targets)),
		"data": map[string]interface{}{
			"dropped":     targets,
			"deletedRows": sumDeprecatedRows(targets),
			"backupPath":  backupPath,
			"storage":     databaseStorageStats(r.Context(), db, s.store.DatabasePath()),
		},
	})
}

// reclaimDroppedTableSpace 按 auto_vacuum 模式回收 DROP 后的空闲页：
// INCREMENTAL 库在用户主动操作（interactive=true，设置页动作）时分批增量回收
// 并用 PASSIVE checkpoint 推进 WAL 回写；自动路径（interactive=false，服务
// 启动清理）只做 PASSIVE checkpoint——硬性规则禁止自动路径 TRUNCATE/RESTART
// （常驻轮询读者 + modernc 驱动会无限阻塞，是 database is locked 风暴根因），
// 增量回收留给下一次用户主动压缩。
// NONE 存量库无法增量回收，排队后台全量迁移压缩（复用既有异步任务机制，不阻塞）。
func (s *Service) reclaimDroppedTableSpace(ctx context.Context, db *sql.DB, interactive bool) {
	mode, modeErr := autoVacuumMode(ctx, db)
	if modeErr == nil && mode == 2 {
		if !interactive {
			_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
			return
		}
		for rounds := 0; rounds < 64; rounds++ {
			freePages, freeErr := freelistPageCount(ctx, db)
			if freeErr != nil || freePages == 0 {
				break
			}
			_, _ = db.ExecContext(ctx, `PRAGMA incremental_vacuum(4096)`)
		}
		_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
	} else if modeErr == nil && mode == 0 {
		_, _ = s.enqueueVacuumTask(ctx)
	}
}

// StartupCleanupDeprecatedTables 在服务启动阶段自动清理废弃表：
// 仅当存在按 deprecatedTableRules 匹配的表时，先备份数据库再 DROP 并回收空间；
// 无候选时静默跳过。供系统更新/重启后的启动流程调用，替代手动到设置页清理。
func (s *Service) StartupCleanupDeprecatedTables(ctx context.Context) {
	s.cleanupStaleImportPreviews(time.Now())
	db, err := s.store.Open(ctx)
	if err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup skipped", "error", err.Error())
		return
	}
	defer db.Close()

	candidates, err := deprecatedTableCandidates(ctx, db)
	if err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup scan failed", "error", err.Error())
		return
	}
	targets, err := selectedDeprecatedTables(nil, false, candidates)
	if err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup select failed", "error", err.Error())
		return
	}
	if len(targets) == 0 {
		return
	}

	backupPath := filepath.Join(s.backupDir(), "api-monitor-before-deprecated-cleanup-"+timestampForFile(time.Now())+".db")
	if err := s.backupCurrentDatabase(ctx, backupPath); err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup backup failed", "error", err.Error())
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup begin failed", "error", err.Error())
		return
	}
	for _, table := range targets {
		if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteIdentifier(table.Table)); err != nil {
			_ = tx.Rollback()
			applog.Warn(ctx, "settings", "startup deprecated table cleanup drop failed", "table", table.Table, "error", err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		applog.Warn(ctx, "settings", "startup deprecated table cleanup commit failed", "error", err.Error())
		return
	}
	s.reclaimDroppedTableSpace(ctx, db, false)
	applog.Info(ctx, "settings", fmt.Sprintf("startup deprecated table cleanup dropped %d tables", len(targets)), "tables", strings.Join(tableNames(targets), ", "))
}

func tableNames(tables []deprecatedTableCandidate) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Table)
	}
	return names
}

func deprecatedTableCandidates(ctx context.Context, db *sql.DB) ([]deprecatedTableCandidate, error) {
	tables, err := listUserTables(ctx, db)
	if err != nil {
		return nil, err
	}
	pageSizes, _ := tablePageSizes(ctx, db)
	candidates := make([]deprecatedTableCandidate, 0)
	for _, table := range tables {
		category, reason, ok := deprecatedTableReason(table)
		if !ok {
			continue
		}
		rows, err := countTableRows(ctx, db, table)
		if err != nil {
			return nil, err
		}
		pageSize := pageSizes[table]
		sizeBytes := pageSize.TableBytes + pageSize.IndexBytes
		candidates = append(candidates, deprecatedTableCandidate{
			Table:     table,
			Rows:      rows,
			SizeBytes: sizeBytes,
			SizeMB:    sizeMBString(sizeBytes),
			Category:  category,
			Reason:    reason,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].SizeBytes == candidates[j].SizeBytes {
			return candidates[i].Table < candidates[j].Table
		}
		return candidates[i].SizeBytes > candidates[j].SizeBytes
	})
	return candidates, nil
}

func deprecatedTableReason(table string) (string, string, bool) {
	name := strings.ToLower(table)
	if isMigratedBackupTable(name) {
		return "migration_backup", "Migration backup table superseded by the current uptime schema", true
	}
	for _, rule := range deprecatedTableRules {
		if rule.Exact != "" && name == rule.Exact {
			return rule.Category, rule.Reason, true
		}
		if rule.Prefix != "" && strings.HasPrefix(name, rule.Prefix) {
			return rule.Category, rule.Reason, true
		}
	}
	return "", "", false
}

func isMigratedBackupTable(name string) bool {
	return strings.HasPrefix(name, "uptime_") &&
		strings.Contains(name, "_backup_") &&
		strings.HasSuffix(name, "_migrated")
}

func selectedDeprecatedTables(raw interface{}, hasSelection bool, candidates []deprecatedTableCandidate) ([]deprecatedTableCandidate, error) {
	requested := stringSetFromPayload(raw)
	if len(requested) == 0 && !hasSelection {
		return candidates, nil
	}
	byName := map[string]deprecatedTableCandidate{}
	for _, candidate := range candidates {
		byName[candidate.Table] = candidate
	}
	selected := make([]deprecatedTableCandidate, 0, len(requested))
	for table := range requested {
		candidate, ok := byName[table]
		if !ok {
			return nil, fmt.Errorf("%s is not a cleanup candidate", table)
		}
		selected = append(selected, candidate)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].Table < selected[j].Table
	})
	return selected, nil
}

func stringSetFromPayload(value interface{}) map[string]bool {
	result := map[string]bool{}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result[strings.TrimSpace(text)] = true
			}
		}
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				result[strings.TrimSpace(item)] = true
			}
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			if strings.TrimSpace(item) != "" {
				result[strings.TrimSpace(item)] = true
			}
		}
	}
	return result
}

func sumDeprecatedRows(candidates []deprecatedTableCandidate) int64 {
	var total int64
	for _, candidate := range candidates {
		total += candidate.Rows
	}
	return total
}

func sumDeprecatedSize(candidates []deprecatedTableCandidate) int64 {
	var total int64
	for _, candidate := range candidates {
		total += candidate.SizeBytes
	}
	return total
}
