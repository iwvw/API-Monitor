package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestUserSettingsReadPatchAndPost(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performSettingsRequest(service, http.MethodGet, "/api/settings", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get settings status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecodeSettings(t, res, &payload)
	if !payload.Success {
		t.Fatal("expected success")
	}
	if payload.Data["customCss"] != "" {
		t.Fatalf("customCss = %#v", payload.Data["customCss"])
	}
	if payload.Data["pageWidthMode"] != "full" {
		t.Fatalf("pageWidthMode default = %#v", payload.Data["pageWidthMode"])
	}
	if payload.Data["uiFont"] != "default" {
		t.Fatalf("uiFont default = %#v", payload.Data["uiFont"])
	}
	visibility := payload.Data["moduleVisibility"].(map[string]interface{})
	if visibility["scheduler"] != false || visibility["self-h"] != nil {
		t.Fatalf("unexpected module visibility: %#v", visibility)
	}

	res = performSettingsRequest(service, http.MethodPatch, "/api/settings", `{
		"themeMode":"dark",
		"pageWidthMode":"wide",
		"sidebarCollapsed":true,
		"uiFont":"lxgw-wenkai-screen",
		"totpSettings":{"maskAccount":true},
		"moduleOrder":["server","antigravity"]
	}`)
	if res.Code != http.StatusOK {
		t.Fatalf("patch settings status = %d body=%s", res.Code, res.Body.String())
	}

	res = performSettingsRequest(service, http.MethodGet, "/api/settings", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get patched settings status = %d body=%s", res.Code, res.Body.String())
	}
	mustDecodeSettings(t, res, &payload)
	if payload.Data["themeMode"] != "dark" || payload.Data["pageWidthMode"] != "wide" || payload.Data["sidebarCollapsed"] != true {
		t.Fatalf("appearance settings not persisted: %#v", payload.Data)
	}
	if payload.Data["uiFont"] != "lxgw-wenkai-screen" {
		t.Fatalf("uiFont not persisted: %#v", payload.Data["uiFont"])
	}
	totpSettings := payload.Data["totpSettings"].(map[string]interface{})
	if totpSettings["maskAccount"] != true {
		t.Fatalf("totp settings not persisted: %#v", totpSettings)
	}
	order := payload.Data["moduleOrder"].([]interface{})
	for _, item := range order {
		if item == "antigravity" {
			t.Fatalf("antigravity should be filtered from module order: %#v", order)
		}
	}

	res = performSettingsRequest(service, http.MethodPost, "/api/settings", `{
		"agentDownloadUrl":"https://example.com/agent",
		"koyebRefreshInterval":45000,
		"channelEnabled":{"openai":false}
	}`)
	if res.Code != http.StatusOK {
		t.Fatalf("post settings status = %d body=%s", res.Code, res.Body.String())
	}

	res = performSettingsRequest(service, http.MethodGet, "/api/settings", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get post settings status = %d body=%s", res.Code, res.Body.String())
	}
	mustDecodeSettings(t, res, &payload)
	if payload.Data["agentDownloadUrl"] != "https://example.com/agent" || payload.Data["koyebRefreshInterval"] != float64(45000) {
		t.Fatalf("post settings not persisted: %#v", payload.Data)
	}
	channelEnabled := payload.Data["channelEnabled"].(map[string]interface{})
	if channelEnabled["openai"] != false {
		t.Fatalf("channelEnabled not persisted: %#v", channelEnabled)
	}
}

func TestSiteBrandIconUploadSelectAndServe(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect width="16" height="16" fill="#111827"/></svg>`)
	uploadRes := performMultipartFileSettingsRequest(t, service, "/api/settings/site-brand/icons", "file", "brand.svg", svg, map[string]string{
		"name": "品牌图标",
	})
	if uploadRes.Code != http.StatusOK {
		t.Fatalf("upload site brand icon status = %d body=%s", uploadRes.Code, uploadRes.Body.String())
	}
	var uploadPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			URL         string `json:"url"`
			ContentType string `json:"contentType"`
		} `json:"data"`
	}
	mustDecodeSettings(t, uploadRes, &uploadPayload)
	if !uploadPayload.Success || uploadPayload.Data.ID == "" || uploadPayload.Data.URL == "" || uploadPayload.Data.Name != "品牌图标" {
		t.Fatalf("unexpected upload payload: %#v", uploadPayload)
	}
	if !strings.Contains(uploadPayload.Data.ContentType, "image/svg+xml") {
		t.Fatalf("unexpected upload content type: %#v", uploadPayload)
	}

	listRes := performSettingsRequest(service, http.MethodGet, "/api/settings/site-brand/icons", "")
	if listRes.Code != http.StatusOK {
		t.Fatalf("list site brand icons status = %d body=%s", listRes.Code, listRes.Body.String())
	}
	var listPayload struct {
		Success bool `json:"success"`
		Data    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"data"`
	}
	mustDecodeSettings(t, listRes, &listPayload)
	if !listPayload.Success || len(listPayload.Data) != 1 || listPayload.Data[0].ID != uploadPayload.Data.ID {
		t.Fatalf("unexpected site brand icon list: %#v", listPayload)
	}
	if listPayload.Data[0].Name != "品牌图标" {
		t.Fatalf("unexpected site brand icon name in list: %#v", listPayload)
	}

	assetRes := performSettingsRequest(service, http.MethodGet, uploadPayload.Data.URL, "")
	if assetRes.Code != http.StatusOK {
		t.Fatalf("site brand icon asset status = %d body=%s", assetRes.Code, assetRes.Body.String())
	}
	if !strings.Contains(assetRes.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(assetRes.Body.String(), "<svg") {
		t.Fatalf("unexpected site brand icon asset response: content-type=%q body=%q", assetRes.Header().Get("Content-Type"), assetRes.Body.String())
	}

	patchBody, err := json.Marshal(map[string]string{"siteBrandIconId": uploadPayload.Data.ID})
	if err != nil {
		t.Fatal(err)
	}
	saveRes := performSettingsRequest(service, http.MethodPatch, "/api/settings", string(patchBody))
	if saveRes.Code != http.StatusOK {
		t.Fatalf("patch site brand setting status = %d body=%s", saveRes.Code, saveRes.Body.String())
	}
	var savePayload struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecodeSettings(t, saveRes, &savePayload)
	if !savePayload.Success || savePayload.Data["siteBrandIconId"] != uploadPayload.Data.ID {
		t.Fatalf("site brand selection not returned from patch response: %#v", savePayload)
	}

	getRes := performSettingsRequest(service, http.MethodGet, "/api/settings", "")
	if getRes.Code != http.StatusOK {
		t.Fatalf("get settings after site brand patch status = %d body=%s", getRes.Code, getRes.Body.String())
	}
	var getPayload struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecodeSettings(t, getRes, &getPayload)
	if !getPayload.Success || getPayload.Data["siteBrandIconId"] != uploadPayload.Data.ID {
		t.Fatalf("site brand icon id not persisted: %#v", getPayload)
	}

	deleteRes := performSettingsRequest(service, http.MethodDelete, "/api/settings/site-brand/icons/"+uploadPayload.Data.ID, "")
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete site brand icon status = %d body=%s", deleteRes.Code, deleteRes.Body.String())
	}

	listRes = performSettingsRequest(service, http.MethodGet, "/api/settings/site-brand/icons", "")
	if listRes.Code != http.StatusOK {
		t.Fatalf("list site brand icons after delete status = %d body=%s", listRes.Code, listRes.Body.String())
	}
	mustDecodeSettings(t, listRes, &listPayload)
	if !listPayload.Success || len(listPayload.Data) != 0 {
		t.Fatalf("unexpected site brand icon list after delete: %#v", listPayload)
	}

	assetRes = performSettingsRequest(service, http.MethodGet, uploadPayload.Data.URL, "")
	if assetRes.Code != http.StatusNotFound {
		t.Fatalf("deleted site brand icon asset status = %d body=%s", assetRes.Code, assetRes.Body.String())
	}
}

