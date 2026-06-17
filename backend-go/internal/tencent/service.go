package tencent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	defaultRegion      = "ap-guangzhou"
	dnspodVersion      = "2021-03-23"
	cvmVersion         = "2017-03-12"
	lighthouseVersion  = "2020-03-24"
	monitorVersion     = "2018-07-24"
	requestTimeout     = 20 * time.Second
	authorizationAlgo  = "TC3-HMAC-SHA256"
	contentTypeTencent = "application/json; charset=utf-8"
)

type Service struct {
	cfg                config.Config
	store              *database.Store
	client             *http.Client
	apiBase            string
	dnspodEndpoint     string
	cvmEndpoint        string
	lighthouseEndpoint string
	monitorEndpoint    string
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:                cfg,
		store:              database.New(cfg),
		client:             &http.Client{Timeout: requestTimeout},
		apiBase:            cleanURL(os.Getenv("TENCENT_API_BASE_URL")),
		dnspodEndpoint:     envURL("TENCENT_DNSPOD_ENDPOINT", "https://dnspod.tencentcloudapi.com"),
		cvmEndpoint:        envURL("TENCENT_CVM_ENDPOINT", "https://cvm.tencentcloudapi.com"),
		lighthouseEndpoint: envURL("TENCENT_LIGHTHOUSE_ENDPOINT", "https://lighthouse.tencentcloudapi.com"),
		monitorEndpoint:    envURL("TENCENT_MONITOR_ENDPOINT", "https://monitor.tencentcloudapi.com"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tencent")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "domains":
		s.domains(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "domains" && r.Method == http.MethodDelete:
		s.deleteDomain(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "domains" && parts[4] == "records":
		s.domainRecords(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "domains" && parts[4] == "records" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.recordMutation(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "domains" && parts[4] == "records" && parts[6] == "status" && r.Method == http.MethodPatch:
		s.recordStatus(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "cvm" && r.Method == http.MethodGet:
		s.cvmInstances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "cvm" && parts[4] == "control" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3], "cvm")
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "lighthouse" && r.Method == http.MethodGet:
		s.lighthouseInstances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "lighthouse" && parts[4] == "control" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3], "lighthouse")
	default:
		response.Error(w, http.StatusNotFound, "tencent route not implemented")
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
		`CREATE TABLE IF NOT EXISTS tencent_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			secret_id TEXT NOT NULL,
			secret_key TEXT NOT NULL,
			region_id TEXT DEFAULT 'ap-guangzhou',
			description TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tencent_domains (
			domain_id TEXT PRIMARY KEY,
			domain_name TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			status TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES tencent_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tencent_accounts_created_at ON tencent_accounts(created_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure tencent schema: %w", err)
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
		secretID := strings.TrimSpace(stringValue(payload["secretId"], stringValue(payload["secret_id"], "")))
		secretKey := strings.TrimSpace(stringValue(payload["secretKey"], stringValue(payload["secret_key"], "")))
		regionID := strings.TrimSpace(stringValue(payload["regionId"], stringValue(payload["region_id"], defaultRegion)))
		if regionID == "" {
			regionID = defaultRegion
		}
		if name == "" || secretID == "" || secretKey == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing required fields"})
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		result, err := db.ExecContext(
			r.Context(),
			`INSERT INTO tencent_accounts (name, secret_id, secret_key, region_id, description) VALUES (?, ?, ?, ?, ?)`,
			name,
			secretID,
			secretKey,
			regionID,
			stringValue(payload["description"], ""),
		)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		id, _ := result.LastInsertId()
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid account id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM tencent_accounts WHERE id = ?`, id); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
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
				message = "Account not found"
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
		secretID := strings.TrimSpace(stringValue(payload["secretId"], stringValue(payload["secret_id"], "")))
		if secretID == "" || strings.Contains(secretID, "****") {
			secretID = stringValue(existing["secret_id"], "")
		}
		secretKey := strings.TrimSpace(stringValue(payload["secretKey"], stringValue(payload["secret_key"], "")))
		if secretKey == "" {
			secretKey = stringValue(existing["secret_key"], "")
		}
		regionID := strings.TrimSpace(stringValue(payload["regionId"], stringValue(payload["region_id"], stringValue(existing["region_id"], defaultRegion))))
		if regionID == "" {
			regionID = defaultRegion
		}
		description := stringValue(payload["description"], stringValue(existing["description"], ""))
		if name == "" || secretID == "" || secretKey == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing required fields"})
			return
		}
		if _, err := db.ExecContext(
			r.Context(),
			`UPDATE tencent_accounts SET name = ?, secret_id = ?, secret_key = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			name,
			secretID,
			secretKey,
			regionID,
			description,
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

func (s *Service) domains(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "DescribeDomainList", dnspodVersion, map[string]interface{}{})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "DescribeDomainList Failed: " + err.Error()})
			return
		}
		domains := normalizeDomains(arrayValue(firstPresent(result, "DomainList", "Domains", "domains")))
		total := firstNumber(result["DomainCount"], result["TotalCount"], len(domains))
		response.JSON(w, http.StatusOK, map[string]interface{}{"Domains": domains, "domains": domains, "DomainList": domains, "total": total})
	case http.MethodPost:
		payload, _ := readObject(r)
		domain := strings.TrimSpace(firstNonEmpty(stringValue(payload["domain"], ""), stringValue(payload["domainName"], "")))
		if domain == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing domain"})
			return
		}
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "CreateDomain", dnspodVersion, map[string]interface{}{"Domain": domain})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "CreateDomain Failed: " + err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteDomain(w http.ResponseWriter, r *http.Request, accountID, domain string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "DeleteDomain", dnspodVersion, map[string]interface{}{"Domain": domain})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "DeleteDomain Failed: " + err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) domainRecords(w http.ResponseWriter, r *http.Request, accountID, domain string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "DescribeRecordList", dnspodVersion, map[string]interface{}{"Domain": domain})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "DescribeRecordList Failed: " + err.Error()})
			return
		}
		records := normalizeRecords(arrayValue(firstPresent(result, "RecordList", "Records", "records")))
		total := firstNumber(result["RecordCount"], result["TotalCount"], len(records))
		response.JSON(w, http.StatusOK, map[string]interface{}{"Records": records, "records": records, "RecordList": records, "total": total})
	case http.MethodPost:
		payload, _ := readObject(r)
		params := map[string]interface{}{
			"Domain":     domain,
			"SubDomain":  stringValue(payload["subDomain"], stringValue(payload["SubDomain"], "")),
			"RecordType": stringValue(payload["recordType"], stringValue(payload["RecordType"], "")),
			"RecordLine": stringValue(payload["recordLine"], stringValue(payload["RecordLine"], "默认")),
			"Value":      stringValue(payload["value"], stringValue(payload["Value"], "")),
			"TTL":        numberOrString(payload["ttl"], payload["TTL"], 600),
			"MX":         optionalNumberOrString(payload["mx"], payload["MX"]),
			"Status":     "ENABLE",
		}
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "CreateRecord", dnspodVersion, compactParams(params))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "CreateRecord Failed: " + err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordMutation(w http.ResponseWriter, r *http.Request, accountID, domain, recordID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	recordValue := numberOrString(recordID, nil, recordID)
	switch r.Method {
	case http.MethodDelete:
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "DeleteRecord", dnspodVersion, map[string]interface{}{"Domain": domain, "RecordId": recordValue})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "DeleteRecord Failed: " + err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	case http.MethodPut:
		payload, _ := readObject(r)
		params := map[string]interface{}{
			"Domain":     domain,
			"RecordId":   recordValue,
			"SubDomain":  stringValue(payload["subDomain"], stringValue(payload["SubDomain"], "")),
			"RecordType": stringValue(payload["recordType"], stringValue(payload["RecordType"], "")),
			"RecordLine": stringValue(payload["recordLine"], stringValue(payload["RecordLine"], "默认")),
			"Value":      stringValue(payload["value"], stringValue(payload["Value"], "")),
			"TTL":        numberOrString(payload["ttl"], payload["TTL"], 600),
			"MX":         optionalNumberOrString(payload["mx"], payload["MX"]),
		}
		result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "ModifyRecord", dnspodVersion, compactParams(params))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "ModifyRecord Failed: " + err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordStatus(w http.ResponseWriter, r *http.Request, accountID, domain, recordID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	payload, _ := readObject(r)
	status := strings.ToUpper(stringValue(payload["status"], "DISABLE"))
	if status != "ENABLE" {
		status = "DISABLE"
	}
	result, err := s.callTencent(r.Context(), "dnspod", stringValue(account["region_id"], defaultRegion), "ModifyRecordStatus", dnspodVersion, map[string]interface{}{
		"Domain":   domain,
		"RecordId": numberOrString(recordID, nil, recordID),
		"Status":   status,
	})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "ModifyRecordStatus Failed: " + err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) cvmInstances(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	instances := s.listAllInstances(r.Context(), account, "cvm")
	response.JSON(w, http.StatusOK, map[string]interface{}{"instances": instances, "total": len(instances)})
}

func (s *Service) lighthouseInstances(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	instances := s.listAllInstances(r.Context(), account, "lighthouse")
	response.JSON(w, http.StatusOK, map[string]interface{}{"instances": instances, "total": len(instances)})
}

func (s *Service) instanceAction(w http.ResponseWriter, r *http.Request, accountID, instanceID, kind string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	payload, _ := readObject(r)
	region := firstNonEmpty(stringValue(payload["region"], ""), stringValue(payload["regionId"], ""), stringValue(account["region_id"], defaultRegion))
	action, err := actionName(kind, stringValue(payload["action"], ""))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result, err := s.callTencent(r.Context(), kind, region, action, serviceVersion(kind), map[string]interface{}{"InstanceIds": []string{instanceID}})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": fmt.Sprintf("%s %s Failed: %s", strings.ToUpper(kind), payload["action"], err.Error())})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) listAllInstances(ctx context.Context, account map[string]interface{}, kind string) []map[string]interface{} {
	regions := defaultCVMRegions()
	key := "InstanceSet"
	if kind == "lighthouse" {
		regions = defaultLighthouseRegions()
	}
	instances := []map[string]interface{}{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for _, region := range regions {
		region := region
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			result, err := s.callTencent(ctx, kind, region, "DescribeInstances", serviceVersion(kind), map[string]interface{}{})
			if err != nil {
				return
			}
			items := make([]map[string]interface{}, 0)
			for _, item := range arrayValue(firstPresent(result, key, "instances")) {
				instance := objectValue(item)
				if len(instance) == 0 {
					continue
				}
				instance["_Region"] = region
				instance["Region"] = region
				instance["RegionName"] = regionName(region)
				items = append(items, instance)
			}
			mu.Lock()
			instances = append(instances, items...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return instances
}

func (s *Service) callTencent(ctx context.Context, service, region, action, version string, payload map[string]interface{}) (map[string]interface{}, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := s.endpoint(service)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Unix()
	host := req.URL.Host
	account, ok := ctx.Value(accountContextKey{}).(map[string]interface{})
	if !ok {
		return nil, errors.New("missing account context")
	}
	req.Header.Set("Content-Type", contentTypeTencent)
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", tc3Authorization(
		stringValue(account["secret_id"], ""),
		stringValue(account["secret_key"], ""),
		service,
		host,
		string(body),
		timestamp,
	))
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("tencent %s status %d: %s", action, res.StatusCode, string(raw))
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]interface{}{}, nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode tencent %s: %w", action, err)
	}
	result := decoded
	if responseObject, ok := decoded["Response"].(map[string]interface{}); ok {
		result = responseObject
	}
	if errObject, ok := result["Error"].(map[string]interface{}); ok {
		code := stringValue(errObject["Code"], "TencentError")
		message := stringValue(errObject["Message"], "request failed")
		return nil, fmt.Errorf("%s: %s", code, message)
	}
	return result, nil
}

type accountContextKey struct{}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, idText string) (map[string]interface{}, bool) {
	id, err := parseID(idText)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing accountId"})
		return nil, false
	}
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
			message = "Account not found"
		}
		response.JSON(w, status, map[string]interface{}{"error": message})
		return nil, false
	}
	*r = *r.WithContext(context.WithValue(r.Context(), accountContextKey{}, account))
	return account, true
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, secret_id, secret_key, region_id, description, is_default, created_at, updated_at FROM tencent_accounts ORDER BY created_at DESC`)
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

func loadAccount(ctx context.Context, db *sql.DB, id int64) (map[string]interface{}, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, secret_id, secret_key, region_id, description, is_default, created_at, updated_at FROM tencent_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner accountScanner) (map[string]interface{}, error) {
	var id int64
	var name, secretID, secretKey, regionID string
	var description, createdAt, updatedAt sql.NullString
	var isDefault sql.NullInt64
	if err := scanner.Scan(&id, &name, &secretID, &secretKey, &regionID, &description, &isDefault, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":          id,
		"name":        name,
		"secret_id":   secretID,
		"secret_key":  secretKey,
		"region_id":   regionID,
		"description": description.String,
		"is_default":  isDefault.Int64,
		"created_at":  createdAt.String,
		"updated_at":  updatedAt.String,
	}, nil
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	masked := maskSecretID(stringValue(account["secret_id"], ""))
	return map[string]interface{}{
		"id":          account["id"],
		"name":        account["name"],
		"secretId":    masked,
		"secret_id":   masked,
		"regionId":    account["region_id"],
		"region_id":   account["region_id"],
		"description": account["description"],
		"is_default":  account["is_default"],
		"created_at":  account["created_at"],
	}
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

func tc3Authorization(secretID, secretKey, service, host, body string, timestamp int64) string {
	hashedPayload := sha256Hex(body)
	canonicalHeaders := "content-type:" + contentTypeTencent + "\n" + "host:" + host + "\n"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\ncontent-type;host\n" + hashedPayload
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := authorizationAlgo + "\n" + strconv.FormatInt(timestamp, 10) + "\n" + credentialScope + "\n" + sha256Hex(canonicalRequest)
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	return authorizationAlgo + " Credential=" + secretID + "/" + credentialScope + ", SignedHeaders=content-type;host, Signature=" + signature
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) endpoint(service string) string {
	if s.apiBase != "" {
		return s.apiBase
	}
	switch service {
	case "dnspod":
		return s.dnspodEndpoint
	case "cvm":
		return s.cvmEndpoint
	case "lighthouse":
		return s.lighthouseEndpoint
	case "monitor":
		return s.monitorEndpoint
	default:
		return s.apiBase
	}
}

func serviceVersion(kind string) string {
	switch kind {
	case "dnspod":
		return dnspodVersion
	case "lighthouse":
		return lighthouseVersion
	case "monitor":
		return monitorVersion
	default:
		return cvmVersion
	}
}

func actionName(kind, action string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(action))
	switch normalized {
	case "START", "STARTINSTANCE", "STARTINSTANCES":
		return "StartInstances", nil
	case "STOP", "STOPINSTANCE", "STOPINSTANCES":
		return "StopInstances", nil
	case "REBOOT", "REBOOTINSTANCE", "REBOOTINSTANCES":
		return "RebootInstances", nil
	default:
		return "", fmt.Errorf("%s %s Failed: Invalid action", strings.ToUpper(kind), action)
	}
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

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func maskSecretID(value string) string {
	if value == "" {
		return "-"
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return string(runes[:minInt(4, len(runes))]) + "****"
	}
	if len(runes) <= 12 {
		return string(runes[:4]) + "****" + string(runes[len(runes)-2:])
	}
	return string(runes[:8]) + "****" + string(runes[len(runes)-4:])
}

func normalizeDomains(items []interface{}) []map[string]interface{} {
	domains := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		domain := objectValue(item)
		if len(domain) == 0 {
			continue
		}
		name := firstNonEmpty(stringValue(domain["Name"], ""), stringValue(domain["Domain"], ""), stringValue(domain["DomainName"], ""))
		if name != "" {
			domain["Name"] = name
			domain["DomainName"] = name
		}
		if _, ok := domain["Status"]; !ok {
			domain["Status"] = "ENABLE"
		}
		domains = append(domains, domain)
	}
	return domains
}

func normalizeRecords(items []interface{}) []map[string]interface{} {
	records := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		record := objectValue(item)
		if len(record) == 0 {
			continue
		}
		if value := firstPresent(record, "RecordId", "Id"); value != nil {
			record["RecordId"] = value
		}
		records = append(records, record)
	}
	return records
}

