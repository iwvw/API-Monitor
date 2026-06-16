package koyeb

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
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	defaultAPIBase = "https://app.koyeb.com"
	requestTimeout = 15 * time.Second
)

type Service struct {
	cfg     config.Config
	store   *database.Store
	apiBase string
	client  *http.Client
}

func New(cfg config.Config) *Service {
	apiBase := strings.TrimRight(os.Getenv("KOYEB_API_BASE_URL"), "/")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	service := &Service{
		cfg:     cfg,
		store:   database.New(cfg),
		apiBase: apiBase,
		client:  &http.Client{Timeout: requestTimeout},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/koyeb")
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
	case len(parts) == 2 && parts[0] == "accounts":
		s.deleteAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "refresh" && r.Method == http.MethodPost:
		s.refreshAccount(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "data" && r.Method == http.MethodGet:
		s.data(w, r)
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "pause" && r.Method == http.MethodPost:
		s.serviceAction(w, r, parts[1], "pause")
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "restart" && r.Method == http.MethodPost:
		s.serviceAction(w, r, parts[1], "restart")
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "redeploy" && r.Method == http.MethodPost:
		s.serviceAction(w, r, parts[1], "redeploy")
	case len(parts) == 2 && parts[0] == "services" && r.Method == http.MethodDelete:
		s.serviceAction(w, r, parts[1], "delete")
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "rename" && r.Method == http.MethodPost:
		s.renameService(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "logs" && r.Method == http.MethodGet:
		s.serviceLogs(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "instances" && r.Method == http.MethodGet:
		s.serviceInstances(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "services" && parts[2] == "metrics" && r.Method == http.MethodGet:
		s.serviceMetrics(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "apps" && r.Method == http.MethodDelete:
		s.deleteApp(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "apps" && parts[2] == "rename" && r.Method == http.MethodPost:
		s.renameApp(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "usage" && r.Method == http.MethodGet:
		s.usage(w, r)
	default:
		response.Error(w, http.StatusNotFound, "koyeb route not implemented")
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
		`CREATE TABLE IF NOT EXISTS koyeb_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			token TEXT NOT NULL,
			email TEXT DEFAULT '',
			balance REAL DEFAULT 0,
			status TEXT DEFAULT 'unknown',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_koyeb_accounts_name ON koyeb_accounts(name)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure koyeb schema: %w", err)
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
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": safe})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		token := cleanToken(stringValue(payload["token"], ""))
		if name == "" || token == "" {
			response.Error(w, http.StatusBadRequest, "Name and Token are required")
			return
		}
		data, err := s.fetchAccountData(r.Context(), token)
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
			"name":    name,
			"token":   token,
			"email":   stringValue(objectValue(data["user"])["email"], ""),
			"balance": floatValue(data["balance"], 0),
			"status":  "active",
		})
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"account": map[string]interface{}{
				"id":     account["id"],
				"name":   account["name"],
				"email":  account["email"],
				"status": account["status"],
			},
			"data": data,
		})
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

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, idText string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(r.Context(), `DELETE FROM koyeb_accounts WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) refreshAccount(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText, false)
	if !ok {
		return
	}
	defer db.Close()
	data, err := s.fetchAccountData(r.Context(), stringValue(account["token"], ""))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = updateAccount(r.Context(), db, int64Value(account["id"], 0), map[string]interface{}{
		"balance": floatValue(data["balance"], 0),
		"email":   stringValue(objectValue(data["user"])["email"], stringValue(account["email"], "")),
		"status":  "active",
	})
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": data})
}

func (s *Service) data(w http.ResponseWriter, r *http.Request) {
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
		data, err := s.fetchAccountData(r.Context(), stringValue(account["token"], ""))
		if err != nil {
			_, _ = updateAccount(r.Context(), db, int64Value(account["id"], 0), map[string]interface{}{"status": "error"})
			results = append(results, map[string]interface{}{
				"id":       account["id"],
				"name":     account["name"],
				"data":     nil,
				"projects": []interface{}{},
				"balance":  nil,
				"error":    err.Error(),
			})
			continue
		}
		_, _ = updateAccount(r.Context(), db, int64Value(account["id"], 0), map[string]interface{}{
			"balance": floatValue(data["balance"], 0),
			"email":   stringValue(objectValue(data["user"])["email"], stringValue(account["email"], "")),
			"status":  "active",
		})
		results = append(results, map[string]interface{}{
			"id":           account["id"],
			"name":         account["name"],
			"data":         data["user"],
			"projects":     firstNonNil(data["projects"], []interface{}{}),
			"balance":      data["balance"],
			"organization": data["organization"],
			"error":        nil,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": results})
}

func (s *Service) serviceAction(w http.ResponseWriter, r *http.Request, serviceID, action string) {
	payload, _ := readObject(r)
	accountID := stringValue(payload["accountId"], "")
	account, db, ok := s.accountForRequest(w, r, accountID, false)
	if !ok {
		return
	}
	defer db.Close()
	token := stringValue(account["token"], "")
	var err error
	switch action {
	case "pause":
		err = s.pauseService(r.Context(), token, serviceID)
	case "restart":
		err = s.restartService(r.Context(), token, serviceID)
	case "redeploy":
		_, err = s.koyebRequest(r.Context(), token, "/services/"+url.PathEscape(serviceID)+"/redeploy", http.MethodPost, nil)
	case "delete":
		_, err = s.koyebRequest(r.Context(), token, "/services/"+url.PathEscape(serviceID), http.MethodDelete, nil)
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) renameService(w http.ResponseWriter, r *http.Request, serviceID string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""), false)
	if !ok {
		return
	}
	defer db.Close()
	_, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), "/services/"+url.PathEscape(serviceID), http.MethodPatch, map[string]interface{}{"name": payload["name"]})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) renameApp(w http.ResponseWriter, r *http.Request, appID string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""), false)
	if !ok {
		return
	}
	defer db.Close()
	_, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), "/apps/"+url.PathEscape(appID), http.MethodPatch, map[string]interface{}{"name": payload["name"]})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteApp(w http.ResponseWriter, r *http.Request, appID string) {
	payload, _ := readObject(r)
	account, db, ok := s.accountForRequest(w, r, stringValue(payload["accountId"], ""), false)
	if !ok {
		return
	}
	defer db.Close()
	_, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), "/apps/"+url.PathEscape(appID), http.MethodDelete, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) serviceLogs(w http.ResponseWriter, r *http.Request, serviceID string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"), false)
	if !ok {
		return
	}
	defer db.Close()
	limit := intValue(r.URL.Query().Get("limit"), 100)
	logs, err := s.fetchServiceLogs(r.Context(), stringValue(account["token"], ""), serviceID, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "logs": logs})
}

func (s *Service) serviceInstances(w http.ResponseWriter, r *http.Request, serviceID string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"), false)
	if !ok {
		return
	}
	defer db.Close()
	payload, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), "/instances?service_id="+url.QueryEscape(serviceID), http.MethodGet, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "instances": arrayValue(payload["instances"])})
}

