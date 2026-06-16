package flyio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const requestTimeout = 30 * time.Second

type Service struct {
	cfg         config.Config
	store       *database.Store
	graphqlURL  string
	machinesURL string
	logsURL     string
	client      *http.Client
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:         cfg,
		store:       database.New(cfg),
		graphqlURL:  envURL("FLY_GRAPHQL_URL", "https://api.fly.io/graphql"),
		machinesURL: envURL("FLY_MACHINES_URL", "https://api.machines.dev/v1"),
		logsURL:     envURL("FLY_LOGS_URL", "https://api.fly.io/api/v1"),
		client:      &http.Client{Timeout: requestTimeout},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/flyio")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "export" && r.Method == http.MethodGet:
		s.exportAccounts(w, r)
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodDelete:
		s.deleteAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "update-all-images" && r.Method == http.MethodPost:
		s.updateAllImages(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "proxy" && parts[1] == "apps" && r.Method == http.MethodGet:
		s.proxyApps(w, r)
	case len(parts) == 1 && parts[0] == "apps" && r.Method == http.MethodPost:
		s.createApp(w, r)
	case len(parts) == 2 && parts[0] == "apps" && r.Method == http.MethodDelete:
		s.deleteApp(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "rename" && r.Method == http.MethodPost:
		s.renameApp(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "redeploy" && r.Method == http.MethodPost:
		s.redeployApp(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "update-image" && r.Method == http.MethodPost:
		s.updateAppImage(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "machines" && r.Method == http.MethodGet:
		s.machines(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "events" && r.Method == http.MethodGet:
		s.events(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "config" && r.Method == http.MethodGet:
		s.config(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "logs" && r.Method == http.MethodGet:
		s.logs(w, r, parts[1])
	default:
		response.Error(w, http.StatusNotFound, "flyio route not implemented")
	}
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
		`CREATE TABLE IF NOT EXISTS fly_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			api_token TEXT NOT NULL,
			email TEXT,
			organization_id TEXT,
			created_at INTEGER,
			updated_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fly_accounts_created_at ON fly_accounts(created_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure flyio schema: %w", err)
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
		safe := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			safe = append(safe, safeAccount(account))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": safe})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		token := cleanToken(stringValue(payload["api_token"], ""))
		if name == "" || token == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "名称和 API Token 必填"})
			return
		}
		accountInfo, err := s.fetchAccountInfo(r.Context(), token)
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
			"name":            name,
			"api_token":       token,
			"email":           stringValue(accountInfo["email"], ""),
			"organization_id": stringValue(accountInfo["organization_id"], ""),
		})
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": account})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
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
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": accounts})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(r.Context(), `DELETE FROM fly_accounts WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) proxyApps(w http.ResponseWriter, r *http.Request) {
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
	results := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		payload, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), queryApps(), nil)
		if err != nil {
			results = append(results, map[string]interface{}{
				"accountId":   account["id"],
				"accountName": account["name"],
				"apps":        []interface{}{},
				"error":       err.Error(),
			})
			continue
		}
		apps := arrayAt(payload, "data", "apps", "nodes")
		results = append(results, map[string]interface{}{
			"accountId":   account["id"],
			"accountName": account["name"],
			"apps":        apps,
			"error":       nil,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": results})
}

func (s *Service) createApp(w http.ResponseWriter, r *http.Request) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""))
	if !ok {
		return
	}
	defer db.Close()
	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"name":           emptyToNil(stringValue(payload["name"], "")),
			"organizationId": emptyToNil(stringValue(account["organization_id"], "")),
		},
	}
	result, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), mutationCreateApp(), variables)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": objectAt(result, "data", "createApp", "app")})
}

func (s *Service) deleteApp(w http.ResponseWriter, r *http.Request, appName string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""))
	if !ok {
		return
	}
	defer db.Close()
	_, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), mutationDeleteApp(), map[string]interface{}{"appId": appName})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) renameApp(w http.ResponseWriter, r *http.Request, appName string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""))
	if !ok {
		return
	}
	defer db.Close()
	result, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), mutationUpdateApp(), map[string]interface{}{
		"input": map[string]interface{}{"appId": appName, "name": payload["newName"]},
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": objectAt(result, "data", "updateApp", "app")})
}