func compactParams(input map[string]interface{}) map[string]interface{} {
	output := map[string]interface{}{}
	for key, value := range input {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		}
		output[key] = value
	}
	return output
}

func numberOrString(primary interface{}, secondary interface{}, fallback interface{}) interface{} {
	for _, value := range []interface{}{primary, secondary, fallback} {
		switch typed := value.(type) {
		case nil:
			continue
		case int, int64, float64:
			return typed
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed
			}
			return typed.String()
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
				return parsed
			}
			return typed
		default:
			return typed
		}
	}
	return fallback
}

func optionalNumberOrString(values ...interface{}) interface{} {
	for _, value := range values {
		if value == nil {
			continue
		}
		if stringValue(value, "") == "" {
			continue
		}
		return numberOrString(value, nil, value)
	}
	return nil
}

func firstPresent(object map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	case nil:
		return fallback
	default:
		return fmt.Sprint(typed)
	}
}

func firstNumber(values ...interface{}) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		case string:
			if parsed, err := strconv.Atoi(typed); err == nil {
				return parsed
			}
		}
	}
	return 0
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
	case map[string]interface{}:
		for _, key := range []string{"Domain", "Record", "Instance", "InstanceSet", "DomainList", "RecordList"} {
			if nested, ok := typed[key]; ok {
				return arrayValue(nested)
			}
		}
	}
	return []interface{}{}
}