func TestDeprecatedTableReasonRecognizesRetiredTables(t *testing.T) {
	cases := []struct {
		table    string
		wantOK   bool
		category string
	}{
		{table: "uptime_heartbeats_backup_1779287377288_migrated", wantOK: true, category: "migration_backup"},
		{table: "uptime_monitors_backup_1779287377288_migrated", wantOK: true, category: "migration_backup"},
		{table: "nextchat_sessions", wantOK: true, category: "legacy_chat"},
		{table: "nextchat_messages", wantOK: true, category: "legacy_chat"},
		{table: "ds_accounts", wantOK: true, category: "legacy_ai_gateway"},
		{table: "ds_settings", wantOK: true, category: "legacy_ai_gateway"},
		{table: "zeabur_accounts", wantOK: true, category: "retired_integration"},
		{table: "nezha_config", wantOK: true, category: "retired_integration"},
		{table: "uptime_monitors", wantOK: false},
		{table: "server_accounts", wantOK: false},
	}
	for _, tc := range cases {
		category, _, ok := deprecatedTableReason(tc.table)
		if ok != tc.wantOK {
			t.Fatalf("%s candidate mismatch: got %v want %v", tc.table, ok, tc.wantOK)
		}
		if ok && category != tc.category {
			t.Fatalf("%s category = %s want %s", tc.table, category, tc.category)
		}
	}
}

