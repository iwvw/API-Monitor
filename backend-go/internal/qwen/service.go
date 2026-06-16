package qwen

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type Account struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Mobile       string    `json:"mobile"`
	Password     string    `json:"password"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refreshToken"`
	UID          string    `json:"uid"`
	Enable       bool      `json:"enable"`
	Status       string    `json:"status"`
	LastUseAt    *string   `json:"lastUseAt"`
	CreatedAt    string    `json:"createdAt"`
}

type LogItem struct {
	ID               int64   `json:"id"`
	TraceID          string  `json:"traceId"`
	AccountID        string  `json:"accountId"`
	AccountName      string  `json:"accountName"`
	Model            string  `json:"model"`
	Prompt           string  `json:"prompt"`
	Response         string  `json:"response"`
	ReasoningContent *string `json:"reasoningContent"`
	Messages         string  `json:"messages"`
	Tokens           int     `json:"tokens"`
	Status           string  `json:"status"`
	Error            *string `json:"error"`
	Duration         int     `json:"duration"`
	FirstTokenTimeMs *int    `json:"firstTokenTimeMs"`
	CreatedAt        string  `json:"createdAt"`
}

type Redirect struct {
	SourceModel string `json:"sourceModel"`
	TargetModel string `json:"targetModel"`
	CreatedAt   string `json:"createdAt"`
}

type Service struct {
	cfg        config.Config
	store      *database.Store
	client     *http.Client
	apiBaseURL string
}

func New(cfg config.Config) *Service {
	baseURL := os.Getenv("QWEN_API_BASE_URL")
	if baseURL == "" {
		baseURL = "https://chat.qwen.ai"
	}
	s := &Service{
		cfg:        cfg,
		store:      database.New(cfg),
		client:     &http.Client{Timeout: 30 * time.Second},
		apiBaseURL: baseURL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		db.Close()
	}
	return s
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
		`CREATE TABLE IF NOT EXISTS qwen_accounts (
			id TEXT PRIMARY KEY,
			name TEXT,
			email TEXT,
			mobile TEXT,
			password TEXT,
			token TEXT,
			refresh_token TEXT,
			uid TEXT,
			enable INTEGER DEFAULT 1,
			status TEXT DEFAULT 'unknown',
			last_use_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS qwen_settings (
			key TEXT PRIMARY KEY,
			value TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS qwen_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trace_id TEXT,
			account_id TEXT,
			model TEXT,
			prompt TEXT,
			response TEXT,
			reasoning_content TEXT,
			messages TEXT,
			tokens INTEGER,
			status TEXT,
			error TEXT,
			duration INTEGER,
			first_token_time_ms INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS qwen_model_redirects (
			source_model TEXT PRIMARY KEY,
			target_model TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("qwen ensure schema: %w", err)
		}
	}

	// Migrations: check and add missing columns
	alterStatements := []struct {
		table, col, alter string
	}{
		{"qwen_logs", "messages", "ALTER TABLE qwen_logs ADD COLUMN messages TEXT"},
		{"qwen_logs", "first_token_time_ms", "ALTER TABLE qwen_logs ADD COLUMN first_token_time_ms INTEGER"},
		{"qwen_logs", "reasoning_content", "ALTER TABLE qwen_logs ADD COLUMN reasoning_content TEXT"},
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

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// Completions and Image generation paths
	if method == http.MethodPost && (path == "/v1/chat/completions" || path == "/api/qwen/v1/chat/completions") {
		s.proxyChatCompletions(w, r)
		return
	}
	if method == http.MethodPost && (path == "/v1/images/generations" || path == "/api/qwen/v1/images/generations") {
		s.proxyImagesGenerations(w, r)
		return
	}

	adminPath := strings.TrimPrefix(path, "/api/qwen")
	adminPath = strings.Trim(adminPath, "/")
	parts := []string{}
	if adminPath != "" {
		parts = strings.Split(adminPath, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "stats" && method == http.MethodGet:
		s.getStats(w, r)
	case len(parts) == 1 && parts[0] == "matrix" && method == http.MethodGet:
		s.getMatrix(w, r)
	case len(parts) == 2 && parts[0] == "matrix" && method == http.MethodPut:
		s.updateMatrixItem(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "sync-models" && method == http.MethodPost:
		s.syncModels(w, r)
	case len(parts) == 1 && parts[0] == "accounts" && method == http.MethodGet:
		s.listAccounts(w, r)
	case len(parts) == 1 && parts[0] == "accounts" && method == http.MethodPost:
		s.createAccount(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && method == http.MethodDelete:
		s.deleteAccount(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "settings" && method == http.MethodGet:
		s.getSettings(w, r)
	case len(parts) == 1 && parts[0] == "settings" && method == http.MethodPost:
		s.saveSettings(w, r)
	case len(parts) == 1 && parts[0] == "logs" && method == http.MethodGet:
		s.getLogs(w, r)
	case len(parts) == 1 && parts[0] == "logs" && method == http.MethodDelete:
		s.clearLogs(w, r)
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodGet:
		s.getRedirects(w, r)
	case len(parts) == 2 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodPost:
		s.createRedirect(w, r)
	case len(parts) == 3 && parts[0] == "models" && parts[1] == "redirects" && method == http.MethodDelete:
		s.deleteRedirect(w, r, parts[2])
	default:
		response.Error(w, http.StatusNotFound, "qwen admin route not found")
	}
}

func (s *Service) getStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var totalCalls, successCalls, totalTokens int
	var avgDuration float64
	err = db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(tokens), 0),
			COALESCE(AVG(duration), 0)
		FROM qwen_logs`).Scan(&totalCalls, &successCalls, &totalTokens, &avgDuration)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_calls":   totalCalls,
		"success_calls": successCalls,
		"total_tokens":  totalTokens,
		"avg_duration":  avgDuration,
	})
}

