package openai

import (
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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	defaultTimeout       = 60 * time.Second
	degradedThreshold    = 20 * time.Second
	healthTimeoutDefault = 60 * time.Second
)

type Endpoint struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	BaseURL      string   `json:"baseUrl"`
	APIKey       string   `json:"apiKey"`
	Notes        string   `json:"notes"`
	Status       string   `json:"status"`
	Enabled      bool     `json:"enabled"`
	Models       []string `json:"models"`
	CreatedAt    string   `json:"createdAt"`
	LastUsed     *string  `json:"lastUsed"`
	LastChecked  *string  `json:"lastChecked"`
	HealthStatus string   `json:"healthStatus,omitempty"`
}

type HealthRecord struct {
	Model      string `json:"model"`
	Status     string `json:"status"`
	Latency    int64  `json:"latency"`
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checkedAt"`
}

type HealthSummary struct {
	TotalModels   int            `json:"totalModels"`
	Operational   int            `json:"operational"`
	Degraded      int            `json:"degraded"`
	Failed        int            `json:"failed"`
	OverallStatus string         `json:"overallStatus"`
	Results       []HealthRecord `json:"results"`
	CheckedAt     string         `json:"checkedAt"`
}

type Service struct {
	cfg    config.Config
	store  *database.Store
	client *http.Client
}

