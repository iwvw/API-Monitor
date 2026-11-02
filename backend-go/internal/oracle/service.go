package oracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type Service struct {
	cfg     config.Config
	store   *database.Store
	schema  database.SchemaEnsurer
	clients clientFactory
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:     cfg,
		store:   database.New(cfg),
		clients: clientFactory{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/oracle")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "export" && parts[1] == "accounts" && r.Method == http.MethodGet:
		s.exportAccounts(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "accounts" && r.Method == http.MethodPost:
		s.importAccounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.accountVerify(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "compartments" && r.Method == http.MethodGet:
		s.compartments(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "availability-domains" && r.Method == http.MethodGet:
		s.availabilityDomains(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "instances" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.instances(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && parts[4] == "actions" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "instances" && (r.Method == http.MethodGet || r.Method == http.MethodPut):
		s.instanceDetail(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "instances" && r.Method == http.MethodDelete:
		s.instanceTerminate(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && parts[4] == "vnic-attachments" && r.Method == http.MethodGet:
		s.vnicAttachments(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && parts[4] == "boot-volume-attachments" && r.Method == http.MethodGet:
		s.bootVolumeAttachments(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && parts[4] == "volume-attachments" && r.Method == http.MethodGet:
		s.volumeAttachments(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "instances" && parts[4] == "console-connections":
		s.consoleConnections(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "console-connections" && r.Method == http.MethodDelete:
		s.deleteConsoleConnectionHTTP(w, r, parts[1], parts[3])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "shapes" && r.Method == http.MethodGet:
		s.shapes(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "cost" && r.Method == http.MethodGet:
		s.costOverviewHandler(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && (parts[2] == "images" || parts[2] == "subnets"):
		response.OK(w, map[string]interface{}{"items": []interface{}{}, "message": "该选择器将在启动实例阶段启用"})
	default:
		response.Error(w, http.StatusNotFound, "oracle route not implemented")
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

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		accounts, err := listAccounts(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		safe := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			safe = append(safe, safeAccount(account))
		}
		response.OK(w, safe)
	case http.MethodPost:
		var payload accountPayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		id, err := createAccount(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
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

	accounts, err := listAccounts(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	exported := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		exported = append(exported, map[string]interface{}{
			"name":                 account.Name,
			"tenancyOcid":          account.TenancyOCID,
			"userOcid":             account.UserOCID,
			"fingerprint":          account.Fingerprint,
			"region":               account.Region,
			"privateKeyPem":        secure.SecureDecrypt(account.PrivateKeyEncrypted),
			"passphrase":           secure.SecureDecrypt(account.PassphraseEncrypted),
			"defaultCompartmentId": account.DefaultCompartmentID,
			"description":          account.Description,
		})
	}

	response.OK(w, map[string]interface{}{"accounts": exported})
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Accounts  []accountPayload `json:"accounts"`
		Overwrite bool             `json:"overwrite"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(payload.Accounts) == 0 {
		response.Error(w, http.StatusBadRequest, "需要提供 accounts 数组")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if payload.Overwrite {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM oracle_accounts`); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, item := range payload.Accounts {
		if err := upsertImportedAccount(r.Context(), tx, item); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{"imported": len(payload.Accounts)})
}

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, idText string) {
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

	switch r.Method {
	case http.MethodPut:
		var payload accountPayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := updateAccount(r.Context(), db, id, payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	case http.MethodDelete:
		if err := deleteAccount(r.Context(), db, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) accountVerify(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	err := s.verifyAccount(r.Context(), account)
	if err != nil {
		_ = updateVerifyStatus(r.Context(), db, account.ID, "failed", err.Error())
		response.Error(w, http.StatusBadGateway, "验证 Oracle 账号失败："+err.Error())
		return
	}
	_ = updateVerifyStatus(r.Context(), db, account.ID, "success", "")
	response.OK(w, map[string]interface{}{"status": "success", "message": "账号验证成功"})
}

func (s *Service) compartments(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	items, err := s.listCompartments(r.Context(), account)
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) availabilityDomains(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	items, err := s.listAvailabilityDomains(r.Context(), account, r.URL.Query().Get("compartmentId"))
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) shapes(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	items, err := s.listShapes(
		r.Context(),
		account,
		r.URL.Query().Get("compartmentId"),
		r.URL.Query().Get("availabilityDomain"),
		r.URL.Query().Get("imageId"),
	)
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) costOverviewHandler(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	ov, err := s.costOverview(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, "拉取 Oracle 成本数据失败："+err.Error())
		return
	}
	response.OK(w, ov)
}

func (s *Service) instances(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		items, err := s.listInstances(r.Context(), account, r.URL.Query().Get("compartmentId"))
		writeResult(w, map[string]interface{}{"instances": items}, err)
	case http.MethodPost:
		response.Error(w, http.StatusNotImplemented, "启动实例将在下一阶段开放")
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) instanceDetail(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		item, err := s.getInstance(r.Context(), account, r.URL.Query().Get("compartmentId"), instanceID)
		writeResult(w, map[string]interface{}{"instance": item}, err)
	case http.MethodPut:
		var payload updateInstancePayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.updateInstance(r.Context(), account, instanceID, payload)
		writeResult(w, map[string]interface{}{"instance": item, "message": "实例配置更新请求已提交"}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) instanceAction(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	var payload instanceActionPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.runInstanceAction(r.Context(), account, instanceID, payload.Action)
	writeResult(w, result, err)
}

func (s *Service) instanceTerminate(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	var payload instanceActionPayload
	_ = decodeJSON(r, &payload)
	preserve := payload.PreserveBootVolume != nil && *payload.PreserveBootVolume
	err := s.terminateInstance(r.Context(), account, instanceID, preserve)
	writeResult(w, map[string]interface{}{"success": true, "instanceId": instanceID}, err)
}

func (s *Service) vnicAttachments(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	compartmentID := firstNonEmpty(r.URL.Query().Get("compartmentId"), account.DefaultCompartmentID, account.TenancyOCID)
	items, err := s.listVNICs(r.Context(), account, compartmentID, instanceID)
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) bootVolumeAttachments(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	compartmentID := firstNonEmpty(r.URL.Query().Get("compartmentId"), account.DefaultCompartmentID, account.TenancyOCID)
	items, err := s.listBootVolumes(r.Context(), account, compartmentID, r.URL.Query().Get("availabilityDomain"), instanceID)
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) volumeAttachments(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	compartmentID := firstNonEmpty(r.URL.Query().Get("compartmentId"), account.DefaultCompartmentID, account.TenancyOCID)
	items, err := s.listBlockVolumes(r.Context(), account, compartmentID, instanceID)
	writeResult(w, map[string]interface{}{"items": items}, err)
}

func (s *Service) consoleConnections(w http.ResponseWriter, r *http.Request, idText, instanceID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	compartmentID := firstNonEmpty(r.URL.Query().Get("compartmentId"), account.DefaultCompartmentID, account.TenancyOCID)
	switch r.Method {
	case http.MethodGet:
		items, err := s.listConsoleConnections(r.Context(), account, compartmentID, instanceID)
		writeResult(w, map[string]interface{}{"items": items}, err)
	case http.MethodPost:
		var payload consoleConnectionPayload
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.createConsoleConnection(r.Context(), account, instanceID, payload.PublicKey)
		writeResult(w, map[string]interface{}{"connection": item}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteConsoleConnectionHTTP(w http.ResponseWriter, r *http.Request, idText, connectionID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	err := s.deleteConsoleConnection(r.Context(), account, connectionID)
	writeResult(w, map[string]interface{}{"success": true, "connectionId": connectionID}, err)
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, idText string) (Account, *sql.DB, bool) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
		return Account{}, nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Account{}, nil, false
	}
	account, err := getAccount(r.Context(), db, id)
	if err != nil {
		db.Close()
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "oracle account not found")
			return Account{}, nil, false
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Account{}, nil, false
	}
	return account, db, true
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func writeResult(w http.ResponseWriter, data interface{}, err error) {
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, data)
}