func (s *Service) serviceMetrics(w http.ResponseWriter, r *http.Request, serviceID string) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"), false)
	if !ok {
		return
	}
	defer db.Close()
	query := r.URL.Query()
	metricName := stringFallback(query.Get("name"), "CPU_TOTAL_PERCENT")
	path := "/streams/metrics?name=" + url.QueryEscape(metricName)
	if instanceID := query.Get("instanceId"); instanceID != "" {
		path += "&instance_id=" + url.QueryEscape(instanceID)
	} else {
		path += "&service_id=" + url.QueryEscape(serviceID)
	}
	if start := query.Get("start"); start != "" {
		path += "&start=" + url.QueryEscape(start)
	}
	if end := query.Get("end"); end != "" {
		path += "&end=" + url.QueryEscape(end)
	}
	payload, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), path, http.MethodGet, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "metrics": arrayValue(payload["metrics"])})
}

func (s *Service) usage(w http.ResponseWriter, r *http.Request) {
	account, db, ok := s.accountForRequest(w, r, r.URL.Query().Get("accountId"), false)
	if !ok {
		return
	}
	defer db.Close()
	params := []string{}
	if start := r.URL.Query().Get("start"); start != "" {
		params = append(params, "starting_time="+url.QueryEscape(start))
	}
	if end := r.URL.Query().Get("end"); end != "" {
		params = append(params, "ending_time="+url.QueryEscape(end))
	}
	path := "/usages"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	payload, err := s.koyebRequest(r.Context(), stringValue(account["token"], ""), path, http.MethodGet, nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "usage": payload})
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, idText string, keepOpen bool) (map[string]interface{}, *sql.DB, bool) {
	id, err := parseID(idText)
	if err != nil {
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
		response.Error(w, http.StatusNotFound, "Account not found")
		return nil, nil, false
	}
	if !keepOpen {
		return account, db, true
	}
	return account, db, true
}