func New(cfg config.Config) *Service {
	s := &Service{
		cfg:    cfg,
		store:  database.New(cfg),
		client: &http.Client{Timeout: defaultTimeout},
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
		`CREATE TABLE IF NOT EXISTS openai_endpoints (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			status TEXT DEFAULT 'unknown',
			enabled INTEGER DEFAULT 1,
			models TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			last_checked DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS openai_health_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id TEXT NOT NULL,
			status TEXT NOT NULL,
			response_time INTEGER,
			error_message TEXT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (endpoint_id) REFERENCES openai_endpoints(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_endpoints_status ON openai_endpoints(status)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_health_endpoint ON openai_health_history(endpoint_id, checked_at)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_personas (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			icon TEXT,
			system_prompt TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			model TEXT,
			endpoint_id TEXT,
			persona_id TEXT,
			system_prompt TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			reasoning TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES openai_chat_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS openai_gateway_analytics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id TEXT,
			model TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_analytics_timestamp ON openai_gateway_analytics(timestamp)`,
		`INSERT OR IGNORE INTO openai_chat_personas (id, name, icon, system_prompt, is_default)
		 VALUES ('1', '默认助手', 'fa-robot', '你是一个有用的 AI 助手。', 1)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("openai ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// Completions proxy (intelligent routing or specific load balancer)
	if method == http.MethodPost && (path == "/v1/chat/completions" || path == "/chat/completions" || path == "/api/openai" || path == "/api/openai/v1/chat/completions" || path == "/api/openai/chat/completions") {
		s.proxyChatCompletions(w, r)
		return
	}

	// Models proxy
	if method == http.MethodGet && (path == "/v1/models" || path == "/models" || path == "/api/openai/v1/models" || path == "/api/openai/models") {
		s.proxyModels(w, r)
		return
	}

	// Admin CRUD prefix
	adminPath := strings.TrimPrefix(path, "/api/openai")
	adminPath = strings.Trim(adminPath, "/")
	parts := []string{}
	if adminPath != "" {
		parts = strings.Split(adminPath, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodGet:
		s.listEndpoints(w, r)
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodPost:
		s.createEndpoint(w, r)
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodPut:
		s.updateEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleEndpoint(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodDelete:
		s.deleteEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "verify" && method == http.MethodPost:
		s.verifyEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "models" && method == http.MethodGet:
		s.getEndpointModels(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "test" && method == http.MethodPost:
		s.testEndpointChat(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check" && method == http.MethodPost:
		s.healthCheckModelRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllModelsRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health" && method == http.MethodGet:
		s.getEndpointHealthRoute(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && (parts[1] == "refresh" || parts[1] == "refresh-all") && method == http.MethodPost:
		s.refreshAllEndpointsRoute(w, r)
	case len(parts) == 1 && parts[0] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllRoute(w, r)
	case len(parts) == 1 && parts[0] == "export" && method == http.MethodGet:
		s.exportEndpointsRoute(w, r)
	case len(parts) == 1 && parts[0] == "import" && method == http.MethodPost:
		s.importEndpointsRoute(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "summary" && method == http.MethodGet:
		s.getAnalyticsSummary(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "charts" && method == http.MethodGet:
		s.getAnalyticsCharts(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "logs" && method == http.MethodGet:
		s.getAnalyticsLogs(w, r)
	case len(parts) == 1 && parts[0] == "personas" && method == http.MethodGet:
		s.listPersonas(w, r)
	case len(parts) == 1 && parts[0] == "personas" && method == http.MethodPost:
		s.createPersona(w, r)
	case len(parts) == 2 && parts[0] == "personas" && method == http.MethodPut:
		s.updatePersona(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "personas" && method == http.MethodDelete:
		s.deletePersona(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodGet:
		s.listSessions(w, r)
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodPost:
		s.createSession(w, r)
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodDelete:
		s.clearSessions(w, r)
	case len(parts) == 2 && parts[0] == "sessions" && method == http.MethodPut:
		s.updateSession(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "sessions" && method == http.MethodDelete:
		s.deleteSession(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodGet:
		s.listSessionMessages(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodPost:
		s.createSessionMessage(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodDelete:
		s.clearSessionMessages(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodDelete:
		s.deleteSessionMessage(w, r, parts[1], parts[3])
	default:
		response.Error(w, http.StatusNotFound, "openai admin route not found")
	}
}

func (s *Service) listEndpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, status, enabled, models, created_at, last_used, last_checked FROM openai_endpoints")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var modelsRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &ep.Status, &enabledInt, &modelsRaw, &created, &used, &checked)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
		}

		ep.Models = []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, endpoints)
}

func (s *Service) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		BaseURL    string `json:"baseUrl"`
		APIKey     string `json:"apiKey"`
		Notes      string `json:"notes"`
		SkipVerify bool   `json:"skipVerify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "名称、API 地址和 API Key 必填"})
		return
	}

	normalizedURL := s.normalizeBaseURL(req.BaseURL)
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	id := fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
	status := "unknown"
	modelsList := []string{}
	var verification map[string]interface{}

	if !req.SkipVerify {
		vOk, count, err := s.verifyAPIKeyRaw(ctx, normalizedURL, req.APIKey)
		if err == nil && vOk {
			status = "valid"
			verification = map[string]interface{}{
				"valid":       true,
				"modelsCount": count,
			}
			mList, mErr := s.listModelsRaw(ctx, normalizedURL, req.APIKey)
			if mErr == nil {
				modelsList = mList
			}
		} else {
			status = "invalid"
			errMsg := "API Key 验证失败"
			if err != nil {
				errMsg = err.Error()
			}
			verification = map[string]interface{}{
				"valid": false,
				"error": errMsg,
			}
		}
	}

	modelsJSON, _ := json.Marshal(modelsList)
	createdAt := time.Now().Format(time.RFC3339)
	var lastCheckedVal interface{} = nil
	if !req.SkipVerify {
		lastCheckedVal = createdAt
	}

	encryptedKey, err := secure.SecureEncrypt(req.APIKey)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
		return
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, status, enabled, models, created_at, last_checked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, normalizedURL, encryptedKey, status, 1, string(modelsJSON), createdAt, lastCheckedVal)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var checkedStr *string
	if !req.SkipVerify {
		checkedStr = &createdAt
	}

	resEndpoint := Endpoint{
		ID:          id,
		Name:        req.Name,
		BaseURL:     normalizedURL,
		APIKey:      req.APIKey,
		Notes:       req.Notes,
		Status:      status,
		Enabled:     true,
		Models:      modelsList,
		CreatedAt:   createdAt,
		LastChecked: checkedStr,
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"endpoint":     resEndpoint,
		"verification": verification,
	})
}