func (s *Service) getMatrix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'QWEN_MATRIX'").Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			response.JSON(w, http.StatusOK, map[string]interface{}{})
			return
		}
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal([]byte(value), &parsed)
	response.JSON(w, http.StatusOK, parsed)
}

func (s *Service) updateMatrixItem(w http.ResponseWriter, r *http.Request, id string) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'QWEN_MATRIX'").Scan(&value)
	matrix := make(map[string]interface{})
	if err == nil {
		_ = json.Unmarshal([]byte(value), &matrix)
	}

	matrix[id] = req
	updatedBytes, _ := json.Marshal(matrix)

	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO qwen_settings (key, value) VALUES ('QWEN_MATRIX', ?)", string(updatedBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, matrix)
}

func (s *Service) syncModels(w http.ResponseWriter, r *http.Request) {
	// Dummy sync response
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, email, mobile, password, token, refresh_token, uid, enable, status, last_use_at, created_at FROM qwen_accounts")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	accounts := []Account{}
	for rows.Next() {
		var acc Account
		var name, email, mobile, password, token, refresh, uid, status sql.NullString
		var created, lastUsed sql.NullString
		var enabledVal int

		err := rows.Scan(&acc.ID, &name, &email, &mobile, &password, &token, &refresh, &uid, &enabledVal, &status, &lastUsed, &created)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		acc.Name = name.String
		acc.Email = email.String
		acc.Mobile = mobile.String
		acc.Password = secure.SecureDecrypt(password.String)
		acc.Token = secure.SecureDecrypt(token.String)
		acc.RefreshToken = refresh.String
		acc.UID = uid.String
		acc.Enable = enabledVal == 1
		acc.Status = status.String
		acc.CreatedAt = created.String
		if lastUsed.Valid {
			v := lastUsed.String
			acc.LastUseAt = &v
		}

		accounts = append(accounts, acc)
	}

	response.JSON(w, http.StatusOK, accounts)

	// Asynchronously trigger status check
	go func(accountsToCheck []Account) {
		for _, acc := range accountsToCheck {
			stat := s.checkAccountStatusRaw(acc.Token)
			if stat != acc.Status {
				if dbConn, dbErr := s.open(context.Background()); dbErr == nil {
					defer dbConn.Close()
					_, _ = dbConn.Exec("UPDATE qwen_accounts SET status = ? WHERE id = ?", stat, acc.ID)
				}
			}
		}
	}(accounts)
}