func (s *Service) koyebRequest(ctx context.Context, token, path, method string, body interface{}) (map[string]interface{}, error) {
	cleaned := cleanToken(token)
	if cleaned == "" {
		return nil, errors.New("missing Koyeb token")
	}
	target := s.apiBase + "/v1" + path
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
	req.Header.Set("Authorization", "Bearer "+cleaned)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 5*1024*1024))
	payload := map[string]interface{}{}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, errors.New("Invalid JSON response")
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New(stringValue(firstNonNil(payload["message"], payload["error"]), fmt.Sprintf("HTTP %d", res.StatusCode)))
	}
	return payload, nil
}

func (s *Service) fetchAccountData(ctx context.Context, token string) (map[string]interface{}, error) {
	appsPayload, err := s.koyebRequest(ctx, token, "/apps", http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	apps := objectSlice(appsPayload["apps"])
	var profile map[string]interface{}
	if profilePayload, err := s.koyebRequest(ctx, token, "/account/profile", http.MethodGet, nil); err == nil {
		profile = objectValue(profilePayload["user"])
	}
	var organization map[string]interface{}
	if orgPayload, err := s.koyebRequest(ctx, token, "/organizations", http.MethodGet, nil); err == nil {
		orgs := objectSlice(orgPayload["organizations"])
		if len(orgs) > 0 {
			organization = orgs[0]
		}
	}
	if len(profile) == 0 && len(organization) > 0 {
		profile = map[string]interface{}{
			"id":         organization["id"],
			"name":       organization["name"],
			"email":      stringValue(organization["email"], ""),
			"avatar_url": organization["avatar_url"],
		}
	}
	if len(profile) == 0 {
		profile = map[string]interface{}{"id": "unknown", "name": "Koyeb User", "email": ""}
	}
	projects := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		projects = append(projects, s.fetchAppProject(ctx, token, app))
	}
	return map[string]interface{}{
		"user": map[string]interface{}{
			"_id":      firstNonNil(profile["id"], organization["id"]),
			"username": stringFallback(stringValue(firstNonNil(profile["name"], organization["name"]), ""), "Unknown"),
			"email":    stringValue(firstNonNil(profile["email"], organization["email"]), ""),
		},
		"organization": organization,
		"projects":     projects,
		"balance":      floatValue(organization["remaining_credits"], 0),
	}, nil
}

func (s *Service) fetchAppProject(ctx context.Context, token string, app map[string]interface{}) map[string]interface{} {
	appID := stringValue(app["id"], "")
	servicesPayload, err := s.koyebRequest(ctx, token, "/services?app_id="+url.QueryEscape(appID), http.MethodGet, nil)
	if err != nil {
		return map[string]interface{}{
			"_id":       appID,
			"name":      app["name"],
			"region":    "unknown",
			"services":  []interface{}{},
			"createdAt": app["created_at"],
			"updatedAt": app["updated_at"],
			"error":     err.Error(),
		}
	}
	services := objectSlice(servicesPayload["services"])
	outServices := make([]map[string]interface{}, 0, len(services))
	for _, service := range services {
		outServices = append(outServices, s.fetchServiceDetails(ctx, token, app, service))
	}
	region := "unknown"
	if len(services) > 0 {
		firstServiceID := stringValue(services[0]["id"], "")
		if instancePayload, err := s.koyebRequest(ctx, token, "/instances?service_id="+url.QueryEscape(firstServiceID)+"&limit=1", http.MethodGet, nil); err == nil {
			instances := objectSlice(instancePayload["instances"])
			if len(instances) > 0 {
				region = mapRegion(stringValue(instances[0]["region"], "unknown"))
			}
		}
	}
	return map[string]interface{}{
		"_id":       appID,
		"name":      app["name"],
		"region":    region,
		"services":  outServices,
		"createdAt": app["created_at"],
		"updatedAt": app["updated_at"],
	}
}