func (s *Service) redeployApp(w http.ResponseWriter, r *http.Request, appName string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""))
	if !ok {
		return
	}
	defer db.Close()
	machines, err := s.machineList(r.Context(), stringValue(account["api_token"], ""), appName)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(machines) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "No running machines found"})
		return
	}
	failed := 0
	for _, machine := range machines {
		if _, err := s.machine(r.Context(), stringValue(account["api_token"], ""), http.MethodPost, "/apps/"+url.PathEscape(appName)+"/machines/"+url.PathEscape(stringValue(machine["id"], ""))+"/restart", nil); err != nil {
			failed++
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "restarted": len(machines) - failed, "failed": failed})
}

func (s *Service) updateAppImage(w http.ResponseWriter, r *http.Request, appName string) {
	payload, _ := readObject(r)
	image := strings.TrimSpace(stringValue(payload["image"], ""))
	if image == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Image is required"})
		return
	}
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""))
	if !ok {
		return
	}
	defer db.Close()
	machines, err := s.machineList(r.Context(), stringValue(account["api_token"], ""), appName)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(machines) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "No machines found"})
		return
	}
	result := s.updateMachinesImage(r.Context(), stringValue(account["api_token"], ""), appName, machines, image)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": result.updated, "failed": result.failed, "errors": result.errors})
}

func (s *Service) machines(w http.ResponseWriter, r *http.Request, appName string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"))
	if !ok {
		return
	}
	defer db.Close()
	machines, err := s.machineList(r.Context(), stringValue(account["api_token"], ""), appName)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": machines})
}

func (s *Service) events(w http.ResponseWriter, r *http.Request, appName string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"))
	if !ok {
		return
	}
	defer db.Close()
	result, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), queryAppEvents(), map[string]interface{}{"appName": appName})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	releases := objectSlice(arrayAt(result, "data", "app", "releases", "nodes"))
	events := make([]map[string]interface{}, 0, len(releases))
	for _, release := range releases {
		events = append(events, map[string]interface{}{
			"timestamp": epochMillis(stringValue(release["createdAt"], "")),
			"message":   "Release v" + stringValue(release["version"], "") + " - " + stringValue(release["status"], "") + reasonSuffix(release["reason"]),
			"region":    "global",
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": events})
}

func (s *Service) config(w http.ResponseWriter, r *http.Request, appName string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"))
	if !ok {
		return
	}
	defer db.Close()
	result, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), queryAppConfig(), map[string]interface{}{"appName": appName})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": objectAt(result, "data", "app")})
}

func (s *Service) logs(w http.ResponseWriter, r *http.Request, appName string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"))
	if !ok {
		return
	}
	defer db.Close()
	machines, err := s.machineList(r.Context(), stringValue(account["api_token"], ""), appName)
	if err != nil || len(machines) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": []interface{}{}, "error": errorString(err)})
		return
	}
	logs, err := s.fetchLogs(r.Context(), stringValue(account["api_token"], ""), appName)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": []interface{}{}, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": logs})
}

func (s *Service) updateAllImages(w http.ResponseWriter, r *http.Request, accountID string) {
	payload, _ := readObject(r)
	image := stringValue(payload["image"], "")
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	result, err := s.graphql(r.Context(), stringValue(account["api_token"], ""), queryAppNames(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	apps := objectSlice(arrayAt(result, "data", "apps", "nodes"))
	if len(apps) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "No apps found", "updated": 0})
		return
	}
	details := []map[string]interface{}{}
	updated := 0
	failed := 0
	for _, app := range apps {
		appName := stringValue(app["name"], "")
		machines, err := s.machineList(r.Context(), stringValue(account["api_token"], ""), appName)
		if err != nil {
			failed++
			details = append(details, map[string]interface{}{"app": appName, "error": err.Error()})
			continue
		}
		if len(machines) == 0 {
			details = append(details, map[string]interface{}{"app": appName, "skipped": true})
			continue
		}
		machineResult := s.updateMachinesImage(r.Context(), stringValue(account["api_token"], ""), appName, machines, image)
		if machineResult.failed > 0 {
			failed++
			details = append(details, map[string]interface{}{"app": appName, "error": "machine update failed"})
			continue
		}
		updated++
		details = append(details, map[string]interface{}{"app": appName, "success": true})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "total": len(apps), "updated": updated, "failed": failed, "details": details})
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, id string) (map[string]interface{}, *sql.DB, bool) {
	if strings.TrimSpace(id) == "" {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return nil, nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return nil, nil, false
	}
	account, ok, err := loadAccount(r.Context(), db, id)
	if err != nil {
		db.Close()
		response.Error(w, http.StatusInternalServerError, err.Error())
		return nil, nil, false
	}
	if !ok {
		db.Close()
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Account not found"})
		return nil, nil, false
	}
	return account, db, true
}

