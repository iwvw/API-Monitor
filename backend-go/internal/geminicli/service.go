package geminicli

import (
	"bufio"
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
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type Authenticator interface {
	IsAuthenticated(context.Context, *http.Request) (bool, error)
}

type Account struct {
	ID                        string  `json:"id"`
	Name                      string  `json:"name"`
	Email                     string  `json:"email"`
	ClientID                  string  `json:"client_id"`
	ClientSecret              string  `json:"client_secret"`
	RefreshToken              string  `json:"refresh_token"`
	Enable                    bool    `json:"enable"`
	Status                    string  `json:"status"`
	CreatedAt                 string  `json:"created_at"`
	LastUsed                  *string `json:"last_used"`
	ProjectID                 string  `json:"project_id"`
	CloudAICompanionProjectID string  `json:"cloudaicompanion_project_id"`
	SuccessCount              int     `json:"success_count"`
	ErrorCount                int     `json:"error_count"`
}

type LogItem struct {
	ID               int64   `json:"id"`
	AccountID        string  `json:"accountId"`
	AccountName      string  `json:"accountName"`
	Model            string  `json:"model"`
	IsBalanced       bool    `json:"isBalanced"`
	Path             string  `json:"path"`
	Method           string  `json:"method"`
	StatusCode       int     `json:"statusCode"`
	DurationMs       int     `json:"durationMs"`
	FirstTokenTimeMs *int    `json:"firstTokenTimeMs"`
	TotalTokens      int     `json:"totalTokens"`
	ClientIP         string  `json:"clientIp"`
	UserAgent        string  `json:"userAgent"`
	Timestamp        string  `json:"timestamp"`
	Detail           *string `json:"detail,omitempty"`
}

type Redirect struct {
	SourceModel string `json:"sourceModel"`
	TargetModel string `json:"targetModel"`
	CreatedAt   string `json:"createdAt"`
}

type Service struct {
	cfg              config.Config
	store            *database.Store
	auth             Authenticator
	client           *http.Client
	oauthTokenUrl    string
	userInfoUrl      string
	projectsUrl      string
	codeAssistBase   string
	tokenCache       sync.Map // accountID -> tokenCacheEntry
	projectCache     sync.Map // accountID -> string
	coolDowns        sync.Map // string (accountID:model) -> time.Time
	mu               sync.Mutex
	quotaCache       sync.Map // accountID -> quotaCacheEntry
	autoCheckRunning bool
	stopAutoCheck    chan struct{}
}

type tokenCacheEntry struct {
	token  string
	expiry int64
}

type quotaCacheEntry struct {
	buckets   []map[string]interface{}
	fetchedAt time.Time
}

func New(cfg config.Config, auth Authenticator) *Service {
	s := &Service{
		cfg:            cfg,
		store:          database.New(cfg),
		auth:           auth,
		client:         &http.Client{Timeout: 30 * time.Second},
		oauthTokenUrl:  "https://oauth2.googleapis.com/token",
		userInfoUrl:    "https://www.googleapis.com/oauth2/v2/userinfo",
		projectsUrl:    "https://cloudresourcemanager.googleapis.com/v1/projects",
		codeAssistBase: "https://cloudcode-pa.googleapis.com/v1internal",
		stopAutoCheck:  make(chan struct{}),
	}
	if override := os.Getenv("GEMINI_CLI_OAUTH_TOKEN_URL"); override != "" {
		s.oauthTokenUrl = override
	}
	if override := os.Getenv("GEMINI_CLI_USERINFO_URL"); override != "" {
		s.userInfoUrl = override
	}
	if override := os.Getenv("GEMINI_CLI_PROJECTS_URL"); override != "" {
		s.projectsUrl = override
	}
	if override := os.Getenv("GEMINI_CLI_BASE_URL"); override != "" {
		s.codeAssistBase = override
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		db.Close()
	}
	s.ensureMatrixFile()
	go s.startAutoCheckWorker()
	return s
}

func (s *Service) Close() {
	close(s.stopAutoCheck)
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
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

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS gemini_cli_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			client_id TEXT,
			client_secret TEXT,
			refresh_token TEXT,
			enable INTEGER DEFAULT 1,
			status TEXT DEFAULT 'unknown',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			project_id TEXT,
			cloudaicompanion_project_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS gemini_cli_tokens (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			access_token TEXT NOT NULL,
			expires_at INTEGER,
			project_id TEXT,
			email TEXT,
			enable INTEGER DEFAULT 1,
			FOREIGN KEY (account_id) REFERENCES gemini_cli_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS gemini_cli_settings (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gemini_cli_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id TEXT,
			model TEXT,
			is_balanced INTEGER DEFAULT 0,
			request_path TEXT,
			request_method TEXT,
			status_code INTEGER,
			duration_ms INTEGER,
			client_ip TEXT,
			user_agent TEXT,
			detail TEXT,
			first_token_time_ms INTEGER,
			total_tokens INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gemini_cli_model_redirects (
			source_model TEXT PRIMARY KEY,
			target_model TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gemini_cli_model_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			check_time INTEGER NOT NULL,
			passed_accounts TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gemini_cli_tokens_account ON gemini_cli_tokens(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gemini_cli_logs_account ON gemini_cli_logs(account_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gcli_model_checks_unique ON gemini_cli_model_checks(model_id, check_time)`,
		`CREATE INDEX IF NOT EXISTS idx_gcli_model_checks_time ON gemini_cli_model_checks(check_time)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("gemini_cli ensure schema: %w", err)
		}
	}

	// Columns check and migrations
	alterStatements := []struct {
		table, col, alter string
	}{
		{"gemini_cli_accounts", "project_id", "ALTER TABLE gemini_cli_accounts ADD COLUMN project_id TEXT"},
		{"gemini_cli_accounts", "cloudaicompanion_project_id", "ALTER TABLE gemini_cli_accounts ADD COLUMN cloudaicompanion_project_id TEXT"},
	}
	for _, ast := range alterStatements {
		exists, err := hasColumn(ctx, db, ast.table, ast.col)
		if err == nil && !exists {
			_, _ = db.ExecContext(ctx, ast.alter)
		}
	}
	return nil
}

func hasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltVal sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltVal, &pk); err == nil {
			if name == columnName {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Service) ensureMatrixFile() {
	p := filepath.Join(s.cfg.DataDir, "gemini-matrix.json")
	if _, err := os.Stat(p); err == nil {
		return
	}
	_ = os.MkdirAll(s.cfg.DataDir, 0o755)
	defaultMatrix := map[string]map[string]bool{
		"gemini-3.1-pro-preview":        {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-3-pro-preview":          {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-3-flash-preview":        {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-2.5-pro":                {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-2.5-flash":              {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-2.5-flash-lite":         {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-3.1-flash-lite-preview": {"base": false, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
		"gemini-3.1-flash-lite":         {"base": true, "maxThinking": false, "noThinking": false, "search": false, "fakeStream": false, "antiTrunc": false},
	}
	data, _ := json.MarshalIndent(defaultMatrix, "", "  ")
	_ = os.WriteFile(p, data, 0o644)
}

func (s *Service) getMatrixConfig() map[string]interface{} {
	p := filepath.Join(s.cfg.DataDir, "gemini-matrix.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]interface{}{}
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res
}

func (s *Service) saveMatrixConfig(config map[string]interface{}) error {
	p := filepath.Join(s.cfg.DataDir, "gemini-matrix.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (s *Service) ServeHTTP(w http.ResponseWriter, wRequest *http.Request) {
	path := wRequest.URL.Path
	method := wRequest.Method

	// Chat completion paths
	if method == http.MethodPost && (path == "/v1/chat/completions" || path == "/api/gemini-cli/v1/chat/completions" || path == "/api/gemini-cli/chat/completions") {
		s.proxyChatCompletions(w, wRequest)
		return
	}
	// Models paths
	if method == http.MethodGet && (path == "/v1/models" || path == "/models" || path == "/api/gemini-cli/v1/models" || path == "/api/gemini-cli/models") {
		s.proxyModels(w, wRequest)
		return
	}

	// Session Auth middleware check for all other admin routes
	ok, err := s.auth.IsAuthenticated(wRequest.Context(), wRequest)
	if err != nil || !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Unauthorized"})
		return
	}

	adminPath := strings.TrimPrefix(path, "/api/gemini-cli")
	adminPath = strings.Trim(adminPath, "/")
	parts := []string{}
	if adminPath != "" {
		parts = strings.Split(adminPath, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "stats" && method == http.MethodGet:
		s.statsRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "config" && parts[1] == "matrix" && method == http.MethodGet:
		s.getMatrixRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "config" && parts[1] == "matrix" && method == http.MethodPost:
		s.saveMatrixRoute(w, wRequest)
	case len(parts) == 1 && parts[0] == "settings" && method == http.MethodGet:
		s.getSettingsRoute(w, wRequest)
	case len(parts) == 1 && parts[0] == "settings" && method == http.MethodPost:
		s.saveSettingsRoute(w, wRequest)
	case len(parts) == 1 && parts[0] == "accounts" && method == http.MethodGet:
		s.listAccountsRoute(w, wRequest)
	case len(parts) == 1 && parts[0] == "accounts" && method == http.MethodPost:
		s.createAccountRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "refresh" && method == http.MethodPost:
		s.refreshAccountsRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "fetch-email" && method == http.MethodPost:
		s.fetchEmailRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "oauth" && parts[1] == "exchange" && method == http.MethodPost:
		s.oauthExchangeRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "accounts" && method == http.MethodPut:
		s.updateAccountRoute(w, wRequest, parts[1])
	case len(parts) == 2 && parts[0] == "accounts" && method == http.MethodDelete:
		s.deleteAccountRoute(w, wRequest, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleAccountRoute(w, wRequest, parts[1])
	case len(parts) == 1 && parts[0] == "logs" && method == http.MethodGet:
		s.getLogsRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "logs" && method == http.MethodGet:
		s.getLogDetailRoute(w, wRequest, parts[1])
	case len(parts) == 1 && parts[0] == "logs" && method == http.MethodDelete:
		s.clearLogsRoute(w, wRequest)
	case len(parts) == 1 && parts[0] == "quotas" && method == http.MethodGet:
		s.getQuotasRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "quotas" && parts[1] == "all" && method == http.MethodGet:
		s.getQuotasAllRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "status" && method == http.MethodPost:
		s.setModelStatusRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodGet:
		s.getRedirectsRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodPost:
		s.createRedirectRoute(w, wRequest)
	case len(parts) == 3 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodDelete:
		s.deleteRedirectRoute(w, wRequest, parts[2])
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "check-history" && method == http.MethodGet:
		s.getCheckHistoryRoute(w, wRequest)
	case len(parts) == 3 && parts[0] == "models" && parts[1] == "check-history" && parts[2] == "clear" && method == http.MethodPost:
		s.clearCheckHistoryRoute(w, wRequest)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "check" && method == http.MethodPost:
		s.checkAccountsRoute(w, wRequest)
	default:
		response.Error(w, http.StatusNotFound, "gemini-cli route not found")
	}
}

// ==================== REST ROUTE HANDLERS ====================

func (s *Service) statsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var totalCalls, successCalls, failCalls, totalTokens int
	var avgDuration float64
	err = db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(AVG(CASE WHEN status_code >= 200 AND status_code < 300 THEN duration_ms ELSE NULL END), 0)
		FROM gemini_cli_logs`).Scan(&totalCalls, &successCalls, &failCalls, &totalTokens, &avgDuration)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	dailyTrend, err := s.getDailyTrend(ctx, db)
	if err != nil {
		dailyTrend = []map[string]interface{}{}
	}

	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil {
		accounts = []Account{}
	}

	var onlineCount, enabledCount int
	for _, a := range accounts {
		if a.Enable {
			enabledCount++
		}
		if a.Status == "online" {
			onlineCount++
		}
	}

	successRate := "0.0"
	if totalCalls > 0 {
		successRate = fmt.Sprintf("%.1f", (float64(successCalls)/float64(totalCalls))*100.0)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_calls":   totalCalls,
		"success_calls": successCalls,
		"fail_calls":    failCalls,
		"total_tokens":  totalTokens,
		"avg_duration":  int(avgDuration + 0.5),
		"success_rate":  successRate,
		"daily_trend":   dailyTrend,
		"accounts": map[string]interface{}{
			"total":   len(accounts),
			"online":  onlineCount,
			"enabled": enabledCount,
		},
	})
}

func (s *Service) getDailyTrend(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	trendDays := 30
	now := time.Now().UTC()
	startDay := now.AddDate(0, 0, -(trendDays - 1))
	startDayStr := startDay.Format("2006-01-02")

	rows, err := db.QueryContext(ctx, `
		SELECT 
			date(created_at) as day,
			COUNT(*) as total,
			SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END) as success
		FROM gemini_cli_logs
		WHERE created_at >= ?
		GROUP BY day
		ORDER BY day ASC`, startDayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	countsMap := make(map[string]map[string]int)
	for rows.Next() {
		var day string
		var total, success int
		if err := rows.Scan(&day, &total, &success); err == nil {
			countsMap[day] = map[string]int{"total": total, "success": success}
		}
	}

	trend := []map[string]interface{}{}
	for i := 0; i < trendDays; i++ {
		d := startDay.AddDate(0, 0, i)
		dStr := d.Format("2006-01-02")
		label := d.Format("01-02")
		total := 0
		success := 0
		if val, exists := countsMap[dStr]; exists {
			total = val["total"]
			success = val["success"]
		}
		trend = append(trend, map[string]interface{}{
			"date":      label,
			"bucket":    dStr,
			"timestamp": d.UnixNano() / 1e6,
			"total":     total,
			"success":   success,
		})
	}
	return trend, nil
}

func (s *Service) getMatrixRoute(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, s.getMatrixConfig())
}

func (s *Service) saveMatrixRoute(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.saveMatrixConfig(body); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getSettingsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT key, value FROM gemini_cli_settings")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err == nil {
			settings[key] = value
		}
	}
	response.JSON(w, http.StatusOK, settings)
}

func (s *Service) saveSettingsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	for k, v := range body {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO gemini_cli_settings (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, k, v)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) listAccountsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Trigger async health status verification
	go s.verifyAccountsHealthAsync(accounts)

	response.JSON(w, http.StatusOK, accounts)
}

func (s *Service) listAccountsInternal(ctx context.Context, db *sql.DB) ([]Account, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT 
			a.id, a.name, a.email, a.client_id, a.client_secret, a.refresh_token, a.enable, a.status, a.created_at, a.last_used, a.project_id, a.cloudaicompanion_project_id,
			(SELECT COUNT(*) FROM gemini_cli_logs l WHERE l.account_id = a.id AND l.status_code >= 200 AND l.status_code < 300) as success_count,
			(SELECT COUNT(*) FROM gemini_cli_logs l WHERE l.account_id = a.id AND l.status_code >= 400) as error_count
		FROM gemini_cli_accounts a
		ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := []Account{}
	for rows.Next() {
		var a Account
		var email, clientSec, refresh, lastUsed, proj, companionProj sql.NullString
		var enableInt int
		err := rows.Scan(
			&a.ID, &a.Name, &email, &a.ClientID, &clientSec, &refresh, &enableInt, &a.Status, &a.CreatedAt, &lastUsed, &proj, &companionProj,
			&a.SuccessCount, &a.ErrorCount,
		)
		if err != nil {
			return nil, err
		}
		a.Email = email.String
		a.ClientSecret = secure.SecureDecrypt(clientSec.String)
		a.RefreshToken = secure.SecureDecrypt(refresh.String)
		a.Enable = enableInt == 1
		a.ProjectID = proj.String
		a.CloudAICompanionProjectID = companionProj.String
		if lastUsed.Valid {
			v := lastUsed.String
			a.LastUsed = &v
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (s *Service) createAccountRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var a Account
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if a.ClientID == "" || a.RefreshToken == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "client_id and refresh_token are required"})
		return
	}

	if a.ID == "" {
		a.ID = fmt.Sprintf("gcli_%s", s.randString(8))
	}
	if a.Name == "" {
		a.Name = "Unnamed Account"
	}

	encryptedClientSec, _ := secure.SecureEncrypt(a.ClientSecret)
	encryptedRefresh, _ := secure.SecureEncrypt(a.RefreshToken)

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO gemini_cli_accounts (id, name, email, client_id, client_secret, refresh_token, enable, status, created_at, project_id, cloudaicompanion_project_id)
		VALUES (?, ?, ?, ?, ?, ?, 1, 'unknown', CURRENT_TIMESTAMP, ?, ?)`,
		a.ID, a.Name, a.Email, a.ClientID, encryptedClientSec, encryptedRefresh, a.ProjectID, a.CloudAICompanionProjectID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	a.ClientSecret = "" // Sanitize
	a.RefreshToken = ""
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": a.ID, "account": a})
}

func (s *Service) updateAccountRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var exists int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM gemini_cli_accounts WHERE id = ?", id).Scan(&exists)
	if exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "Account not found"})
		return
	}

	allowedFields := []string{"name", "email", "client_id", "client_secret", "refresh_token", "enable", "status", "project_id", "cloudaicompanion_project_id"}
	fields := []string{}
	values := []interface{}{}

	for _, field := range allowedFields {
		if val, ok := updates[field]; ok {
			fields = append(fields, fmt.Sprintf("%s = ?", field))
			if field == "client_secret" || field == "refresh_token" {
				enc, _ := secure.SecureEncrypt(fmt.Sprint(val))
				values = append(values, enc)
			} else if field == "enable" {
				if b, ok := val.(bool); ok {
					if b {
						values = append(values, 1)
					} else {
						values = append(values, 0)
					}
				} else {
					values = append(values, val)
				}
			} else {
				values = append(values, val)
			}
		}
	}

	if len(fields) > 0 {
		values = append(values, id)
		query := fmt.Sprintf("UPDATE gemini_cli_accounts SET %s WHERE id = ?", strings.Join(fields, ", "))
		_, err = db.ExecContext(ctx, query, values...)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	s.tokenCache.Delete(id)
	s.projectCache.Delete(id)

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteAccountRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx, "DELETE FROM gemini_cli_accounts WHERE id = ?", id)
	_, _ = tx.ExecContext(ctx, "DELETE FROM gemini_cli_tokens WHERE account_id = ?", id)

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.tokenCache.Delete(id)
	s.projectCache.Delete(id)

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) toggleAccountRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var enableVal int
	err = db.QueryRowContext(ctx, "SELECT enable FROM gemini_cli_accounts WHERE id = ?", id).Scan(&enableVal)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "Account not found"})
		return
	}

	newStatus := 0
	if enableVal == 0 {
		newStatus = 1
	}

	_, err = db.ExecContext(ctx, "UPDATE gemini_cli_accounts SET enable = ? WHERE id = ?", newStatus, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "enable": newStatus == 1})
}