func (s *Service) fetchServiceDetails(ctx context.Context, token string, app, service map[string]interface{}) map[string]interface{} {
	serviceID := stringValue(service["id"], "")
	deploymentsPayload, err := s.koyebRequest(ctx, token, "/deployments?service_id="+url.QueryEscape(serviceID)+"&limit=1", http.MethodGet, nil)
	deployments := []map[string]interface{}{}
	if err == nil {
		deployments = objectSlice(deploymentsPayload["deployments"])
	}
	var latest interface{}
	if len(deployments) > 0 {
		latest = deployments[0]
	}
	domains := []map[string]interface{}{}
	if service["active_deployment"] != nil {
		appName := stringValue(app["name"], "")
		serviceName := stringValue(service["name"], "")
		if appName != "" && serviceName != "" {
			domains = append(domains, map[string]interface{}{"domain": appName + "-" + serviceName + ".koyeb.app", "isGenerated": true})
		}
	}
	for _, domain := range arrayValue(service["domains"]) {
		if domainMap, ok := domain.(map[string]interface{}); ok {
			domains = append(domains, map[string]interface{}{"domain": firstNonNil(domainMap["name"], domain), "isGenerated": false})
		} else {
			domains = append(domains, map[string]interface{}{"domain": domain, "isGenerated": false})
		}
	}
	instanceType := firstInstanceType(service)
	out := map[string]interface{}{
		"_id":    serviceID,
		"name":   service["name"],
		"status": mapStatus(stringValue(firstNonNil(service["status"], service["state"]), "")),
		"type":   stringFallback(stringValue(service["type"], ""), "web"),
		"resourceLimit": map[string]interface{}{
			"cpu":    cpuFromInstance(instanceType),
			"memory": memoryFromInstance(instanceType),
		},
		"domains":          domains,
		"latestDeployment": latest,
		"messages":         firstNonNil(service["messages"], []interface{}{}),
		"createdAt":        service["created_at"],
		"updatedAt":        service["updated_at"],
	}
	if err != nil {
		out["error"] = err.Error()
	}
	return out
}

func (s *Service) fetchServiceLogs(ctx context.Context, token, serviceID string, limit int) ([]interface{}, error) {
	payload, err := s.koyebRequest(ctx, token, "/streams/logs/query?service_id="+url.QueryEscape(serviceID)+"&type=runtime&limit="+strconv.Itoa(limit), http.MethodGet, nil)
	if err == nil {
		return arrayValue(payload["result"]), nil
	}
	fallback, fallbackErr := s.koyebRequest(ctx, token, "/logs?service_id="+url.QueryEscape(serviceID)+"&limit="+strconv.Itoa(limit)+"&order=desc", http.MethodGet, nil)
	if fallbackErr != nil {
		return nil, err
	}
	return arrayValue(fallback["logs"]), nil
}

func (s *Service) pauseService(ctx context.Context, token, serviceID string) error {
	_, _ = s.koyebRequest(ctx, token, "/services/"+url.PathEscape(serviceID), http.MethodGet, nil)
	_, err := s.koyebRequest(ctx, token, "/services/"+url.PathEscape(serviceID)+"/pause", http.MethodPost, nil)
	return err
}

func (s *Service) restartService(ctx context.Context, token, serviceID string) error {
	payload, err := s.koyebRequest(ctx, token, "/services/"+url.PathEscape(serviceID), http.MethodGet, nil)
	if err != nil {
		return err
	}
	service := objectValue(payload["service"])
	status := strings.ToUpper(stringValue(service["status"], ""))
	if status == "PAUSED" || status == "STOPPED" || status == "SUSPENDED" {
		_, err = s.koyebRequest(ctx, token, "/services/"+url.PathEscape(serviceID)+"/resume", http.MethodPost, nil)
	} else {
		_, err = s.koyebRequest(ctx, token, "/services/"+url.PathEscape(serviceID)+"/redeploy", http.MethodPost, nil)
	}
	return err
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM koyeb_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func loadAccount(ctx context.Context, db *sql.DB, id int64) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM koyeb_accounts WHERE id = ?`, id)
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
	result, err := db.ExecContext(ctx, `
		INSERT INTO koyeb_accounts (name, token, email, balance, status, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, data["name"], data["token"], stringValue(data["email"], ""), floatValue(data["balance"], 0), stringValue(data["status"], "unknown"))
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	account, _, err := loadAccount(ctx, db, id)
	return account, err
}

