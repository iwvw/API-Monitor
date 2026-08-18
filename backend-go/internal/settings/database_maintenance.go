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
	{Prefix: "antigravity_", Category: "legacy_ai_gateway", Reason: "Legacy Antigravity gateway tables are not used by the current Go backend"},
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
	mode, modeErr := autoVacuumMode(r.Context(), db)
	if modeErr == nil && mode == 2 {
		for rounds := 0; rounds < 64; rounds++ {
			freePages, freeErr := freelistPageCount(r.Context(), db)
			if freeErr != nil || freePages == 0 {
				break
			}
			_, _ = db.ExecContext(r.Context(), `PRAGMA incremental_vacuum(4096)`)
		}
		_, _ = db.ExecContext(r.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`)
	} else if modeErr == nil && mode == 0 {
		_, _ = s.enqueueVacuumTask(r.Context())
	}

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