func (s *Service) refreshAccountsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		AccountID string `json:"accountId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	if body.AccountID != "" {
		tok, err := s.getAccessToken(ctx, db, body.AccountID, true)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "token": tok})
	} else {
		accounts, err := s.listAccountsInternal(ctx, db)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		refreshed := 0
		errorsList := []string{}
		for _, a := range accounts {
			if a.Enable {
				_, err := s.getAccessToken(ctx, db, a.ID, true)
				if err != nil {
					errorsList = append(errorsList, fmt.Sprintf("%s: %s", a.Name, err.Error()))
				} else {
					refreshed++
				}
			}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"refreshed": refreshed,
			"errors":    errorsList,
		})
	}
}

func (s *Service) fetchEmailRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// 1. Get access token
	params := url.Values{}
	params.Set("client_id", body.ClientID)
	params.Set("client_secret", body.ClientSecret)
	params.Set("refresh_token", body.RefreshToken)
	params.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, "POST", s.oauthTokenUrl, strings.NewReader(params.Encode()))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		response.JSON(w, resp.StatusCode, map[string]string{"error": fmt.Sprintf("OAuth error: %s", string(b))})
		return
	}

	var oauthRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&oauthRes); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 2. Discover user info email
	reqInfo, err := http.NewRequestWithContext(ctx, "GET", s.userInfoUrl, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	reqInfo.Header.Set("Authorization", "Bearer "+oauthRes.AccessToken)

	respInfo, err := s.client.Do(reqInfo)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer respInfo.Body.Close()

	var userInfo struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(respInfo.Body).Decode(&userInfo)

	response.JSON(w, http.StatusOK, map[string]string{"email": userInfo.Email})
}

func (s *Service) oauthExchangeRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		ProjectID    string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Code == "" || body.RedirectURI == "" || body.ClientID == "" || body.ClientSecret == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code, redirect_uri, client_id, or client_secret"})
		return
	}

	params := url.Values{}
	params.Set("code", body.Code)
	params.Set("client_id", body.ClientID)
	params.Set("client_secret", body.ClientSecret)
	params.Set("redirect_uri", body.RedirectURI)
	params.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", s.oauthTokenUrl, strings.NewReader(params.Encode()))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		response.JSON(w, resp.StatusCode, map[string]string{"error": fmt.Sprintf("Exchange failed: %s", string(b))})
		return
	}

	var tokenRes map[string]interface{}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = json.Unmarshal(bodyBytes, &tokenRes)

	accessToken, _ := tokenRes["access_token"].(string)
	email := ""
	projectID := body.ProjectID

	// Discover email
	reqInfo, err := http.NewRequestWithContext(ctx, "GET", s.userInfoUrl, nil)
	if err == nil {
		reqInfo.Header.Set("Authorization", "Bearer "+accessToken)
		if respInfo, err := s.client.Do(reqInfo); err == nil {
			var userInfo struct {
				Email string `json:"email"`
			}
			_ = json.NewDecoder(respInfo.Body).Decode(&userInfo)
			email = userInfo.Email
			respInfo.Body.Close()
		}
	}

	// Discover Project ID if empty
	if projectID == "" {
		reqProj, err := http.NewRequestWithContext(ctx, "GET", s.projectsUrl, nil)
		if err == nil {
			reqProj.Header.Set("Authorization", "Bearer "+accessToken)
			if respProj, err := s.client.Do(reqProj); err == nil {
				var projRes struct {
					Projects []struct {
						ProjectID string `json:"projectId"`
					} `json:"projects"`
				}
				if errScan := json.NewDecoder(respProj.Body).Decode(&projRes); errScan == nil && len(projRes.Projects) > 0 {
					projectID = projRes.Projects[0].ProjectID
				}
				respProj.Body.Close()
			}
		}
	}

	tokenRes["email"] = email
	tokenRes["project_id"] = projectID

	response.JSON(w, http.StatusOK, tokenRes)
}

func (s *Service) getLogsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	rows, err := db.QueryContext(ctx, `
		SELECT 
			l.id, l.account_id, a.name as account_name, l.model, l.is_balanced, l.request_path, l.request_method, l.status_code, l.duration_ms, l.first_token_time_ms, l.total_tokens, l.client_ip, l.user_agent, l.created_at, l.detail
		FROM gemini_cli_logs l
		LEFT JOIN gemini_cli_accounts a ON l.account_id = a.id
		ORDER BY l.created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	logs := []LogItem{}
	for rows.Next() {
		var item LogItem
		var accID, accName, detail sql.NullString
		var firstToken sql.NullInt64
		var isBalancedInt int
		err := rows.Scan(
			&item.ID, &accID, &accName, &item.Model, &isBalancedInt, &item.Path, &item.Method, &item.StatusCode, &item.DurationMs, &firstToken, &item.TotalTokens, &item.ClientIP, &item.UserAgent, &item.Timestamp, &detail,
		)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.AccountID = accID.String
		item.AccountName = accName.String
		item.IsBalanced = isBalancedInt == 1
		if firstToken.Valid {
			v := int(firstToken.Int64)
			item.FirstTokenTimeMs = &v
		}

		// Try to fallback model from detail if empty
		if item.Model == "" && detail.Valid {
			var dMap map[string]interface{}
			if err := json.Unmarshal([]byte(detail.String), &dMap); err == nil {
				if m, ok := dMap["model"].(string); ok {
					item.Model = m
				}
			}
		}

		logs = append(logs, item)
	}
	response.JSON(w, http.StatusOK, logs)
}