func (s *Service) updateEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name    string `json:"name"`
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
		Notes   string `json:"notes"`
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

	var currentBaseURL, currentAPIKey string
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&currentBaseURL, &currentAPIKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	currentAPIKey = secure.SecureDecrypt(currentAPIKey)

	targetBaseURL := currentBaseURL
	if req.BaseURL != "" {
		targetBaseURL = s.normalizeBaseURL(req.BaseURL)
	}
	targetAPIKey := currentAPIKey
	if req.APIKey != "" {
		targetAPIKey = req.APIKey
	}

	if req.APIKey != "" || req.BaseURL != "" {
		status := "unknown"
		modelsList := []string{}

		vOk, _, err := s.verifyAPIKeyRaw(ctx, targetBaseURL, targetAPIKey)
		if err == nil && vOk {
			status = "valid"
			mList, mErr := s.listModelsRaw(ctx, targetBaseURL, targetAPIKey)
			if mErr == nil {
				modelsList = mList
			}
		} else {
			status = "invalid"
		}

		modelsJSON, _ := json.Marshal(modelsList)
		encryptedKey, err := secure.SecureEncrypt(targetAPIKey)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
			return
		}
		lastChecked := time.Now().Format(time.RFC3339)

		_, err = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET name = ?, base_url = ?, api_key = ?, status = ?, models = ?, last_checked = ?
			WHERE id = ?`,
			req.Name, targetBaseURL, encryptedKey, status, string(modelsJSON), lastChecked, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET name = ? WHERE id = ?", req.Name, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) toggleEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Enabled bool `json:"enabled"`
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

	var exists int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", id).Scan(&exists)
	if err != nil || exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	enabledVal := 0
	if req.Enabled {
		enabledVal = 1
	}

	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET enabled = ? WHERE id = ?", enabledVal, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "enabled": req.Enabled})
}

func (s *Service) deleteEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, "DELETE FROM openai_endpoints WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rows, err := res.RowsAffected()
	if err != nil || rows == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) verifyEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var name, baseURL, apiKey string
	err = db.QueryRowContext(ctx, "SELECT name, base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&name, &baseURL, &apiKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	startTime := time.Now()
	status := "invalid"
	modelsList := []string{}
	var errMsg string

	vOk, _, vErr := s.verifyAPIKeyRaw(ctx, baseURL, apiKey)
	responseTime := time.Since(startTime).Milliseconds()

	if vErr == nil && vOk {
		status = "valid"
		mList, mErr := s.listModelsRaw(ctx, baseURL, apiKey)
		if mErr == nil {
			modelsList = mList
		}
	} else if vErr != nil {
		errMsg = vErr.Error()
	}

	checkedAt := time.Now().Format(time.RFC3339)
	modelsJSON, _ := json.Marshal(modelsList)

	_, err = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, models = ?, last_checked = ?
		WHERE id = ?`,
		status, string(modelsJSON), checkedAt, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	res := map[string]interface{}{
		"status":       status,
		"responseTime": responseTime,
		"modelsCount":  len(modelsList),
		"models":       modelsList,
		"checkedAt":    checkedAt,
		"valid":        status == "valid",
	}
	if errMsg != "" {
		res["error"] = errMsg
	}

	response.JSON(w, http.StatusOK, res)
}

func (s *Service) getEndpointModels(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	modelsList, err := s.listModelsRaw(ctx, baseURL, apiKey)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	modelsJSON, _ := json.Marshal(modelsList)
	checkedAt := time.Now().Format(time.RFC3339)

	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET models = ?, last_checked = ? WHERE id = ?", string(modelsJSON), checkedAt, id)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"models":  modelsList,
	})
}

func (s *Service) testEndpointChat(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	modelName := req.Model
	if modelName == "" {
		modelName = "gpt-3.5-turbo"
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	chatPayload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "Say \"Hello, API test successful!\" in exactly those words."},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(chatPayload)

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))})
		return
	}

	var chatResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage interface{} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "无法解析响应 JSON"})
		return
	}

	reply := ""
	if len(chatResponse.Choices) > 0 {
		reply = chatResponse.Choices[0].Message.Content
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": reply,
		"usage":    chatResponse.Usage,
	})
}