func (s *Service) fetchAccountInfo(ctx context.Context, token string) (map[string]interface{}, error) {
	result, err := s.graphql(ctx, token, queryAccountInfo(), nil)
	if err != nil {
		return nil, err
	}
	orgs := objectSlice(arrayAt(result, "data", "organizations", "nodes"))
	orgID := ""
	if len(orgs) > 0 {
		orgID = stringValue(orgs[0]["id"], "")
	}
	return map[string]interface{}{
		"email":           stringValue(objectAt(result, "data", "viewer")["email"], ""),
		"organization_id": orgID,
	}, nil
}

func (s *Service) graphql(ctx context.Context, token, query string, variables map[string]interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{"query": query}
	if variables != nil {
		body["variables"] = variables
	} else {
		body["variables"] = map[string]interface{}{}
	}
	payload, err := s.httpJSON(ctx, http.MethodPost, s.graphqlURL, token, body)
	if err != nil {
		return nil, err
	}
	errorsValue := arrayValue(payload["errors"])
	if len(errorsValue) > 0 {
		if first, ok := errorsValue[0].(map[string]interface{}); ok {
			return nil, errors.New(stringValue(first["message"], "GraphQL error"))
		}
		return nil, errors.New("GraphQL error")
	}
	return payload, nil
}

func (s *Service) machineList(ctx context.Context, token, appName string) ([]map[string]interface{}, error) {
	payload, err := s.machine(ctx, token, http.MethodGet, "/apps/"+url.PathEscape(appName)+"/machines", nil)
	if err != nil {
		return nil, err
	}
	return objectSlice(payload), nil
}

func (s *Service) machine(ctx context.Context, token, method, path string, body interface{}) (interface{}, error) {
	return s.httpAny(ctx, method, s.machinesURL+path, token, body)
}

func (s *Service) fetchLogs(ctx context.Context, token, appName string) ([]map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.logsURL+"/apps/"+url.PathEscape(appName)+"/logs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cleanToken(token))
	req.Header.Set("Accept", "application/json, text/plain, */*")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 5*1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	out := []map[string]interface{}{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			item = map[string]interface{}{"message": line, "timestamp": time.Now().UTC().Format(time.RFC3339)}
		}
		if strings.TrimSpace(stringValue(item["message"], "")) == "" {
			continue
		}
		out = append(out, map[string]interface{}{
			"timestamp": stringFallback(stringValue(item["timestamp"], ""), time.Now().UTC().Format(time.RFC3339)),
			"message":   item["message"],
			"level":     stringFallback(stringValue(item["level"], ""), "info"),
			"instance":  stringValue(item["instance"], ""),
			"region":    stringValue(item["region"], ""),
		})
	}
	return out, nil
}

func (s *Service) httpJSON(ctx context.Context, method, target, token string, body interface{}) (map[string]interface{}, error) {
	payload, err := s.httpAny(ctx, method, target, token, body)
	if err != nil {
		return nil, err
	}
	if object, ok := payload.(map[string]interface{}); ok {
		return object, nil
	}
	return nil, errors.New("Invalid JSON response")
}