func TestDatabaseStatsSelfCheckAndAnalysis(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE chat_messages (
			id INTEGER PRIMARY KEY,
			content TEXT,
			reasoning TEXT
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO chat_messages (content, reasoning)
		VALUES ('hello', 'why'), ('longer', NULL)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO operation_logs (operation_type, table_name, details, user_agent)
		VALUES ('TEST', 'chat_messages', '{}', 'agent')
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	statsRes := performSettingsRequest(service, http.MethodGet, "/api/settings/database-stats", "")
	if statsRes.Code != http.StatusOK {
		t.Fatalf("database-stats status = %d body=%s", statsRes.Code, statsRes.Body.String())
	}
	var statsPayload struct {
		Success bool `json:"success"`
		Data    struct {
			DBPath      string           `json:"dbPath"`
			DBSize      int64            `json:"dbSize"`
			TotalSize   int64            `json:"totalSize"`
			Tables      map[string]int64 `json:"tables"`
			CountsExact bool             `json:"countsExact"`
			Storage     struct {
				MainSizeBytes  int64 `json:"mainSizeBytes"`
				TotalSizeBytes int64 `json:"totalSizeBytes"`
				PageSize       int64 `json:"pageSize"`
				PageCount      int64 `json:"pageCount"`
			} `json:"storage"`
		} `json:"data"`
	}
	mustDecodeSettings(t, statsRes, &statsPayload)
	if !statsPayload.Success || statsPayload.Data.DBPath == "" || statsPayload.Data.DBSize == 0 || statsPayload.Data.TotalSize == 0 {
		t.Fatalf("unexpected stats payload: %#v", statsPayload)
	}
	if statsPayload.Data.Storage.MainSizeBytes == 0 || statsPayload.Data.Storage.TotalSizeBytes < statsPayload.Data.Storage.MainSizeBytes || statsPayload.Data.Storage.PageSize == 0 || statsPayload.Data.Storage.PageCount == 0 {
		t.Fatalf("unexpected storage stats: %#v", statsPayload.Data.Storage)
	}
	if statsPayload.Data.CountsExact || statsPayload.Data.Tables["chat_messages"] != -1 || statsPayload.Data.Tables["operation_logs"] != -1 {
		t.Fatalf("default stats should avoid table counts: %#v", statsPayload.Data)
	}

	deepStatsRes := performSettingsRequest(service, http.MethodGet, "/api/settings/database-stats?deep=1", "")
	if deepStatsRes.Code != http.StatusOK {
		t.Fatalf("deep database-stats status = %d body=%s", deepStatsRes.Code, deepStatsRes.Body.String())
	}
	var deepStatsPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Tables      map[string]int64 `json:"tables"`
			CountsExact bool             `json:"countsExact"`
		} `json:"data"`
	}
	mustDecodeSettings(t, deepStatsRes, &deepStatsPayload)
	if !deepStatsPayload.Data.CountsExact || deepStatsPayload.Data.Tables["chat_messages"] != 2 || deepStatsPayload.Data.Tables["operation_logs"] != 1 {
		t.Fatalf("unexpected deep table stats: %#v", deepStatsPayload.Data)
	}

	selfCheckRes := performSettingsRequest(service, http.MethodGet, "/api/settings/migration-self-check", "")
	if selfCheckRes.Code != http.StatusOK {
		t.Fatalf("migration-self-check status = %d body=%s", selfCheckRes.Code, selfCheckRes.Body.String())
	}
	var selfCheckPayload struct {
		Success bool `json:"success"`
		Data    struct {
			OK     bool `json:"ok"`
			Tables map[string]struct {
				OK             bool     `json:"ok"`
				Exists         bool     `json:"exists"`
				MissingColumns []string `json:"missingColumns"`
			} `json:"tables"`
		} `json:"data"`
	}
	mustDecodeSettings(t, selfCheckRes, &selfCheckPayload)
	if !selfCheckPayload.Success {
		t.Fatal("expected self-check success")
	}
	if !selfCheckPayload.Data.Tables["user_settings"].OK || !selfCheckPayload.Data.Tables["operation_logs"].OK {
		t.Fatalf("core tables should pass self-check: %#v", selfCheckPayload.Data.Tables)
	}
	if selfCheckPayload.Data.OK || selfCheckPayload.Data.Tables["totp_accounts"].Exists {
		t.Fatalf("module tables should still report missing in empty test DB: %#v", selfCheckPayload.Data.Tables["totp_accounts"])
	}

	analysisRes := performSettingsRequest(service, http.MethodGet, "/api/settings/database-analysis", "")
	if analysisRes.Code != http.StatusOK {
		t.Fatalf("database-analysis status = %d body=%s", analysisRes.Code, analysisRes.Body.String())
	}
	var analysisPayload struct {
		Success bool `json:"success"`
		Data    struct {
			DBFileSizeMB string `json:"dbFileSizeMB"`
			CountsExact  bool   `json:"countsExact"`
			Storage      struct {
				MainSizeBytes  int64 `json:"mainSizeBytes"`
				TotalSizeBytes int64 `json:"totalSizeBytes"`
			} `json:"storage"`
			Tables []struct {
				Table              string `json:"table"`
				Rows               int64  `json:"rows"`
				EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
				EstimatedSizeMB    string `json:"estimatedSizeMB"`
				AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
				SizeSource         string `json:"sizeSource"`
			} `json:"tables"`
		} `json:"data"`
	}
	mustDecodeSettings(t, analysisRes, &analysisPayload)
	if !analysisPayload.Success || analysisPayload.Data.DBFileSizeMB == "" || analysisPayload.Data.Storage.MainSizeBytes == 0 {
		t.Fatalf("unexpected analysis payload: %#v", analysisPayload)
	}
	chatAnalysis := findAnalysisTable(analysisPayload.Data.Tables, "chat_messages")
	if analysisPayload.Data.CountsExact || chatAnalysis == nil || chatAnalysis.Rows != -1 || chatAnalysis.EstimatedSizeBytes == 0 || chatAnalysis.SizeSource == "" {
		t.Fatalf("unexpected chat analysis: %#v", chatAnalysis)
	}

	deepAnalysisRes := performSettingsRequest(service, http.MethodGet, "/api/settings/database-analysis?deep=1", "")
	if deepAnalysisRes.Code != http.StatusOK {
		t.Fatalf("deep database-analysis status = %d body=%s", deepAnalysisRes.Code, deepAnalysisRes.Body.String())
	}
	var deepAnalysisPayload struct {
		Success bool `json:"success"`
		Data    struct {
			CountsExact bool `json:"countsExact"`
			Tables      []struct {
				Table              string `json:"table"`
				Rows               int64  `json:"rows"`
				EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
				EstimatedSizeMB    string `json:"estimatedSizeMB"`
				AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
				SizeSource         string `json:"sizeSource"`
			} `json:"tables"`
		} `json:"data"`
	}
	mustDecodeSettings(t, deepAnalysisRes, &deepAnalysisPayload)
	deepChatAnalysis := findAnalysisTable(deepAnalysisPayload.Data.Tables, "chat_messages")
	if !deepAnalysisPayload.Data.CountsExact || deepChatAnalysis == nil || deepChatAnalysis.Rows != 2 || deepChatAnalysis.EstimatedSizeBytes < 14 || deepChatAnalysis.AvgRowSizeBytes == 0 || deepChatAnalysis.SizeSource == "" {
		t.Fatalf("unexpected deep chat analysis: %#v", deepChatAnalysis)
	}
}