func (s *Service) getLogDetailRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var item LogItem
	var accID, detail sql.NullString
	var firstToken sql.NullInt64
	var isBalancedInt int
	err = db.QueryRowContext(ctx, `
		SELECT id, account_id, model, is_balanced, request_path, request_method, status_code, duration_ms, first_token_time_ms, total_tokens, client_ip, user_agent, created_at, detail
		FROM gemini_cli_logs WHERE id = ?`, id).Scan(
		&item.ID, &accID, &item.Model, &isBalancedInt, &item.Path, &item.Method, &item.StatusCode, &item.DurationMs, &firstToken, &item.TotalTokens, &item.ClientIP, &item.UserAgent, &item.Timestamp, &detail,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			response.Error(w, http.StatusNotFound, "Log not found")
			return
		}
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	item.AccountID = accID.String
	item.IsBalanced = isBalancedInt == 1
	if firstToken.Valid {
		v := int(firstToken.Int64)
		item.FirstTokenTimeMs = &v
	}
	if detail.Valid {
		v := detail.String
		item.Detail = &v
	}
	response.JSON(w, http.StatusOK, item)
}

func (s *Service) clearLogsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM gemini_cli_logs")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getQuotasRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accID := r.URL.Query().Get("accountId")
	if accID == "" {
		response.JSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var account *Account
	for i := range accounts {
		if accounts[i].ID == accID {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "Account not found"})
		return
	}

	quotas, err := s.getQuotasInternal(ctx, db, account)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, quotas)
}

func (s *Service) getQuotasInternal(ctx context.Context, db *sql.DB, account *Account) (map[string]interface{}, error) {
	accessToken, err := s.getAccessToken(ctx, db, account.ID, false)
	if err != nil {
		return nil, fmt.Errorf("auth token error: %w", err)
	}

	modelsUrl := fmt.Sprintf("%s:fetchAvailableModels", s.codeAssistBase)

	proxyVal := s.getProxySetting(ctx, db)
	transport := &http.Transport{}
	if proxyVal != "" {
		if pURL, err := url.Parse(proxyVal); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "POST", modelsUrl, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.buildUserAgent("unknown"))
	req.Header.Set("X-Goog-Api-Client", "google-genai-sdk/1.41.0 gl-node/v22.19.0")

	resp, err := client.Do(req)
	if err != nil {
		return s.getFallbackModelsQuotas(ctx, db), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return s.getFallbackModelsQuotas(ctx, db), nil
	}

	var data struct {
		Models map[string]struct {
			QuotaInfo struct {
				RemainingFraction float64 `json:"remainingFraction"`
				Remaining         float64 `json:"remaining"`
				ResetTime         string  `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return s.getFallbackModelsQuotas(ctx, db), nil
	}

	disabledModels := s.getDisabledModelsList(ctx, db)
	quotas := make(map[string]interface{})
	for modelId, mData := range data.Models {
		if modelId == "" {
			continue
		}
		rem := mData.QuotaInfo.RemainingFraction
		if rem == 0 {
			rem = mData.QuotaInfo.Remaining
		}
		if rem == 0 {
			rem = 100
		}

		enabled := true
		for _, dm := range disabledModels {
			if dm == modelId {
				enabled = false
				break
			}
		}

		quotas[modelId] = map[string]interface{}{
			"remaining": rem,
			"resetTime": mData.QuotaInfo.ResetTime,
			"enabled":   enabled,
		}
	}

	// Auto sync models to matrix config
	modelIds := []string{}
	for k := range quotas {
		modelIds = append(modelIds, k)
	}
	s.syncModelsToMatrix(modelIds, false)

	return quotas, nil
}

func (s *Service) getFallbackModelsQuotas(ctx context.Context, db *sql.DB) map[string]interface{} {
	disabledModels := s.getDisabledModelsList(ctx, db)
	fallbackModels := []string{
		"gemini-3-pro-preview",
		"gemini-3-flash-preview",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash-exp",
		"gemini-2.0-flash-thinking-exp",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
	}

	quotas := make(map[string]interface{})
	for _, m := range fallbackModels {
		enabled := true
		for _, dm := range disabledModels {
			if dm == m {
				enabled = false
				break
			}
		}
		quotas[m] = map[string]interface{}{
			"remaining": 100.0,
			"resetTime": nil,
			"enabled":   enabled,
		}
	}
	return quotas
}

func (s *Service) getQuotasAllRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	forceRefresh := r.URL.Query().Get("refresh") == "1"

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	enabledAccounts := []Account{}
	for _, a := range accounts {
		if a.Enable {
			enabledAccounts = append(enabledAccounts, a)
		}
	}

	results := []map[string]interface{}{}
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, a := range enabledAccounts {
		wg.Add(1)
		go func(acc Account) {
			defer wg.Done()
			quota, err := s.retrieveUserQuota(ctx, db, acc, forceRefresh)
			mu.Lock()
			if err != nil {
				results = append(results, map[string]interface{}{
					"accountId":   acc.ID,
					"accountName": acc.Name,
					"error":       err.Error(),
				})
			} else if quota != nil {
				results = append(results, quota)
			}
			mu.Unlock()
		}(a)
	}
	wg.Wait()

	// Sync models to matrix
	allUpstreamModels := []string{}
	for _, r := range results {
		if buckets, ok := r["buckets"].([]map[string]interface{}); ok {
			for _, b := range buckets {
				if modelID, ok := b["modelId"].(string); ok {
					allUpstreamModels = append(allUpstreamModels, modelID)
				}
			}
		}
	}
	s.syncModelsToMatrix(allUpstreamModels, true)

	response.JSON(w, http.StatusOK, results)
}

func (s *Service) retrieveUserQuota(ctx context.Context, db *sql.DB, account Account, forceRefresh bool) (map[string]interface{}, error) {
	cacheAgeLimit := 60 * time.Second
	if val, exists := s.quotaCache.Load(account.ID); exists && !forceRefresh {
		entry := val.(quotaCacheEntry)
		if time.Since(entry.fetchedAt) < cacheAgeLimit {
			return map[string]interface{}{
				"accountId":   account.ID,
				"accountName": account.Name,
				"project":     account.ProjectID,
				"buckets":     entry.buckets,
			}, nil
		}
	}

	accessToken, err := s.getAccessToken(ctx, db, account.ID, false)
	if err != nil {
		return nil, err
	}

	companionProject := account.ProjectID
	if companionProject == "" {
		companionProject = account.CloudAICompanionProjectID
	}

	// Auto discover if project empty
	if companionProject == "" {
		var dErr error
		companionProject, dErr = s.fetchGcpProjectId(ctx, db, account.ID)
		if dErr != nil {
			return nil, dErr
		}
	}

	if companionProject == "" {
		return nil, fmt.Errorf("no companion project ID found")
	}

	proxyVal := s.getProxySetting(ctx, db)
	transport := &http.Transport{}
	if proxyVal != "" {
		if pURL, err := url.Parse(proxyVal); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	reqBody, _ := json.Marshal(map[string]string{"project": companionProject})
	quotaUrl := fmt.Sprintf("%s:retrieveUserQuota", s.codeAssistBase)

	req, err := http.NewRequestWithContext(ctx, "POST", quotaUrl, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.buildUserAgent("unknown"))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var data struct {
		Buckets []struct {
			ModelID           string  `json:"modelId"`
			RemainingFraction float64 `json:"remainingFraction"`
			ResetTime         string  `json:"resetTime"`
			TokenType         string  `json:"tokenType"`
		} `json:"buckets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	// Filter vertex suffixes and aggregate by modelId
	bucketMap := make(map[string]map[string]interface{})
	for _, b := range data.Buckets {
		if b.ModelID == "" || strings.HasSuffix(b.ModelID, "_vertex") {
			continue
		}
		existing, ok := bucketMap[b.ModelID]
		fraction := b.RemainingFraction
		if !ok || fraction < existing["remainingFraction"].(float64) {
			bucketMap[b.ModelID] = map[string]interface{}{
				"modelId":           b.ModelID,
				"remainingFraction": fraction,
				"resetTime":         b.ResetTime,
				"tokenType":         b.TokenType,
			}
		}
	}

	buckets := []map[string]interface{}{}
	for _, v := range bucketMap {
		buckets = append(buckets, v)
	}

	s.quotaCache.Store(account.ID, quotaCacheEntry{
		buckets:   buckets,
		fetchedAt: time.Now(),
	})

	return map[string]interface{}{
		"accountId":   account.ID,
		"accountName": account.Name,
		"project":     companionProject,
		"buckets":     buckets,
	}, nil
}

func (s *Service) setModelStatusRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ModelID string `json:"modelId"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	disabled := s.getDisabledModelsList(ctx, db)
	newDisabled := []string{}
	if body.Enabled {
		// Remove from disabled list
		for _, m := range disabled {
			if m != body.ModelID {
				newDisabled = append(newDisabled, m)
			}
		}
	} else {
		// Add to disabled list
		newDisabled = append(disabled, body.ModelID)
	}

	marshalled, _ := json.Marshal(newDisabled)
	_, err = db.ExecContext(ctx, `
		INSERT INTO gemini_cli_settings (key, value, updated_at)
		VALUES ('disabled_models', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, string(marshalled))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "modelId": body.ModelID, "enabled": body.Enabled})
}

func (s *Service) getRedirectsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT source_model, target_model, created_at FROM gemini_cli_model_redirects ORDER BY created_at DESC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	redirects := []Redirect{}
	for rows.Next() {
		var red Redirect
		if err := rows.Scan(&red.SourceModel, &red.TargetModel, &red.CreatedAt); err == nil {
			redirects = append(redirects, red)
		}
	}
	response.JSON(w, http.StatusOK, redirects)
}

func (s *Service) createRedirectRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		SourceModel string `json:"sourceModel"`
		TargetModel string `json:"targetModel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.SourceModel == "" || body.TargetModel == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "sourceModel and targetModel required"})
		return
	}

	if body.SourceModel == body.TargetModel {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "Cannot redirect to self"})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO gemini_cli_model_redirects (source_model, target_model, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_model) DO UPDATE SET target_model = excluded.target_model`, body.SourceModel, body.TargetModel)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "sourceModel": body.SourceModel, "targetModel": body.TargetModel})
}

func (s *Service) deleteRedirectRoute(w http.ResponseWriter, r *http.Request, source string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM gemini_cli_model_redirects WHERE source_model = ?", source)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "sourceModel": source})
}

