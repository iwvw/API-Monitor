package dockerhub

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const requestTimeout = 30 * time.Second

type Service struct {
	cfg    config.Config
	store  *database.Store
	schema database.SchemaEnsurer
	apiURL string
	client *http.Client
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:    cfg,
		store:  database.New(cfg),
		apiURL: envURL("DOCKERHUB_API_URL", "https://hub.docker.com/v2"),
		client: &http.Client{Timeout: requestTimeout},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dockerhub")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodDelete:
		s.deleteAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "repositories" && r.Method == http.MethodGet:
		s.accountRepositories(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "search" && r.Method == http.MethodGet:
		s.searchRepositories(w, r)
	case len(parts) == 4 && parts[0] == "repositories" && parts[3] == "tags" && r.Method == http.MethodGet:
		s.repositoryTags(w, r, parts[1], parts[2])
	case len(parts) == 3 && parts[0] == "repositories" && r.Method == http.MethodGet:
		s.repositoryDetail(w, r, parts[1], parts[2])
	default:
		response.Error(w, http.StatusNotFound, "dockerhub route not implemented")
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
		`CREATE TABLE IF NOT EXISTS dockerhub_accounts (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			token_encrypted TEXT NOT NULL,
			created_at INTEGER,
			updated_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dockerhub_accounts_created_at ON dockerhub_accounts(created_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure dockerhub schema: %w", err)
		}
	}
	return nil
}

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		accounts, err := loadAccounts(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": accounts})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		username := strings.TrimSpace(stringValue(payload["username"], ""))
		token := cleanToken(stringValue(payload["token"], ""))
		if username == "" || token == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "用户名和访问令牌必填"})
			return
		}
		if err := s.verifyCredentials(r.Context(), username, token); err != nil {
			response.Error(w, http.StatusBadRequest, "令牌验证失败: "+err.Error())
			return
		}
		encrypted, err := secure.SecureEncrypt(token)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		account, err := createAccount(r.Context(), db, map[string]interface{}{
			"username":        username,
			"token_encrypted": encrypted,
		})
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": safeAccount(account)})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(r.Context(), `DELETE FROM dockerhub_accounts WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) verifyAccount(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	account, _, err := loadAccount(r.Context(), db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "账号不存在")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	username := stringValue(account["username"], "")
	token := secure.SecureDecrypt(stringValue(account["token_encrypted"], ""))
	if err := s.verifyCredentials(r.Context(), username, token); err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "valid": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "valid": true})
}

func (s *Service) accountRepositories(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	account, _, err := loadAccount(r.Context(), db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "账号不存在")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	username := stringValue(account["username"], "")
	token := secure.SecureDecrypt(stringValue(account["token_encrypted"], ""))
	pageSize := strings.TrimSpace(r.URL.Query().Get("page_size"))
	if pageSize == "" {
		pageSize = "100"
	}
	target := s.apiURL + "/repositories/" + url.PathEscape(username) + "/?page_size=" + url.QueryEscape(pageSize)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Authorization", basicAuth(username, token))
	req.Header.Set("Accept", "application/json")
	res, err := s.doWithRetry(r.Context(), req)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 5*1024*1024))
	var payload interface{} = map[string]interface{}{}
	if strings.TrimSpace(string(data)) != "" {
		if err := json.Unmarshal(data, &payload); err != nil {
			response.Error(w, http.StatusBadGateway, "invalid JSON response")
			return
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		response.Error(w, http.StatusBadGateway, errorMessage(payload, fmt.Sprintf("HTTP %d", res.StatusCode)))
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    objectSlice(objectValue(payload)["results"]),
		"count":   intValue(objectValue(payload)["count"], 0),
	})
}