func (s *Service) httpAny(ctx context.Context, method, target, token string, body interface{}) (interface{}, error) {
	if cleanToken(token) == "" {
		return nil, errors.New("missing Fly.io token")
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cleanToken(token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "API-Monitor/1.0")
	res, err := s.client.Do(req)
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

type machineUpdateResult struct {
	updated int
	failed  int
	errors  []map[string]interface{}
}

func (s *Service) updateMachinesImage(ctx context.Context, token, appName string, machines []map[string]interface{}, image string) machineUpdateResult {
	result := machineUpdateResult{errors: []map[string]interface{}{}}
	for _, machine := range machines {
		machineID := stringValue(machine["id"], "")
		config := objectValue(machine["config"])
		targetImage := image
		if image == "latest" {
			current := stringValue(config["image"], "")
			if current != "" {
				if strings.Contains(current, ":") {
					targetImage = strings.Split(current, ":")[0] + ":latest"
				} else {
					targetImage = current + ":latest"
				}
			}
		}
		if targetImage == "" {
			targetImage = stringValue(config["image"], "")
		}
		updatedConfig := copyMap(config)
		updatedConfig["image"] = targetImage
		_, err := s.machine(ctx, token, http.MethodPost, "/apps/"+url.PathEscape(appName)+"/machines/"+url.PathEscape(machineID), map[string]interface{}{"config": updatedConfig})
		if err != nil {
			result.failed++
			result.errors = append(result.errors, map[string]interface{}{"error": true, "id": machineID, "message": err.Error()})
			continue
		}
		result.updated++
	}
	return result
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM fly_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func loadAccount(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM fly_accounts WHERE id = ?`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	return row, true, err
}

func createAccount(ctx context.Context, db *sql.DB, data map[string]interface{}) (map[string]interface{}, error) {
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `
		INSERT INTO fly_accounts (id, name, api_token, email, organization_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, data["name"], data["api_token"], emptyToNil(stringValue(data["email"], "")), emptyToNil(stringValue(data["organization_id"], "")), now, now)
	if err != nil {
		return nil, err
	}
	account, _, err := loadAccount(ctx, db, id)
	return account, err
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	out := copyMap(account)
	delete(out, "api_token")
	return out
}

func scanAll(rows *sql.Rows) ([]map[string]interface{}, error) {
	out := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
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

func emptyToNil(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func objectValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return map[string]interface{}{}
}

func objectAt(value interface{}, path ...string) map[string]interface{} {
	current := value
	for _, key := range path {
		current = objectValue(current)[key]
	}
	return objectValue(current)
}

func arrayAt(value interface{}, path ...string) []interface{} {
	current := value
	for _, key := range path {
		current = objectValue(current)[key]
	}
	return arrayValue(current)
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

func stringFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func errorString(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}

func errorMessage(value interface{}, fallback string) string {
	object := objectValue(value)
	for _, key := range []string{"error", "message"} {
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

func epochMillis(value string) int64 {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UnixMilli()
	}
	return 0
}

func reasonSuffix(value interface{}) string {
	reason := stringValue(value, "")
	if reason == "" {
		return ""
	}
	return ": " + reason
}

func queryAccountInfo() string {
	return `query {
		viewer { email }
		organizations { nodes { id slug name } }
	}`
}

func queryApps() string {
	return `query {
		apps {
			nodes {
				id name status deployed hostname appUrl
				organization { slug }
				currentRelease { createdAt status }
				machines { nodes { id region state } }
				certificates { nodes { hostname clientStatus } }
				ipAddresses { nodes { address type } }
			}
		}
	}`
}

func queryAppNames() string {
	return `query { apps { nodes { name } } }`
}

func queryAppEvents() string {
	return `query($appName: String!) {
		app(name: $appName) {
			releases(last: 20) {
				nodes { id version status reason createdAt user { email } }
			}
		}
	}`
}

func queryAppConfig() string {
	return `query($appName: String!) {
		app(name: $appName) {
			id name status hostname appUrl
			organization { slug name }
			regions { code name }
			currentRelease { version status createdAt }
			config { definition }
			secrets { name createdAt }
		}
	}`
}

func mutationCreateApp() string {
	return `mutation($input: CreateAppInput!) {
		createApp(input: $input) { app { id name status hostname } }
	}`
}

func mutationDeleteApp() string {
	return `mutation($appId: String!) {
		deleteApp(appId: $appId) { organization { id } }
	}`
}

func mutationUpdateApp() string {
	return `mutation($input: UpdateAppInput!) {
		updateApp(input: $input) { app { id name } }
	}`
}