func (s *Service) getCheckHistoryRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	timesRows, err := db.QueryContext(ctx, "SELECT DISTINCT check_time FROM gemini_cli_model_checks ORDER BY check_time DESC LIMIT 10")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer timesRows.Close()

	times := []int64{}
	for timesRows.Next() {
		var t int64
		if err := timesRows.Scan(&t); err == nil {
			times = append(times, t)
		}
	}

	modelsRows, err := db.QueryContext(ctx, "SELECT DISTINCT model_id FROM gemini_cli_model_checks ORDER BY model_id")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer modelsRows.Close()

	models := []string{}
	for modelsRows.Next() {
		var m string
		if err := modelsRows.Scan(&m); err == nil {
			models = append(models, m)
		}
	}

	if len(times) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"models": []string{}, "times": []int64{}, "matrix": map[string]interface{}{}})
		return
	}

	placeholder := make([]string, len(times))
	args := make([]interface{}, len(times))
	for i, t := range times {
		placeholder[i] = "?"
		args[i] = t
	}

	query := fmt.Sprintf("SELECT model_id, status, check_time, passed_accounts, error_message FROM gemini_cli_model_checks WHERE check_time IN (%s)", strings.Join(placeholder, ","))
	checksRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer checksRows.Close()

	matrix := make(map[string]map[string]map[string]interface{})
	for _, model := range models {
		matrix[model] = make(map[string]map[string]interface{})
	}

	for checksRows.Next() {
		var modelID, status, passedAccs, errMsg string
		var checkTime int64
		var passed sql.NullString
		var errM sql.NullString
		if err := checksRows.Scan(&modelID, &status, &checkTime, &passed, &errM); err == nil {
			passedAccs = passed.String
			errMsg = errM.String

			tKey := strconv.FormatInt(checkTime, 10)
			if matrix[modelID] == nil {
				matrix[modelID] = make(map[string]map[string]interface{})
			}
			matrix[modelID][tKey] = map[string]interface{}{
				"status":         status,
				"passedAccounts": passedAccs,
				"error_log":      errMsg,
			}
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"models": models,
		"times":  times,
		"matrix": matrix,
	})
}

func (s *Service) clearCheckHistoryRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM gemini_cli_model_checks")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) checkAccountsRoute(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	result := s.runCheck()
	if result.AlreadyRunning {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "检测正在进行中",
		})
		return
	}
	if result.Error != "" {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   result.Error,
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"message":         "检测完成",
		"batchTime":       result.BatchTime,
		"totalAccounts":   result.TotalAccounts,
		"enabledAccounts": result.EnabledAccounts,
		"modelsChecked":   result.ModelsChecked,
		"attempts":        result.Attempts,
		"passedModels":    result.PassedModels,
		"failedModels":    result.FailedModels,
		"durationMs":      int(time.Since(start) / time.Millisecond),
	})
}

// ==================== OAUTH & HELPERS ====================

func (s *Service) getProxySetting(ctx context.Context, db *sql.DB) string {
	var val string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'PROXY'").Scan(&val)
	return val
}

func (s *Service) getDisabledModelsList(ctx context.Context, db *sql.DB) []string {
	var val string
	err := db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'disabled_models'").Scan(&val)
	if err != nil || val == "" {
		return []string{}
	}
	var res []string
	_ = json.Unmarshal([]byte(val), &res)
	return res
}