func (s *Service) healthCheckModelRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model   string `json:"model"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Model == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型名称必填"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	result := s.healthCheckSingleModel(ctx, id, baseURL, apiKey, req.Model, timeoutDuration)

	// Save check to health history
	var errMsg sql.NullString
	if result.Error != "" {
		errMsg.Valid = true
		errMsg.String = result.Error
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, result.Status, result.Latency, errMsg, result.CheckedAt)

	response.JSON(w, http.StatusOK, result)
}

func (s *Service) healthCheckAllModelsRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey, modelsRaw string
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, models FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &modelsRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	var models []string
	if modelsRaw != "" {
		_ = json.Unmarshal([]byte(modelsRaw), &models)
	}

	if len(models) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"totalModels": 0,
			"message":     "该端点没有模型可供检测",
		})
		return
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := 5
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}

	summary := s.runBatchHealthCheck(ctx, id, baseURL, apiKey, models, timeoutDuration, concurrency)

	// Save check results to db history
	for _, result := range summary.Results {
		var errMsg sql.NullString
		if result.Error != "" {
			errMsg.Valid = true
			errMsg.String = result.Error
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
			VALUES (?, ?, ?, ?, ?)`,
			id, result.Status, result.Latency, errMsg, result.CheckedAt)
	}

	// Update endpoint status
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, last_checked = ?
		WHERE id = ?`,
		summary.OverallStatus, summary.CheckedAt, id)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"summary": summary,
	})
}

func (s *Service) getEndpointHealthRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var status, lastChecked sql.NullString
	err = db.QueryRowContext(ctx, "SELECT status, last_checked FROM openai_endpoints WHERE id = ?", id).Scan(&status, &lastChecked)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	// Fetch health history per model
	rows, err := db.QueryContext(ctx, `
		SELECT h.status, h.response_time, h.error_message, h.checked_at
		FROM openai_health_history h
		WHERE h.endpoint_id = ?
		ORDER BY h.checked_at DESC
		LIMIT 100`, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	history := []map[string]interface{}{}
	for rows.Next() {
		var hStatus, checked string
		var respTime sql.NullInt64
		var errMsg sql.NullString
		if err := rows.Scan(&hStatus, &respTime, &errMsg, &checked); err == nil {
			item := map[string]interface{}{
				"status":    hStatus,
				"checkedAt": checked,
			}
			if respTime.Valid {
				item["responseTime"] = respTime.Int64
			}
			if errMsg.Valid {
				item["errorMessage"] = errMsg.String
			}
			history = append(history, item)
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"endpointId":      id,
		"healthStatus":    status.String,
		"lastHealthCheck": lastChecked.String,
		"history":         history,
	})
}

func (s *Service) refreshAllEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, url, key string
	}
	items := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.key); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			items = append(items, it)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []map[string]interface{}{}

	for _, it := range items {
		wg.Add(1)
		go func(it item) {
			defer wg.Done()
			status := "invalid"
			modelsList := []string{}
			var errStr string

			vOk, _, err := s.verifyAPIKeyRaw(ctx, it.url, it.key)
			if err == nil && vOk {
				status = "valid"
				mList, mErr := s.listModelsRaw(ctx, it.url, it.key)
				if mErr == nil {
					modelsList = mList
				}
			} else if err != nil {
				errStr = err.Error()
			}

			checkedAt := time.Now().Format(time.RFC3339)
			modelsJSON, _ := json.Marshal(modelsList)

			// Update in DB
			if dbConn, dbErr := s.open(ctx); dbErr == nil {
				defer dbConn.Close()
				_, _ = dbConn.ExecContext(ctx, `
					UPDATE openai_endpoints
					SET status = ?, models = ?, last_checked = ?
					WHERE id = ?`,
					status, string(modelsJSON), checkedAt, it.id)
			}

			mu.Lock()
			res := map[string]interface{}{
				"id":          it.id,
				"name":        it.name,
				"success":     status == "valid",
				"modelsCount": len(modelsList),
			}
			if errStr != "" {
				res["error"] = errStr
			}
			results = append(results, res)
			mu.Unlock()
		}(it)
	}

	wg.Wait()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "results": results})
}

func (s *Service) healthCheckAllRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, models FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, url, key, modelsRaw string
	}
	items := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.key, &it.modelsRaw); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			items = append(items, it)
		}
	}

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := 5
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}

	results := []map[string]interface{}{}

	for _, it := range items {
		var models []string
		if it.modelsRaw != "" {
			_ = json.Unmarshal([]byte(it.modelsRaw), &models)
		}
		if len(models) == 0 {
			results = append(results, map[string]interface{}{
				"endpointId":  it.id,
				"name":        it.name,
				"totalModels": 0,
				"skipped":     true,
			})
			continue
		}

		summary := s.runBatchHealthCheck(ctx, it.id, it.url, it.key, models, timeoutDuration, concurrency)

		// Save history
		for _, result := range summary.Results {
			var errMsg sql.NullString
			if result.Error != "" {
				errMsg.Valid = true
				errMsg.String = result.Error
			}
			_, _ = db.ExecContext(ctx, `
				INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
				VALUES (?, ?, ?, ?, ?)`,
				it.id, result.Status, result.Latency, errMsg, result.CheckedAt)
		}

		// Update status
		_, _ = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET status = ?, last_checked = ?
			WHERE id = ?`,
			summary.OverallStatus, summary.CheckedAt, it.id)

		results = append(results, map[string]interface{}{
			"endpointId":  it.id,
			"name":        it.name,
			"totalModels": summary.TotalModels,
			"operational": summary.Operational,
			"degraded":    summary.Degraded,
			"failed":      summary.Failed,
			"results":     summary.Results,
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"checkedAt": time.Now().Format(time.RFC3339),
		"endpoints": results,
	})
}