func TestLogSettingsAndLogFileRoutes(t *testing.T) {
	dataDir := t.TempDir()
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: dataDir,
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO operation_logs (operation_type, table_name, record_id, details, ip_address, user_agent, trace_id, created_at)
		VALUES ('UPDATE', 'user_settings', '1', '{"changed":true}', '127.0.0.1', 'test-agent', 'trace-1', '2026-06-15T01:02:03Z')
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dataDir, "logs", "app.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("{\"timestamp\":\"2026-06-15T01:02:03Z\",\"level\":\"WARN\",\"module\":\"Settings\",\"message\":\"hello\",\"data\":{\"x\":1}}\n{\"time\":\"2026-06-15T01:02:04Z\",\"level\":\"INFO\",\"module\":\"http\",\"msg\":\"http request\"}\nplain fallback\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	operationRes := performSettingsRequest(service, http.MethodGet, "/api/settings/operation-logs", "")
	if operationRes.Code != http.StatusOK {
		t.Fatalf("operation-logs status = %d body=%s", operationRes.Code, operationRes.Body.String())
	}
	var operationPayload struct {
		Success bool `json:"success"`
		Data    []struct {
			OperationType string `json:"operation_type"`
			TableName     string `json:"table_name"`
			TraceID       string `json:"trace_id"`
		} `json:"data"`
	}
	mustDecodeSettings(t, operationRes, &operationPayload)
	if !operationPayload.Success || len(operationPayload.Data) != 1 || operationPayload.Data[0].OperationType != "UPDATE" || operationPayload.Data[0].TraceID != "trace-1" {
		t.Fatalf("unexpected operation logs payload: %#v", operationPayload)
	}

	logSettingsRes := performSettingsRequest(service, http.MethodPost, "/api/settings/log-settings", `{
		"days": 7,
		"count": 500,
		"dbSizeMB": 64,
		"logFileSizeMB": 2
	}`)
	if logSettingsRes.Code != http.StatusOK {
		t.Fatalf("log-settings post status = %d body=%s", logSettingsRes.Code, logSettingsRes.Body.String())
	}
	var savePayload struct {
		Success  bool `json:"success"`
		FileInfo struct {
			MaxSizeMB int    `json:"maxSizeMB"`
			Path      string `json:"path"`
		} `json:"fileInfo"`
	}
	mustDecodeSettings(t, logSettingsRes, &savePayload)
	if !savePayload.Success || savePayload.FileInfo.MaxSizeMB != 2 || savePayload.FileInfo.Path != logPath {
		t.Fatalf("unexpected saved log settings payload: %#v", savePayload)
	}

	logSettingsGetRes := performSettingsRequest(service, http.MethodGet, "/api/settings/log-settings", "")
	if logSettingsGetRes.Code != http.StatusOK {
		t.Fatalf("log-settings get status = %d body=%s", logSettingsGetRes.Code, logSettingsGetRes.Body.String())
	}
	var getPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Days          int `json:"days"`
			Count         int `json:"count"`
			DBSizeMB      int `json:"dbSizeMB"`
			LogFileSizeMB int `json:"logFileSizeMB"`
		} `json:"data"`
		LogConfig struct {
			MaxFileSizeMB int `json:"maxFileSizeMB"`
		} `json:"logConfig"`
	}
	mustDecodeSettings(t, logSettingsGetRes, &getPayload)
	if !getPayload.Success || getPayload.Data.Days != 7 || getPayload.Data.Count != 500 || getPayload.Data.DBSizeMB != 64 || getPayload.Data.LogFileSizeMB != 2 || getPayload.LogConfig.MaxFileSizeMB != 2 {
		t.Fatalf("unexpected log settings get payload: %#v", getPayload)
	}

	sysLogsRes := performSettingsRequest(service, http.MethodGet, "/api/settings/sys-logs", "")
	if sysLogsRes.Code != http.StatusOK {
		t.Fatalf("sys-logs status = %d body=%s", sysLogsRes.Code, sysLogsRes.Body.String())
	}
	var sysLogsPayload struct {
		Success bool `json:"success"`
		Data    []struct {
			Time    string `json:"time"`
			Level   string `json:"level"`
			Module  string `json:"module"`
			Message string `json:"message"`
		} `json:"data"`
		FileInfo map[string]interface{} `json:"fileInfo"`
	}
	mustDecodeSettings(t, sysLogsRes, &sysLogsPayload)
	if !sysLogsPayload.Success || len(sysLogsPayload.Data) != 3 {
		t.Fatalf("unexpected sys logs payload: %#v", sysLogsPayload)
	}
	if sysLogsPayload.Data[0].Time != "01:02:03" || sysLogsPayload.Data[0].Level != "WARN" || sysLogsPayload.Data[0].Module != "Settings" || sysLogsPayload.Data[0].Message != "hello [DATA]" {
		t.Fatalf("unexpected parsed sys log: %#v", sysLogsPayload.Data[0])
	}
	if sysLogsPayload.Data[1].Time != "01:02:04" || sysLogsPayload.Data[1].Module != "http" || sysLogsPayload.Data[1].Message != "http request" {
		t.Fatalf("unexpected fallback sys log: %#v", sysLogsPayload.Data[1])
	}
	if sysLogsPayload.Data[2].Level != "INFO" || sysLogsPayload.Data[2].Message != "plain fallback" {
		t.Fatalf("unexpected fallback sys log: %#v", sysLogsPayload.Data[2])
	}

	appLogRes := performSettingsRequest(service, http.MethodGet, "/api/settings/app-log-file", "")
	if appLogRes.Code != http.StatusOK {
		t.Fatalf("app-log-file status = %d body=%s", appLogRes.Code, appLogRes.Body.String())
	}
	var appLogPayload struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
		Size    string `json:"size"`
	}
	mustDecodeSettings(t, appLogRes, &appLogPayload)
	if !appLogPayload.Success || !strings.Contains(appLogPayload.Data, "plain fallback") || appLogPayload.Size == "" {
		t.Fatalf("unexpected app log payload: %#v", appLogPayload)
	}

	clearRes := performSettingsRequest(service, http.MethodPost, "/api/settings/clear-app-logs", "")
	if clearRes.Code != http.StatusOK {
		t.Fatalf("clear-app-logs status = %d body=%s", clearRes.Code, clearRes.Body.String())
	}
	cleared, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected empty app log file, got %q", string(cleared))
	}
}