func (s *Service) getAccessToken(ctx context.Context, db *sql.DB, accountID string, forceRefresh bool) (string, error) {
	now := time.Now().Unix()

	// 1. Memory Cache check
	if val, exists := s.tokenCache.Load(accountID); exists && !forceRefresh {
		entry := val.(tokenCacheEntry)
		if entry.expiry > now+60 {
			return entry.token, nil
		}
	}

	// 2. DB cache check
	var dbToken, dbProject, dbEmail string
	var expiresAt int64
	err := db.QueryRowContext(ctx, "SELECT access_token, expires_at, project_id, email FROM gemini_cli_tokens WHERE account_id = ?", accountID).
		Scan(&dbToken, &expiresAt, &dbProject, &dbEmail)
	if err == nil && expiresAt > now+60 && !forceRefresh {
		s.tokenCache.Store(accountID, tokenCacheEntry{token: dbToken, expiry: expiresAt})
		return dbToken, nil
	}

	// 3. Refresh token
	var clientID, clientSecretEnc, refreshEnc, emailVal, projectIDVal sql.NullString
	err = db.QueryRowContext(ctx, "SELECT client_id, client_secret, refresh_token, email, project_id FROM gemini_cli_accounts WHERE id = ?", accountID).
		Scan(&clientID, &clientSecretEnc, &refreshEnc, &emailVal, &projectIDVal)
	if err != nil {
		return "", fmt.Errorf("account not found: %w", err)
	}

	clientSecret := secure.SecureDecrypt(clientSecretEnc.String)
	refreshToken := secure.SecureDecrypt(refreshEnc.String)

	params := url.Values{}
	params.Set("client_id", clientID.String)
	params.Set("client_secret", clientSecret)
	params.Set("refresh_token", refreshToken)
	params.Set("grant_type", "refresh_token")

	proxyVal := s.getProxySetting(ctx, db)
	transport := &http.Transport{}
	if proxyVal != "" {
		if pURL, err := url.Parse(proxyVal); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "POST", s.oauthTokenUrl, strings.NewReader(params.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token refresh HTTP status %d: %s", resp.StatusCode, string(b))
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		return "", err
	}

	newToken := tokenRes.AccessToken
	newExpiry := now + tokenRes.ExpiresIn

	// Save token in DB cache
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO gemini_cli_tokens (id, account_id, access_token, expires_at, project_id, email, enable)
		VALUES (?, ?, ?, ?, ?, ?, 1)`,
		accountID, accountID, newToken, newExpiry, projectIDVal.String, emailVal.String)
	if err != nil {
		// Log but continue
		applog.Warn(ctx, "gemini-cli", "failed to cache token to database", "account_id", accountID, "error", err.Error())
	}

	s.tokenCache.Store(accountID, tokenCacheEntry{token: newToken, expiry: newExpiry})
	return newToken, nil
}

func (s *Service) fetchGcpProjectId(ctx context.Context, db *sql.DB, accountID string) (string, error) {
	if val, ok := s.projectCache.Load(accountID); ok {
		return val.(string), nil
	}

	var account Account
	var clientSec, refresh, lastUsed, proj, companionProj sql.NullString
	var enableInt int
	err := db.QueryRowContext(ctx, `
		SELECT id, name, email, client_id, client_secret, refresh_token, enable, status, created_at, last_used, project_id, cloudaicompanion_project_id
		FROM gemini_cli_accounts WHERE id = ?`, accountID).
		Scan(&account.ID, &account.Name, &account.Email, &account.ClientID, &clientSec, &refresh, &enableInt, &account.Status, &account.CreatedAt, &lastUsed, &proj, &companionProj)
	if err != nil {
		return "", err
	}

	cachedID := proj.String
	if cachedID == "" {
		cachedID = companionProj.String
	}
	if cachedID != "" {
		s.projectCache.Store(accountID, cachedID)
		return cachedID, nil
	}

	accessToken, err := s.getAccessToken(ctx, db, accountID, false)
	if err != nil {
		return "", err
	}

	proxyVal := s.getProxySetting(ctx, db)
	transport := &http.Transport{}
	if proxyVal != "" {
		if pURL, err := url.Parse(proxyVal); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	// Option 1: cloudresourcemanager projects
	req, err := http.NewRequestWithContext(ctx, "GET", s.projectsUrl+"?filter=lifecycleState:ACTIVE", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			var projRes struct {
				Projects []struct {
					ProjectID string `json:"projectId"`
					Name      string `json:"name"`
				} `json:"projects"`
			}
			if errScan := json.NewDecoder(resp.Body).Decode(&projRes); errScan == nil && len(projRes.Projects) > 0 {
				projectId := ""
				for _, p := range projRes.Projects {
					if strings.Contains(strings.ToLower(p.ProjectID), "default") || strings.Contains(strings.ToLower(p.Name), "default") {
						projectId = p.ProjectID
						break
					}
				}
				if projectId == "" {
					projectId = projRes.Projects[0].ProjectID
				}
				if projectId != "" {
					_, _ = db.ExecContext(ctx, "UPDATE gemini_cli_accounts SET project_id = ? WHERE id = ?", projectId, accountID)
					s.projectCache.Store(accountID, projectId)
					return projectId, nil
				}
			}
		}
	}

	// Option 2: loadCodeAssist
	loadCodeAssistUrl := fmt.Sprintf("%s:loadCodeAssist", s.codeAssistBase)
	reqBody, _ := json.Marshal(map[string]interface{}{"metadata": map[string]string{"ideType": "ANTIGRAVITY"}})
	reqLoad, err := http.NewRequestWithContext(ctx, "POST", loadCodeAssistUrl, bytes.NewReader(reqBody))
	if err == nil {
		reqLoad.Header.Set("Authorization", "Bearer "+accessToken)
		reqLoad.Header.Set("Content-Type", "application/json")
		reqLoad.Header.Set("User-Agent", s.buildUserAgent("unknown"))
		if respLoad, err := client.Do(reqLoad); err == nil {
			defer respLoad.Body.Close()
			var loadData struct {
				CloudaicompanionProject string `json:"cloudaicompanionProject"`
			}
			if errScan := json.NewDecoder(respLoad.Body).Decode(&loadData); errScan == nil && loadData.CloudaicompanionProject != "" {
				companionProject := loadData.CloudaicompanionProject
				_, _ = db.ExecContext(ctx, "UPDATE gemini_cli_accounts SET cloudaicompanion_project_id = ? WHERE id = ?", companionProject, accountID)
				s.projectCache.Store(accountID, companionProject)
				return companionProject, nil
			}
		}
	}

	return "", errors.New("failed to fetch project ID")
}

func (s *Service) buildUserAgent(model string) string {
	platform := "win32"
	arch := "x64"
	return fmt.Sprintf("GeminiCLI/0.31.0/%s (%s; %s)", model, platform, arch)
}

func (s *Service) syncModelsToMatrix(upstreamModelIds []string, isFullSync bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	matrix := s.getMatrixConfig()
	updated := false

	for _, mId := range upstreamModelIds {
		if mId == "" || strings.Contains(mId, "/") || strings.Contains(mId, "-search") || strings.Contains(mId, "-thinking") {
			continue
		}
		if _, exists := matrix[mId]; !exists {
			matrix[mId] = map[string]bool{
				"base":        true,
				"maxThinking": false,
				"noThinking":  false,
				"search":      false,
				"fakeStream":  false,
				"antiTrunc":   false,
			}
			updated = true
		}
	}

	if isFullSync {
		// Clean stale models
		upstreamSet := make(map[string]bool)
		for _, m := range upstreamModelIds {
			upstreamSet[m] = true
		}
		for mKey := range matrix {
			if !upstreamSet[mKey] {
				delete(matrix, mKey)
				updated = true
			}
		}
	}

	if updated {
		_ = s.saveMatrixConfig(matrix)
	}
}

// ==================== AUTO CHECK SCHEDULE ====================

func (s *Service) startAutoCheckWorker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkAutoCheckSchedule()
		case <-s.stopAutoCheck:
			return
		}
	}
}

func (s *Service) checkAutoCheckSchedule() {
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	var enabledVal, intervalVal, lastRunVal string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'autoCheckEnabled'").Scan(&enabledVal)
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'autoCheckInterval'").Scan(&intervalVal)
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'lastAutoSyncTime'").Scan(&lastRunVal)

	enabled := enabledVal == "1" || enabledVal == "true"
	if !enabled {
		return
	}

	intervalMs, _ := strconv.ParseInt(intervalVal, 10, 64)
	if intervalMs <= 0 {
		intervalMs = 3600000 // default 1h
	}

	lastRun, _ := strconv.ParseInt(lastRunVal, 10, 64)
	now := time.Now().UnixNano() / 1e6
	if now-lastRun >= intervalMs {
		go s.runCheck()
	}
}

type checkRunResult struct {
	Started         bool
	AlreadyRunning  bool
	BatchTime       int64
	TotalAccounts   int
	EnabledAccounts int
	ModelsChecked   int
	Attempts        int
	PassedModels    int
	FailedModels    int
	Error           string
}

func (s *Service) runCheck() checkRunResult {
	s.mu.Lock()
	if s.autoCheckRunning {
		s.mu.Unlock()
		return checkRunResult{AlreadyRunning: true}
	}
	s.autoCheckRunning = true
	s.mu.Unlock()

	result := checkRunResult{Started: true}

	defer func() {
		s.mu.Lock()
		s.autoCheckRunning = false
		s.mu.Unlock()
	}()

	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer db.Close()

	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil || len(accounts) == 0 {
		if err != nil {
			result.Error = err.Error()
		}
		return result
	}
	result.TotalAccounts = len(accounts)
	type enabledAccount struct {
		index   int
		account Account
	}
	enabledAccounts := []enabledAccount{}
	for _, account := range accounts {
		if account.Enable {
			result.EnabledAccounts++
		}
	}
	for index, account := range accounts {
		if account.Enable {
			enabledAccounts = append(enabledAccounts, enabledAccount{index: index, account: account})
		}
	}

	nowMs := time.Now().UnixNano() / 1e6
	_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO gemini_cli_settings (key, value) VALUES ('lastAutoSyncTime', ?)", strconv.FormatInt(nowMs, 10))

	// Get models to check
	set := make(map[string]bool)
	redirectsRows, err := db.QueryContext(ctx, "SELECT source_model FROM gemini_cli_model_redirects")
	if err == nil {
		for redirectsRows.Next() {
			var m string
			if errScan := redirectsRows.Scan(&m); errScan == nil {
				set[m] = true
			}
		}
		redirectsRows.Close()
	}

	matrix := s.getMatrixConfig()
	for m := range matrix {
		set[m] = true
	}

	// Filter disabled check models
	var disabledCheckModelsVal string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'disabledCheckModels'").Scan(&disabledCheckModelsVal)
	disabledCheckSet := make(map[string]bool)
	if disabledCheckModelsVal != "" {
		var list []string
		if errJSON := json.Unmarshal([]byte(disabledCheckModelsVal), &list); errJSON == nil {
			for _, m := range list {
				disabledCheckSet[m] = true
			}
		}
	}

	modelsToCheck := []string{}
	for m := range set {
		if !disabledCheckSet[m] {
			modelsToCheck = append(modelsToCheck, m)
		}
	}

	// Run checks and store in DB
	batchTime := time.Now().Unix()
	result.BatchTime = batchTime
	for _, model := range modelsToCheck {
		passedAccounts := []string{}
		var lastErr error
		result.ModelsChecked++
		result.Attempts += len(enabledAccounts)

		type accountCheckResult struct {
			index int
			err   error
		}
		results := make(chan accountCheckResult, len(enabledAccounts))
		var wg sync.WaitGroup
		for _, item := range enabledAccounts {
			wg.Add(1)
			go func(item enabledAccount) {
				defer wg.Done()
				results <- accountCheckResult{
					index: item.index,
					err:   s.testAccountModel(ctx, db, item.account, model),
				}
			}(item)
		}
		wg.Wait()
		close(results)

		for check := range results {
			if check.err == nil {
				passedAccounts = append(passedAccounts, strconv.Itoa(check.index))
				continue
			}
			lastErr = check.err
		}
		sort.Strings(passedAccounts)

		status := "error"
		var errLog *string
		if len(passedAccounts) > 0 {
			status = "ok"
			result.PassedModels++
		} else {
			result.FailedModels++
		}
		if lastErr != nil {
			eStr := lastErr.Error()
			errLog = &eStr
		}

		passedStr := strings.Join(passedAccounts, ",")
		_, _ = db.ExecContext(ctx, `
			INSERT INTO gemini_cli_model_checks (model_id, status, error_message, check_time, passed_accounts)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(model_id, check_time) DO UPDATE SET
				status = excluded.status,
				error_message = excluded.error_message,
				passed_accounts = excluded.passed_accounts`,
			model, status, errLog, batchTime, passedStr)
	}

	return result
}

func (s *Service) testAccountModel(ctx context.Context, db *sql.DB, account Account, model string) error {
	// Use the same streaming path as the public OpenAI-compatible examples.
	// Some Gemini models return empty/stream-only payloads on generateContent.
	body := map[string]interface{}{
		"model":  model,
		"stream": true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "你好，请只回复 OK"},
		},
		"max_tokens": 8,
	}

	// Local run completions request using test handler
	w := httptestRecorder{header: make(http.Header)}
	reqBody, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("x-endpoint-id", account.ID)
	req.Header.Set("Authorization", "Bearer 123456") // Bypass key check by using internal request header setup

	s.proxyChatCompletions(&w, req)

	if w.code != http.StatusOK {
		return fmt.Errorf("status %d: %s", w.code, w.body.String())
	}
	return nil
}

type httptestRecorder struct {
	code   int
	header http.Header
	body   bytes.Buffer
}

func (h *httptestRecorder) Header() http.Header {
	return h.header
}

func (h *httptestRecorder) Write(b []byte) (int, error) {
	if h.code == 0 {
		h.code = http.StatusOK
	}
	return h.body.Write(b)
}

func (h *httptestRecorder) WriteHeader(statusCode int) {
	h.code = statusCode
}

// ==================== ASYNC HEALTH VERIFICATION ====================

func (s *Service) verifyAccountsHealthAsync(accounts []Account) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	for _, a := range accounts {
		if !a.Enable {
			continue
		}
		status := "online"
		_, err := s.getAccessToken(ctx, db, a.ID, true)
		if err != nil {
			status = "invalid"
		}
		_, _ = db.ExecContext(ctx, "UPDATE gemini_cli_accounts SET status = ? WHERE id = ?", status, a.ID)
	}
}

// ==================== MODELS LIST ====================

func (s *Service) CanHandleModel(ctx context.Context, model string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		return false
	}
	defer db.Close()

	var prefix string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'channelModelPrefix'").Scan(&prefix)

	stripped := model
	if prefix != "" && strings.HasPrefix(model, prefix) {
		stripped = strings.TrimPrefix(model, prefix)
	}

	// Check model matrix
	matrix := s.getMatrixConfig()
	if _, ok := matrix[stripped]; ok {
		return true
	}

	// Check redirect target
	var redirectTarget string
	err = db.QueryRowContext(ctx, "SELECT target_model FROM gemini_cli_model_redirects WHERE source_model = ?", stripped).Scan(&redirectTarget)
	if err == nil && redirectTarget != "" {
		if _, ok := matrix[redirectTarget]; ok {
			return true
		}
	}

	// Also support parsing bracket suffix
	parsed := parseSuffix(stripped)
	if parsed.hasSuffix {
		if _, ok := matrix[parsed.modelName]; ok {
			return true
		}
	}

	return false
}

func (s *Service) GetModelsList(ctx context.Context) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var prefix string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'channelModelPrefix'").Scan(&prefix)

	matrix := s.getMatrixConfig()
	disabledModels := s.getDisabledModelsList(ctx, db)

	models := []map[string]interface{}{}
	for m := range matrix {
		enabled := true
		for _, dm := range disabledModels {
			if dm == prefix+m {
				enabled = false
				break
			}
		}
		if enabled {
			models = append(models, map[string]interface{}{
				"id":       prefix + m,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "google",
			})
		}
	}
	return models, nil
}

func (s *Service) proxyModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.GetModelsList(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

// ==================== STREAM PROCESSOR & PROXY COMPLETIONS ====================

func (s *Service) proxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Verify local API Key first (optional check)
	authHeader := r.Header.Get("Authorization")
	apiKeyToken := ""
	if strings.HasPrefix(authHeader, "Bearer ") {
		apiKeyToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var configuredKey string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'API_KEY'").Scan(&configuredKey)
	// 如果数据库中没有配置 API_KEY，使用默认值 "123456"
	if configuredKey == "" {
		configuredKey = "123456"
	}

	// 如果客户端提供了 API Key，验证它
	// 注意：x-endpoint-id 是内部路由标记，跳过验证
	if r.Header.Get("x-endpoint-id") == "" && apiKeyToken != "" && apiKeyToken != configuredKey {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{
			"error": map[string]string{
				"message": "Invalid API Key. Please check your API_KEY setting in Gemini CLI settings.",
				"type":    "invalid_request_error",
				"code":    "401",
			},
		})
		return
	}

	var reqBody struct {
		Model           string                   `json:"model"`
		Messages        []map[string]interface{} `json:"messages"`
		Stream          bool                     `json:"stream"`
		Temperature     *float64                 `json:"temperature,omitempty"`
		TopP            *float64                 `json:"top_p,omitempty"`
		MaxTokens       *int                     `json:"max_tokens,omitempty"`
		ReasoningEffort *string                  `json:"reasoning_effort,omitempty"`
		Tools           []map[string]interface{} `json:"tools,omitempty"`
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var prefix string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'channelModelPrefix'").Scan(&prefix)

	model := reqBody.Model
	if prefix != "" && strings.HasPrefix(model, prefix) {
		model = strings.TrimPrefix(model, prefix)
	}

	// Redirects
	var redirectTarget string
	err = db.QueryRowContext(ctx, "SELECT target_model FROM gemini_cli_model_redirects WHERE source_model = ?", model).Scan(&redirectTarget)
	if err == nil && redirectTarget != "" {
		model = redirectTarget
	}

	modelWithPrefix := prefix + model

	// Validate against matrix config
	matrix := s.getMatrixConfig()
	parsedSuffix := parseSuffix(model)
	baseModel := parsedSuffix.modelName
	if _, ok := matrix[baseModel]; !ok {
		// Complete validation fail
		response.JSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("Model '%s' not found in Gemini CLI matrix", modelWithPrefix),
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// Check if model disabled
	disabledModels := s.getDisabledModelsList(ctx, db)
	isDisabled := false
	for _, dm := range disabledModels {
		if dm == modelWithPrefix || dm == prefix+baseModel {
			isDisabled = true
			break
		}
	}
	if isDisabled {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("Model '%s' is disabled", modelWithPrefix),
				"type":    "permission_error",
				"code":    "model_disabled",
			},
		})
		return
	}

	// Fetch enabled accounts
	accounts, err := s.listAccountsInternal(ctx, db)
	if err != nil || len(accounts) == 0 {
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{"message": "No enabled accounts available", "type": "service_unavailable"},
		})
		return
	}

	enabledAccounts := []Account{}
	for _, a := range accounts {
		if a.Enable && a.Status != "invalid" {
			enabledAccounts = append(enabledAccounts, a)
		}
	}

	// Filter by targeted ID if header supplied
	targetEndpointID := r.Header.Get("x-endpoint-id")
	if targetEndpointID != "" {
		filtered := []Account{}
		for _, a := range enabledAccounts {
			if a.ID == targetEndpointID {
				filtered = append(filtered, a)
				break
			}
		}
		enabledAccounts = filtered
	}

	// Cooldown filter
	nonCooldownAccounts := []Account{}
	for _, a := range enabledAccounts {
		key := fmt.Sprintf("%s:%s", a.ID, model)
		if tVal, exists := s.coolDowns.Load(key); exists {
			cooldownUntil := tVal.(time.Time)
			if time.Now().Before(cooldownUntil) {
				continue
			}
		}
		nonCooldownAccounts = append(nonCooldownAccounts, a)
	}

	if len(nonCooldownAccounts) == 0 {
		// Fallback to all enabled accounts if all cooled down
		nonCooldownAccounts = enabledAccounts
	}

	if len(nonCooldownAccounts) == 0 {
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{"message": "No enabled accounts available", "type": "service_unavailable"},
		})
		return
	}

	// Quota-aware sorting (accounts with remainingFraction > 0 or null first)
	quotaSortedAccounts := []Account{}
	for _, a := range nonCooldownAccounts {
		fraction := s.getAccountModelQuota(a.ID, baseModel)
		if fraction == nil || *fraction > 0 {
			quotaSortedAccounts = append(quotaSortedAccounts, a)
		}
	}
	if len(quotaSortedAccounts) == 0 {
		quotaSortedAccounts = nonCooldownAccounts
	}

	// Select account using random load-balancing
	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(quotaSortedAccounts))))
	selectedAccount := quotaSortedAccounts[idx.Int64()]

	// Update updates for client named model
	reqBody.Model = model

	// Run completions proxy
	s.executeCompletionsRequest(w, r, db, selectedAccount, reqBody, modelWithPrefix, startTime)
}

func (s *Service) getAccountModelQuota(accountID string, model string) *float64 {
	if val, ok := s.quotaCache.Load(accountID); ok {
		entry := val.(quotaCacheEntry)
		if time.Since(entry.fetchedAt) < 5*time.Minute {
			for _, b := range entry.buckets {
				if mId, _ := b["modelId"].(string); mId == model {
					if fract, ok := b["remainingFraction"].(float64); ok {
						return &fract
					}
				}
			}
		}
	}
	return nil
}

func (s *Service) executeCompletionsRequest(
	w http.ResponseWriter,
	r *http.Request,
	db *sql.DB,
	account Account,
	reqBody interface{},
	modelWithPrefix string,
	startTime time.Time,
) {
	ctx := r.Context()
	accessToken, err := s.getAccessToken(ctx, db, account.ID, false)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	projectID, err := s.fetchGcpProjectId(ctx, db, account.ID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Build payload
	rawReqBytes, _ := json.Marshal(reqBody)
	var openaiRequest map[string]interface{}
	_ = json.Unmarshal(rawReqBytes, &openaiRequest)

	geminiPayload, err := s.convertOpenAIToGemini(ctx, db, openaiRequest)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	baseModel := s.getBaseModelName(openaiRequest["model"].(string))
	isGemini3 := strings.Contains(baseModel, "gemini-3")
	isClaude := strings.Contains(strings.ToLower(baseModel), "claude")
	streamRequest, _ := openaiRequest["stream"].(bool)
	shouldUseStreamEndpoint := streamRequest || isGemini3 || isClaude

	action := "generateContent"
	if shouldUseStreamEndpoint {
		action = "streamGenerateContent"
	}

	targetUrl := fmt.Sprintf("%s:%s", s.codeAssistBase, action)
	if shouldUseStreamEndpoint {
		targetUrl += "?alt=sse"
	}

	payloadMap := map[string]interface{}{
		"model":   baseModel,
		"project": projectID,
		"request": geminiPayload,
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	proxyVal := s.getProxySetting(ctx, db)
	transport := &http.Transport{}
	if proxyVal != "" {
		if pURL, err := url.Parse(proxyVal); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "POST", targetUrl, bytes.NewReader(payloadBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.buildUserAgent(baseModel))
	req.Header.Set("X-Goog-Api-Client", "google-genai-sdk/1.41.0 gl-node/v22.19.0")
	if shouldUseStreamEndpoint {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		s.recordLogItem(ctx, db, account.ID, modelWithPrefix, r.URL.Path, r.Method, http.StatusInternalServerError, int(time.Since(startTime)/time.Millisecond), r.RemoteAddr, r.UserAgent(), nil, 0, map[string]interface{}{
			"error":    err.Error(),
			"messages": openaiRequest["messages"],
		})
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		// Cooldown logic on 429
		if resp.StatusCode == 429 {
			cooldownMs := s.parse429Cooldown(b)
			key := fmt.Sprintf("%s:%s", account.ID, openaiRequest["model"].(string))
			s.coolDowns.Store(key, time.Now().Add(time.Duration(cooldownMs)*time.Millisecond))
		}

		s.recordLogItem(ctx, db, account.ID, modelWithPrefix, r.URL.Path, r.Method, resp.StatusCode, int(time.Since(startTime)/time.Millisecond), r.RemoteAddr, r.UserAgent(), nil, 0, map[string]interface{}{
			"error":         fmt.Sprintf("HTTP status %d", resp.StatusCode),
			"response_data": string(b),
			"messages":      openaiRequest["messages"],
		})

		w.WriteHeader(resp.StatusCode)
		w.Write(b)
		return
	}

	if streamRequest {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)

		// Stream Processor
		responseID := fmt.Sprintf("chatcmpl-%s", s.randString(12))
		firstTokenTime := -1
		fullContent := ""
		fullReasoning := ""
		totalTokensCount := 0

		scanner := bufio.NewScanner(resp.Body)
		contentStarted := false
		antiTrunc := strings.Contains(openaiRequest["model"].(string), "流抗/")
		foundDone := !antiTrunc

		for scanner.Scan() {
			line := scanner.Text()
			parsedChunk := s.parseGeminiChunk(line)
			if parsedChunk == nil {
				continue
			}

			// Handle safety blocks
			if parsedChunk.Blocked != "" || parsedChunk.FinishReason == "SAFETY" || parsedChunk.FinishReason == "RECITATION" {
				delta := map[string]interface{}{}
				if firstTokenTime == -1 {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(fmt.Sprintf(`{"error":{"message":"Response blocked by safety filter: %s"}}`, parsedChunk.Blocked)))
					return
				} else {
					chunk := map[string]interface{}{
						"id":      responseID,
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   modelWithPrefix,
						"choices": []map[string]interface{}{
							{"index": 0, "delta": delta, "finish_reason": "content_filter"},
						},
					}
					chunkJSON, _ := json.Marshal(chunk)
					w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
					w.Write([]byte("data: [DONE]\n\n"))
					if ok {
						flusher.Flush()
					}
					return
				}
			}

			text := parsedChunk.Text
			reasoning := parsedChunk.Reasoning
			finishReason := parsedChunk.FinishReason

			if parsedChunk.UsageTotal > 0 {
				totalTokensCount = parsedChunk.UsageTotal
			}

			if text != "" {
				contentStarted = true
			}
			if contentStarted && reasoning != "" {
				text += reasoning
				reasoning = ""
			}

			// Nothinking flag
			if text == "" && reasoning != "" && strings.Contains(openaiRequest["model"].(string), "-nothinking") {
				text = reasoning
				reasoning = ""
			}

			// Anti truncation done check
			if antiTrunc && strings.Contains(text, "[done]") {
				foundDone = true
				text = strings.ReplaceAll(text, "[done]", "")
			}
			if finishReason != "" && finishReason != "MAX_TOKENS" {
				foundDone = true
			}

			fullContent += text
			fullReasoning += reasoning

			if firstTokenTime == -1 && (text != "" || reasoning != "") {
				firstTokenTime = int(time.Since(startTime) / time.Millisecond)
			}

			delta := make(map[string]interface{})
			if text != "" {
				delta["content"] = text
			}
			if reasoning != "" {
				delta["reasoning_content"] = reasoning
			}

			if len(delta) > 0 {
				chunk := map[string]interface{}{
					"id":      responseID,
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   modelWithPrefix,
					"choices": []map[string]interface{}{
						{"index": 0, "delta": delta, "finish_reason": nil},
					},
				}
				chunkJSON, _ := json.Marshal(chunk)
				w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
				if ok {
					flusher.Flush()
				}
			}
		}

		// Stream end chunk
		endChunk := map[string]interface{}{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   modelWithPrefix,
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
		}
		endChunkJSON, _ := json.Marshal(endChunk)
		w.Write([]byte("data: " + string(endChunkJSON) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		if ok {
			flusher.Flush()
		}

		// Record log
		s.recordLogItem(ctx, db, account.ID, modelWithPrefix, r.URL.Path, r.Method, http.StatusOK, int(time.Since(startTime)/time.Millisecond), r.RemoteAddr, r.UserAgent(), &firstTokenTime, totalTokensCount, map[string]interface{}{
			"messages": openaiRequest["messages"],
			"response": map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":              "assistant",
							"content":           fullContent,
							"reasoning_content": fullReasoning,
						},
					},
				},
				"usage": map[string]interface{}{
					"total_tokens": totalTokensCount,
				},
			},
		})

		_ = foundDone // Avoid compile warnings
	} else {
		// Non-streaming JSON response
		var geminiData struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Thought bool   `json:"thought"`
						Text    string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason *string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
			PromptFeedback struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(bodyBytes, &geminiData); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse Google response"})
			return
		}

		if len(geminiData.Candidates) == 0 {
			blockReason := geminiData.PromptFeedback.BlockReason
			if blockReason == "" {
				blockReason = "EMPTY_RESPONSE"
			}
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": "Response blocked: " + blockReason, "type": "api_error"},
			})
			return
		}

		candidate := geminiData.Candidates[0]
		if candidate.FinishReason != nil && (*candidate.FinishReason == "SAFETY" || *candidate.FinishReason == "RECITATION") {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": "Response blocked by " + *candidate.FinishReason, "type": "api_error"},
			})
			return
		}

		text := ""
		reasoning := ""
		contentStarted := false
		for _, part := range candidate.Content.Parts {
			if part.Thought && !contentStarted {
				reasoning += part.Text
			} else if part.Thought && contentStarted {
				text += part.Text
			} else if part.Text != "" {
				contentStarted = true
				text += part.Text
			}
		}

		// Fallback extraction
		if text == "" && reasoning != "" {
			if strings.Contains(openaiRequest["model"].(string), "-nothinking") {
				text = reasoning
				reasoning = ""
			} else {
				segments := strings.Split(reasoning, "\n\n")
				if len(segments) > 1 {
					extracted := []string{}
					for i := len(segments) - 1; i >= 0; i-- {
						seg := strings.TrimSpace(segments[i])
						if strings.HasPrefix(seg, "**") && strings.Contains(seg, "**\n") {
							break
						}
						if seg == "" {
							continue
						}
						extracted = append([]string{seg}, extracted...)
						segments = append(segments[:i], segments[i+1:]...)
						if i > 0 && strings.HasPrefix(strings.TrimSpace(segments[i-1]), "**") {
							break
						}
					}
					if len(extracted) > 0 {
						text = strings.Join(extracted, "\n\n")
						reasoning = strings.Join(segments, "\n\n")
					}
				}
			}
		}

		responseID := fmt.Sprintf("chatcmpl-%s", s.randString(12))
		responseData := map[string]interface{}{
			"id":      responseID,
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelWithPrefix,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":              "assistant",
						"content":           text,
						"reasoning_content": reasoning,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     geminiData.UsageMetadata.PromptTokenCount,
				"completion_tokens": geminiData.UsageMetadata.CandidatesTokenCount,
				"total_tokens":      geminiData.UsageMetadata.TotalTokenCount,
			},
		}

		s.recordLogItem(ctx, db, account.ID, modelWithPrefix, r.URL.Path, r.Method, http.StatusOK, int(time.Since(startTime)/time.Millisecond), r.RemoteAddr, r.UserAgent(), nil, geminiData.UsageMetadata.TotalTokenCount, map[string]interface{}{
			"messages": openaiRequest["messages"],
			"response": responseData,
		})

		response.JSON(w, http.StatusOK, responseData)
	}
}

func (s *Service) parse429Cooldown(body []byte) int64 {
	var errRes struct {
		Error struct {
			Message string `json:"message"`
			Details []struct {
				Type       string `json:"@type"`
				RetryDelay string `json:"retryDelay"`
				Metadata   struct {
					QuotaResetDelay string `json:"quotaResetDelay"`
				} `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &errRes)

	// Try details retryDelay
	for _, d := range errRes.Error.Details {
		if d.Type == "type.googleapis.com/google.rpc.RetryInfo" && d.RetryDelay != "" {
			if strings.HasSuffix(d.RetryDelay, "s") {
				secStr := strings.TrimSuffix(d.RetryDelay, "s")
				if sec, err := strconv.ParseFloat(secStr, 64); err == nil {
					return int64(sec * 1000)
				}
			}
		}
		if d.Type == "type.googleapis.com/google.rpc.ErrorInfo" && d.Metadata.QuotaResetDelay != "" {
			qrd := d.Metadata.QuotaResetDelay
			if strings.HasSuffix(qrd, "ms") {
				msStr := strings.TrimSuffix(qrd, "ms")
				if ms, err := strconv.ParseFloat(msStr, 64); err == nil {
					return int64(ms)
				}
			} else if strings.HasSuffix(qrd, "s") {
				secStr := strings.TrimSuffix(qrd, "s")
				if sec, err := strconv.ParseFloat(secStr, 64); err == nil {
					return int64(sec * 1000)
				}
			}
		}
	}

	// Try message "after Xs"
	re := regexp.MustCompile(`after\s+(\d+)s`)
	if match := re.FindStringSubmatch(errRes.Error.Message); len(match) == 2 {
		if sec, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			return sec * 1000
		}
	}

	return 5000 // fallback 5s
}

