package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	dnsVersion  = "2015-01-09"
	ecsVersion  = "2014-05-26"
	cmsVersion  = "2019-01-01"
	swasVersion = "2020-06-01"

	defaultRegion = "cn-hangzhou"
	timeout       = 20 * time.Second
)

type Service struct {
	cfg          config.Config
	store        *database.Store
	client       *http.Client
	apiBase      string
	dnsEndpoint  string
	ecsEndpoint  string
	swasEndpoint string
	cmsEndpoint  string
}

func New(cfg config.Config) *Service {
	apiBase := cleanURL(os.Getenv("ALIYUN_API_BASE_URL"))
	service := &Service{
		cfg:          cfg,
		store:        database.New(cfg),
		client:       &http.Client{Timeout: timeout},
		apiBase:      apiBase,
		dnsEndpoint:  envURL("ALIYUN_DNS_ENDPOINT", "https://alidns.aliyuncs.com"),
		ecsEndpoint:  envURL("ALIYUN_ECS_ENDPOINT", "https://ecs.%s.aliyuncs.com"),
		swasEndpoint: envURL("ALIYUN_SWAS_ENDPOINT", "https://swas.%s.aliyuncs.com"),
		cmsEndpoint:  envURL("ALIYUN_CMS_ENDPOINT", "https://metrics.aliyuncs.com"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/aliyun")
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
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "metrics" && r.Method == http.MethodPost:
		s.metrics(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "domains":
		s.domains(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "domains" && r.Method == http.MethodDelete:
		s.deleteDomain(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "domains" && parts[4] == "records":
		s.domainRecords(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "records" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.recordMutation(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "records" && parts[4] == "status" && r.Method == http.MethodPut:
		s.recordStatus(w, r, parts[1], parts[3])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "instances" && r.Method == http.MethodGet:
		s.instances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3], parts[4], false)
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "swas" && r.Method == http.MethodGet:
		s.swasInstances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "swas" && parts[4] == "firewall":
		s.firewall(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "swas" && parts[4] == "firewall" && r.Method == http.MethodDelete:
		s.deleteFirewallRule(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "swas" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3], parts[4], true)
	default:
		response.Error(w, http.StatusNotFound, "aliyun route not implemented")
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
		`CREATE TABLE IF NOT EXISTS aliyun_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			access_key_id TEXT NOT NULL,
			access_key_secret TEXT NOT NULL,
			region_id TEXT DEFAULT 'cn-hangzhou',
			description TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS aliyun_domains (
			instance_id TEXT PRIMARY KEY,
			domain_name TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			version_name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES aliyun_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aliyun_accounts_created_at ON aliyun_accounts(created_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure aliyun schema: %w", err)
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
		response.JSON(w, http.StatusOK, safe)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		accessKeyID := strings.TrimSpace(stringValue(payload["accessKeyId"], stringValue(payload["access_key_id"], "")))
		accessKeySecret := strings.TrimSpace(stringValue(payload["accessKeySecret"], stringValue(payload["access_key_secret"], "")))
		regionID := strings.TrimSpace(stringValue(payload["regionId"], stringValue(payload["region_id"], defaultRegion)))
		if regionID == "" {
			regionID = defaultRegion
		}
		if name == "" || accessKeyID == "" || accessKeySecret == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing required fields"})
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		result, err := db.ExecContext(
			r.Context(),
			`INSERT INTO aliyun_accounts (name, access_key_id, access_key_secret, region_id, description) VALUES (?, ?, ?, ?, ?)`,
			name,
			accessKeyID,
			accessKeySecret,
			regionID,
			stringValue(payload["description"], ""),
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
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
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		regionID := strings.TrimSpace(stringValue(payload["regionId"], stringValue(payload["region_id"], defaultRegion)))
		if regionID == "" {
			regionID = defaultRegion
		}
		description := stringValue(payload["description"], "")
		accessKeyID := strings.TrimSpace(stringValue(payload["accessKeyId"], stringValue(payload["access_key_id"], "")))
		accessKeySecret := strings.TrimSpace(stringValue(payload["accessKeySecret"], stringValue(payload["access_key_secret"], "")))
		if accessKeyID != "" && accessKeySecret != "" {
			_, err = db.ExecContext(
				r.Context(),
				`UPDATE aliyun_accounts SET name = ?, access_key_id = ?, access_key_secret = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				name,
				accessKeyID,
				accessKeySecret,
				regionID,
				description,
				id,
			)
		} else {
			_, err = db.ExecContext(
				r.Context(),
				`UPDATE aliyun_accounts SET name = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				name,
				regionID,
				description,
				id,
			)
		}
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM aliyun_accounts WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
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
		result, err := s.listDomains(r.Context(), account, map[string]string{
			"PageSize":   firstNonEmpty(r.URL.Query().Get("pageSize"), "100"),
			"PageNumber": firstNonEmpty(r.URL.Query().Get("pageNumber"), "1"),
			"KeyWord":    r.URL.Query().Get("keyword"),
		})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, result)
	case http.MethodPost:
		payload, _ := readObject(r)
		domainName := strings.TrimSpace(stringValue(payload["domainName"], ""))
		if domainName == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing domainName"})
			return
		}
		result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "AddDomain", map[string]string{"DomainName": domainName})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteDomain(w http.ResponseWriter, r *http.Request, accountID, domainName string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "DeleteDomain", map[string]string{"DomainName": domainName})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) domainRecords(w http.ResponseWriter, r *http.Request, accountID, domainName string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.listDomainRecords(r.Context(), account, domainName, map[string]string{
			"PageSize":     firstNonEmpty(r.URL.Query().Get("pageSize"), "100"),
			"PageNumber":   firstNonEmpty(r.URL.Query().Get("pageNumber"), "1"),
			"RRKeyWord":    r.URL.Query().Get("rrKeyword"),
			"TypeKeyWord":  r.URL.Query().Get("typeKeyword"),
			"ValueKeyWord": r.URL.Query().Get("valueKeyword"),
		})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, result)
	case http.MethodPost:
		payload, _ := readObject(r)
		params := map[string]string{
			"DomainName": domainName,
			"RR":         stringValue(payload["rr"], stringValue(payload["RR"], "")),
			"Type":       stringValue(payload["type"], stringValue(payload["Type"], "")),
			"Value":      stringValue(payload["value"], stringValue(payload["Value"], "")),
			"TTL":        stringValue(payload["ttl"], stringValue(payload["TTL"], "600")),
			"Priority":   stringValue(payload["priority"], stringValue(payload["Priority"], "")),
			"Line":       stringValue(payload["line"], stringValue(payload["Line"], "")),
		}
		result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "AddDomainRecord", params)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordMutation(w http.ResponseWriter, r *http.Request, accountID, recordID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		payload, _ := readObject(r)
		params := map[string]string{
			"RecordId": recordID,
			"RR":       stringValue(payload["rr"], stringValue(payload["RR"], "")),
			"Type":     stringValue(payload["type"], stringValue(payload["Type"], "")),
			"Value":    stringValue(payload["value"], stringValue(payload["Value"], "")),
			"TTL":      stringValue(payload["ttl"], stringValue(payload["TTL"], "600")),
			"Priority": stringValue(payload["priority"], stringValue(payload["Priority"], "")),
			"Line":     stringValue(payload["line"], stringValue(payload["Line"], "")),
		}
		result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "UpdateDomainRecord", params)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	case http.MethodDelete:
		result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "DeleteDomainRecord", map[string]string{"RecordId": recordID})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordStatus(w http.ResponseWriter, r *http.Request, accountID, recordID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	payload, _ := readObject(r)
	status := stringValue(payload["status"], "Disable")
	if status != "Enable" {
		status = "Disable"
	}
	result, err := s.callRPC(r.Context(), s.endpoint("dns", ""), account, dnsVersion, "SetDomainRecordStatus", map[string]string{"RecordId": recordID, "Status": status})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) instances(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	result, err := s.listAllInstances(r.Context(), account, map[string]string{
		"PageSize":   firstNonEmpty(r.URL.Query().Get("pageSize"), "100"),
		"PageNumber": firstNonEmpty(r.URL.Query().Get("pageNumber"), "1"),
	})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (s *Service) swasInstances(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	result, err := s.listAllSwasInstances(r.Context(), account, map[string]string{
		"PageSize":   firstNonEmpty(r.URL.Query().Get("pageSize"), "100"),
		"PageNumber": firstNonEmpty(r.URL.Query().Get("pageNumber"), "1"),
	})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (s *Service) instanceAction(w http.ResponseWriter, r *http.Request, accountID, instanceID, action string, swas bool) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	payload, _ := readObject(r)
	regionID := firstNonEmpty(stringValue(payload["regionId"], stringValue(payload["region_id"], "")), stringValue(account["region_id"], defaultRegion))
	if swas && regionID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Missing regionId"})
		return
	}
	var rpcAction string
	params := map[string]string{"InstanceId": instanceID}
	version := ecsVersion
	endpoint := s.endpoint("ecs", regionID)
	switch action {
	case "start":
		rpcAction = "StartInstance"
	case "stop":
		rpcAction = "StopInstance"
		params["ForceStop"] = boolString(payload["force"])
	case "reboot":
		rpcAction = "RebootInstance"
		params["ForceStop"] = boolString(payload["force"])
	default:
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "unknown action"})
		return
	}
	if swas {
		version = swasVersion
		endpoint = s.endpoint("swas", regionID)
		params["RegionId"] = regionID
	}
	result, err := s.callRPC(r.Context(), endpoint, account, version, rpcAction, params)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) metrics(w http.ResponseWriter, r *http.Request, accountID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	payload, _ := readObject(r)
	dimensions := stringValue(payload["dimensions"], "")
	if dimensions == "" && payload["dimensions"] != nil {
		if raw, err := json.Marshal(payload["dimensions"]); err == nil {
			dimensions = string(raw)
		}
	}
	params := map[string]string{
		"Namespace":  firstNonEmpty(stringValue(payload["namespace"], ""), "acs_ecs_dashboard"),
		"MetricName": stringValue(payload["metricName"], ""),
		"Dimensions": dimensions,
		"StartTime":  stringValue(payload["startTime"], ""),
		"EndTime":    stringValue(payload["endTime"], ""),
		"Period":     firstNonEmpty(stringValue(payload["period"], ""), "60"),
	}
	result, err := s.callRPC(r.Context(), s.endpoint("cms", ""), account, cmsVersion, "DescribeMetricList", params)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (s *Service) firewall(w http.ResponseWriter, r *http.Request, accountID, instanceID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		regionID := firstNonEmpty(r.URL.Query().Get("regionId"), stringValue(account["region_id"], defaultRegion))
		payload, err := s.callRPC(r.Context(), s.endpoint("swas", regionID), account, swasVersion, "ListFirewallRules", map[string]string{
			"InstanceId": instanceID,
			"RegionId":   regionID,
		})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, firewallRules(payload))
	case http.MethodPost:
		payload, _ := readObject(r)
		rule := objectValue(payload["rule"])
		regionID := firstNonEmpty(stringValue(payload["regionId"], ""), stringValue(account["region_id"], defaultRegion))
		result, err := s.callRPC(r.Context(), s.endpoint("swas", regionID), account, swasVersion, "CreateFirewallRule", map[string]string{
			"InstanceId":     instanceID,
			"RegionId":       regionID,
			"RuleProtocol":   stringValue(rule["protocol"], ""),
			"Port":           stringValue(rule["port"], ""),
			"Remark":         stringValue(rule["remark"], ""),
			"SourceCidrIp":   stringValue(rule["sourceCidrIp"], ""),
			"FirewallRuleId": stringValue(rule["ruleId"], ""),
		})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteFirewallRule(w http.ResponseWriter, r *http.Request, accountID, instanceID, ruleID string) {
	account, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	regionID := firstNonEmpty(r.URL.Query().Get("regionId"), stringValue(account["region_id"], defaultRegion))
	result, err := s.callRPC(r.Context(), s.endpoint("swas", regionID), account, swasVersion, "DeleteFirewallRule", map[string]string{
		"InstanceId": instanceID,
		"RegionId":   regionID,
		"RuleId":     ruleID,
	})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (s *Service) listDomains(ctx context.Context, account map[string]interface{}, params map[string]string) (map[string]interface{}, error) {
	payload, err := s.callRPC(ctx, s.endpoint("dns", ""), account, dnsVersion, "DescribeDomains", params)
	if err != nil {
		return nil, fmt.Errorf("DescribeDomains Failed: %w", err)
	}
	domains := arrayAt(payload, "Domains", "Domain")
	if len(domains) == 0 {
		domains = arrayValue(payload["domains"])
	}
	total := firstNumber(payload["TotalCount"], payload["total"], len(domains))
	return map[string]interface{}{
		"domains": domains,
		"total":   total,
		"Domains": map[string]interface{}{"Domain": domains},
	}, nil
}

func (s *Service) listDomainRecords(ctx context.Context, account map[string]interface{}, domainName string, params map[string]string) (map[string]interface{}, error) {
	params["DomainName"] = domainName
	payload, err := s.callRPC(ctx, s.endpoint("dns", ""), account, dnsVersion, "DescribeDomainRecords", params)
	if err != nil {
		return nil, fmt.Errorf("DescribeDomainRecords Failed: %w", err)
	}
	records := arrayAt(payload, "DomainRecords", "Record")
	if len(records) == 0 {
		records = arrayValue(payload["records"])
	}
	total := firstNumber(payload["TotalCount"], payload["total"], len(records))
	return map[string]interface{}{
		"records":       records,
		"total":         total,
		"DomainRecords": map[string]interface{}{"Record": records},
	}, nil
}

func (s *Service) listAllInstances(ctx context.Context, account map[string]interface{}, options map[string]string) (map[string]interface{}, error) {
	regions := s.listRegions(ctx, account)
	all := make([]interface{}, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for _, regionID := range prioritizedRegions(regions) {
		regionID := regionID
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			payload, err := s.callRPC(ctx, s.endpoint("ecs", regionID), account, ecsVersion, "DescribeInstances", map[string]string{
				"RegionId":   regionID,
				"PageSize":   options["PageSize"],
				"PageNumber": options["PageNumber"],
			})
			if err != nil {
				return
			}
			items := make([]interface{}, 0)
			for _, item := range arrayAt(payload, "Instances", "Instance") {
				instance := objectValue(item)
				instance["RegionName"] = regionName(stringValue(instance["RegionId"], regionID))
				instance["InstanceTypeFriendly"] = formatFlavor(stringValue(instance["InstanceType"], ""))
				items = append(items, instance)
			}
			mu.Lock()
			all = append(all, items...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return map[string]interface{}{"instances": all, "total": len(all)}, nil
}

func (s *Service) listRegions(ctx context.Context, account map[string]interface{}) []string {
	payload, err := s.callRPC(ctx, s.endpoint("ecs", stringValue(account["region_id"], defaultRegion)), account, ecsVersion, "DescribeRegions", nil)
	if err != nil {
		return defaultRegions()
	}
	regions := make([]string, 0)
	for _, item := range arrayAt(payload, "Regions", "Region") {
		regionID := stringValue(objectValue(item)["RegionId"], "")
		if regionID != "" {
			regions = append(regions, regionID)
		}
	}
	if len(regions) == 0 {
		return defaultRegions()
	}
	return regions
}

func (s *Service) listAllSwasInstances(ctx context.Context, account map[string]interface{}, options map[string]string) (map[string]interface{}, error) {
	regions := s.listSwasRegions(ctx, account)
	all := make([]interface{}, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for _, regionID := range prioritizedRegions(regions) {
		regionID := regionID
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			payload, err := s.callRPC(ctx, s.endpoint("swas", regionID), account, swasVersion, "ListInstances", map[string]string{
				"RegionId":   regionID,
				"PageSize":   options["PageSize"],
				"PageNumber": options["PageNumber"],
			})
			if err != nil {
				return
			}
			items := arrayValue(payload["Instances"])
			if len(items) == 0 {
				items = arrayAt(payload, "Instances", "Instance")
			}
			normalized := make([]interface{}, 0, len(items))
			for _, item := range items {
				instance := objectValue(item)
				instance["RegionName"] = regionName(stringValue(instance["RegionId"], regionID))
				instance["InstanceTypeFriendly"] = formatFlavor(firstNonEmpty(stringValue(instance["PlanId"], ""), stringValue(instance["InstanceType"], "")))
				normalized = append(normalized, instance)
			}
			mu.Lock()
			all = append(all, normalized...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return map[string]interface{}{"instances": all, "total": len(all)}, nil
}

func (s *Service) listSwasRegions(ctx context.Context, account map[string]interface{}) []string {
	payload, err := s.callRPC(ctx, s.endpoint("swas", defaultRegion), account, swasVersion, "ListRegions", nil)
	if err != nil {
		return defaultSwasRegions()
	}
	regions := make([]string, 0)
	for _, item := range arrayValue(payload["Regions"]) {
		regionID := stringValue(objectValue(item)["RegionId"], "")
		if regionID != "" {
			regions = append(regions, regionID)
		}
	}
	if len(regions) == 0 {
		for _, item := range arrayAt(payload, "Regions", "Region") {
			regionID := stringValue(objectValue(item)["RegionId"], "")
			if regionID != "" {
				regions = append(regions, regionID)
			}
		}
	}
	if len(regions) == 0 {
		return defaultSwasRegions()
	}
	return regions
}

func (s *Service) callRPC(ctx context.Context, endpoint string, account map[string]interface{}, version, action string, params map[string]string) (map[string]interface{}, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("missing endpoint")
	}
	if params == nil {
		params = map[string]string{}
	}
	query := map[string]string{}
	for key, value := range params {
		if strings.TrimSpace(value) != "" {
			query[key] = value
		}
	}
	query["Action"] = action
	query["Format"] = "JSON"
	query["Version"] = version
	query["AccessKeyId"] = stringValue(account["access_key_id"], "")
	query["SignatureMethod"] = "HMAC-SHA1"
	query["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	query["SignatureVersion"] = "1.0"
	query["SignatureNonce"] = fmt.Sprintf("%d-%s", time.Now().UnixNano(), action)

	canonical := canonicalQuery(query)
	stringToSign := "GET&%2F&" + percentEncode(canonical)
	query["Signature"] = signature(stringToSign, stringValue(account["access_key_secret"], ""))

	target := endpoint + "?" + canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("aliyun %s status %d: %s", action, res.StatusCode, string(body))
	}
	var payload map[string]interface{}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]interface{}{}, nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode aliyun %s: %w", action, err)
	}
	return payload, nil
}

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
	return account, true
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, access_key_id, access_key_secret, region_id, description, is_default, created_at, updated_at FROM aliyun_accounts ORDER BY created_at DESC`)
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
	row := db.QueryRowContext(ctx, `SELECT id, name, access_key_id, access_key_secret, region_id, description, is_default, created_at, updated_at FROM aliyun_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner accountScanner) (map[string]interface{}, error) {
	var id int64
	var name, accessKeyID, accessKeySecret, regionID string
	var description, createdAt, updatedAt sql.NullString
	var isDefault sql.NullInt64
	if err := scanner.Scan(&id, &name, &accessKeyID, &accessKeySecret, &regionID, &description, &isDefault, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":                id,
		"name":              name,
		"access_key_id":     accessKeyID,
		"access_key_secret": accessKeySecret,
		"region_id":         regionID,
		"description":       description.String,
		"is_default":        isDefault.Int64,
		"created_at":        createdAt.String,
		"updated_at":        updatedAt.String,
	}, nil
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	masked := maskAccessKey(stringValue(account["access_key_id"], ""))
	return map[string]interface{}{
		"id":            account["id"],
		"name":          account["name"],
		"accessKeyId":   masked,
		"access_key_id": masked,
		"regionId":      account["region_id"],
		"region_id":     account["region_id"],
		"description":   account["description"],
		"is_default":    account["is_default"],
		"created_at":    account["created_at"],
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

func canonicalQuery(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, percentEncode(key)+"="+percentEncode(values[key]))
	}
	return strings.Join(parts, "&")
}

func percentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func signature(stringToSign, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) endpoint(kind, region string) string {
	if s.apiBase != "" {
		return s.apiBase
	}
	switch kind {
	case "dns":
		return s.dnsEndpoint
	case "cms":
		return s.cmsEndpoint
	case "swas":
		return regionalEndpoint(s.swasEndpoint, region)
	default:
		return regionalEndpoint(s.ecsEndpoint, region)
	}
}

func regionalEndpoint(template, region string) string {
	if region == "" {
		region = defaultRegion
	}
	if strings.Contains(template, "%s") {
		return fmt.Sprintf(template, region)
	}
	return template
}

func envURL(name, fallback string) string {
	if value := cleanURL(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func cleanURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func maskAccessKey(value string) string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boolString(value interface{}) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "true"
		}
	case string:
		if strings.EqualFold(typed, "true") || typed == "1" {
			return "true"
		}
	case float64:
		if typed != 0 {
			return "true"
		}
	}
	return "false"
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

func arrayAt(value interface{}, keys ...string) []interface{} {
	current := value
	for _, key := range keys {
		current = objectValue(current)[key]
	}
	return arrayValue(current)
}

func arrayValue(value interface{}) []interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return typed
	case []map[string]interface{}:
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return []interface{}{}
	}
}

func firewallRules(payload map[string]interface{}) []interface{} {
	rules := arrayValue(payload["FirewallRules"])
	if len(rules) > 0 {
		return rules
	}
	rules = arrayAt(payload, "FirewallRules", "FirewallRule")
	if len(rules) > 0 {
		return rules
	}
	return []interface{}{}
}

func regionName(regionID string) string {
	names := map[string]string{
		"cn-hangzhou":    "East China 1 (Hangzhou)",
		"cn-shanghai":    "East China 2 (Shanghai)",
		"cn-qingdao":     "North China 1 (Qingdao)",
		"cn-beijing":     "North China 2 (Beijing)",
		"cn-zhangjiakou": "North China 3 (Zhangjiakou)",
		"cn-huhehaote":   "North China 5 (Hohhot)",
		"cn-wulanchabu":  "North China 6 (Ulanqab)",
		"cn-shenzhen":    "South China 1 (Shenzhen)",
		"cn-heyuan":      "South China 2 (Heyuan)",
		"cn-guangzhou":   "South China 3 (Guangzhou)",
		"cn-chengdu":     "Southwest 1 (Chengdu)",
		"cn-hongkong":    "Hong Kong",
		"ap-southeast-1": "Singapore",
		"ap-northeast-1": "Tokyo",
		"us-east-1":      "Virginia",
		"us-west-1":      "Silicon Valley",
		"eu-central-1":   "Frankfurt",
		"eu-west-1":      "London",
		"cn-wuhan-lrb":   "Wuhan SWAS",
	}
	if name, ok := names[regionID]; ok {
		return name
	}
	return regionID
}

func defaultRegions() []string {
	return []string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-shenzhen", "cn-hongkong"}
}

func defaultSwasRegions() []string {
	return []string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-shenzhen", "cn-hongkong", "ap-southeast-1", "cn-wuhan-lrb", "cn-chengdu", "cn-guangzhou", "cn-qingdao"}
}

func prioritizedRegions(regions []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(regions))
	for _, region := range append([]string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-shenzhen", "cn-hongkong"}, regions...) {
		if region == "" || seen[region] {
			continue
		}
		seen[region] = true
		result = append(result, region)
	}
	return result
}

func formatFlavor(flavor string) string {
	if flavor == "" {
		return "-"
	}
	patterns := []struct {
		needle string
		label  string
	}{
		{"c1m1", "1C 1GB"},
		{"c2m2", "2C 2GB"},
		{"c2m4", "2C 4GB"},
		{"c4m4", "4C 4GB"},
		{"c4m8", "4C 8GB"},
		{"c8m16", "8C 16GB"},
	}
	for _, pattern := range patterns {
		if strings.Contains(flavor, pattern.needle) {
			return pattern.label
		}
	}
	return flavor
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
