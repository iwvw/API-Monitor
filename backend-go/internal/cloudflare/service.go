package cloudflare

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	defaultAPIBase = "https://api.cloudflare.com"
	requestTimeout = 20 * time.Second
)

type Service struct {
	cfg     config.Config
	store   *database.Store
	schema  database.SchemaEnsurer
	client  *http.Client
	apiBase string
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:     cfg,
		store:   database.New(cfg),
		client:  &http.Client{Timeout: requestTimeout},
		apiBase: envURL("CLOUDFLARE_API_BASE_URL", defaultAPIBase),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/cloudflare")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
		for i, part := range parts {
			if unescaped, err := url.PathUnescape(part); err == nil {
				parts[i] = unescaped
			}
		}
	}

	switch {
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets":
		s.r2Buckets(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "metrics":
		s.r2Metrics(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets":
		s.deleteR2Bucket(w, r, parts[1], parts[4])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects":
		s.r2Objects(w, r, parts[1], parts[4])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[6] == "folder-download":
		s.r2FolderDownload(w, r, parts[1], parts[4])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects":
		s.r2ObjectMutation(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "download-info":
		s.r2ObjectDownloadInfo(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "download":
		s.r2ObjectDownload(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "preview":
		s.r2ObjectPreview(w, r, parts[1], parts[4], parts[6])

	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "tunnels":
		s.tunnels(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "tunnels":
		s.tunnelMutation(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "configuration":
		s.tunnelConfiguration(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "token":
		s.tunnelToken(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "connections":
		s.tunnelConnections(w, r, parts[1], parts[3])

	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 1 && parts[0] == "record-types" && r.Method == http.MethodGet:
		response.JSON(w, http.StatusOK, supportedRecordTypes())
	case len(parts) == 1 && parts[0] == "zones" && r.Method == http.MethodGet:
		s.allZones(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "export" && r.Method == http.MethodGet:
		s.exportAccounts(w, r)
	case len(parts) == 2 && parts[0] == "export" && parts[1] == "accounts" && r.Method == http.MethodGet:
		s.exportAccountsRaw(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "accounts" && r.Method == http.MethodPost:
		s.importAccounts(w, r)
	case len(parts) == 1 && parts[0] == "templates":
		s.templates(w, r)
	case len(parts) == 2 && parts[0] == "templates":
		s.templateMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "templates" && parts[2] == "apply" && r.Method == http.MethodPost:
		s.applyTemplate(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "templates" && r.Method == http.MethodPost:
		s.importTemplates(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyStoredAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "token" && r.Method == http.MethodGet:
		s.accountToken(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "cf-account-id" && r.Method == http.MethodGet:
		s.cloudflareAccountIDRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "zones":
		s.accountZones(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "pages":
		s.pages(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "pages":
		s.pagesProject(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "deployments":
		s.pagesDeployments(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "deployments":
		s.deletePagesDeployment(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "domains":
		s.pagesDomains(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "domains":
		s.deletePagesDomain(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "workers":
		s.workers(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "workers":
		s.workerScript(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "toggle" && r.Method == http.MethodPost:
		s.toggleWorker(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "analytics" && r.Method == http.MethodGet:
		s.workerAnalytics(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "domains":
		s.workerDomains(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "domains" && r.Method == http.MethodDelete:
		s.deleteWorkerDomain(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "zones" && r.Method == http.MethodDelete:
		s.deleteZone(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "workers" && parts[5] == "routes":
		s.workerRoutes(w, r, parts[1], parts[3])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "workers" && parts[5] == "routes":
		s.workerRouteMutation(w, r, parts[1], parts[3], parts[6])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "records":
		s.zoneRecords(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "purge" && r.Method == http.MethodPost:
		s.purgeZoneCache(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "ssl":
		s.zoneSSL(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "analytics" && r.Method == http.MethodGet:
		s.zoneAnalytics(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "switch" && r.Method == http.MethodPost:
		s.switchDNSContent(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "batch" && r.Method == http.MethodPost:
		s.batchCreateRecords(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "records" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.recordMutation(w, r, parts[1], parts[3], parts[5])
	default:
		response.Error(w, http.StatusNotFound, "cloudflare route not implemented")
	}
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cf_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			api_token TEXT NOT NULL,
			email TEXT,
			user_email TEXT,
			cf_account_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			is_active INTEGER DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS cf_dns_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			records TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS cf_zones (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			name TEXT NOT NULL,
			status TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES cf_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS cf_dns_records (
			id TEXT PRIMARY KEY,
			zone_id TEXT NOT NULL,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			content TEXT NOT NULL,
			ttl INTEGER DEFAULT 1,
			proxied INTEGER DEFAULT 0,
			priority INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (zone_id) REFERENCES cf_zones(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_zones_account ON cf_zones(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_dns_records_zone ON cf_dns_records(zone_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure cloudflare schema: %w", err)
		}
	}
	if err := ensureColumn(ctx, db, "cf_accounts", "cf_account_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "cf_accounts", "user_email", "TEXT"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, name, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition)
	return err
}

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		accounts, err := loadAccounts(r.Context(), db)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		safe := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			safe = append(safe, safeAccount(account))
		}
		response.JSON(w, http.StatusOK, safe)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		apiToken := strings.TrimSpace(stringValue(payload["apiToken"], stringValue(payload["api_token"], "")))
		email := strings.TrimSpace(stringValue(payload["email"], ""))
		userEmail := strings.TrimSpace(stringValue(payload["userEmail"], stringValue(payload["user_email"], "")))
		cfAccountID := strings.TrimSpace(stringValue(payload["cfAccountId"], stringValue(payload["cf_account_id"], "")))
		if name == "" || apiToken == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和 API Token 必填"})
			return
		}
		if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if !boolValue(payload["skipVerify"]) {
			verification := s.verifyToken(r.Context(), apiToken, email, cfAccountID)
			if !verification.Valid {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Token 无效: " + verification.Error})
				return
			}
			if verification.Email != "" {
				userEmail = verification.Email
			}
		}
		encrypted, err := secure.SecureEncrypt(apiToken)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		id := newAccountID()
		createdAt := time.Now().UTC().Format(time.RFC3339)
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		if _, err := db.ExecContext(
			r.Context(),
			`INSERT INTO cf_accounts (id, name, api_token, email, user_email, cf_account_id, created_at, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			id,
			name,
			encrypted,
			nullableString(email),
			nullableString(userEmail),
			nullableString(cfAccountID),
			createdAt,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"account": map[string]interface{}{
				"id":          id,
				"name":        name,
				"email":       email,
				"userEmail":   userEmail,
				"cfAccountId": cfAccountID,
				"createdAt":   createdAt,
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context(), false)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": exported})
}

func (s *Service) exportAccountsRaw(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context(), true)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, exported)
}

func (s *Service) exportedAccounts(ctx context.Context, includeID bool) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return nil, err
	}
	exported := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		item := map[string]interface{}{
			"name":        account["name"],
			"email":       account["email"],
			"userEmail":   stringValue(account["user_email"], ""),
			"cfAccountId": stringValue(account["cf_account_id"], ""),
			"apiToken":    secure.SecureDecrypt(stringValue(account["api_token"], "")),
		}
		if includeID {
			item["id"] = account["id"]
			item["createdAt"] = account["created_at"]
			item["lastUsed"] = account["last_used"]
		}
		exported = append(exported, item)
	}
	return exported, nil
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	items := arrayValue(payload["accounts"])
	if items == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 accounts 数组"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if boolValue(payload["overwrite"]) {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM cf_accounts`); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		account := objectValue(item)
		name := strings.TrimSpace(stringValue(account["name"], ""))
		apiToken := strings.TrimSpace(stringValue(account["apiToken"], stringValue(account["api_token"], "")))
		userEmail := strings.TrimSpace(stringValue(account["userEmail"], stringValue(account["user_email"], "")))
		cfAccountID := strings.TrimSpace(stringValue(account["cfAccountId"], stringValue(account["cf_account_id"], "")))
		if name == "" || apiToken == "" {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "账号名称和 API Token 必填"})
			return
		}
		if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		encrypted, err := secure.SecureEncrypt(secure.SecureDecrypt(apiToken))
		if err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		id := strings.TrimSpace(stringValue(account["id"], ""))
		if id == "" || !boolValue(payload["overwrite"]) {
			id = newAccountID()
		}
		createdAt := stringValue(account["createdAt"], stringValue(account["created_at"], now))
		lastUsed := stringValue(account["lastUsed"], stringValue(account["last_used"], ""))
		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO cf_accounts (id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
			id,
			name,
			encrypted,
			nullableString(strings.TrimSpace(stringValue(account["email"], ""))),
			nullableString(userEmail),
			nullableString(cfAccountID),
			createdAt,
			nullableString(lastUsed),
		); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": len(items)})
}

func (s *Service) templates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		templates, err := loadTemplates(r.Context(), db)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, templates)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		records, _ := templateRecordsFromPayload(payload)
		if name == "" || len(records) == 0 {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和至少一条记录必填"})
			return
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		id := newTemplateID()
		now := time.Now().UTC().Format(time.RFC3339)
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		if _, err := db.ExecContext(
			r.Context(),
			`INSERT INTO cf_dns_templates (id, name, description, records, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id,
			name,
			nullableString(stringValue(payload["description"], "")),
			string(recordJSON),
			now,
			now,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"template": map[string]interface{}{
				"id":          id,
				"name":        name,
				"description": stringValue(payload["description"], ""),
				"records":     records,
				"createdAt":   now,
				"updatedAt":   now,
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) templateMutation(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM cf_dns_templates WHERE id = ?`, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "模板不存在"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		existing, err := loadTemplate(r.Context(), db, id)
		if err != nil {
			status := http.StatusInternalServerError
			message := err.Error()
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
				message = "模板不存在"
			}
			response.JSON(w, status, map[string]interface{}{"error": message})
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := stringValue(existing["name"], "")
		if _, ok := payload["name"]; ok {
			name = strings.TrimSpace(stringValue(payload["name"], ""))
		}
		description := stringValue(existing["description"], "")
		if _, ok := payload["description"]; ok {
			description = stringValue(payload["description"], "")
		}
		records := arrayValue(existing["records"])
		if nextRecords, provided := templateRecordsFromPayload(payload); provided {
			records = nextRecords
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339)
		if _, err := db.ExecContext(
			r.Context(),
			`UPDATE cf_dns_templates SET name = ?, description = ?, records = ?, updated_at = ? WHERE id = ?`,
			name,
			nullableString(description),
			string(recordJSON),
			updatedAt,
			id,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		updated, err := loadTemplate(r.Context(), db, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "template": updated})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) applyTemplate(w http.ResponseWriter, r *http.Request, templateID string) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	accountID := strings.TrimSpace(stringValue(payload["accountId"], ""))
	zoneID := strings.TrimSpace(stringValue(payload["zoneId"], ""))
	if accountID == "" || zoneID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "accountId 和 zoneId 必填"})
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	template, err := loadTemplate(r.Context(), db, templateID)
	_ = db.Close()
	if err != nil {
		status := http.StatusInternalServerError
		message := err.Error()
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			message = "模板不存在"
		}
		response.JSON(w, status, map[string]interface{}{"error": message})
		return
	}
	templateRecords := arrayValue(template["records"])
	if len(templateRecords) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "模板没有可应用的记录"})
		return
	}
	overrideName := strings.TrimSpace(stringValue(payload["recordName"], ""))
	results := []map[string]interface{}{}
	errorsOut := []map[string]interface{}{}
	for _, item := range templateRecords {
		record := objectValue(item)
		next := map[string]interface{}{
			"type":     record["type"],
			"name":     stringValue(record["name"], "@"),
			"content":  record["content"],
			"ttl":      record["ttl"],
			"proxied":  record["proxied"],
			"priority": record["priority"],
		}
		if overrideName != "" {
			next["name"] = overrideName
		}
		created, err := s.createDNSRecord(r.Context(), auth, zoneID, next)
		if err != nil {
			errorsOut = append(errorsOut, map[string]interface{}{"success": false, "record": next, "error": err.Error()})
			continue
		}
		results = append(results, map[string]interface{}{"success": true, "record": created})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": len(errorsOut) == 0,
		"created": len(results),
		"failed":  len(errorsOut),
		"results": results,
		"errors":  errorsOut,
	})
}

func (s *Service) importTemplates(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	items := arrayValue(payload["templates"])
	if items == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 templates 数组"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if boolValue(payload["overwrite"]) {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM cf_dns_templates`); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		template := objectValue(item)
		name := strings.TrimSpace(stringValue(template["name"], ""))
		records, _ := templateRecordsFromPayload(template)
		if name == "" || len(records) == 0 {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "模板名称和 records 必填"})
			return
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		id := strings.TrimSpace(stringValue(template["id"], ""))
		if id == "" || !boolValue(payload["overwrite"]) {
			id = newTemplateID()
		}
		createdAt := stringValue(template["createdAt"], stringValue(template["created_at"], now))
		updatedAt := stringValue(template["updatedAt"], stringValue(template["updated_at"], now))
		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO cf_dns_templates (id, name, description, records, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id,
			name,
			nullableString(stringValue(template["description"], "")),
			string(recordJSON),
			createdAt,
			updatedAt,
		); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": len(items)})
}

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM cf_accounts WHERE id = ?`, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "账号不存在"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		existing, err := loadAccount(r.Context(), db, id)
		if err != nil {
			status := http.StatusInternalServerError
			message := err.Error()
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
				message = "账号不存在"
			}
			response.JSON(w, status, map[string]interface{}{"error": message})
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], stringValue(existing["name"], "")))
		email := strings.TrimSpace(stringValue(payload["email"], stringValue(existing["email"], "")))
		userEmail := strings.TrimSpace(stringValue(payload["userEmail"], stringValue(payload["user_email"], stringValue(existing["user_email"], ""))))
		cfAccountID := strings.TrimSpace(stringValue(payload["cfAccountId"], stringValue(payload["cf_account_id"], stringValue(existing["cf_account_id"], ""))))
		apiToken := strings.TrimSpace(stringValue(payload["apiToken"], stringValue(payload["api_token"], "")))
		if apiToken == "" || strings.Contains(apiToken, "****") {
			apiToken = secure.SecureDecrypt(stringValue(existing["api_token"], ""))
		} else {
			if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
				return
			}
			verification := s.verifyToken(r.Context(), apiToken, email, cfAccountID)
			if !verification.Valid {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Token 无效: " + verification.Error})
				return
			}
			if verification.Email != "" {
				userEmail = verification.Email
			}
		}
		encrypted, err := secure.SecureEncrypt(apiToken)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		if name == "" || apiToken == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和 API Token 必填"})
			return
		}
		if _, err := db.ExecContext(
			r.Context(),
			`UPDATE cf_accounts SET name = ?, api_token = ?, email = ?, user_email = ?, cf_account_id = ? WHERE id = ?`,
			name,
			encrypted,
			nullableString(email),
			nullableString(userEmail),
			nullableString(cfAccountID),
			id,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) verifyStoredAccount(w http.ResponseWriter, r *http.Request, id string) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return
	}
	verification := s.verifyToken(r.Context(), secure.SecureDecrypt(stringValue(account["api_token"], "")), stringValue(account["email"], ""), stringValue(account["cf_account_id"], ""))
	if verification.Valid {
		response.JSON(w, http.StatusOK, map[string]interface{}{"valid": true, "status": verification.Status, "expiresOn": verification.ExpiresOn})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"valid": false, "error": verification.Error})
}

func (s *Service) accountToken(w http.ResponseWriter, r *http.Request, id string) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"apiToken": secure.SecureDecrypt(stringValue(account["api_token"], "")),
	})
}

func (s *Service) allZones(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	accounts, err := loadAccounts(r.Context(), db)
	_ = db.Close()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	allZones := []interface{}{}
	for _, account := range accounts {
		token := secure.SecureDecrypt(stringValue(account["api_token"], ""))
		if token == "" {
			continue
		}
		zones, _, err := s.listZones(r.Context(), cloudflareAuthForAccount(token, account), nil)
		if err != nil {
			continue
		}
		allZones = append(allZones, zones...)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": allZones})
}

func (s *Service) accountZones(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		zones, pagination, err := s.listZones(r.Context(), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := make([]map[string]interface{}, 0, len(zones))
		for _, item := range zones {
			mapped = append(mapped, mapZone(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"zones": mapped, "pagination": pagination})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		if name == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "域名不能为空"})
			return
		}
		cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		body := map[string]interface{}{
			"name":       name,
			"jump_start": boolValue(payload["jumpStart"]),
			"type":       "full",
			"account":    map[string]interface{}{"id": cfAccountID},
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		zone := objectValue(apiPayload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"zone": map[string]interface{}{
				"id":          zone["id"],
				"name":        zone["name"],
				"status":      zone["status"],
				"nameServers": zone["name_servers"],
				"createdOn":   zone["created_on"],
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) cloudflareAccountIDRoute(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "cfAccountId": cfAccountID})
}

func (s *Service) pages(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/pages/projects", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	projects := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		projects = append(projects, mapPagesProject(objectValue(item)))
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"projects": projects, "cfAccountId": cfAccountID})
}

func (s *Service) pagesProject(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/pages/projects/"+url.PathEscape(projectName), auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) pagesDeployments(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/deployments?per_page=20"
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	deployments := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		deployment := objectValue(item)
		if len(deployment) == 0 {
			continue
		}
		deployments = append(deployments, mapPagesDeployment(deployment))
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "deployments": deployments})
}

func (s *Service) deletePagesDeployment(w http.ResponseWriter, r *http.Request, accountID, projectName, deploymentID string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/deployments/" + url.PathEscape(deploymentID)
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) pagesDomains(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/domains"
	switch r.Method {
	case http.MethodGet:
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		domains := []map[string]interface{}{}
		for _, item := range arrayValue(payload["result"]) {
			domains = append(domains, mapPagesDomain(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domains": domains})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		domain := strings.TrimSpace(stringValue(payload["domain"], ""))
		if domain == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "domain is required"})
			return
		}
		result, err := s.cfRequest(r.Context(), http.MethodPost, path, auth, map[string]interface{}{"name": domain})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domain": result["result"]})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deletePagesDomain(w http.ResponseWriter, r *http.Request, accountID, projectName, domain string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/domains/" + url.PathEscape(domain)
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) workers(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	subdomainPayload, _ := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/subdomain", auth, nil)
	subdomain := objectValue(subdomainPayload["result"])
	workers := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		worker := objectValue(item)
		workers = append(workers, map[string]interface{}{
			"id":         worker["id"],
			"name":       stringValue(worker["id"], stringValue(worker["name"], "")),
			"createdOn":  worker["created_on"],
			"modifiedOn": worker["modified_on"],
			"etag":       worker["etag"],
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"workers":     workers,
		"subdomain":   nullableString(stringValue(subdomain["subdomain"], "")),
		"cfAccountId": cfAccountID,
	})
}

func (s *Service) workerScript(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		script, err := s.getWorkerScript(r.Context(), auth, cfAccountID, scriptName)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"worker":  map[string]interface{}{"name": scriptName, "script": script, "meta": nil},
		})
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		script := stringValue(payload["script"], "")
		if strings.TrimSpace(script) == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "脚本内容不能为空"})
			return
		}
		result, err := s.putWorkerScript(r.Context(), auth, cfAccountID, scriptName, script, payload)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "worker": result})
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) toggleWorker(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts/"+url.PathEscape(scriptName)+"/subdomain", auth, map[string]interface{}{"enabled": boolValue(payload["enabled"])})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result["result"]})
}

func (s *Service) workerRoutes(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/workers/routes", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		routes := []map[string]interface{}{}
		for _, item := range arrayValue(payload["result"]) {
			routes = append(routes, mapWorkerRoute(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"routes": routes})
	case http.MethodPost:
		body, ok := s.workerRouteBody(w, r)
		if !ok {
			return
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/workers/routes", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "route": mapWorkerRoute(objectValue(payload["result"]))})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) workerRouteMutation(w http.ResponseWriter, r *http.Request, accountID, zoneID, routeID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/workers/routes/" + url.PathEscape(routeID)
	switch r.Method {
	case http.MethodPut:
		body, ok := s.workerRouteBody(w, r)
		if !ok {
			return
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPut, path, auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "route": mapWorkerRoute(objectValue(payload["result"]))})
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) workerRouteBody(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return nil, false
	}
	pattern := strings.TrimSpace(stringValue(payload["pattern"], ""))
	script := strings.TrimSpace(stringValue(payload["script"], ""))
	if pattern == "" || script == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "pattern 和 script 必填"})
		return nil, false
	}
	return map[string]interface{}{"pattern": pattern, "script": script}, true
}

func (s *Service) workerAnalytics(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/workers/scripts/" + url.PathEscape(scriptName) + "/analytics"
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": nil})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": payload["result"]})
}

func (s *Service) workerDomains(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		domains, err := s.listWorkerDomains(r.Context(), auth, cfAccountID, scriptName)
		if err != nil {
			domains = []map[string]interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domains": domains})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		hostname := strings.TrimSpace(stringValue(payload["hostname"], ""))
		if hostname == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "请输入域名"})
			return
		}
		domain, err := s.addWorkerDomain(r.Context(), auth, cfAccountID, scriptName, hostname, stringValue(payload["environment"], "production"))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domain": domain})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteWorkerDomain(w http.ResponseWriter, r *http.Request, accountID, scriptName, domainID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	_ = scriptName
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/domains/"+url.PathEscape(domainID), auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteZone(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodDelete, "/zones/"+url.PathEscape(zoneID), auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": payload["result"]})
}

func (s *Service) zoneRecords(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		records, pagination, err := s.listDNSRecords(r.Context(), auth, zoneID, r.URL.Query())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := make([]map[string]interface{}, 0, len(records))
		for _, item := range records {
			mapped = append(mapped, mapDNSRecord(objectValue(item), true))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"records": mapped, "pagination": pagination})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if errors := validateDNSRecord(payload); len(errors) > 0 {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": strings.Join(errors, ", ")})
			return
		}
		record, err := s.createDNSRecord(r.Context(), auth, zoneID, payload)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "record": mapDNSRecord(record, false)})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordMutation(w http.ResponseWriter, r *http.Request, accountID, zoneID, recordID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, dnsRecordBody(payload, true))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "record": mapDNSRecord(objectValue(apiPayload["result"]), false)})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) batchCreateRecords(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	records := arrayValue(payload["records"])
	if records == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 records 数组"})
		return
	}
	results := []map[string]interface{}{}
	errorsOut := []map[string]interface{}{}
	for _, item := range records {
		recordPayload := objectValue(item)
		created, err := s.createDNSRecord(r.Context(), auth, zoneID, recordPayload)
		if err != nil {
			errorsOut = append(errorsOut, map[string]interface{}{"success": false, "record": recordPayload, "error": err.Error()})
			continue
		}
		results = append(results, map[string]interface{}{"success": true, "record": created})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": len(errorsOut) == 0,
		"created": len(results),
		"failed":  len(errorsOut),
		"results": results,
		"errors":  errorsOut,
	})
}

func (s *Service) switchDNSContent(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	recordType := strings.TrimSpace(stringValue(payload["type"], ""))
	name := strings.TrimSpace(stringValue(payload["name"], ""))
	newContent := strings.TrimSpace(stringValue(payload["newContent"], ""))
	if recordType == "" || name == "" || newContent == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "type, name, newContent 必填"})
		return
	}
	params := url.Values{}
	params.Set("type", recordType)
	params.Set("name", name)
	records, _, err := s.listDNSRecords(r.Context(), auth, zoneID, params)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if len(records) == 0 {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": fmt.Sprintf("No %s record found for %s", recordType, name)})
		return
	}
	updated := []map[string]interface{}{}
	for _, item := range records {
		record := objectValue(item)
		recordID := stringValue(record["id"], "")
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, map[string]interface{}{"content": newContent})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := objectValue(apiPayload["result"])
		updated = append(updated, map[string]interface{}{"id": mapped["id"], "name": mapped["name"], "content": mapped["content"]})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": len(updated), "records": updated})
}

func (s *Service) purgeZoneCache(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	body := map[string]interface{}{"purge_everything": true}
	if boolValue(payload["purge_everything"]) {
		body = map[string]interface{}{"purge_everything": true}
	} else if files := arrayValue(payload["files"]); files != nil {
		body = map[string]interface{}{"files": files}
	} else if tags := arrayValue(payload["tags"]); tags != nil {
		body = map[string]interface{}{"tags": tags}
	}
	apiPayload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/purge_cache", auth, body)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "缓存已清除",
		"result":  apiPayload["result"],
	})
}

func (s *Service) zoneSSL(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/settings/ssl", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		certificates := []map[string]interface{}{}
		if payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/ssl/certificate_packs", auth, nil); err == nil {
			for _, item := range arrayValue(payload["result"]) {
				cert := objectValue(item)
				certificates = append(certificates, map[string]interface{}{
					"id":                   cert["id"],
					"type":                 cert["type"],
					"hosts":                cert["hosts"],
					"status":               cert["status"],
					"validityDays":         cert["validity_days"],
					"certificateAuthority": cert["certificate_authority"],
					"primary":              cert["primary"],
				})
			}
		}
		verification := []interface{}{}
		if payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/ssl/verification", auth, nil); err == nil {
			verification = arrayValue(payload["result"])
			if verification == nil {
				verification = []interface{}{}
			}
		}
		result := objectValue(settings["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"ssl": map[string]interface{}{
				"mode":         result["value"],
				"modifiedOn":   result["modified_on"],
				"editable":     result["editable"],
				"certificates": certificates,
				"verification": verification,
			},
		})
	case http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		mode := strings.TrimSpace(stringValue(payload["mode"], ""))
		if !containsString([]string{"off", "flexible", "full", "strict"}, mode) {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "无效的 SSL 模式"})
			return
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/settings/ssl", auth, map[string]interface{}{"value": mode})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		result := objectValue(apiPayload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"ssl": map[string]interface{}{
				"mode":       result["value"],
				"modifiedOn": result["modified_on"],
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) zoneAnalytics(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	timeRange := strings.TrimSpace(r.URL.Query().Get("timeRange"))
	if timeRange == "" {
		timeRange = "24h"
	}
	analytics, err := s.simpleAnalytics(r.Context(), auth, zoneID, timeRange)
	if err != nil {
		analytics = emptyAnalytics()
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": analytics, "timeRange": timeRange})
}

func (s *Service) simpleAnalytics(ctx context.Context, auth map[string]string, zoneID, timeRange string) (map[string]interface{}, error) {
	window := analyticsWindow(timeRange)
	groupName := "httpRequests1hGroups"
	dimensionField := "datetime"
	filter := fmt.Sprintf(`datetime_geq: "%s", datetime_leq: "%s"`, window["since"], window["until"])
	limit := 24
	if timeRange == "7d" || timeRange == "30d" {
		groupName = "httpRequests1dGroups"
		dimensionField = "date"
		filter = fmt.Sprintf(`date_geq: "%s", date_leq: "%s"`, window["sinceDate"], window["untilDate"])
		if timeRange == "7d" {
			limit = 7
		} else {
			limit = 30
		}
	}
	query := fmt.Sprintf(`{
 viewer {
  zones(filter: {zoneTag: "%s"}) {
   totals: %s(limit: 1, filter: { %s }) {
    sum { requests bytes cachedRequests cachedBytes threats pageViews }
    uniq { uniques }
   }
   series: %s(limit: %d, filter: { %s }, orderBy: [%s_ASC]) {
    dimensions { %s }
    sum { requests bytes cachedRequests cachedBytes threats pageViews }
    uniq { uniques }
   }
  }
 }
}`, zoneID, groupName, filter, groupName, limit, filter, dimensionField, dimensionField)
	payload, err := s.cfRequest(ctx, http.MethodPost, "/graphql", auth, map[string]interface{}{"query": query})
	if err != nil {
		return nil, err
	}
	if gqlErrors := arrayValue(payload["errors"]); len(gqlErrors) > 0 {
		first := objectValue(gqlErrors[0])
		return nil, errors.New(stringValue(first["message"], "GraphQL error"))
	}
	data := objectValue(payload["data"])
	viewer := objectValue(data["viewer"])
	zones := arrayValue(viewer["zones"])
	if len(zones) == 0 {
		return emptyAnalytics(), nil
	}
	zone := objectValue(zones[0])
	totals := map[string]interface{}{}
	if groups := arrayValue(zone["totals"]); len(groups) > 0 {
		totals = objectValue(groups[0])
	}
	totalSum := objectValue(totals["sum"])
	totalUniq := objectValue(totals["uniq"])
	totalRequests := numberValue(totalSum["requests"])
	totalCached := numberValue(totalSum["cachedRequests"])
	timeseries := []map[string]interface{}{}
	for _, item := range arrayValue(zone["series"]) {
		group := objectValue(item)
		sum := objectValue(group["sum"])
		uniq := objectValue(group["uniq"])
		dimensions := objectValue(group["dimensions"])
		requests := numberValue(sum["requests"])
		cachedRequests := numberValue(sum["cachedRequests"])
		timeseries = append(timeseries, map[string]interface{}{
			"datetime":       stringValue(dimensions["datetime"], stringValue(dimensions["date"], "")),
			"requests":       requests,
			"bandwidth":      numberValue(sum["bytes"]),
			"cachedRequests": cachedRequests,
			"cachedBytes":    numberValue(sum["cachedBytes"]),
			"threats":        numberValue(sum["threats"]),
			"pageViews":      numberValue(sum["pageViews"]),
			"uniques":        numberValue(uniq["uniques"]),
			"cacheHitRate":   cacheHitRate(requests, cachedRequests),
		})
	}
	return map[string]interface{}{
		"requests":       totalRequests,
		"bandwidth":      numberValue(totalSum["bytes"]),
		"cachedRequests": totalCached,
		"cachedBytes":    numberValue(totalSum["cachedBytes"]),
		"threats":        numberValue(totalSum["threats"]),
		"pageViews":      numberValue(totalSum["pageViews"]),
		"uniques":        numberValue(totalUniq["uniques"]),
		"cacheHitRate":   cacheHitRate(totalRequests, totalCached),
		"timeseries":     timeseries,
	}, nil
}

type verificationResult struct {
	Valid     bool
	Status    string
	ExpiresOn interface{}
	Email     string
	Error     string
}

func (s *Service) verifyToken(ctx context.Context, apiToken, email, cfAccountID string) verificationResult {
	path := "/user/tokens/verify"
	auth := authHeaders(apiToken, email)
	if email != "" {
		path = "/user"
	}
	if cfAccountID != "" && email == "" {
		path = "/accounts/" + url.PathEscape(cfAccountID) + "/tokens/verify"
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		if cfAccountID != "" || email != "" {
			return verificationResult{Valid: false, Error: err.Error()}
		}
		accountID, accountErr := s.cloudflareAccountID(ctx, auth)
		if accountErr != nil {
			return verificationResult{Valid: false, Error: err.Error()}
		}
		payload, err = s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/tokens/verify", auth, nil)
		if err != nil {
			return verificationResult{Valid: false, Error: err.Error()}
		}
	}
	result := objectValue(payload["result"])
	userEmail := stringValue(result["email"], "")
	if userEmail == "" && email == "" && cfAccountID == "" {
		userEmail = s.cloudflareUserEmail(ctx, auth)
	}
	return verificationResult{
		Valid:     true,
		Status:    stringValue(result["status"], "active"),
		ExpiresOn: result["expires_on"],
		Email:     userEmail,
	}
}

func (s *Service) cloudflareUserEmail(ctx context.Context, auth map[string]string) string {
	payload, err := s.cfRequest(ctx, http.MethodGet, "/user", auth, nil)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stringValue(objectValue(payload["result"])["email"], ""))
}

func (s *Service) authForAccount(w http.ResponseWriter, r *http.Request, id string) (map[string]string, bool) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return nil, false
	}
	token := secure.SecureDecrypt(stringValue(account["api_token"], ""))
	if token == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "账号 Token 为空"})
		return nil, false
	}
	_ = s.touchAccount(r.Context(), id)
	return cloudflareAuthForAccount(token, account), true
}

func cloudflareAuthForAccount(token string, account map[string]interface{}) map[string]string {
	auth := authHeaders(token, stringValue(account["email"], ""))
	if id := strings.TrimSpace(stringValue(account["cf_account_id"], "")); id != "" {
		auth["__cf_account_id"] = id
	}
	return auth
}

func (s *Service) touchAccount(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE cf_accounts SET last_used = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Service) listZones(ctx context.Context, auth map[string]string, params url.Values) ([]interface{}, interface{}, error) {
	query := url.Values{}
	for key, values := range params {
		for _, value := range values {
			if value != "" {
				query.Add(key, value)
			}
		}
	}
	if query.Get("per_page") == "" {
		query.Set("per_page", "50")
	}
	path := "/zones"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		return nil, nil, err
	}
	results := arrayValue(payload["result"])
	firstInfo := payload["result_info"]
	// per_page 默认 50 会静默截断，按 result_info.total_pages 翻页拉全
	totalPages := intValue(objectValue(firstInfo)["total_pages"], 1)
	for page := 2; page <= totalPages; page++ {
		query.Set("page", strconv.Itoa(page))
		pagePayload, perr := s.cfRequest(ctx, http.MethodGet, "/zones?"+query.Encode(), auth, nil)
		if perr != nil {
			applog.Warn(ctx, "cloudflare", "failed to list zones page", "page", page, "error", perr.Error())
			break
		}
		before := len(results)
		results = append(results, arrayValue(pagePayload["result"])...)
		if len(results) == before {
			// 防御：服务端返回空页时终止，避免异常响应下死循环
			break
		}
	}
	return results, firstInfo, nil
}

func (s *Service) listDNSRecords(ctx context.Context, auth map[string]string, zoneID string, params url.Values) ([]interface{}, interface{}, error) {
	query := url.Values{}
	for key, values := range params {
		for _, value := range values {
			if value != "" {
				query.Add(key, value)
			}
		}
	}
	if query.Get("per_page") == "" {
		query.Set("per_page", "100")
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		return nil, nil, err
	}
	return arrayValue(payload["result"]), payload["result_info"], nil
}

func (s *Service) createDNSRecord(ctx context.Context, auth map[string]string, zoneID string, payload map[string]interface{}) (map[string]interface{}, error) {
	apiPayload, err := s.cfRequest(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", auth, dnsRecordBody(payload, false))
	if err != nil {
		return nil, err
	}
	return objectValue(apiPayload["result"]), nil
}

func (s *Service) getWorkerScript(ctx context.Context, auth map[string]string, accountID, scriptName string) (string, error) {
	raw, contentType, err := s.cfRawRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, "application/javascript", "", nil)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToLower(contentType), "multipart/") {
		if script := extractMultipartScript(raw, contentType); script != "" {
			return script, nil
		}
	}
	return strings.TrimSpace(string(raw)), nil
}

func (s *Service) putWorkerScript(ctx context.Context, auth map[string]string, accountID, scriptName, script string, payload map[string]interface{}) (map[string]interface{}, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	isModule := strings.Contains(script, "export default") || strings.Contains(script, "export {") || strings.Contains(script, "export async")
	meta := map[string]interface{}{
		"bindings":           arrayValue(payload["bindings"]),
		"compatibility_date": stringValue(payload["compatibility_date"], time.Now().UTC().Format("2006-01-02")),
	}
	if meta["bindings"] == nil {
		meta["bindings"] = []interface{}{}
	}
	if isModule {
		meta["main_module"] = "worker.js"
	} else {
		meta["body_part"] = "script"
	}
	metaPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="metadata"`},
		"Content-Type":        []string{"application/json"},
	})
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
		return nil, err
	}
	fieldName := "script"
	fileName := "script.js"
	contentType := "application/javascript"
	if isModule {
		fieldName = "worker.js"
		fileName = "worker.js"
		contentType = "application/javascript+module"
	}
	scriptPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileName)},
		"Content-Type":        []string{contentType},
	})
	if err != nil {
		return nil, err
	}
	if _, err := scriptPart.Write([]byte(script)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	raw, _, err := s.cfRawRequest(ctx, http.MethodPut, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, "application/json", writer.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid worker upload response")
	}
	return objectValue(decoded["result"]), nil
}

func (s *Service) listWorkerDomains(ctx context.Context, auth map[string]string, accountID, scriptName string) ([]map[string]interface{}, error) {
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/workers/domains", auth, nil)
	if err != nil {
		return nil, err
	}
	domains := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		domain := objectValue(item)
		if stringValue(domain["service"], "") != scriptName {
			continue
		}
		domains = append(domains, mapWorkerDomain(domain))
	}
	return domains, nil
}

func (s *Service) addWorkerDomain(ctx context.Context, auth map[string]string, accountID, scriptName, hostname, environment string) (map[string]interface{}, error) {
	zoneID := ""
	parts := strings.Split(hostname, ".")
	for i := 0; i < len(parts)-1; i++ {
		name := strings.Join(parts[i:], ".")
		zones, _, err := s.listZones(ctx, auth, url.Values{"name": []string{name}})
		if err != nil || len(zones) == 0 {
			continue
		}
		zoneID = stringValue(objectValue(zones[0])["id"], "")
		if zoneID != "" {
			break
		}
	}
	if zoneID == "" {
		return nil, fmt.Errorf("未找到域名 %s 对应的 Zone，请确保该域名已在 Cloudflare DNS 中托管", hostname)
	}
	payload, err := s.cfRequest(ctx, http.MethodPut, "/accounts/"+url.PathEscape(accountID)+"/workers/domains", auth, map[string]interface{}{
		"hostname":    hostname,
		"service":     scriptName,
		"environment": stringValue(environment, "production"),
		"zone_id":     zoneID,
	})
	if err != nil {
		return nil, err
	}
	return objectValue(payload["result"]), nil
}

func (s *Service) cloudflareAccountID(ctx context.Context, auth map[string]string) (string, error) {
	if id := strings.TrimSpace(auth["__cf_account_id"]); id != "" {
		return id, nil
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts?page=1&per_page=1", auth, nil)
	if err != nil {
		return "", err
	}
	accounts := arrayValue(payload["result"])
	if len(accounts) == 0 {
		return "", errors.New("未找到 Cloudflare Account ID")
	}
	id := stringValue(objectValue(accounts[0])["id"], "")
	if id == "" {
		return "", errors.New("Cloudflare Account ID 为空")
	}
	return id, nil
}

func (s *Service) cfRequest(ctx context.Context, method, path string, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	target := s.apiBase + cloudflarePath(path)
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if strings.HasPrefix(key, "__") {
			continue
		}
		req.Header.Set(key, cleanHeader(value))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	var payload map[string]interface{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("HTTP %d: invalid JSON response", res.StatusCode)
		}
	} else {
		payload = map[string]interface{}{}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || payload["success"] == false {
		return nil, cloudflareError(res.StatusCode, payload, raw)
	}
	return payload, nil
}

func (s *Service) cfRawRequest(ctx context.Context, method, path string, headers map[string]string, accept, contentType string, body io.Reader) ([]byte, string, error) {
	target := s.apiBase + cloudflarePath(path)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, "", err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		if strings.HasPrefix(key, "__") {
			continue
		}
		req.Header.Set(key, cleanHeader(value))
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	// cfRawRequest 通用请求读取上限：超过该大小的响应不再静默截断，
	// 而是返回错误，避免大文件（如 R2 对象）被截断后以 200 交付损坏数据。
	const maxCFResponseBytes = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxCFResponseBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxCFResponseBytes {
		return nil, "", fmt.Errorf("response exceeds download limit of %d bytes", maxCFResponseBytes)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var payload map[string]interface{}
		if json.Unmarshal(raw, &payload) == nil {
			return nil, "", cloudflareError(res.StatusCode, payload, raw)
		}
		return nil, "", fmt.Errorf("HTTP %d: %s", res.StatusCode, string(raw))
	}
	return raw, res.Header.Get("Content-Type"), nil
}

func cloudflareError(status int, payload map[string]interface{}, raw []byte) error {
	messages := []string{}
	for _, item := range arrayValue(payload["errors"]) {
		object := objectValue(item)
		message := stringValue(object["message"], "")
		if message != "" {
			messages = append(messages, message)
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, ", "))
	}
	if message := stringValue(payload["message"], ""); message != "" {
		return errors.New(message)
	}
	if len(raw) > 0 {
		return fmt.Errorf("HTTP %d: %s", status, string(raw))
	}
	return fmt.Errorf("HTTP %d", status)
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, id string) (map[string]interface{}, bool) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Database error"})
		return nil, false
	}
	defer db.Close()
	account, err := loadAccount(r.Context(), db, id)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Database error"
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			message = "账号不存在"
		}
		response.JSON(w, status, map[string]interface{}{"error": message})
		return nil, false
	}
	return account, true
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active FROM cf_accounts WHERE COALESCE(is_active, 1) = 1 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []map[string]interface{}{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func loadAccount(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active FROM cf_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

func loadTemplates(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, description, records, created_at, updated_at FROM cf_dns_templates ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []map[string]interface{}{}
	for rows.Next() {
		template, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

func loadTemplate(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, description, records, created_at, updated_at FROM cf_dns_templates WHERE id = ?`, id)
	return scanTemplate(row)
}

func scanTemplate(scanner accountScanner) (map[string]interface{}, error) {
	var id, name, recordsRaw string
	var description, createdAt, updatedAt sql.NullString
	if err := scanner.Scan(&id, &name, &description, &recordsRaw, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	records := []interface{}{}
	if strings.TrimSpace(recordsRaw) != "" {
		_ = json.Unmarshal([]byte(recordsRaw), &records)
	}
	return map[string]interface{}{
		"id":          id,
		"name":        name,
		"description": description.String,
		"records":     records,
		"createdAt":   createdAt.String,
		"updatedAt":   updatedAt.String,
	}, nil
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner accountScanner) (map[string]interface{}, error) {
	var id, name, apiToken string
	var email, userEmail, cfAccountID, createdAt, lastUsed sql.NullString
	var isActive sql.NullInt64
	if err := scanner.Scan(&id, &name, &apiToken, &email, &userEmail, &cfAccountID, &createdAt, &lastUsed, &isActive); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":            id,
		"name":          name,
		"api_token":     apiToken,
		"email":         email.String,
		"user_email":    userEmail.String,
		"cf_account_id": cfAccountID.String,
		"created_at":    createdAt.String,
		"last_used":     lastUsed.String,
		"is_active":     isActive.Int64,
	}, nil
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          account["id"],
		"name":        account["name"],
		"email":       account["email"],
		"userEmail":   account["user_email"],
		"cfAccountId": account["cf_account_id"],
		"createdAt":   account["created_at"],
		"lastUsed":    account["last_used"],
		"hasToken":    stringValue(account["api_token"], "") != "",
	}
}

func mapZone(zone map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          zone["id"],
		"name":        zone["name"],
		"status":      zone["status"],
		"paused":      zone["paused"],
		"type":        zone["type"],
		"nameServers": zone["name_servers"],
		"createdOn":   zone["created_on"],
		"modifiedOn":  zone["modified_on"],
	}
}

func mapDNSRecord(record map[string]interface{}, includeTimes bool) map[string]interface{} {
	mapped := map[string]interface{}{
		"id":       record["id"],
		"type":     record["type"],
		"name":     record["name"],
		"content":  record["content"],
		"proxied":  record["proxied"],
		"ttl":      record["ttl"],
		"priority": record["priority"],
	}
	if includeTimes {
		mapped["createdOn"] = record["created_on"]
		mapped["modifiedOn"] = record["modified_on"]
	}
	return mapped
}

func mapWorkerRoute(route map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":      route["id"],
		"pattern": route["pattern"],
		"script":  route["script"],
	}
}

func mapWorkerDomain(domain map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          domain["id"],
		"hostname":    domain["hostname"],
		"service":     domain["service"],
		"environment": domain["environment"],
		"zoneId":      domain["zone_id"],
		"zoneName":    domain["zone_name"],
	}
}

func mapPagesProject(project map[string]interface{}) map[string]interface{} {
	var latest interface{}
	if deployment := objectValue(project["latest_deployment"]); len(deployment) > 0 {
		stage := objectValue(deployment["latest_stage"])
		latest = map[string]interface{}{
			"id":        deployment["id"],
			"url":       deployment["url"],
			"status":    stringValue(stage["status"], "unknown"),
			"createdOn": deployment["created_on"],
		}
	}
	return map[string]interface{}{
		"name":             project["name"],
		"subdomain":        project["subdomain"],
		"domains":          arrayValue(project["domains"]),
		"createdOn":        project["created_on"],
		"productionBranch": project["production_branch"],
		"latestDeployment": latest,
	}
}

func mapPagesDeployment(deployment map[string]interface{}) map[string]interface{} {
	stage := objectValue(deployment["latest_stage"])
	return map[string]interface{}{
		"id":          deployment["id"],
		"url":         deployment["url"],
		"environment": deployment["environment"],
		"status":      stringValue(stage["status"], "unknown"),
		"createdOn":   deployment["created_on"],
		"source":      deployment["source"],
		"buildConfig": deployment["build_config"],
	}
}

func mapPagesDomain(domain map[string]interface{}) map[string]interface{} {
	validation := objectValue(domain["validation_data"])
	return map[string]interface{}{
		"id":               domain["id"],
		"name":             domain["name"],
		"status":           domain["status"],
		"validationStatus": nullableString(stringValue(validation["status"], "")),
		"createdOn":        domain["created_on"],
	}
}

func extractMultipartScript(raw []byte, contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary := params["boundary"]
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return ""
		}
		partType := strings.ToLower(part.Header.Get("Content-Type"))
		fileName := strings.ToLower(part.FileName())
		if strings.Contains(partType, "javascript") || strings.HasSuffix(fileName, ".js") {
			body, _ := io.ReadAll(io.LimitReader(part, 16<<20))
			return strings.TrimSpace(string(body))
		}
	}
}

func dnsRecordBody(payload map[string]interface{}, partial bool) map[string]interface{} {
	body := map[string]interface{}{}
	for _, key := range []string{"type", "name", "content"} {
		if value := strings.TrimSpace(stringValue(payload[key], "")); value != "" {
			body[key] = value
		}
	}
	if value, ok := payload["ttl"]; ok {
		body["ttl"] = intValue(value, 1)
	} else if !partial {
		body["ttl"] = 1
	}
	if value, ok := payload["proxied"]; ok {
		body["proxied"] = boolValue(value)
	} else if !partial {
		body["proxied"] = true
	}
	if value, ok := payload["priority"]; ok {
		body["priority"] = intValue(value, 0)
	}
	return body
}

func supportedRecordTypes() []string {
	return []string{"A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA", "PTR"}
}

func validateDNSRecord(record map[string]interface{}) []string {
	errorsOut := []string{}
	recordType := strings.ToUpper(strings.TrimSpace(stringValue(record["type"], "")))
	name := strings.TrimSpace(stringValue(record["name"], ""))
	content := strings.TrimSpace(stringValue(record["content"], ""))
	if recordType == "" {
		errorsOut = append(errorsOut, "Type is required")
	} else if !containsString(supportedRecordTypes(), recordType) {
		errorsOut = append(errorsOut, "Invalid type: "+recordType)
	}
	if name == "" {
		errorsOut = append(errorsOut, "Name is required")
	}
	if content == "" {
		errorsOut = append(errorsOut, "Content is required")
	}
	if recordType == "A" {
		parts := strings.Split(content, ".")
		if len(parts) != 4 {
			errorsOut = append(errorsOut, "Invalid IPv4 address")
		} else {
			for _, part := range parts {
				value := intValue(part, -1)
				if value < 0 || value > 255 || part == "" {
					errorsOut = append(errorsOut, "Invalid IPv4 address")
					break
				}
			}
		}
	}
	if recordType == "MX" {
		if _, ok := record["priority"]; !ok {
			errorsOut = append(errorsOut, "MX record requires priority")
		}
	}
	return errorsOut
}

func templateRecordsFromPayload(payload map[string]interface{}) ([]interface{}, bool) {
	if value, ok := payload["records"]; ok {
		records := arrayValue(value)
		if records == nil {
			return []interface{}{}, true
		}
		return records, true
	}
	recordType := strings.TrimSpace(stringValue(payload["type"], ""))
	content := strings.TrimSpace(stringValue(payload["content"], ""))
	if recordType == "" || content == "" {
		return nil, false
	}
	return []interface{}{map[string]interface{}{
		"type":     recordType,
		"name":     stringValue(payload["recordName"], "@"),
		"content":  content,
		"proxied":  payload["proxied"],
		"ttl":      payload["ttl"],
		"priority": payload["priority"],
	}}, true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func emptyAnalytics() map[string]interface{} {
	return map[string]interface{}{
		"requests":       0,
		"bandwidth":      0,
		"cachedRequests": 0,
		"cachedBytes":    0,
		"threats":        0,
		"pageViews":      0,
		"uniques":        0,
		"cacheHitRate":   0,
		"timeseries":     []interface{}{},
	}
}

func analyticsWindow(timeRange string) map[string]string {
	until := time.Now().UTC()
	if timeRange == "7d" || timeRange == "30d" {
		days := 7
		if timeRange == "30d" {
			days = 30
		}
		since := until.Add(-time.Duration(days-1) * 24 * time.Hour)
		return map[string]string{
			"since":     since.Format(time.RFC3339),
			"until":     until.Format(time.RFC3339),
			"sinceDate": since.Format("2006-01-02"),
			"untilDate": until.Format("2006-01-02"),
		}
	}
	since := until.Add(-24 * time.Hour)
	return map[string]string{
		"since":     since.Format(time.RFC3339),
		"until":     until.Format(time.RFC3339),
		"sinceDate": since.Format("2006-01-02"),
		"untilDate": until.Format("2006-01-02"),
	}
}

func numberValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
		return 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func cacheHitRate(requests, cachedRequests float64) int {
	if requests <= 0 {
		return 0
	}
	return int((cachedRequests/requests)*100 + 0.5)
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	defer r.Body.Close()
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return payload, nil
}

func authHeaders(apiToken, email string) map[string]string {
	if email != "" {
		return map[string]string{
			"X-Auth-Email": email,
			"X-Auth-Key":   apiToken,
		}
	}
	return map[string]string{"Authorization": "Bearer " + apiToken}
}

func validateCloudflareCredential(apiToken, cfAccountID string) error {
	if strings.HasPrefix(strings.TrimSpace(apiToken), "v1.0-") {
		return errors.New("Origin CA Key / Service Key 已被 Cloudflare 弃用，请改用 API Token；创建 Origin CA 证书需授予 Zone - SSL and Certificates - Edit 权限")
	}
	if strings.HasPrefix(strings.TrimSpace(apiToken), "cfat_") && strings.TrimSpace(cfAccountID) == "" {
		return errors.New("账户 API 令牌需要填写 Cloudflare Account ID")
	}
	return nil
}

func cleanHeader(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, value)
	return strings.TrimSpace(cleaned)
}

func cloudflarePath(path string) string {
	if strings.HasPrefix(path, "/client/v4") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "/client/v4" + path
}

func newAccountID() string {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("cf_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("cf_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}

func newTemplateID() string {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("tpl_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("tpl_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}

func envURL(name, fallback string) string {
	if value := cleanURL(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func cleanURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	if _, err := url.ParseRequestURI(value); err != nil {
		return ""
	}
	return value
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func boolValue(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func stringValue(value interface{}, fallback string) string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return fallback
		}
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return fallback
	default:
		return fmt.Sprint(typed)
	}
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
		return fallback
	default:
		return fallback
	}
}

func objectValue(value interface{}) map[string]interface{} {
	if object, ok := value.(map[string]interface{}); ok {
		return object
	}
	return map[string]interface{}{}
}

func arrayValue(value interface{}) []interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return typed
	case []map[string]interface{}:
		items := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	default:
		return nil
	}
}

// R2 Storage Management

func (s *Service) r2Buckets(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		resVal := objectValue(payload["result"])
		buckets := arrayValue(resVal["buckets"])
		if buckets == nil {
			buckets = []interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "buckets": buckets})
	} else if r.Method == http.MethodPost {
		var reqBody struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "桶名称必填")
			return
		}
		body := map[string]interface{}{"name": reqBody.Name}
		if reqBody.Location != "" && reqBody.Location != "auto" {
			body["location"] = reqBody.Location
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "bucket": payload["result"]})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteR2Bucket(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	_, err = s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets/"+url.PathEscape(bucketName), auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) r2Objects(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	query := url.Values{}
	if p := r.URL.Query().Get("prefix"); p != "" {
		query.Set("prefix", p)
	}
	if c := r.URL.Query().Get("cursor"); c != "" {
		query.Set("cursor", c)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		query.Set("limit", l)
	}
	if d := r.URL.Query().Get("delimiter"); d != "" {
		query.Set("delimiter", d)
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	resInfo := objectValue(payload["result_info"])
	delimited := arrayValue(resInfo["delimited"])
	if delimited == nil {
		delimited = []interface{}{}
	}
	objects := arrayValue(payload["result"])
	if objects == nil {
		objects = []interface{}{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"objects":            objects,
		"delimited_prefixes": delimited,
		"cursor":             resInfo["cursor"],
	})
}

func (s *Service) r2Metrics(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/metrics"
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result := objectValue(payload["result"])
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"standard":         result["standard"],
		"infrequentAccess": result["infrequentAccess"],
	})
}

func (s *Service) r2ObjectMutation(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	switch r.Method {
	case http.MethodDelete:
		s.deleteR2Object(w, r, accountID, bucketName, objectKey)
	case http.MethodPut:
		s.uploadR2Object(w, r, accountID, bucketName, objectKey)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteR2Object(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	_, err = s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) uploadR2Object(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if strings.Trim(objectKey, "/") == "" {
		response.Error(w, http.StatusBadRequest, "对象路径必填")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	_, _, err = s.cfRawRequest(r.Context(), http.MethodPut, path, auth, "application/json", contentType, r.Body)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"objectKey":  objectKey,
		"bucketName": bucketName,
	})
}

func (s *Service) r2ObjectDownloadInfo(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	var publicUrl interface{} = nil
	bucketPayload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets/"+url.PathEscape(bucketName), auth, nil)
	if err == nil {
		bucketInfo := objectValue(bucketPayload["result"])
		if base, ok := bucketInfo["public_url_base"].(string); ok && base != "" {
			publicUrl = base + "/" + objectKey
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"publicUrl":  publicUrl,
		"objectKey":  objectKey,
		"bucketName": bucketName,
	})
}

func (s *Service) r2ObjectDownload(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	raw, contentType, err := s.cfRawRequest(r.Context(), http.MethodGet, path, auth, "*/*", "", nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": objectFileName(objectKey)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) r2FolderDownload(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	prefix := r.URL.Query().Get("prefix")
	// 打包时每个对象全量读入内存再写入 zip，内存占用与最大单对象成正比；
	// 超过上限的对象跳过并记录，避免超大对象拖垮进程。
	const maxObjectBytes = 512 << 20
	type objectEntry struct {
		key  string
		size int64
	}
	var objects []objectEntry
	cursor := ""
	for {
		query := url.Values{"limit": {"1000"}}
		if prefix != "" {
			query.Set("prefix", prefix)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects"
		if len(query) > 0 {
			path += "?" + query.Encode()
		}
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		for _, item := range arrayValue(payload["result"]) {
			if obj, ok := item.(map[string]interface{}); ok {
				if key, ok := obj["key"].(string); ok && key != "" {
					objects = append(objects, objectEntry{key: key, size: int64(numberValue(obj["size"]))})
				}
			}
		}
		resInfo := objectValue(payload["result_info"])
		cursor, _ = resInfo["cursor"].(string)
		if cursor == "" {
			break
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].key < objects[j].key })

	folderName := bucketName
	if base := strings.TrimSuffix(prefix, "/"); base != "" {
		folderName = base
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			folderName = base[idx+1:]
		}
	}
	if folderName == "" {
		folderName = bucketName
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": folderName + ".zip"}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(w)
	defer zw.Close()
	ctx := r.Context()
	downloaded, skipped, failed := 0, 0, 0
	for _, object := range objects {
		if object.size > maxObjectBytes {
			skipped++
			applog.Warn(ctx, "cloudflare", "r2 folder download skipped oversized object", "bucket", bucketName, "key", object.key, "size_bytes", object.size, "max_bytes", maxObjectBytes)
			continue
		}
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(object.key)
		raw, _, err := s.cfRawRequest(ctx, http.MethodGet, path, auth, "*/*", "", nil)
		if err != nil {
			failed++
			applog.Warn(ctx, "cloudflare", "r2 folder download object failed", "bucket", bucketName, "key", object.key, "error", err.Error())
			continue
		}
		rel := object.key
		if prefix != "" {
			rel = strings.TrimPrefix(object.key, prefix)
		}
		// key 恰好等于 prefix 的对象是目录标记（空对象），打包无意义且会产生空名称条目。
		if rel == "" {
			skipped++
			continue
		}
		// R2 对象 key 可含任意字符（含 ../ 与 / 前缀），直接作为 zip 条目名会在
		// 解压时逃逸目标目录（zip-slip）。拒绝此类条目，跳过并记录。
		if !zipEntryNameSafe(rel) {
			skipped++
			applog.Warn(ctx, "cloudflare", "r2 folder download skipped unsafe entry name", "bucket", bucketName, "key", object.key, "entry", rel)
			continue
		}
		header := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		header.SetModTime(time.Now())
		entry, err := zw.CreateHeader(header)
		if err != nil {
			return
		}
		if _, err := entry.Write(raw); err != nil {
			return
		}
		downloaded++
	}
	if skipped > 0 || failed > 0 {
		applog.Warn(ctx, "cloudflare", "r2 folder download completed with skips", "bucket", bucketName, "prefix", prefix, "downloaded", downloaded, "skipped", skipped, "failed", failed)
	}
}

// zipEntryNameSafe 报告相对路径能否安全作为 zip 条目名：
// 拒绝绝对路径（/ 开头）与任何为 ".." 的路径段（/ 与 \ 均视为分隔符），
// 防止解压时条目逃逸目标目录。
func zipEntryNameSafe(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, segment := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return false
		}
	}
	return true
}

func (s *Service) r2ObjectPreview(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	raw, contentType, err := s.cfRawRequest(r.Context(), http.MethodGet, path, auth, "*/*", "", nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "application/octet-stream") {
		if detected := mime.TypeByExtension(strings.ToLower(pathExt(objectKey))); detected != "" {
			contentType = detected
		} else if len(raw) > 0 {
			contentType = http.DetectContentType(raw)
		} else {
			contentType = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": objectFileName(objectKey)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func pathExt(value string) string {
	name := objectFileName(value)
	index := strings.LastIndex(name, ".")
	if index < 0 {
		return ""
	}
	return name[index:]
}

func objectFileName(value string) string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return "object"
	}
	parts := strings.Split(trimmed, "/")
	name := parts[len(parts)-1]
	if name == "" {
		return "object"
	}
	return name
}

// Tunnel Management

func (s *Service) tunnels(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/cfd_tunnel?is_deleted=false&per_page=100"
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		tunnelsList := arrayValue(payload["result"])
		mapped := []map[string]interface{}{}
		for _, item := range tunnelsList {
			t := objectValue(item)
			conns := arrayValue(t["connections"])
			if conns == nil {
				conns = []interface{}{}
			}
			mapped = append(mapped, map[string]interface{}{
				"id":            t["id"],
				"name":          t["name"],
				"status":        t["status"],
				"createdAt":     t["created_at"],
				"deletedAt":     t["deleted_at"],
				"connections":   conns,
				"connsActiveAt": t["conns_active_at"],
				"connsPending":  t["conns_pending"],
				"remoteConfig":  t["remote_config"],
			})
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "tunnels": mapped})
	} else if r.Method == http.MethodPost {
		var reqBody struct {
			Name         string  `json:"name"`
			TunnelSecret *string `json:"tunnelSecret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "Tunnel 名称不能为空")
			return
		}
		var secret string
		if reqBody.TunnelSecret != nil && *reqBody.TunnelSecret != "" {
			secret = *reqBody.TunnelSecret
		} else {
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "failed to generate tunnel secret"})
				return
			}
			secret = base64.StdEncoding.EncodeToString(bytes)
		}
		body := map[string]interface{}{
			"name":          reqBody.Name,
			"tunnel_secret": secret,
			"config_src":    "cloudflare",
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":        t["id"],
				"name":      t["name"],
				"status":    t["status"],
				"createdAt": t["created_at"],
			},
		})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelMutation(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		conns := arrayValue(t["connections"])
		if conns == nil {
			conns = []interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":           t["id"],
				"name":         t["name"],
				"status":       t["status"],
				"createdAt":    t["created_at"],
				"connections":  conns,
				"remoteConfig": t["remote_config"],
			},
		})
	} else if r.Method == http.MethodDelete {
		// 删除前清理所有连接
		s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/connections", auth, nil)
		_, err = s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	} else if r.Method == http.MethodPatch {
		var reqBody struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "名称不能为空")
			return
		}
		body := map[string]interface{}{"name": reqBody.Name}
		payload, err := s.cfRequest(r.Context(), http.MethodPatch, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":   t["id"],
				"name": t["name"],
			},
		})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelConfiguration(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/configurations", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		resVal := objectValue(payload["result"])
		config := resVal["config"]
		if config == nil {
			config = map[string]interface{}{"ingress": []interface{}{}}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "config": config})
	} else if r.Method == http.MethodPut {
		var reqBody struct {
			Config map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Config == nil {
			response.Error(w, http.StatusBadRequest, "配置不能为空")
			return
		}
		body := map[string]interface{}{"config": reqBody.Config}
		payload, err := s.cfRequest(r.Context(), http.MethodPut, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/configurations", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": payload["result"]})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelToken(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/token", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "token": payload["result"]})
}

func (s *Service) tunnelConnections(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/connections", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	connectionsList := arrayValue(payload["result"])
	mapped := []map[string]interface{}{}
	for _, item := range connectionsList {
		c := objectValue(item)

		id := c["id"]
		if id == nil || id == "" {
			id = c["uuid"]
		}
		clientId := c["client_id"]
		if clientId == nil || clientId == "" {
			clientId = c["id"]
		}
		clientVersion := c["client_version"]
		if clientVersion == nil || clientVersion == "" {
			clientVersion = c["version"]
		}
		arch := c["arch"]
		if arch == nil || arch == "" {
			arch = c["platform"]
		}
		connectedAt := c["opened_at"]
		if connectedAt == nil || connectedAt == "" {
			connectedAt = c["connected_at"]
		}
		if connectedAt == nil || connectedAt == "" {
			connectedAt = c["created_at"]
		}
		originIp := c["origin_ip"]
		if originIp == nil || originIp == "" {
			originIp = c["origin"]
		}
		uuid := c["uuid"]
		if uuid == nil || uuid == "" {
			uuid = c["id"]
		}
		coloName := c["colo_name"]
		if coloName == nil || coloName == "" {
			coloName = c["colo"]
		}

		mapped = append(mapped, map[string]interface{}{
			"id":            id,
			"clientId":      clientId,
			"clientVersion": clientVersion,
			"arch":          arch,
			"connectedAt":   connectedAt,
			"originIp":      originIp,
			"uuid":          uuid,
			"coloName":      coloName,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "connections": mapped})
}