type parsedGeminiChunk struct {
	Text         string
	Reasoning    string
	FinishReason string
	UsageTotal   int
	Blocked      string
}

func (s *Service) parseGeminiChunk(line string) *parsedGeminiChunk {
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}
	raw := strings.TrimPrefix(line, "data: ")
	if raw == "[DONE]" || strings.TrimSpace(raw) == "" {
		return nil
	}

	var wrapper struct {
		Response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Thought bool   `json:"thought"`
						Text    string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason *string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata struct {
				TotalTokenCount int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
			PromptFeedback struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
		} `json:"response"`
	}

	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		// Try unmarshall direct candidate structure if no wrapper
		var direct struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Thought bool   `json:"thought"`
						Text    string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason *string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata struct {
				TotalTokenCount int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}
		if err2 := json.Unmarshal([]byte(raw), &direct); err2 == nil {
			wrapper.Response.Candidates = direct.Candidates
			wrapper.Response.UsageMetadata = direct.UsageMetadata
		} else {
			return nil
		}
	}

	res := &parsedGeminiChunk{}
	if wrapper.Response.PromptFeedback.BlockReason != "" {
		res.Blocked = wrapper.Response.PromptFeedback.BlockReason
		return res
	}

	if len(wrapper.Response.Candidates) > 0 {
		c := wrapper.Response.Candidates[0]
		if c.FinishReason != nil {
			res.FinishReason = *c.FinishReason
		}
		for _, part := range c.Content.Parts {
			if part.Thought {
				res.Reasoning += part.Text
			} else {
				res.Text += part.Text
			}
		}
	}

	res.UsageTotal = wrapper.Response.UsageMetadata.TotalTokenCount
	return res
}