func TestDatabaseMaintenanceActions(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE api_logs (
			id INTEGER PRIMARY KEY,
			details TEXT,
			user_agent TEXT,
			created_at TEXT
		);
		CREATE TABLE job_history (
			id INTEGER PRIMARY KEY,
			created_at TEXT
		);
		INSERT INTO api_logs (id, details, user_agent, created_at) VALUES
			(1, 'old', 'agent', '2026-06-10T00:00:00Z'),
			(2, 'middle', 'agent', '2026-06-11T00:00:00Z'),
			(3, 'new', 'agent', '2026-06-12T00:00:00Z');
		INSERT INTO job_history (id, created_at) VALUES
			(1, '2026-06-10T00:00:00Z'),
			(2, '2026-06-12T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	enforceRes := performSettingsRequest(service, http.MethodPost, "/api/settings/enforce-log-limits", `{"count":1}`)
	if enforceRes.Code != http.StatusOK {
		t.Fatalf("enforce-log-limits status = %d body=%s", enforceRes.Code, enforceRes.Body.String())
	}
	var enforcePayload struct {
		Success bool `json:"success"`
		Data    struct {
			Deleted int64 `json:"deleted"`
		} `json:"data"`
	}
	mustDecodeSettings(t, enforceRes, &enforcePayload)
	if !enforcePayload.Success || enforcePayload.Data.Deleted != 3 {
		t.Fatalf("unexpected enforce payload: %#v", enforcePayload)
	}

	db, err = service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countRowsForTest(t, db, "api_logs") != 1 || countRowsForTest(t, db, "job_history") != 1 {
		t.Fatalf("expected each log table to keep newest row")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	clearRes := performSettingsRequest(service, http.MethodPost, "/api/settings/clear-logs", "")
	if clearRes.Code != http.StatusOK {
		t.Fatalf("clear-logs status = %d body=%s", clearRes.Code, clearRes.Body.String())
	}
	var clearPayload struct {
		Success bool  `json:"success"`
		Count   int64 `json:"count"`
	}
	mustDecodeSettings(t, clearRes, &clearPayload)
	if !clearPayload.Success || clearPayload.Count != 2 {
		t.Fatalf("unexpected clear logs payload: %#v", clearPayload)
	}

	vacuumRes := performSettingsRequest(service, http.MethodPost, "/api/settings/vacuum-database", "")
	if vacuumRes.Code != http.StatusOK {
		t.Fatalf("vacuum status = %d body=%s", vacuumRes.Code, vacuumRes.Body.String())
	}

	// vacuum 异步执行：轮询状态直至任务结束，避免后台 goroutine 持有临时
	// 数据库文件导致 TempDir 清理失败。
	deadline := time.Now().Add(10 * time.Second)
	for {
		statusRes := performSettingsRequest(service, http.MethodGet, "/api/settings/vacuum-database", "")
		if statusRes.Code != http.StatusOK {
			t.Fatalf("vacuum status query = %d body=%s", statusRes.Code, statusRes.Body.String())
		}
		var statusPayload struct {
			Success bool `json:"success"`
			Data    struct {
				Running      bool   `json:"running"`
				Error        string `json:"error"`
				BeforeSizeMB string `json:"beforeSizeMB"`
				AfterSizeMB  string `json:"afterSizeMB"`
				SavedMB      string `json:"savedMB"`
			} `json:"data"`
		}
		mustDecodeSettings(t, statusRes, &statusPayload)
		if !statusPayload.Data.Running {
			if statusPayload.Data.Error != "" {
				t.Fatalf("vacuum failed: %s", statusPayload.Data.Error)
			}
			if statusPayload.Data.BeforeSizeMB == "" || statusPayload.Data.AfterSizeMB == "" || statusPayload.Data.SavedMB == "" {
				t.Fatalf("unexpected vacuum payload: %#v", statusPayload)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("vacuum did not complete within 10s")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestEnforceLogLimitsExtendedTables(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE github_repository_snapshots (
			id INTEGER PRIMARY KEY,
			repository_id INTEGER,
			collected_at DATETIME
		);
		CREATE TABLE ai_access_audit (
			id INTEGER PRIMARY KEY,
			agent_name TEXT,
			created_at DATETIME
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	seedTimes := map[string][]string{
		"github_repository_snapshots": {"2026-06-10T00:00:00Z", "2026-06-11T00:00:00Z", "2026-06-12T00:00:00Z"},
		"ai_access_audit":             {"2026-06-10T00:00:00Z", "2026-06-11T00:00:00Z", "2026-06-12T00:00:00Z"},
		"system_api_stats":            {"2026-06-10", "2026-06-11", "2026-06-12"},
	}
	for table, times := range seedTimes {
		for _, at := range times {
			var err error
			switch table {
			case "github_repository_snapshots":
				_, err = db.ExecContext(ctx, `INSERT INTO github_repository_snapshots (repository_id, collected_at) VALUES (1, ?)`, at)
			case "ai_access_audit":
				_, err = db.ExecContext(ctx, `INSERT INTO ai_access_audit (agent_name, created_at) VALUES ('agent', ?)`, at)
			case "system_api_stats":
				_, err = db.ExecContext(ctx, `INSERT INTO system_api_stats (date, audit_count, ops_count, updated_at) VALUES (?, 0, 0, ?)`, at, at)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	tables, err := listLogTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ai_access_audit"} {
		found := false
		for _, got := range tables {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("listLogTables missing %q, got %v", want, tables)
		}
	}
	// 统计/趋势表不参与日志清理（独立长保留）。
	for _, want := range []string{"github_repository_snapshots", "system_api_stats"} {
		for _, got := range tables {
			if got == want {
				t.Fatalf("listLogTables should not include statistics table %q, got %v", want, tables)
			}
		}
	}

	for table, wantCol := range map[string]string{
		"github_repository_snapshots": "collected_at",
		"ai_access_audit":             "created_at",
		"system_api_stats":            "date",
	} {
		col, err := logTimeColumn(ctx, db, table)
		if err != nil {
			t.Fatal(err)
		}
		if col != wantCol {
			t.Fatalf("logTimeColumn(%s) = %q, want %q", table, col, wantCol)
		}
	}

	result, err := enforceLogTableLimits(ctx, db, service.store.DatabasePath(), 0, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	// 仅 ai_access_audit（日志表）受 count 限制；统计表（snapshots/system_api_stats）保留。
	if result["deleted"].(int64) != 1 {
		t.Fatalf("expected 1 deleted row, got %d", result["deleted"].(int64))
	}
	if n, _ := countTableRows(ctx, db, "ai_access_audit"); n != 2 {
		t.Fatalf("ai_access_audit rows = %d, want 2", n)
	}
	for _, table := range []string{"github_repository_snapshots", "system_api_stats"} {
		n, err := countTableRows(ctx, db, table)
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Fatalf("statistics table %s rows = %d, want 3 (preserved)", table, n)
		}
	}
}

func TestEnforceLogLimitsPreviewDoesNotDelete(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE api_logs (
			id INTEGER PRIMARY KEY,
			details TEXT,
			user_agent TEXT,
			created_at TEXT
		);
		INSERT INTO api_logs (id, details, user_agent, created_at) VALUES
			(1, 'old', 'agent', '2026-06-10T00:00:00Z'),
			(2, 'middle', 'agent', '2026-06-11T00:00:00Z'),
			(3, 'new', 'agent', '2026-06-12T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	previewRes := performSettingsRequest(service, http.MethodPost, "/api/settings/enforce-log-limits", `{"count":1,"preview":true}`)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("preview enforce-log-limits status = %d body=%s", previewRes.Code, previewRes.Body.String())
	}
	var previewPayload struct {
		Success      bool  `json:"success"`
		Preview      bool  `json:"preview"`
		TotalDeleted int64 `json:"totalDeleted"`
		Tables       []struct {
			Table   string `json:"table"`
			Current int64  `json:"current"`
			Kept    int64  `json:"kept"`
			Deleted int64  `json:"deleted"`
		} `json:"tables"`
	}
	mustDecodeSettings(t, previewRes, &previewPayload)
	if !previewPayload.Success || !previewPayload.Preview || previewPayload.TotalDeleted != 2 {
		t.Fatalf("unexpected preview payload: %#v", previewPayload)
	}
	if len(previewPayload.Tables) != 1 || previewPayload.Tables[0].Table != "api_logs" ||
		previewPayload.Tables[0].Current != 3 || previewPayload.Tables[0].Kept != 1 || previewPayload.Tables[0].Deleted != 2 {
		t.Fatalf("unexpected preview tables: %#v", previewPayload.Tables)
	}

	db, err = service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countRowsForTest(t, db, "api_logs") != 3 {
		t.Fatalf("preview must not delete rows, api_logs rows = %d", countRowsForTest(t, db, "api_logs"))
	}
}

func TestEnforceLogLimitsEntityFloor(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE github_action_runs (
			id INTEGER PRIMARY KEY,
			repository_id INTEGER,
			created_at DATETIME
		);
		INSERT INTO github_action_runs (repository_id, created_at) VALUES
			(1, '2026-06-10T00:00:00Z'), (1, '2026-06-11T00:00:00Z'), (1, '2026-06-12T00:00:00Z'),
			(2, '2026-06-10T00:00:00Z'), (2, '2026-06-11T00:00:00Z'), (2, '2026-06-12T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}

	plans, err := planLogTableLimits(ctx, db, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	var plan *logTablePlan
	for i := range plans {
		if plans[i].Table == "github_action_runs" {
			plan = &plans[i]
			break
		}
	}
	if plan == nil {
		t.Fatalf("plan missing github_action_runs, got %#v", plans)
	}
	if plan.Current != 6 || plan.Kept != 2 || plan.Deleted != 4 || plan.Floor != 2 {
		t.Fatalf("unexpected floor plan: %#v", plan)
	}

	result, err := enforceLogTableLimits(ctx, db, service.store.DatabasePath(), 0, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if result["deleted"].(int64) != 4 {
		t.Fatalf("expected 4 deleted rows, got %d", result["deleted"].(int64))
	}
	for _, repoID := range []int64{1, 2} {
		var remaining int64
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_action_runs WHERE repository_id = ?`, repoID).Scan(&remaining)
		if err != nil {
			t.Fatal(err)
		}
		if remaining < 1 {
			t.Fatalf("repository %d lost its newest run", repoID)
		}
	}
}

func TestAutoCleanupOnce(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE api_logs (
			id INTEGER PRIMARY KEY,
			details TEXT,
			user_agent TEXT,
			created_at TEXT
		);
		INSERT INTO api_logs (id, details, user_agent, created_at) VALUES
			(1, 'old', 'agent', '2026-06-10T00:00:00Z'),
			(2, 'middle', 'agent', '2026-06-11T00:00:00Z'),
			(3, 'new', 'agent', '2026-06-12T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"log_auto_cleanup":       "1",
		"log_retention_days":     "0",
		"log_max_count":          "1",
		"log_max_db_size_mb":     "0",
		"log_auto_cleanup_hours": "24",
	} {
		if err := setSystemConfig(ctx, db, key, value, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	service.autoCleanupOnce(context.Background())

	db, err = service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countRowsForTest(t, db, "api_logs") != 1 {
		t.Fatalf("auto cleanup should keep 1 row, api_logs rows = %d", countRowsForTest(t, db, "api_logs"))
	}
}

func TestBackgroundCleanupStartStop(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	service.StartBackgroundCleanup()
	service.StartBackgroundCleanup()
	service.Stop()
	service.Stop()
}

func TestWALMaintenanceStartStop(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	service.StartWALMaintenance()
	service.StartWALMaintenance()
	service.Stop()
	service.Stop()
}

func TestListLogTablesExcludesUserData(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE server_snippets (id INTEGER PRIMARY KEY, name TEXT, created_at DATETIME);
		CREATE TABLE prompt_entries (id INTEGER PRIMARY KEY, title TEXT, created_at DATETIME);
		CREATE TABLE totp_accounts (id INTEGER PRIMARY KEY, name TEXT, created_at DATETIME);
		CREATE TABLE settings_registry (domain TEXT, defaults_json TEXT);
		CREATE TABLE subscription_profiles (id INTEGER PRIMARY KEY, name TEXT, created_at DATETIME);
	`)
	if err != nil {
		t.Fatal(err)
	}

	tables, err := listLogTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, table := range tables {
		got[table] = true
	}
	for _, protected := range []string{"server_snippets", "prompt_entries", "totp_accounts", "settings_registry", "subscription_profiles"} {
		if got[protected] {
			t.Fatalf("listLogTables must not include user data table %q, got %v", protected, tables)
		}
	}
}

func TestDeprecatedTablePreviewAndCleanup(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE music_settings (id INTEGER PRIMARY KEY, value TEXT);
		CREATE TABLE openlist_accounts (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE ai_chat_messages (id INTEGER PRIMARY KEY, content TEXT);
		CREATE TABLE qwen_logs (id INTEGER PRIMARY KEY, details TEXT);
		CREATE TABLE active_records (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO music_settings (value) VALUES ('old');
		INSERT INTO openlist_accounts (name) VALUES ('legacy');
		INSERT INTO ai_chat_messages (content) VALUES ('message');
		INSERT INTO qwen_logs (details) VALUES ('log');
		INSERT INTO active_records (name) VALUES ('keep');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	previewRes := performSettingsRequest(service, http.MethodGet, "/api/settings/deprecated-tables", "")
	if previewRes.Code != http.StatusOK {
		t.Fatalf("deprecated preview status = %d body=%s", previewRes.Code, previewRes.Body.String())
	}
	var previewPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Count     int   `json:"count"`
			TotalRows int64 `json:"totalRows"`
			Tables    []struct {
				Table    string `json:"table"`
				Rows     int64  `json:"rows"`
				Category string `json:"category"`
			} `json:"tables"`
		} `json:"data"`
	}
	mustDecodeSettings(t, previewRes, &previewPayload)
	if !previewPayload.Success || previewPayload.Data.Count != 4 || previewPayload.Data.TotalRows != 4 {
		t.Fatalf("unexpected deprecated preview payload: %#v", previewPayload)
	}

	cleanupRes := performSettingsRequest(service, http.MethodPost, "/api/settings/cleanup-deprecated-tables", `{"tables":["music_settings","qwen_logs"]}`)
	if cleanupRes.Code != http.StatusOK {
		t.Fatalf("deprecated cleanup status = %d body=%s", cleanupRes.Code, cleanupRes.Body.String())
	}
	var cleanupPayload struct {
		Success bool `json:"success"`
		Data    struct {
			DeletedRows int64  `json:"deletedRows"`
			BackupPath  string `json:"backupPath"`
			Dropped     []struct {
				Table string `json:"table"`
			} `json:"dropped"`
		} `json:"data"`
	}
	mustDecodeSettings(t, cleanupRes, &cleanupPayload)
	if !cleanupPayload.Success || cleanupPayload.Data.DeletedRows != 2 || cleanupPayload.Data.BackupPath == "" || len(cleanupPayload.Data.Dropped) != 2 {
		t.Fatalf("unexpected deprecated cleanup payload: %#v", cleanupPayload)
	}
	if _, err := os.Stat(cleanupPayload.Data.BackupPath); err != nil {
		t.Fatalf("expected cleanup backup to exist: %v", err)
	}

	db, err = service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"music_settings", "qwen_logs"} {
		exists, err := tableExists(context.Background(), db, table)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("expected %s to be dropped", table)
		}
	}
	for _, table := range []string{"openlist_accounts", "ai_chat_messages", "active_records"} {
		exists, err := tableExists(context.Background(), db, table)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("expected %s to remain", table)
		}
	}
	if countRowsForTest(t, db, "active_records") != 1 {
		t.Fatal("active table should keep its row")
	}
}

func TestDatabaseExportImportPreviewCommit(t *testing.T) {
	dataDir := t.TempDir()
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: dataDir,
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE current_records (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO current_records (name) VALUES ('before-export');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	exportRes := performSettingsRequest(service, http.MethodGet, "/api/settings/export-database", "")
	if exportRes.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exportRes.Code, exportRes.Body.String())
	}
	if !strings.Contains(exportRes.Header().Get("Content-Disposition"), "api-monitor-backup-") {
		t.Fatalf("unexpected export content-disposition: %s", exportRes.Header().Get("Content-Disposition"))
	}
	if !bytes.HasPrefix(exportRes.Body.Bytes(), []byte("SQLite format 3")) {
		t.Fatalf("export did not return sqlite bytes, first bytes=%q", exportRes.Body.Bytes()[:min(16, exportRes.Body.Len())])
	}

	importPath := filepath.Join(t.TempDir(), "incoming.db")
	writeSQLiteFixture(t, importPath, "imported_records", 2)
	previewRes := performMultipartSettingsRequest(t, service, "/api/settings/database/import/preview", importPath, "incoming.db")
	if previewRes.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", previewRes.Code, previewRes.Body.String())
	}
	var previewPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Token        string `json:"token"`
			OriginalName string `json:"originalName"`
			Analysis     struct {
				Integrity  string `json:"integrity"`
				TableCount int    `json:"tableCount"`
				Tables     []struct {
					Name string `json:"name"`
					Rows *int64 `json:"rows"`
				} `json:"tables"`
			} `json:"analysis"`
		} `json:"data"`
	}
	mustDecodeSettings(t, previewRes, &previewPayload)
	if !previewPayload.Success || previewPayload.Data.Token == "" || previewPayload.Data.OriginalName != "incoming.db" || previewPayload.Data.Analysis.Integrity != "ok" || previewPayload.Data.Analysis.TableCount < 3 {
		t.Fatalf("unexpected preview payload: %#v", previewPayload)
	}

	commitBody, err := json.Marshal(map[string]interface{}{"token": previewPayload.Data.Token, "confirm": true})
	if err != nil {
		t.Fatal(err)
	}
	commitRes := performSettingsRequest(service, http.MethodPost, "/api/settings/database/import/commit", string(commitBody))
	if commitRes.Code != http.StatusOK {
		t.Fatalf("commit status = %d body=%s", commitRes.Code, commitRes.Body.String())
	}
	var commitPayload struct {
		Success    bool   `json:"success"`
		BackupPath string `json:"backupPath"`
	}
	mustDecodeSettings(t, commitRes, &commitPayload)
	if !commitPayload.Success || commitPayload.BackupPath == "" {
		t.Fatalf("unexpected commit payload: %#v", commitPayload)
	}
	if _, err := os.Stat(commitPayload.BackupPath); err != nil {
		t.Fatalf("expected import backup to exist: %v", err)
	}

	db, err = service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countRowsForTest(t, db, "imported_records") != 2 {
		t.Fatalf("expected imported_records to be present")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClearLogTablesIncludesTelemetrySamples(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE server_network_quality_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE uptime_heartbeats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE regular_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO server_network_quality_samples DEFAULT VALUES;
		INSERT INTO uptime_heartbeats DEFAULT VALUES;
		INSERT INTO regular_records DEFAULT VALUES;
	`)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := clearLogTables(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted telemetry rows = %d, want 2", deleted)
	}
	if countRowsForTest(t, db, "server_network_quality_samples") != 0 {
		t.Fatal("network quality samples should be cleared")
	}
	if countRowsForTest(t, db, "uptime_heartbeats") != 0 {
		t.Fatal("uptime heartbeats should be cleared")
	}
	if countRowsForTest(t, db, "regular_records") != 1 {
		t.Fatal("non-log tables should remain")
	}
}

func performSettingsRequest(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func performMultipartSettingsRequest(t *testing.T, service *Service, path, filePath, fileName string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("database", fileName)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func performMultipartFileSettingsRequest(t *testing.T, service *Service, path, fieldName, fileName string, content []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func writeSQLiteFixture(t *testing.T, path, table string, rows int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			description TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE user_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			custom_css TEXT,
			module_visibility TEXT,
			module_order TEXT
		);
		INSERT INTO user_settings (id, custom_css, module_visibility, module_order)
		VALUES (1, '', '{}', '[]');
		CREATE TABLE `+quoteIdentifier(table)+` (
			id INTEGER PRIMARY KEY,
			name TEXT
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO `+quoteIdentifier(table)+` (name) VALUES (?)`, "row"); err != nil {
			t.Fatal(err)
		}
	}
}

func mustDecodeSettings(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
}

func countRowsForTest(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, table string) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+quoteIdentifier(table)).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", table, err)
	}
	return count
}

func findAnalysisTable(tables []struct {
	Table              string `json:"table"`
	Rows               int64  `json:"rows"`
	EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
	EstimatedSizeMB    string `json:"estimatedSizeMB"`
	AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
	SizeSource         string `json:"sizeSource"`
}, name string) *struct {
	Table              string `json:"table"`
	Rows               int64  `json:"rows"`
	EstimatedSizeBytes int64  `json:"estimatedSizeBytes"`
	EstimatedSizeMB    string `json:"estimatedSizeMB"`
	AvgRowSizeBytes    int64  `json:"avgRowSizeBytes"`
	SizeSource         string `json:"sizeSource"`
} {
	for i := range tables {
		if tables[i].Table == name {
			return &tables[i]
		}
	}
	return nil
}