func (s *Service) exportEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, status, enabled, models, created_at, last_used, last_checked FROM openai_endpoints")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var modelsRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &ep.Status, &enabledInt, &modelsRaw, &created, &used, &checked)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
		}

		ep.Models = []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
			_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"endpoints":  endpoints,
		"exportedAt": time.Now().Format(time.RFC3339),
	})
}

func (s *Service) importEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoints []Endpoint `json:"endpoints"`
		Overwrite bool       `json:"overwrite"`
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	if req.Overwrite {
		_, err = tx.ExecContext(ctx, "DELETE FROM openai_endpoints")
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	importedCount := 0
	skippedCount := 0

	for _, ep := range req.Endpoints {
		if ep.Name == "" || ep.BaseURL == "" || ep.APIKey == "" {
			skippedCount++
			continue
		}

		if !req.Overwrite {
			var exists int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE base_url = ?", ep.BaseURL).Scan(&exists)
			if exists > 0 {
				skippedCount++
				continue
			}
		}

		id := ep.ID
		if id == "" {
			id = fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
		}

		enabledInt := 0
		if ep.Enabled {
			enabledInt = 1
		}
		status := ep.Status
		if status == "" {
			status = "unknown"
		}
		createdAt := ep.CreatedAt
		if createdAt == "" {
			createdAt = time.Now().Format(time.RFC3339)
		}
		var modelsJSON []byte
		if len(ep.Models) > 0 {
			modelsJSON, _ = json.Marshal(ep.Models)
		} else {
			modelsJSON = []byte("[]")
		}

		encryptedKey, err := secure.SecureEncrypt(ep.APIKey)
		if err != nil {
			skippedCount++
			continue
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO openai_endpoints (id, name, base_url, api_key, status, enabled, models, created_at, last_used, last_checked)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ep.Name, ep.BaseURL, encryptedKey, status, enabledInt, string(modelsJSON), createdAt, ep.LastUsed, ep.LastChecked)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		importedCount++
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"imported": importedCount,
		"skipped":  skippedCount,
		"total":    importedCount + skippedCount,
	})
}