func updateAccount(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (map[string]interface{}, error) {
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	for key, column := range map[string]string{"name": "name", "token": "token", "email": "email", "status": "status"} {
		if value, ok := data[key]; ok {
			add(column, value)
		}
	}
	if value, ok := data["balance"]; ok {
		add("balance", floatValue(value, 0))
	}
	if len(fields) == 0 {
		return loadAccountOnly(ctx, db, id)
	}
	values = append(values, id)
	if _, err := db.ExecContext(ctx, `UPDATE koyeb_accounts SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
		return nil, err
	}
	return loadAccountOnly(ctx, db, id)
}

func loadAccountOnly(ctx context.Context, db *sql.DB, id int64) (map[string]interface{}, error) {
	account, _, err := loadAccount(ctx, db, id)
	return account, err
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":        account["id"],
		"name":      account["name"],
		"email":     stringValue(account["email"], ""),
		"status":    stringFallback(stringValue(account["status"], ""), "unknown"),
		"balance":   account["balance"],
		"createdAt": account["created_at"],
	}
}

func firstInstanceType(service map[string]interface{}) string {
	definition := objectValue(service["definition"])
	instanceTypes := arrayValue(definition["instance_types"])
	if len(instanceTypes) == 0 {
		return ""
	}
	first := objectValue(instanceTypes[0])
	return stringValue(first["type"], "")
}

func mapStatus(status string) string {
	statusMap := map[string]string{
		"STARTING": "STARTING", "HEALTHY": "RUNNING", "UNHEALTHY": "ERROR",
		"STOPPING": "STOPPING", "STOPPED": "SUSPENDED", "PAUSING": "STOPPING",
		"PAUSED": "SUSPENDED", "RESUMING": "STARTING", "ERRORED": "ERROR", "DELETING": "DELETING",
	}
	upper := strings.ToUpper(status)
	if mapped, ok := statusMap[upper]; ok {
		return mapped
	}
	if status == "" {
		return "UNKNOWN"
	}
	return status
}

func mapRegion(region string) string {
	if region == "" {
		return "未知地区"
	}
	regionMap := map[string]string{
		"was": "华盛顿", "fra": "法兰克福", "par": "巴黎", "sin": "新加坡", "tok": "东京", "sfo": "旧金山",
		"silicon valley": "硅谷", "united states": "美国", "germany": "德国", "france": "法国",
		"singapore": "新加坡", "japan": "日本", "washington": "华盛顿", "frankfurt": "法兰克福",
		"paris": "巴黎", "tokyo": "东京",
	}
	lower := strings.ToLower(region)
	if mapped, ok := regionMap[lower]; ok {
		return mapped
	}
	for key, mapped := range regionMap {
		if strings.Contains(lower, key) {
			return mapped
		}
	}
	return region
}

func cpuFromInstance(instanceType string) int {
	values := map[string]int{"free": 100, "nano": 100, "micro": 250, "small": 500, "medium": 1000, "large": 2000, "xlarge": 4000}
	return values[strings.ToLower(instanceType)]
}

func memoryFromInstance(instanceType string) int {
	values := map[string]int{"free": 256, "nano": 256, "micro": 512, "small": 1024, "medium": 2048, "large": 4096, "xlarge": 8192}
	return values[strings.ToLower(instanceType)]
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

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func cleanToken(token string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", "", "\n", "", "\t", "").Replace(token))
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
		if itemMap, ok := item.(map[string]interface{}); ok {
			out = append(out, itemMap)
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

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
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

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func int64Value(value interface{}, fallback int64) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func floatValue(value interface{}, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int64:
		return float64(typed)
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