func (s *Service) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Email        string `json:"email"`
		Mobile       string `json:"mobile"`
		Password     string `json:"password"`
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
		UID          string `json:"uid"`
		Enable       bool   `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	name := req.Name
	bearer := s.extractBearerToken(req.Token)
	if name == "" && bearer != "" {
		parts := strings.Split(bearer, ".")
		if len(parts) >= 2 {
			if decodedBytes, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var jwtPayload map[string]interface{}
				if err := json.Unmarshal(decodedBytes, &jwtPayload); err == nil {
					for _, k := range []string{"nickname", "username", "email", "sub", "userId"} {
						if val, ok := jwtPayload[k].(string); ok && val != "" {
							name = val
							break
						}
					}
				}
			}
		}
	}
	if name == "" {
		name = "未命名凭证"
	}

	id := fmt.Sprintf("qw_%d_%s", time.Now().UnixNano(), s.randString(9))
	encryptedPass, _ := secure.SecureEncrypt(req.Password)
	encryptedToken, _ := secure.SecureEncrypt(req.Token)
	createdAt := time.Now().Format(time.RFC3339)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	enableVal := 0
	if req.Enable {
		enableVal = 1
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO qwen_accounts (id, name, email, mobile, password, token, refresh_token, uid, enable, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, req.Email, req.Mobile, encryptedPass, encryptedToken, req.RefreshToken, req.UID, enableVal, "unknown", createdAt)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM qwen_accounts WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var apiKey, systemInstruction string
	_ = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'API_KEY'").Scan(&apiKey)
	_ = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'SYSTEM_INSTRUCTION'").Scan(&systemInstruction)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"API_KEY":            apiKey,
		"SYSTEM_INSTRUCTION": systemInstruction,
	})
}

func (s *Service) saveSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey            string `json:"API_KEY"`
		SystemInstruction string `json:"SYSTEM_INSTRUCTION"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO qwen_settings (key, value) VALUES ('API_KEY', ?)", req.APIKey)
	_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO qwen_settings (key, value) VALUES ('SYSTEM_INSTRUCTION', ?)", req.SystemInstruction)

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT l.id, l.trace_id, l.account_id, COALESCE(a.name, ''), l.model, l.prompt, l.response, l.reasoning_content, l.messages, l.tokens, l.status, l.error, l.duration, l.first_token_time_ms, l.created_at
		FROM qwen_logs l
		LEFT JOIN qwen_accounts a ON l.account_id = a.id
		ORDER BY l.created_at DESC
		LIMIT 200`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	logs := []LogItem{}
	for rows.Next() {
		var it LogItem
		var reasoning, errStr sql.NullString
		var firstToken sql.NullInt64
		var created sql.NullString

		err := rows.Scan(&it.ID, &it.TraceID, &it.AccountID, &it.AccountName, &it.Model, &it.Prompt, &it.Response, &reasoning, &it.Messages, &it.Tokens, &it.Status, &errStr, &it.Duration, &firstToken, &created)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		it.CreatedAt = created.String
		if reasoning.Valid {
			v := reasoning.String
			it.ReasoningContent = &v
		}
		if errStr.Valid {
			v := errStr.String
			it.Error = &v
		}
		if firstToken.Valid {
			v := int(firstToken.Int64)
			it.FirstTokenTimeMs = &v
		}

		logs = append(logs, it)
	}

	response.JSON(w, http.StatusOK, logs)
}

func (s *Service) clearLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM qwen_logs")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getRedirects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT source_model, target_model, created_at FROM qwen_model_redirects")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	redirects := []Redirect{}
	for rows.Next() {
		var rd Redirect
		if err := rows.Scan(&rd.SourceModel, &rd.TargetModel, &rd.CreatedAt); err == nil {
			redirects = append(redirects, rd)
		}
	}

	response.JSON(w, http.StatusOK, redirects)
}

func (s *Service) createRedirect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceModel string `json:"sourceModel"`
		TargetModel string `json:"targetModel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.SourceModel == "" || req.TargetModel == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "Missing parameters"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO qwen_model_redirects (source_model, target_model, created_at) VALUES (?, ?, ?)",
		req.SourceModel, req.TargetModel, time.Now().Format(time.RFC3339))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteRedirect(w http.ResponseWriter, r *http.Request, source string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM qwen_model_redirects WHERE source_model = ?", source)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) proxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	ts := startTime.Unix()

	var reqBody struct {
		Model    string                   `json:"model"`
		Messages []map[string]interface{} `json:"messages"`
		Stream   bool                     `json:"stream"`
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

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// Apply redirects
	targetModel := reqBody.Model
	var redirectTarget string
	err = db.QueryRowContext(ctx, "SELECT target_model FROM qwen_model_redirects WHERE source_model = ?", targetModel).Scan(&redirectTarget)
	if err == nil && redirectTarget != "" {
		targetModel = redirectTarget
	}

	// Build context prompt
	var contextPrompt string
	if len(reqBody.Messages) > 1 {
		parts := []string{}
		for _, msg := range reqBody.Messages[:len(reqBody.Messages)-1] {
			roleName := "User"
			if msg["role"] == "assistant" {
				roleName = "Assistant"
			}
			contentVal, _ := msg["content"].(string)
			parts = append(parts, fmt.Sprintf("%s: %s", roleName, contentVal))
		}
		contextPrompt = strings.Join(parts, "\n\n") + "\n\n---\n\n"
	}

	lastMsg := reqBody.Messages[len(reqBody.Messages)-1]
	currentContent, _ := lastMsg["content"].(string)
	finalContent := contextPrompt + currentContent

	// Intent recognition for image generation
	isImageRequest := strings.Contains(currentContent, "画") || strings.Contains(currentContent, "生成图片") || strings.Contains(strings.ToLower(currentContent), "image")
	subType := "t2t"
	if isImageRequest {
		subType = "t2i"
	}

	var featureConfig map[string]interface{}
	if isImageRequest {
		featureConfig = map[string]interface{}{
			"thinking_enabled":     false,
			"output_schema":        "phase",
			"auto_thinking":        false,
			"thinking_mode":        "off",
			"auto_search":          false,
			"code_interpreter":     false,
			"function_calling":     false,
			"plugins_enabled":      true,
			"image_generation":     true,
			"default_aspect_ratio": "16:9",
		}
	} else {
		featureConfig = map[string]interface{}{
			"thinking_enabled":     true,
			"output_schema":        "phase",
			"research_mode":        "normal",
			"auto_thinking":        true,
			"thinking_mode":        "Auto",
			"thinking_format":      "summary",
			"auto_search":          false,
			"code_interpreter":     false,
			"function_calling":     false,
			"plugins_enabled":      true,
			"image_generation":     false,
			"default_aspect_ratio": "1:1",
		}
	}

	extraMeta := map[string]interface{}{
		"subChatType": subType,
	}
	if isImageRequest {
		extraMeta["mode"] = "image_generation"
		extraMeta["aspectRatio"] = "16:9"
	}

	qwenMsg := map[string]interface{}{
		"fid":            s.randUUID(),
		"parentId":       nil,
		"childrenIds":    []string{s.randUUID()},
		"role":           "user",
		"content":        finalContent,
		"user_action":    "chat",
		"timestamp":      ts,
		"models":         []string{targetModel},
		"chat_type":      "t2t",
		"feature_config": featureConfig,
		"extra": map[string]interface{}{
			"meta": extraMeta,
		},
		"sub_chat_type": subType,
		"parent_id":     nil,
	}

	// Select random enabled, non-invalid account
	rows, err := db.QueryContext(ctx, "SELECT id, name, token, status FROM qwen_accounts WHERE enable = 1 AND status != 'invalid'")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type accountItem struct {
		id, name, token, status string
	}
	accounts := []accountItem{}
	for rows.Next() {
		var acc accountItem
		if err := rows.Scan(&acc.id, &acc.name, &acc.token, &acc.status); err == nil {
			acc.token = secure.SecureDecrypt(acc.token)
			accounts = append(accounts, acc)
		}
	}

	if len(accounts) == 0 {
		response.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "No valid accounts"})
		return
	}

	nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(len(accounts))))
	selectedAccount := accounts[nBig.Int64()]
	bearerToken := s.extractBearerToken(selectedAccount.token)

	commonHeaders := map[string]string{
		"Authorization":  "Bearer " + bearerToken,
		"Content-Type":   "application/json",
		"User-Agent":     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Referer":        s.apiBaseURL + "/",
		"X-Request-With": "XMLHttpRequest",
	}

	// 1. Create chat session
	createPayload := map[string]interface{}{
		"title":     fmt.Sprintf("chat_%d", ts),
		"models":    []string{targetModel},
		"chat_mode": "normal",
		"chat_type": subType,
		"timestamp": ts,
	}
	createPayloadBytes, _ := json.Marshal(createPayload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.apiBaseURL+"/api/v2/chats/new", bytes.NewReader(createPayloadBytes))
	if err != nil {
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, err.Error(), startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for k, v := range commonHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, err.Error(), startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		errStr := fmt.Sprintf("Failed to create chat (%d): %s", resp.StatusCode, string(respBytes))
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, errStr, startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": errStr})
		return
	}

	var createRes map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&createRes)

	var chatId string
	if dataMap, ok := createRes["data"].(map[string]interface{}); ok {
		chatId, _ = dataMap["id"].(string)
	}

	if chatId == "" {
		errStr := "创建会话失败: JSON invalid"
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, errStr, startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": errStr})
		return
	}

	// 2. Call Completions Stream
	compPayload := map[string]interface{}{
		"stream":             true,
		"version":            "2.1",
		"incremental_output": true,
		"chat_id":            chatId,
		"chat_mode":          "normal",
		"model":              targetModel,
		"parent_id":          nil,
		"messages":           []interface{}{qwenMsg},
		"timestamp":          ts,
	}
	compPayloadBytes, _ := json.Marshal(compPayload)

	compURL := fmt.Sprintf("%s/api/v2/chat/completions?chat_id=%s", s.apiBaseURL, chatId)
	compReq, err := http.NewRequestWithContext(ctx, "POST", compURL, bytes.NewReader(compPayloadBytes))
	if err != nil {
		s.deleteChatRaw(chatId, commonHeaders)
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, err.Error(), startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for k, v := range commonHeaders {
		compReq.Header.Set(k, v)
	}
	compReq.Header.Set("Accept", "text/event-stream")

	compResp, err := s.client.Do(compReq)
	if err != nil {
		s.deleteChatRaw(chatId, commonHeaders)
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, err.Error(), startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer compResp.Body.Close()

	if compResp.StatusCode != http.StatusOK {
		s.deleteChatRaw(chatId, commonHeaders)
		respBytes, _ := io.ReadAll(compResp.Body)
		errStr := fmt.Sprintf("Completions stream error (%d): %s", compResp.StatusCode, string(respBytes))
		s.writeFailureLog(db, selectedAccount.id, targetModel, currentContent, bodyBytes, errStr, startTime)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": errStr})
		return
	}

	responseID := fmt.Sprintf("chatcmpl-%s", s.randUUID())
	var firstTokenTimeMs *int
	var fullContent strings.Builder
	var fullReasoning strings.Builder

	if reqBody.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		scanner := bufio.NewScanner(compResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			lineTrimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(lineTrimmed, "data:") {
				continue
			}
			dataStr := strings.TrimPrefix(lineTrimmed, "data:")
			dataStr = strings.TrimSpace(dataStr)
			if dataStr == "[DONE]" {
				continue
			}

			var data struct {
				Phase   string `json:"phase"`
				Content string `json:"content"`
				Text    string `json:"text"`
				Choices []struct {
					Delta struct {
						Phase            string `json:"phase"`
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						Extra            struct {
							SummaryThought struct {
								Content interface{} `json:"content"`
							} `json:"summary_thought"`
							ToolResult   []interface{} `json:"tool_result"`
							ImageURL     string        `json:"image_url"`
							WanxImageURL string        `json:"wanx_image_url"`
							ImageURL2    string        `json:"imageUrl"`
							ImageURLs    []interface{} `json:"image_urls"`
							Images       []interface{} `json:"images"`
							ImageURLs2   []interface{} `json:"imageUrls"`
						} `json:"extra"`
					} `json:"delta"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(dataStr), &data); err == nil {
				deltaVal := ""
				reasoningVal := ""
				isThinking := false

				if len(data.Choices) > 0 {
					d := data.Choices[0].Delta
					isThinking = strings.Contains(d.Phase, "think") || strings.Contains(d.Phase, "thought") || strings.Contains(data.Phase, "think") || strings.Contains(data.Phase, "thought")
					deltaVal = d.Content
					if deltaVal == "" {
						deltaVal = data.Content
					}
					if deltaVal == "" {
						deltaVal = data.Text
					}
					reasoningVal = d.ReasoningContent

					// summary thought extraction
					if d.Extra.SummaryThought.Content != nil {
						switch st := d.Extra.SummaryThought.Content.(type) {
						case string:
							deltaVal += st
						case []interface{}:
							for _, it := range st {
								if sStr, ok := it.(string); ok {
									deltaVal += sStr
								}
							}
						}
					}

					// Captured URLs extraction
					extractedURLs := []string{}
					if d.Extra.ImageURL != "" && strings.HasPrefix(d.Extra.ImageURL, "http") {
						extractedURLs = append(extractedURLs, d.Extra.ImageURL)
					}
					if d.Extra.WanxImageURL != "" && strings.HasPrefix(d.Extra.WanxImageURL, "http") {
						extractedURLs = append(extractedURLs, d.Extra.WanxImageURL)
					}
					if d.Extra.ImageURL2 != "" && strings.HasPrefix(d.Extra.ImageURL2, "http") {
						extractedURLs = append(extractedURLs, d.Extra.ImageURL2)
					}
					for _, item := range d.Extra.ToolResult {
						if itemMap, ok := item.(map[string]interface{}); ok {
							for _, k := range []string{"image", "url", "src", "imageUrl", "image_url"} {
								if val, ok := itemMap[k].(string); ok && strings.HasPrefix(val, "http") {
									extractedURLs = append(extractedURLs, val)
								}
							}
						} else if val, ok := item.(string); ok && strings.HasPrefix(val, "http") {
							extractedURLs = append(extractedURLs, val)
						}
					}
					for _, kList := range [][]interface{}{d.Extra.ImageURLs, d.Extra.Images, d.Extra.ImageURLs2} {
						for _, item := range kList {
							if val, ok := item.(string); ok && strings.HasPrefix(val, "http") {
								extractedURLs = append(extractedURLs, val)
							} else if itemMap, ok := item.(map[string]interface{}); ok {
								for _, sk := range []string{"url", "src", "image"} {
									if val, ok := itemMap[sk].(string); ok && strings.HasPrefix(val, "http") {
										extractedURLs = append(extractedURLs, val)
									}
								}
							}
						}
					}

					for _, imgURL := range extractedURLs {
						if !strings.Contains(fullContent.String(), imgURL) {
							imgMd := fmt.Sprintf("\n\n![Generated Image](%s)\n\n", imgURL)
							fullContent.WriteString(imgMd)

							chunkPayload := map[string]interface{}{
								"id":      responseID,
								"object":  "chat.completion.chunk",
								"created": ts,
								"model":   targetModel,
								"choices": []map[string]interface{}{
									{
										"index": 0,
										"delta": map[string]interface{}{
											"content": imgMd,
										},
										"finish_reason": nil,
									},
								},
							}
							chunkBytes, _ := json.Marshal(chunkPayload)
							_, _ = w.Write([]byte(fmt.Sprintf("data: %s\n\n", string(chunkBytes))))
							if ok {
								flusher.Flush()
							}
						}
					}
				}

				if deltaVal != "" && firstTokenTimeMs == nil {
					val := int(time.Since(startTime).Milliseconds())
					firstTokenTimeMs = &val
				}

				if isThinking {
					fullReasoning.WriteString(deltaVal)
					if reasoningVal == "" {
						reasoningVal = deltaVal
					}
				} else {
					fullContent.WriteString(deltaVal)
				}

				// Output delta chunk to client
				if deltaVal != "" || reasoningVal != "" {
					outDelta := make(map[string]interface{})
					if isThinking {
						outDelta["reasoning_content"] = reasoningVal
					} else {
						outDelta["content"] = deltaVal
					}

					chunkPayload := map[string]interface{}{
						"id":      responseID,
						"object":  "chat.completion.chunk",
						"created": ts,
						"model":   targetModel,
						"choices": []map[string]interface{}{
							{
								"index":         0,
								"delta":         outDelta,
								"finish_reason": nil,
							},
						},
					}
					chunkBytes, _ := json.Marshal(chunkPayload)
					_, _ = w.Write([]byte(fmt.Sprintf("data: %s\n\n", string(chunkBytes))))
					if ok {
						flusher.Flush()
					}
				}
			}
		}

		// Stream End
		endPayload := map[string]interface{}{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": ts,
			"model":   targetModel,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": "stop",
				},
			},
		}
		endBytes, _ := json.Marshal(endPayload)
		_, _ = w.Write([]byte(fmt.Sprintf("data: %s\n\n", string(endBytes))))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if ok {
			flusher.Flush()
		}

		// Async delete chat and save logs
		go func() {
			s.deleteChatRaw(chatId, commonHeaders)
			s.writeSuccessLog(selectedAccount.id, targetModel, currentContent, fullContent.String(), fullReasoning.String(), bodyBytes, firstTokenTimeMs, startTime)
		}()
	} else {
		// Non-streaming completion
		var fullContent strings.Builder
		var fullReasoning strings.Builder

		scanner := bufio.NewScanner(compResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			lineTrimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(lineTrimmed, "data:") {
				continue
			}
			dataStr := strings.TrimPrefix(lineTrimmed, "data:")
			dataStr = strings.TrimSpace(dataStr)
			if dataStr == "[DONE]" {
				continue
			}

			var data struct {
				Phase   string `json:"phase"`
				Content string `json:"content"`
				Text    string `json:"text"`
				Choices []struct {
					Delta struct {
						Phase            string `json:"phase"`
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(dataStr), &data); err == nil {
				deltaVal := ""
				isThinking := false
				if len(data.Choices) > 0 {
					d := data.Choices[0].Delta
					isThinking = strings.Contains(d.Phase, "think") || strings.Contains(d.Phase, "thought") || strings.Contains(data.Phase, "think") || strings.Contains(data.Phase, "thought")
					deltaVal = d.Content
					if deltaVal == "" {
						deltaVal = data.Content
					}
					if deltaVal == "" {
						deltaVal = data.Text
					}
				}

				if deltaVal != "" && firstTokenTimeMs == nil {
					val := int(time.Since(startTime).Milliseconds())
					firstTokenTimeMs = &val
				}

				if isThinking {
					fullReasoning.WriteString(deltaVal)
				} else {
					fullContent.WriteString(deltaVal)
				}
			}
		}

		s.deleteChatRaw(chatId, commonHeaders)
		s.writeSuccessLog(selectedAccount.id, targetModel, currentContent, fullContent.String(), fullReasoning.String(), bodyBytes, firstTokenTimeMs, startTime)

		res := map[string]interface{}{
			"id":      responseID,
			"object":  "chat.completion",
			"created": ts,
			"model":   targetModel,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":              "assistant",
						"content":           fullContent.String(),
						"reasoning_content": fullReasoning.String(),
					},
					"finish_reason": "stop",
				},
			},
		}
		response.JSON(w, http.StatusOK, res)
	}
}

func (s *Service) proxyImagesGenerations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	ts := startTime.Unix()

	var reqBody struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
		Size   string `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if reqBody.Prompt == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}

	modelName := reqBody.Model
	if modelName == "" {
		modelName = "qwen3.6-plus"
	}
	sizeVal := reqBody.Size
	if sizeVal == "" {
		sizeVal = "1024x1024"
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// Select random account
	rows, err := db.QueryContext(ctx, "SELECT id, name, token, status FROM qwen_accounts WHERE enable = 1 AND status != 'invalid'")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type accountItem struct {
		id, name, token, status string
	}
	accounts := []accountItem{}
	for rows.Next() {
		var acc accountItem
		if err := rows.Scan(&acc.id, &acc.name, &acc.token, &acc.status); err == nil {
			acc.token = secure.SecureDecrypt(acc.token)
			accounts = append(accounts, acc)
		}
	}

	if len(accounts) == 0 {
		response.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "No valid accounts"})
		return
	}

	nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(len(accounts))))
	selectedAccount := accounts[nBig.Int64()]
	bearerToken := s.extractBearerToken(selectedAccount.token)

	commonHeaders := map[string]string{
		"Authorization":  "Bearer " + bearerToken,
		"Content-Type":   "application/json",
		"User-Agent":     "Mozilla/5.0",
		"Referer":        s.apiBaseURL + "/",
		"X-Request-With": "XMLHttpRequest",
	}

	// 1. Create chat session (t2i type)
	createPayload := map[string]interface{}{
		"title":     fmt.Sprintf("t2i_%d", ts),
		"models":    []string{modelName},
		"chat_mode": "normal",
		"chat_type": "t2i",
		"timestamp": ts,
	}
	createPayloadBytes, _ := json.Marshal(createPayload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.apiBaseURL+"/api/v2/chats/new", bytes.NewReader(createPayloadBytes))
	if err != nil {
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", err.Error())
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for k, v := range commonHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", err.Error())
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var createRes map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&createRes)

	var chatId string
	if dataMap, ok := createRes["data"].(map[string]interface{}); ok {
		chatId, _ = dataMap["id"].(string)
	}

	if chatId == "" {
		errStr := "创建会话失败"
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", errStr)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": errStr})
		return
	}

	// 2. Call completions to generate image
	aspectRatio := "1:1"
	if sizeVal == "16:9" {
		aspectRatio = "16:9"
	}
	compPayload := map[string]interface{}{
		"stream":             true,
		"version":            "2.1",
		"incremental_output": true,
		"chat_id":            chatId,
		"chat_mode":          "normal",
		"model":              modelName,
		"parent_id":          nil,
		"messages": []interface{}{
			map[string]interface{}{
				"fid":         s.randUUID(),
				"role":        "user",
				"content":     reqBody.Prompt,
				"user_action": "chat",
				"feature_config": map[string]interface{}{
					"image_generation":     true,
					"plugins_enabled":      true,
					"default_aspect_ratio": aspectRatio,
				},
				"extra": map[string]interface{}{
					"meta": map[string]interface{}{
						"subChatType": "t2i",
						"mode":        "image_generation",
					},
				},
				"sub_chat_type": "t2i",
			},
		},
		"timestamp": ts,
	}
	compPayloadBytes, _ := json.Marshal(compPayload)

	compReq, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v2/chat/completions?chat_id=%s", s.apiBaseURL, chatId), bytes.NewReader(compPayloadBytes))
	if err != nil {
		s.deleteChatRaw(chatId, commonHeaders)
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", err.Error())
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for k, v := range commonHeaders {
		compReq.Header.Set(k, v)
	}
	compReq.Header.Set("Accept", "text/event-stream")

	compResp, err := s.client.Do(compReq)
	if err != nil {
		s.deleteChatRaw(chatId, commonHeaders)
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", err.Error())
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer compResp.Body.Close()

	var imageURL string
	scanner := bufio.NewScanner(compResp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		lineTrimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(lineTrimmed, "data:") {
			continue
		}
		dataStr := strings.TrimPrefix(lineTrimmed, "data:")
		dataStr = strings.TrimSpace(dataStr)
		if dataStr == "[DONE]" {
			continue
		}

		var data struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					Text    string `json:"text"`
					Extra   struct {
						WanxImageURL string        `json:"wanx_image_url"`
						ToolResult   []interface{} `json:"tool_result"`
					} `json:"extra"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(dataStr), &data); err == nil && len(data.Choices) > 0 {
			d := data.Choices[0].Delta
			if strings.HasPrefix(d.Content, "http") {
				imageURL = d.Content
			} else if strings.HasPrefix(d.Text, "http") {
				imageURL = d.Text
			} else if d.Extra.WanxImageURL != "" {
				imageURL = d.Extra.WanxImageURL
			} else {
				for _, item := range d.Extra.ToolResult {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if val, ok := itemMap["image"].(string); ok && strings.HasPrefix(val, "http") {
							imageURL = val
							break
						}
					}
				}
			}
		}
	}

	s.deleteChatRaw(chatId, commonHeaders)

	if imageURL == "" {
		errStr := "Failed to extract image URL"
		s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, "", startTime, "failed", errStr)
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": errStr})
		return
	}

	s.writeImageLog(selectedAccount.id, modelName, reqBody.Prompt, imageURL, startTime, "success", "")

	// Mark account online
	if dbConn, dbErr := s.open(context.Background()); dbErr == nil {
		defer dbConn.Close()
		_, _ = dbConn.Exec("UPDATE qwen_accounts SET status = 'online' WHERE id = ?", selectedAccount.id)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"created": ts,
		"data": []map[string]string{
			{"url": imageURL},
		},
	})
}

func (s *Service) GetModelsList(ctx context.Context) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'QWEN_MATRIX'").Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}

	var matrix map[string]interface{}
	if err := json.Unmarshal([]byte(value), &matrix); err != nil {
		return nil, err
	}

	modelList := []map[string]interface{}{}
	now := time.Now().Unix()
	for id, item := range matrix {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		enabled, _ := itemMap["enabled"].(bool)
		if enabled {
			modelList = append(modelList, map[string]interface{}{
				"id":       id,
				"object":   "model",
				"created":  now,
				"owned_by": "qwen",
			})
		}
	}

	return modelList, nil
}

func (s *Service) CanHandleModel(ctx context.Context, model string) bool {
	modelLower := strings.ToLower(model)
	if strings.HasPrefix(modelLower, "qwen") {
		return true
	}
	db, err := s.open(ctx)
	if err != nil {
		return false
	}
	defer db.Close()

	// Check redirects
	var target string
	err = db.QueryRowContext(ctx, "SELECT target_model FROM qwen_model_redirects WHERE source_model = ?", model).Scan(&target)
	if err == nil && target != "" {
		return true
	}

	// Check matrix
	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM qwen_settings WHERE key = 'QWEN_MATRIX'").Scan(&value)
	if err == nil {
		var matrix map[string]interface{}
		if err := json.Unmarshal([]byte(value), &matrix); err == nil {
			if _, ok := matrix[model]; ok {
				return true
			}
		}
	}

	return false
}

// ==================== Helper methods ====================

func (s *Service) extractBearerToken(token string) string {
	if token == "" {
		return ""
	}
	if strings.Contains(token, "token=") {
		// Regex match
		reg := regexp.MustCompile(`token=([^;]+)`)
		match := reg.FindStringSubmatch(token)
		if len(match) > 1 {
			return match[1]
		}
	}
	return token
}

func (s *Service) checkAccountStatusRaw(token string) string {
	bearer := s.extractBearerToken(token)
	req, err := http.NewRequest("GET", s.apiBaseURL+"/api/models", nil)
	if err != nil {
		return "offline"
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", s.apiBaseURL+"/")

	resp, err := s.client.Do(req)
	if err != nil {
		return "offline"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return "online"
	}
	return "offline"
}

func (s *Service) deleteChatRaw(chatId string, headers map[string]string) {
	if chatId == "" {
		return
	}
	delURL := fmt.Sprintf("%s/api/v2/chats/%s", s.apiBaseURL, chatId)
	req, err := http.NewRequest("DELETE", delURL, nil)
	if err != nil {
		return
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (s *Service) writeSuccessLog(accountId, model, prompt, responseVal, reasoning string, messages []byte, firstToken *int, startTime time.Time) {
	db, err := s.open(context.Background())
	if err != nil {
		return
	}
	defer db.Close()

	duration := int(time.Since(startTime).Milliseconds())
	tokens := int(float64(len(prompt)+len(responseVal)) * 1.5)

	var reasoningVal interface{} = nil
	if reasoning != "" {
		reasoningVal = reasoning
	}

	_, _ = db.Exec(`
		INSERT INTO qwen_logs (trace_id, account_id, model, prompt, response, reasoning_content, messages, tokens, status, duration, first_token_time_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.randUUID(), accountId, model, prompt, responseVal, reasoningVal, string(messages), tokens, "success", duration, firstToken)
}

func (s *Service) writeFailureLog(db *sql.DB, accountId, model, prompt string, messages []byte, errStr string, startTime time.Time) {
	duration := int(time.Since(startTime).Milliseconds())
	_, _ = db.Exec(`
		INSERT INTO qwen_logs (trace_id, account_id, model, prompt, response, messages, tokens, status, error, duration)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.randUUID(), accountId, model, prompt, "", string(messages), 0, "failed", errStr, duration)

	if accountId != "" {
		_, _ = db.Exec("UPDATE qwen_accounts SET status = 'offline' WHERE id = ?", accountId)
	}
}

func (s *Service) writeImageLog(accountId, model, prompt, responseVal string, startTime time.Time, status, errStr string) {
	db, err := s.open(context.Background())
	if err != nil {
		return
	}
	defer db.Close()

	duration := int(time.Since(startTime).Milliseconds())
	var errVal interface{} = nil
	if errStr != "" {
		errVal = errStr
	}

	_, _ = db.Exec(`
		INSERT INTO qwen_logs (trace_id, account_id, model, prompt, response, tokens, status, error, duration)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.randUUID(), accountId, model, prompt, responseVal, 0, status, errVal, duration)

	if accountId != "" && status == "failed" {
		_, _ = db.Exec("UPDATE qwen_accounts SET status = 'offline' WHERE id = ?", accountId)
	}
}

func (s *Service) randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "xxxx"
		}
		b[i] = letters[num.Int64()]
	}
	return string(b)
}

func (s *Service) randUUID() string {
	u := make([]byte, 16)
	_, _ = rand.Read(u)
	u[8] = (u[8] | 0x40) & 0x7F
	u[6] = (u[6] & 0xF) | (4 << 4)
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}