func (s *Service) searchRepositories(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少搜索关键词"})
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	pageSize := strings.TrimSpace(r.URL.Query().Get("page_size"))
	if pageSize == "" {
		pageSize = "25"
	}
	target := s.apiURL + "/search/repositories/?query=" + url.QueryEscape(query) + "&page_size=" + url.QueryEscape(pageSize) + "&page=1"
	if namespace != "" {
		target += "&namespace=" + url.QueryEscape(namespace)
	}
	username, token, err := s.accountCredentials(r, r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload, err := s.httpAny(r.Context(), http.MethodGet, target, username, token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	results := objectSlice(objectValue(payload)["results"])
	count := 0
	for _, item := range results {
		if boolValue(item["is_official"]) {
			count++
		}
	}
	s.enrichSearchResults(r.Context(), username, token, results)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    results,
		"count":   count,
	})
}

// enrichSearchResults 为搜索结果补充仓库大小与更新时间（搜索接口本身不返回）。
// 并发拉取每个仓库的详情，失败静默跳过，避免单个仓库异常拖垮整体。
func (s *Service) enrichSearchResults(ctx context.Context, username, token string, results []map[string]interface{}) {
	if len(results) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, item := range results {
		wg.Add(1)
		go func(repo map[string]interface{}) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			name := stringValue(repo["repo_name"], "")
			if name == "" {
				return
			}
			parts := strings.Split(name, "/")
			namespace, repoName := "library", name
			if len(parts) == 2 {
				namespace, repoName = parts[0], parts[1]
			}
			detail, err := s.fetchRepositoryDetail(ctx, username, token, namespace, repoName)
			if err != nil {
				return
			}
			repo["storage_size"] = detail["storage_size"]
			repo["last_updated"] = detail["last_updated"]
		}(item)
	}
	wg.Wait()
}

func (s *Service) fetchRepositoryDetail(ctx context.Context, username, token, namespace, name string) (map[string]interface{}, error) {
	target := s.apiURL + "/repositories/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/"
	payload, err := s.httpAny(ctx, http.MethodGet, target, username, token)
	if err != nil {
		return nil, err
	}
	return objectValue(payload), nil
}

func (s *Service) repositoryTags(w http.ResponseWriter, r *http.Request, namespace, name string) {
	pageSize := strings.TrimSpace(r.URL.Query().Get("page_size"))
	if pageSize == "" {
		pageSize = "30"
	}
	username, token, err := s.accountCredentials(r, r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	target := s.apiURL + "/repositories/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/tags/?page_size=" + url.QueryEscape(pageSize)
	payload, err := s.httpAny(r.Context(), http.MethodGet, target, username, token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    objectSlice(objectValue(payload)["results"]),
	})
}

func (s *Service) repositoryDetail(w http.ResponseWriter, r *http.Request, namespace, name string) {
	username, token, err := s.accountCredentials(r, r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	target := s.apiURL + "/repositories/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/"
	payload, err := s.httpAny(r.Context(), http.MethodGet, target, username, token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": payload})
}

func (s *Service) accountCredentials(r *http.Request, ctx context.Context) (string, string, error) {
	accountID := strings.TrimSpace(r.URL.Query().Get("accountId"))
	if accountID == "" {
		return "", "", nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return "", "", err
	}
	defer db.Close()
	account, _, err := loadAccount(ctx, db, accountID)
	if err != nil {
		return "", "", err
	}
	username := stringValue(account["username"], "")
	token := secure.SecureDecrypt(stringValue(account["token_encrypted"], ""))
	return username, token, nil
}

func (s *Service) verifyCredentials(ctx context.Context, username, token string) error {
	target := s.apiURL + "/repositories/" + url.PathEscape(username) + "/?page_size=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", basicAuth(username, token))
	req.Header.Set("Accept", "application/json")
	res, err := s.doWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}

func (s *Service) httpAny(ctx context.Context, method, target, username, token string) (interface{}, error) {
	var reader io.Reader
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if cleanToken(token) != "" && username != "" {
		req.Header.Set("Authorization", basicAuth(username, token))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "API-Monitor/1.0")
	res, err := s.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 5*1024*1024))
	var payload interface{} = map[string]interface{}{}
	if strings.TrimSpace(string(data)) != "" {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, errors.New("Invalid JSON response")
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New(errorMessage(payload, fmt.Sprintf("HTTP %d", res.StatusCode)))
	}
	return payload, nil
}