func (s *Service) proxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model, _ := parsedBody["model"].(string)
	stream, _ := parsedBody["stream"].(bool)
	targetEndpointID := r.Header.Get("x-endpoint-id")

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var selected Endpoint
	var found bool

	if targetEndpointID != "" {
		var ep Endpoint
		var modelsRaw sql.NullString
		var enabledInt int
		err := db.QueryRowContext(ctx, `
			SELECT id, name, base_url, api_key, status, enabled, models
			FROM openai_endpoints WHERE id = ?`, targetEndpointID).
			Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &ep.Status, &enabledInt, &modelsRaw)

		if err == nil {
			ep.APIKey = secure.SecureDecrypt(ep.APIKey)
			ep.Enabled = enabledInt == 1
			if modelsRaw.Valid {
				_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
			}
			selected = ep
			found = true
		}
	}

	if !found {
		// Get all valid enabled endpoints
		rows, err := db.QueryContext(ctx, `
			SELECT id, name, base_url, api_key, status, enabled, models
			FROM openai_endpoints WHERE status = 'valid' AND enabled = 1`)
		if err == nil {
			defer rows.Close()
			endpoints := []Endpoint{}
			for rows.Next() {
				var ep Endpoint
				var modelsRaw sql.NullString
				var enabledInt int
				if errScan := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &ep.Status, &enabledInt, &modelsRaw); errScan == nil {
					ep.APIKey = secure.SecureDecrypt(ep.APIKey)
					ep.Enabled = enabledInt == 1
					if modelsRaw.Valid {
						_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
					}
					endpoints = append(endpoints, ep)
				}
			}

			eligible := []Endpoint{}
			for _, ep := range endpoints {
				for _, m := range ep.Models {
					if m == model {
						eligible = append(eligible, ep)
						break
					}
				}
			}

			targets := eligible
			if len(targets) == 0 {
				targets = endpoints
			}

			if len(targets) > 0 {
				nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(len(targets))))
				selected = targets[nBig.Int64()]
				found = true
			}
		}
	}

	if !found {
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": "No valid OpenAI endpoints available",
				"type":    "service_unavailable",
			},
		})
		return
	}

	// Format base url
	fullURL := strings.TrimSuffix(selected.BaseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/chat/completions"

	isLocal := regexp.MustCompile(`(?i)^https?://(localhost|127\.0\.0\.1|0\.0\.0\.0|192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`).MatchString(fullURL)

	if !isLocal {
		if messages, ok := parsedBody["messages"].([]interface{}); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					if contentArr, ok := msgMap["content"].([]interface{}); ok {
						for _, part := range contentArr {
							if partMap, ok := part.(map[string]interface{}); ok {
								if partMap["type"] == "image_url" {
									if imgURLMap, ok := partMap["image_url"].(map[string]interface{}); ok {
										if imgURL, ok := imgURLMap["url"].(string); ok && strings.HasPrefix(imgURL, "/uploads/") {
											relativePath := strings.TrimPrefix(imgURL, "/")
											filePath := filepath.Join(s.cfg.DataDir, relativePath)

											if fileBytes, err := os.ReadFile(filePath); err == nil {
												ext := strings.ToLower(filepath.Ext(filePath))
												mimeType := "image/jpeg"
												switch ext {
												case ".png":
													mimeType = "image/png"
												case ".webp":
													mimeType = "image/webp"
												case ".gif":
													mimeType = "image/gif"
												}
												b64 := base64.StdEncoding.EncodeToString(fileBytes)
												imgURLMap["url"] = fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
												imgURLMap["_original_url"] = imgURL
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	upstreamBodyBytes, _ := json.Marshal(parsedBody)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(upstreamBodyBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpReq.Header.Set("Authorization", "Bearer "+selected.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}

	startTime := time.Now()
	resp, err := s.client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": map[string]string{"message": err.Error(), "type": "proxy_error"}})
		return
	}
	defer resp.Body.Close()

	// Update last used timestamp
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), selected.ID)

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)

		flusher, ok := w.(http.Flusher)
		buf := make([]byte, 4096)
		var accumulatedResponse strings.Builder
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
				accumulatedResponse.Write(buf[:n])
				if ok {
					flusher.Flush()
				}
			}
			if err != nil {
				break
			}
		}
		latencyMs := time.Since(startTime).Milliseconds()

		promptTokens := 0
		completionTokens := 0
		totalTokens := 0

		promptRegex := regexp.MustCompile(`"prompt_tokens"\s*:\s*(\d+)`)
		completionRegex := regexp.MustCompile(`"completion_tokens"\s*:\s*(\d+)`)
		totalRegex := regexp.MustCompile(`"total_tokens"\s*:\s*(\d+)`)

		accumulatedStr := accumulatedResponse.String()
		if matches := promptRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			promptTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := completionRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			completionTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := totalRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			totalTokens, _ = strconv.Atoi(matches[1])
		} else if promptTokens > 0 || completionTokens > 0 {
			totalTokens = promptTokens + completionTokens
		}

		s.RecordAnalytics(selected.ID, model, resp.StatusCode, latencyMs, promptTokens, completionTokens, totalTokens)
	} else {
		respBodyBytes, _ := io.ReadAll(resp.Body)
		latencyMs := time.Since(startTime).Milliseconds()

		var usageInfo struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBodyBytes, &usageInfo)

		s.RecordAnalytics(selected.ID, model, resp.StatusCode, latencyMs, usageInfo.Usage.PromptTokens, usageInfo.Usage.CompletionTokens, usageInfo.Usage.TotalTokens)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}

func (s *Service) GetModelsList(ctx context.Context) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, enabled, status, models FROM openai_endpoints WHERE enabled = 1 AND status = 'valid'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]map[string]interface{})
	for rows.Next() {
		var name, status, modelsRaw string
		var enabledInt int
		if err := rows.Scan(&name, &enabledInt, &status, &modelsRaw); err == nil {
			var models []string
			if modelsRaw != "" {
				_ = json.Unmarshal([]byte(modelsRaw), &models)
			}
			for _, mID := range models {
				if _, ok := modelMap[mID]; !ok {
					modelMap[mID] = map[string]interface{}{
						"id":       mID,
						"object":   "model",
						"created":  time.Now().Unix(),
						"owned_by": name,
					}
				}
			}
		}
	}

	modelList := []map[string]interface{}{}
	for _, m := range modelMap {
		modelList = append(modelList, m)
	}
	return modelList, nil
}

func (s *Service) proxyModels(w http.ResponseWriter, r *http.Request) {
	modelList, err := s.GetModelsList(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	sort.Slice(modelList, func(i, j int) bool {
		idI, _ := modelList[i]["id"].(string)
		idJ, _ := modelList[j]["id"].(string)
		return idI < idJ
	})

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   modelList,
	})
}

// ==================== Helper methods ====================