func (s *Service) recordLogItem(
	ctx context.Context,
	db *sql.DB,
	accountId string,
	model string,
	path string,
	method string,
	statusCode int,
	durationMs int,
	clientIP string,
	userAgent string,
	firstTokenTimeMs *int,
	totalTokens int,
	detail map[string]interface{},
) {
	detailJSON, _ := json.Marshal(detail)
	var firstTokenVal interface{} = nil
	if firstTokenTimeMs != nil {
		firstTokenVal = *firstTokenTimeMs
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO gemini_cli_logs (account_id, model, is_balanced, request_path, request_method, status_code, duration_ms, client_ip, user_agent, detail, first_token_time_ms, total_tokens, created_at)
		VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		accountId, model, path, method, statusCode, durationMs, clientIP, userAgent, string(detailJSON), firstTokenVal, totalTokens)
	if err != nil {
		applog.Warn(ctx, "gemini-cli", "failed to record request log", "account_id", accountId, "error", err.Error())
	}

	// Update account last_used time
	_, _ = db.ExecContext(ctx, "UPDATE gemini_cli_accounts SET last_used = CURRENT_TIMESTAMP WHERE id = ?", accountId)
}

func (s *Service) convertOpenAIToGemini(ctx context.Context, db *sql.DB, openaiRequest map[string]interface{}) (map[string]interface{}, error) {
	messages, _ := openaiRequest["messages"].([]interface{})
	modelVal, _ := openaiRequest["model"].(string)

	contents := []map[string]interface{}{}
	systemParts := []string{}

	for _, m := range messages {
		msgMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content := msgMap["content"]

		if role == "system" || role == "developer" {
			txt := s.extractTextContent(content)
			if strings.TrimSpace(txt) != "" {
				systemParts = append(systemParts, txt)
			}
		} else {
			geminiRole := "user"
			if role == "assistant" || role == "model" {
				geminiRole = "model"
			}
			parts, err := s.convertMessageToParts(ctx, db, msgMap)
			if err != nil {
				return nil, err
			}

			// Alternate role merging
			if len(contents) > 0 && contents[len(contents)-1]["role"].(string) == geminiRole {
				prevParts := contents[len(contents)-1]["parts"].([]map[string]interface{})
				contents[len(contents)-1]["parts"] = append(prevParts, parts...)
			} else {
				contents = append(contents, map[string]interface{}{
					"role":  geminiRole,
					"parts": parts,
				})
			}
		}
	}

	// System instruction + time anchor
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	currentTimeStr := fmt.Sprintf("Current Time: %s (Beijing Time)\n\n", now.Format("2006/1/2 15:04:05"))

	var systemInstruction map[string]interface{}
	var customSystem string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'SYSTEM_INSTRUCTION'").Scan(&customSystem)

	if len(systemParts) > 0 {
		systemInstruction = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": currentTimeStr + strings.Join(systemParts, "\n\n")},
			},
		}
	} else if customSystem != "" {
		systemInstruction = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": currentTimeStr + customSystem},
			},
		}
	} else {
		systemInstruction = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": currentTimeStr},
			},
		}
	}

	// Generation config
	var defTemp, defTopP, defTopK, defMaxTok string
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'DEFAULT_TEMPERATURE'").Scan(&defTemp)
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'DEFAULT_TOP_P'").Scan(&defTopP)
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'DEFAULT_TOP_K'").Scan(&defTopK)
	_ = db.QueryRowContext(ctx, "SELECT value FROM gemini_cli_settings WHERE key = 'DEFAULT_MAX_TOKENS'").Scan(&defMaxTok)

	temp := 1.0
	if defTemp != "" {
		temp, _ = strconv.ParseFloat(defTemp, 64)
	}
	if t, ok := openaiRequest["temperature"].(float64); ok {
		temp = t
	}

	topP := 0.95
	if defTopP != "" {
		topP, _ = strconv.ParseFloat(defTopP, 64)
	}
	if p, ok := openaiRequest["top_p"].(float64); ok {
		topP = p
	}

	topK := 64
	if defTopK != "" {
		topK, _ = strconv.Atoi(defTopK)
	}

	generationConfig := map[string]interface{}{
		"temperature": temp,
		"topP":        topP,
		"topK":        topK,
	}

	maxTokens := 0
	if defMaxTok != "" {
		maxTokens, _ = strconv.Atoi(defMaxTok)
	}
	if m, ok := openaiRequest["max_tokens"].(float64); ok {
		maxTokens = int(m)
	}
	if maxTokens > 0 {
		generationConfig["maxOutputTokens"] = s.minInt(maxTokens, 65536)
	}

	// Thinking configuration
	effort := ""
	if eff, ok := openaiRequest["reasoning_effort"].(string); ok {
		effort = eff
	}
	thinkingConf := s.getThinkingConfig(ctx, db, modelVal, effort)
	if len(thinkingConf) > 0 {
		generationConfig["thinkingConfig"] = thinkingConf
		if budget, ok := thinkingConf["thinkingBudget"].(int); ok && budget > 0 {
			currentMaxOut, okMax := generationConfig["maxOutputTokens"].(int)
			if !okMax || currentMaxOut < budget+1024 {
				generationConfig["maxOutputTokens"] = s.minInt(budget+4096, 65536)
			}
		}
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if len(generationConfig) > 0 {
		payload["generationConfig"] = generationConfig
	}
	if len(systemInstruction) > 0 {
		payload["systemInstruction"] = systemInstruction
	}

	// Google search tool integration
	geminiTools := []map[string]interface{}{}
	if strings.Contains(modelVal, "-search") {
		geminiTools = append(geminiTools, map[string]interface{}{"googleSearch": map[string]interface{}{}})
	}

	// Manual tools mapping
	if tools, ok := openaiRequest["tools"].([]interface{}); ok && len(tools) > 0 {
		funcs := []map[string]interface{}{}
		for _, t := range tools {
			tMap, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			tType, _ := tMap["type"].(string)
			if tType == "function" {
				fn, _ := tMap["function"].(map[string]interface{})
				fnDecl := map[string]interface{}{
					"name":        fn["name"],
					"description": fn["description"],
				}
				if params, ok := fn["parameters"].(map[string]interface{}); ok {
					fnDecl["parametersJsonSchema"] = params
				} else {
					fnDecl["parametersJsonSchema"] = map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					}
				}
				funcs = append(funcs, fnDecl)
			}
		}
		if len(funcs) > 0 {
			geminiTools = append(geminiTools, map[string]interface{}{"functionDeclarations": funcs})
		}
	}

	if len(geminiTools) > 0 {
		payload["tools"] = geminiTools
	}

	payload["safetySettings"] = []map[string]interface{}{
		{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "BLOCK_NONE"},
	}

	return payload, nil
}

func (s *Service) extractTextContent(content interface{}) string {
	if sVal, ok := content.(string); ok {
		return sVal
	}
	if arr, ok := content.([]interface{}); ok {
		parts := []string{}
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok && m["type"] == "text" {
				parts = append(parts, fmt.Sprint(m["text"]))
			}
		}
		return strings.Join(parts, "")
	}
	return fmt.Sprint(content)
}

func (s *Service) convertMessageToParts(ctx context.Context, db *sql.DB, msg map[string]interface{}) ([]map[string]interface{}, error) {
	parts := []map[string]interface{}{}
	content := msg["content"]

	if content != nil {
		if sVal, ok := content.(string); ok {
			parts = append(parts, map[string]interface{}{"text": sVal})
		} else if arr, ok := content.([]interface{}); ok {
			proxyVal := s.getProxySetting(ctx, db)
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					iType, _ := m["type"].(string)
					if iType == "text" {
						parts = append(parts, map[string]interface{}{"text": m["text"]})
					} else if iType == "image_url" {
						if imgUrlMap, ok := m["image_url"].(map[string]interface{}); ok {
							imgURL, _ := imgUrlMap["url"].(string)
							imagePart, err := s.parseImageUrl(ctx, imgURL, proxyVal)
							if err == nil && imagePart != nil {
								parts = append(parts, imagePart)
							}
						}
					}
				}
			}
		}
	}

	if parts == nil || len(parts) == 0 {
		parts = []map[string]interface{}{{"text": ""}}
	}

	return parts, nil
}