// doWithRetry 对网络错误与可重试的 5xx/429 做小幅重试，缓解 Docker Hub 偶发限流或连接重置。
func (s *Service) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	attempts := 2
	for attempt := 0; attempt < attempts; attempt++ {
		res, err := s.client.Do(req.Clone(ctx))
		if err != nil {
			if attempt < attempts-1 {
				select {
				case <-ctx.Done():
					return nil, err
				case <-time.After(250 * time.Millisecond):
				}
				continue
			}
			return nil, err
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			res.Body.Close()
			if attempt < attempts-1 {
				select {
				case <-ctx.Done():
				case <-time.After(300 * time.Millisecond):
				}
				continue
			}
			return &http.Response{Header: res.Header, StatusCode: res.StatusCode, Body: http.NoBody}, nil
		}
		return res, nil
	}
	return nil, errors.New("Docker Hub request failed after retries")
}

func basicAuth(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM dockerhub_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, safeAccount(row))
	}
	return out, rows.Err()
}

func loadAccount(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM dockerhub_accounts WHERE id = ?`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, false, err
		}
		return nil, false, sql.ErrNoRows
	}
	row, err := scanMap(rows)
	return row, true, err
}

func createAccount(ctx context.Context, db *sql.DB, data map[string]interface{}) (map[string]interface{}, error) {
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `
		INSERT INTO dockerhub_accounts (id, username, token_encrypted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, data["username"], data["token_encrypted"], now, now)
	if err != nil {
		return nil, err
	}
	account, _, err := loadAccount(ctx, db, id)
	return account, err
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	out := copyMap(account)
	delete(out, "token_encrypted")
	return out
}

func scanMap(rows *sql.Rows) (map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	dest := make([]interface{}, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	row := map[string]interface{}{}
	for i, column := range columns {
		if bytes, ok := values[i].([]byte); ok {
			row[column] = string(bytes)
		} else {
			row[column] = values[i]
		}
	}
	return row, nil
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return map[string]interface{}{}, nil
	}
	payload := map[string]interface{}{}
	return payload, json.Unmarshal(body, &payload)
}

func envURL(name, fallback string) string {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv(name)), "/")
	if value == "" {
		return fallback
	}
	return value
}

func cleanToken(token string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", "", "\n", "", "\t", "").Replace(token))
}

func stringValue(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return fallback
		}
		return text
	}
	text := fmt.Sprint(value)
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func boolValue(value interface{}) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	text := strings.ToLower(strings.TrimSpace(stringValue(value, "")))
	return text == "true" || text == "1" || text == "yes"
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	text := strings.TrimSpace(stringValue(value, ""))
	if text == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(text, "%d", &parsed); err == nil {
		return parsed
	}
	return fallback
}

func objectValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return map[string]interface{}{}
}

func objectSlice(value interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, item := range arrayValue(value) {
		if object, ok := item.(map[string]interface{}); ok {
			out = append(out, object)
		}
	}
	return out
}

func arrayValue(value interface{}) []interface{} {
	if typed, ok := value.([]interface{}); ok {
		return typed
	}
	return []interface{}{}
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func errorMessage(value interface{}, fallback string) string {
	object := objectValue(value)
	for _, key := range []string{"error", "message", "detail"} {
		if text := stringValue(object[key], ""); text != "" {
			return text
		}
	}
	errorsValue := arrayValue(object["errors"])
	if len(errorsValue) > 0 {
		if first, ok := errorsValue[0].(map[string]interface{}); ok {
			return stringValue(first["message"], fallback)
		}
	}
	return fallback
}