func (s *Service) normalizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")

	stripSuffixes := []string{"/chat/completions", "/completions", "/models", "/embeddings"}
	for _, suffix := range stripSuffixes {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			u = u[:len(u)-len(suffix)]
			u = strings.TrimSuffix(u, "/")
		}
	}

	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}

	// Append version path if missing
	hasVersion := false
	if reg := regexp.MustCompile(`(?i)/v\d+/?`); reg.MatchString(u) {
		hasVersion = true
	}
	if !hasVersion {
		u += "/v1"
	}

	return u
}

func (s *Service) verifyAPIKeyRaw(ctx context.Context, u string, key string) (bool, int, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := s.client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, 0, fmt.Errorf("verify failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 0, err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			return true, len(dataArr), nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		return true, len(parsedArr), nil
	}

	return true, 0, nil
}

func (s *Service) listModelsRaw(ctx context.Context, u string, key string) ([]string, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list models failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	models := []string{}
	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		// OpenAI structure
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			for _, item := range dataArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					}
				}
			}
			return models, nil
		}
		// Custom models key
		if modelsArr, ok := parsed["models"].([]interface{}); ok {
			for _, item := range modelsArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					} else if name, ok := itemMap["name"].(string); ok {
						models = append(models, name)
					}
				}
			}
			return models, nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		for _, item := range parsedArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, ok := itemMap["id"].(string); ok {
					models = append(models, id)
				}
			}
		}
		return models, nil
	}

	return nil, fmt.Errorf("unexpected models structure")
}

func (s *Service) healthCheckSingleModel(ctx context.Context, endpointID, baseURL, apiKey, model string, timeout time.Duration) HealthRecord {
	startTime := time.Now()
	record := HealthRecord{
		Model:     model,
		Status:    "failed",
		CheckedAt: startTime.Format(time.RFC3339),
	}

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))
	payload := map[string]interface{}{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"stream":   true,
	}
	bodyBytes, _ := json.Marshal(payload)

	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(childCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		record.Error = err.Error()
		return record
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		record.Error = err.Error()
		record.Latency = time.Since(startTime).Milliseconds()
		return record
	}
	defer resp.Body.Close()

	record.StatusCode = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		record.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))
		record.Latency = time.Since(startTime).Milliseconds()
		return record
	}

	// Read first chunk
	buf := make([]byte, 256)
	_, _ = resp.Body.Read(buf) // Ignore error, we just care if we read something

	latency := time.Since(startTime)
	record.Latency = latency.Milliseconds()
	if latency <= degradedThreshold {
		record.Status = "operational"
	} else {
		record.Status = "degraded"
	}

	return record
}

func (s *Service) runBatchHealthCheck(ctx context.Context, endpointID, baseURL, apiKey string, models []string, timeout time.Duration, concurrency int) HealthSummary {
	var mu sync.Mutex
	results := []HealthRecord{}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := s.healthCheckSingleModel(ctx, endpointID, baseURL, apiKey, m, timeout)

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(model)
	}

	wg.Wait()

	operationalCount := 0
	degradedCount := 0
	failedCount := 0

	for _, r := range results {
		switch r.Status {
		case "operational":
			operationalCount++
		case "degraded":
			degradedCount++
		default:
			failedCount++
		}
	}

	overall := "unknown"
	if len(results) > 0 {
		if failedCount == len(results) {
			overall = "failed"
		} else if operationalCount == len(results) {
			overall = "operational"
		} else {
			overall = "degraded"
		}
	}

	return HealthSummary{
		TotalModels:   len(models),
		Operational:   operationalCount,
		Degraded:      degradedCount,
		Failed:        failedCount,
		OverallStatus: overall,
		Results:       results,
		CheckedAt:     time.Now().Format(time.RFC3339),
	}
}

func (s *Service) randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			applog.Warn(context.Background(), "openai", "secure random failed, using timestamp fallback", "error", err.Error())
			b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
			continue
		}
		b[i] = letters[num.Int64()]
	}
	return string(b)
}