func (s *Service) parseImageUrl(ctx context.Context, imgURL string, proxy string) (map[string]interface{}, error) {
	if imgURL == "" {
		return nil, nil
	}

	// 1. Data Base64
	if strings.HasPrefix(imgURL, "data:image/") {
		parts := strings.Split(imgURL, ";base64,")
		if len(parts) == 2 {
			mime := strings.TrimPrefix(parts[0], "data:")
			return map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": mime,
					"data":     parts[1],
				},
			}, nil
		}
	}

	// 2. Local File Uploads path
	if strings.HasPrefix(imgURL, "/uploads/") {
		cleanPath := strings.TrimPrefix(imgURL, "/")
		filePath := filepath.Join(s.cfg.DataDir, cleanPath)
		data, err := os.ReadFile(filePath)
		if err == nil {
			base64Data := base64.StdEncoding.EncodeToString(data)
			ext := strings.ToLower(filepath.Ext(filePath))
			mime := "image/jpeg"
			if ext == ".png" {
				mime = "image/png"
			} else if ext == ".webp" {
				mime = "image/webp"
			}
			return map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": mime,
					"data":     base64Data,
				},
			}, nil
		}
	}

	// 3. Remote URL -> download
	if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
		transport := &http.Transport{}
		if proxy != "" {
			if pURL, err := url.Parse(proxy); err == nil {
				transport.Proxy = http.ProxyURL(pURL)
			}
		}
		client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
		if err == nil {
			if resp, err := client.Do(req); err == nil {
				defer resp.Body.Close()
				data, errIo := io.ReadAll(resp.Body)
				if errIo == nil && resp.StatusCode == http.StatusOK {
					base64Data := base64.StdEncoding.EncodeToString(data)
					mime := resp.Header.Get("Content-Type")
					if mime == "" {
						mime = "image/jpeg"
					}
					return map[string]interface{}{
						"inlineData": map[string]interface{}{
							"mimeType": mime,
							"data":     base64Data,
						},
					}, nil
				}
			}
		}
	}

	return nil, errors.New("unsupported image URL format")
}

func (s *Service) getThinkingConfig(ctx context.Context, db *sql.DB, model, reasoningEffort string) map[string]interface{} {
	// 1. Strip prefix
	prefixes := []string{"假流/", "流抗/"}
	stripped := model
	for _, p := range prefixes {
		if strings.HasPrefix(stripped, p) {
			stripped = strings.TrimPrefix(stripped, p)
			break
		}
	}

	// 2. Strip search
	if strings.HasSuffix(stripped, "-search") {
		stripped = strings.TrimSuffix(stripped, "-search")
	}

	// 3. Parse suffix
	suffixParsed := parseSuffix(stripped)
	baseModel := suffixParsed.modelName

	// 4. Check support capability
	matrix := s.getMatrixConfig()
	support := s.getThinkingSupport(baseModel, matrix)
	if support == nil {
		return nil
	}

	mode := "none"
	budget := 0
	level := ""

	if suffixParsed.hasSuffix {
		if suffixParsed.legacySuffix != "" {
			// Legacy
			if strings.Contains(suffixParsed.legacySuffix, "-nothinking") {
				mode = "none"
			} else if strings.Contains(suffixParsed.legacySuffix, "-maxthinking") {
				if strings.Contains(baseModel, "gemini-3") {
					mode = "level"
					level = "high"
				} else if strings.Contains(baseModel, "flash") {
					mode = "budget"
					budget = 24576
				} else {
					mode = "budget"
					budget = 65536
				}
			}
		} else {
			// Bracket suffix
			val := strings.ToLower(strings.TrimSpace(suffixParsed.rawSuffix))
			if val == "none" {
				mode = "none"
			} else if val == "auto" || val == "-1" {
				mode = "auto"
				budget = -1
			} else if val == "minimal" || val == "low" || val == "medium" || val == "high" || val == "xhigh" || val == "max" {
				mode = "level"
				level = val
			} else if num, err := strconv.Atoi(val); err == nil && num >= 0 {
				if num == 0 {
					mode = "none"
				} else {
					mode = "budget"
					budget = num
				}
			}
		}
	} else if reasoningEffort != "" {
		val := strings.ToLower(strings.TrimSpace(reasoningEffort))
		if val == "none" {
			mode = "none"
		} else if val == "auto" || val == "-1" {
			mode = "auto"
			budget = -1
		} else if val == "minimal" || val == "low" || val == "medium" || val == "high" || val == "xhigh" || val == "max" {
			mode = "level"
			level = val
		} else if num, err := strconv.Atoi(val); err == nil && num > 0 {
			mode = "budget"
			budget = num
		}
	} else if !strings.Contains(model, "-nothinking") {
		// Smart defaults
		mode = "auto"
		budget = -1
	}

	// Validate & Normalize against model capability
	hasBudget := support.Min > 0 || support.Max > 0
	hasLevels := len(support.Levels) > 0

	if !hasBudget && hasLevels && mode == "budget" {
		// Convert budget to level
		lvl := s.budgetToLevel(budget)
		if lvl == "none" {
			mode = "none"
			budget = 0
			level = ""
		} else if lvl == "auto" {
			mode = "auto"
			budget = -1
			level = ""
		} else {
			mode = "level"
			level = s.clampLevel(lvl, support.Levels)
			budget = 0
		}
	}

	if hasBudget && !hasLevels && mode == "level" {
		// Convert level to budget
		budMap := map[string]int{
			"none": 0, "auto": -1, "minimal": 512, "low": 1024, "medium": 8192, "high": 24576, "xhigh": 32768, "max": 128000,
		}
		if bud, ok := budMap[strings.ToLower(level)]; ok {
			if bud == 0 {
				mode = "none"
				budget = 0
			} else if bud == -1 {
				mode = "auto"
				budget = -1
			} else {
				mode = "budget"
				budget = bud
			}
			level = ""
		}
	}

	// Clamping
	if mode == "level" && hasLevels {
		found := false
		for _, sl := range support.Levels {
			if strings.ToLower(sl) == strings.ToLower(level) {
				found = true
				break
			}
		}
		if !found {
			level = s.clampLevel(level, support.Levels)
		}
	}

	if mode == "budget" && hasBudget {
		if budget != -1 {
			if budget == 0 && !support.ZeroAllowed {
				budget = support.Min
			} else if support.Min != 0 || support.Max != 0 {
				if budget < support.Min {
					if budget == 0 && support.ZeroAllowed {
						budget = 0
					} else {
						budget = support.Min
					}
				} else if budget > support.Max {
					budget = support.Max
				}
			}
		}
	}

	// Return Gemini format
	switch mode {
	case "none":
		return nil
	case "auto":
		return map[string]interface{}{"thinkingBudget": -1, "includeThoughts": true}
	case "level":
		return map[string]interface{}{"thinkingLevel": strings.ToUpper(level), "includeThoughts": true}
	case "budget":
		return map[string]interface{}{"thinkingBudget": budget, "includeThoughts": true}
	}
	return nil
}

type ThinkingSupport struct {
	Min            int      `json:"min"`
	Max            int      `json:"max"`
	ZeroAllowed    bool     `json:"zeroAllowed"`
	DynamicAllowed bool     `json:"dynamicAllowed"`
	Levels         []string `json:"levels"`
}

func (s *Service) getThinkingSupport(baseModel string, matrixConfig map[string]interface{}) *ThinkingSupport {
	// Check matrix config
	if matrixConfig != nil {
		if mVal, exists := matrixConfig[baseModel]; exists {
			if m, ok := mVal.(map[string]interface{}); ok {
				if thinkingVal, exists := m["thinking"]; exists {
					var ts ThinkingSupport
					tb, _ := json.Marshal(thinkingVal)
					if err := json.Unmarshal(tb, &ts); err == nil {
						return &ts
					}
				}
			}
		}
	}

	// Fallback static capabilities
	staticSupport := map[string]*ThinkingSupport{
		"gemini-2.5-pro":   {Min: 128, Max: 65536, ZeroAllowed: true, DynamicAllowed: true, Levels: []string{}},
		"gemini-2.5-flash": {Min: 1, Max: 24576, ZeroAllowed: true, DynamicAllowed: true, Levels: []string{}},
	}

	if val, ok := staticSupport[baseModel]; ok {
		return val
	}

	for k, val := range staticSupport {
		if strings.HasPrefix(baseModel, k) {
			return val
		}
	}

	if strings.Contains(baseModel, "gemini-3") {
		return &ThinkingSupport{Min: 0, Max: 0, ZeroAllowed: false, DynamicAllowed: false, Levels: []string{"low", "medium", "high"}}
	}
	if strings.Contains(baseModel, "gemini-2.5") {
		return &ThinkingSupport{Min: 1, Max: 65536, ZeroAllowed: true, DynamicAllowed: true, Levels: []string{}}
	}

	return nil
}

func (s *Service) getBaseModelName(model string) string {
	prefixes := []string{"假流/", "流抗/"}
	stripped := model
	for _, p := range prefixes {
		if strings.HasPrefix(stripped, p) {
			stripped = strings.TrimPrefix(stripped, p)
			break
		}
	}

	if strings.HasSuffix(stripped, "-search") {
		stripped = strings.TrimSuffix(stripped, "-search")
	}

	suffixParsed := parseSuffix(stripped)
	return suffixParsed.modelName
}

type suffixResult struct {
	modelName    string
	hasSuffix    bool
	rawSuffix    string
	legacySuffix string
}

func parseSuffix(model string) suffixResult {
	if model == "" {
		return suffixResult{modelName: model}
	}

	// 1. Bracket suffix: model(value)
	lastOpen := strings.LastIndex(model, "(")
	if lastOpen != -1 && strings.HasSuffix(model, ")") {
		return suffixResult{
			modelName: model[:lastOpen],
			hasSuffix: true,
			rawSuffix: model[lastOpen+1 : len(model)-1],
		}
	}

	// 2. Legacy suffixes
	legacySuffixes := []string{
		"-maxthinking-search",
		"-nothinking-search",
		"-maxthinking",
		"-nothinking",
	}
	for _, sfx := range legacySuffixes {
		if strings.HasSuffix(model, sfx) {
			return suffixResult{
				modelName:    strings.TrimSuffix(model, sfx),
				hasSuffix:    true,
				legacySuffix: sfx,
			}
		}
	}

	return suffixResult{modelName: model}
}

func (s *Service) budgetToLevel(budget int) string {
	if budget == -1 {
		return "auto"
	}
	if budget == 0 {
		return "none"
	}
	if budget <= 512 {
		return "minimal"
	}
	if budget <= 1024 {
		return "low"
	}
	if budget <= 8192 {
		return "medium"
	}
	if budget <= 24576 {
		return "high"
	}
	return "xhigh"
}

func (s *Service) clampLevel(level string, supported []string) string {
	standardOrder := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	lower := strings.ToLower(level)
	for _, sl := range supported {
		if strings.ToLower(sl) == lower {
			return lower
		}
	}

	pos := -1
	for idx, val := range standardOrder {
		if val == lower {
			pos = idx
			break
		}
	}
	if pos == -1 {
		if len(supported) > 0 {
			return strings.ToLower(supported[len(supported)/2])
		}
		return "medium"
	}

	bestLevel := supported[0]
	bestDist := 99999
	for _, sl := range supported {
		idx := -1
		for innerIdx, val := range standardOrder {
			if val == strings.ToLower(sl) {
				idx = innerIdx
				break
			}
		}
		if idx != -1 {
			dist := s.absInt(pos - idx)
			if dist < bestDist {
				bestLevel = strings.ToLower(sl)
				bestDist = dist
			}
		}
	}
	return bestLevel
}

func (s *Service) randString(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}

func (s *Service) minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