func defaultCVMRegions() []string {
	return []string{"ap-guangzhou", "ap-shanghai", "ap-beijing", "ap-hongkong", "ap-singapore"}
}

func defaultLighthouseRegions() []string {
	return []string{"ap-guangzhou", "ap-shanghai", "ap-beijing", "ap-hongkong", "ap-singapore", "ap-nanjing", "ap-chengdu"}
}

func regionName(region string) string {
	names := map[string]string{
		"ap-guangzhou":     "华南地区 (广州)",
		"ap-shanghai":      "华东地区 (上海)",
		"ap-nanjing":       "华东地区 (南京)",
		"ap-beijing":       "华北地区 (北京)",
		"ap-chengdu":       "西南地区 (成都)",
		"ap-chongqing":     "西南地区 (重庆)",
		"ap-hongkong":      "中国香港",
		"ap-singapore":     "新加坡",
		"ap-tokyo":         "日本 (东京)",
		"ap-seoul":         "韩国 (首尔)",
		"ap-bangkok":       "泰国 (曼谷)",
		"ap-mumbai":        "印度 (孟买)",
		"na-siliconvalley": "美西 (硅谷)",
		"na-ashburn":       "美东 (弗吉尼亚)",
		"eu-frankfurt":     "欧洲地区 (法兰克福)",
		"eu-moscow":        "欧洲地区 (莫斯科)",
	}
	if name, ok := names[region]; ok {
		return name
	}
	return region
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