// Personas Handlers
func (s *Service) listPersonas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, icon, system_prompt, is_default, created_at FROM openai_chat_personas ORDER BY is_default DESC, created_at DESC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Persona struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
		IsDefault    int    `json:"is_default"`
		CreatedAt    string `json:"created_at"`
	}

	var list []Persona
	for rows.Next() {
		var p Persona
		var icon sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &icon, &p.SystemPrompt, &p.IsDefault, &p.CreatedAt); err == nil {
			p.Icon = icon.String
			list = append(list, p)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createPersona(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Name == "" || body.SystemPrompt == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "name and system_prompt are required"})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	createdAt := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_personas (id, name, icon, system_prompt, is_default, created_at) VALUES (?, ?, ?, ?, 0, ?)",
		id, body.Name, body.Icon, body.SystemPrompt, createdAt)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) updatePersona(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var body struct {
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Name == "" || body.SystemPrompt == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "name and system_prompt are required"})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "UPDATE openai_chat_personas SET name = ?, icon = ?, system_prompt = ? WHERE id = ?",
		body.Name, body.Icon, body.SystemPrompt, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deletePersona(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	if id == "1" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete default persona"})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_personas WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Sessions Handlers
func (s *Service) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, title, model, endpoint_id, persona_id, system_prompt, created_at, updated_at FROM openai_chat_sessions ORDER BY updated_at DESC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Session struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Model        string `json:"model"`
		EndpointID   string `json:"endpoint_id"`
		PersonaID    string `json:"persona_id"`
		SystemPrompt string `json:"system_prompt"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
	}

	var list []Session
	for rows.Next() {
		var s Session
		var model, epID, persID, sysPrompt sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &model, &epID, &persID, &sysPrompt, &s.CreatedAt, &s.UpdatedAt); err == nil {
			s.Model = model.String
			s.EndpointID = epID.String
			s.PersonaID = persID.String
			s.SystemPrompt = sysPrompt.String
			list = append(list, s)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Model        string `json:"model"`
		EndpointID   string `json:"endpoint_id"`
		PersonaID    string `json:"persona_id"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	title := body.Title
	if title == "" {
		title = "新对话"
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_sessions (id, title, model, endpoint_id, persona_id, system_prompt, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, title, body.Model, body.EndpointID, body.PersonaID, body.SystemPrompt, now, now)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) updateSession(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var body struct {
		Title        *string `json:"title"`
		Model        *string `json:"model"`
		EndpointID   *string `json:"endpoint_id"`
		PersonaID    *string `json:"persona_id"`
		SystemPrompt *string `json:"system_prompt"`
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

	// Fetch current session values first
	var currentTitle, currentModel, currentEpID, currentPersID, currentSysPrompt string
	err = db.QueryRowContext(ctx, "SELECT title, model, endpoint_id, persona_id, system_prompt FROM openai_chat_sessions WHERE id = ?", id).
		Scan(&currentTitle, &currentModel, &currentEpID, &currentPersID, &currentSysPrompt)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	title := currentTitle
	if body.Title != nil {
		title = *body.Title
	}
	model := currentModel
	if body.Model != nil {
		model = *body.Model
	}
	epID := currentEpID
	if body.EndpointID != nil {
		epID = *body.EndpointID
	}
	persID := currentPersID
	if body.PersonaID != nil {
		persID = *body.PersonaID
	}
	sysPrompt := currentSysPrompt
	if body.SystemPrompt != nil {
		sysPrompt = *body.SystemPrompt
	}

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "UPDATE openai_chat_sessions SET title = ?, model = ?, endpoint_id = ?, persona_id = ?, system_prompt = ?, updated_at = ? WHERE id = ?",
		title, model, epID, persID, sysPrompt, now, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_sessions WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) clearSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_sessions")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Messages Handlers
func (s *Service) listSessionMessages(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, role, content, reasoning, timestamp FROM openai_chat_messages WHERE session_id = ? ORDER BY timestamp ASC", sessionId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Message struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning,omitempty"`
		Timestamp string `json:"timestamp"`
	}

	var list []Message
	for rows.Next() {
		var m Message
		var reasoning sql.NullString
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &reasoning, &m.Timestamp); err == nil {
			m.Reasoning = reasoning.String
			list = append(list, m)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createSessionMessage(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	var body struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Role == "" || body.Content == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "role and content are required"})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	timestamp := body.Timestamp
	if timestamp == "" {
		timestamp = time.Now().Format(time.RFC3339)
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// Insert message
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_messages (id, session_id, role, content, reasoning, timestamp) VALUES (?, ?, ?, ?, ?, ?)",
		id, sessionId, body.Role, body.Content, body.Reasoning, timestamp)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update session updated_at timestamp
	now := time.Now().Format(time.RFC3339)
	_, _ = db.ExecContext(ctx, "UPDATE openai_chat_sessions SET updated_at = ? WHERE id = ?", now, sessionId)

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) deleteSessionMessage(w http.ResponseWriter, r *http.Request, sessionId, msgId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_messages WHERE session_id = ? AND id = ?", sessionId, msgId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) clearSessionMessages(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_messages WHERE session_id = ?", sessionId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
