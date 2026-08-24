package m365

import (
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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	defaultGraphBase = "https://graph.microsoft.com/v1.0"
	defaultLoginBase = "https://login.microsoftonline.com"
	requestTimeout   = 30 * time.Second
)

var (
	errAccountNotFound  = errors.New("account not found")
	requiredPermissions = []permissionRequirement{
		{Name: "User.Read.All", Note: "读取用户列表与详情"},
		{Name: "User.ReadWrite.All", Note: "创建、编辑、删除用户"},
		{Name: "Organization.Read.All", Note: "读取租户订阅与许可证到期时间"},
		{Name: "LicenseAssignment.Read.All", Note: "读取用户和组的许可证信息"},
		{Name: "LicenseAssignment.ReadWrite.All", Note: "分配或回收许可证"},
		{Name: "Group.Create", Note: "创建组"},
		{Name: "GroupMember.ReadWrite.All", Note: "添加或移除组成员"},
	}
)

type Service struct {
	cfg       config.Config
	store     *database.Store
	schema    database.SchemaEnsurer
	client    *http.Client
	graphBase string
	loginBase string
}

type accountRecord struct {
	ID              int64
	Name            string
	TenantID        string
	ClientID        string
	ClientSecret    string
	Description     string
	DefaultDomain   string
	VerifiedDomains []string
	Organization    string
	Enabled         bool
	LastVerifiedAt  string
	LastVerifiedErr string
	CreatedAt       string
	UpdatedAt       string
}

type graphErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type permissionRequirement struct {
	Name string
	Note string
}

type inviteRecord struct {
	ID                            int64
	Code                          string
	Name                          string
	AccountID                     int64
	AccountName                   string
	AccountIDs                    []int64
	Domain                        string
	Domains                       []string
	UsageLocation                 string
	SKUIDs                        []string
	MaxUses                       int64
	UsedCount                     int64
	Enabled                       bool
	ForceChangePasswordNextSignIn bool
	BatchID                       string
	ExpiresAt                     string
	CreatedAt                     string
	UpdatedAt                     string
}

type registrationRecord struct {
	ID                int64
	InviteID          int64
	InviteName        string
	InviteCode        string
	AccountID         int64
	AccountName       string
	DisplayName       string
	UserPrincipalName string
	GraphUserID       string
	Status            string
	ErrorMessage      string
	CreatedAt         string
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:       cfg,
		store:     database.New(cfg),
		client:    &http.Client{Timeout: requestTimeout},
		graphBase: envURL("M365_GRAPH_BASE_URL", defaultGraphBase),
		loginBase: envURL("M365_LOGIN_BASE_URL", defaultLoginBase),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/m365")
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
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "export" && parts[1] == "accounts" && r.Method == http.MethodGet:
		s.exportAccounts(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "accounts" && r.Method == http.MethodPost:
		s.importAccounts(w, r)
	case len(parts) == 1 && parts[0] == "public-pages":
		s.publicPages(w, r)
	case len(parts) == 2 && parts[0] == "public-pages" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.publicPageMutation(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "invite-codes":
		s.inviteCodes(w, r)
	case len(parts) == 2 && parts[0] == "invite-codes" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.inviteCodeMutation(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "registrations" && (r.Method == http.MethodGet || r.Method == http.MethodDelete):
		s.publicPageRegistrations(w, r)
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "invites" && r.Method == http.MethodGet:
		s.newPublicInvite(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "public" && parts[1] == "register" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.newPublicRegister(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "organization" && r.Method == http.MethodGet:
		s.organization(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "permissions" && r.Method == http.MethodGet:
		s.permissions(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "users":
		s.users(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "users" && r.Method == http.MethodGet:
		s.userDetails(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "users" && (r.Method == http.MethodPatch || r.Method == http.MethodDelete):
		s.userMutation(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "users" && parts[4] == "license-details" && r.Method == http.MethodGet:
		s.userLicenseDetails(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "users" && parts[4] == "assign-license" && r.Method == http.MethodPost:
		s.assignUserLicense(w, r, parts[1], parts[3])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "licenses" && parts[3] == "skus" && r.Method == http.MethodGet:
		s.listSKUs(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "groups":
		s.groups(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "groups" && parts[4] == "members" && r.Method == http.MethodGet:
		s.groupMembers(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "groups" && parts[4] == "members" && r.Method == http.MethodPost:
		s.addGroupMember(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "groups" && parts[4] == "members" && r.Method == http.MethodDelete:
		s.removeGroupMember(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "groups" && parts[4] == "assign-license" && r.Method == http.MethodPost:
		s.assignGroupLicense(w, r, parts[1], parts[3])
	default:
		response.Error(w, http.StatusNotFound, "m365 route not implemented")
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
		`CREATE TABLE IF NOT EXISTS m365_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			client_id TEXT NOT NULL,
			client_secret TEXT NOT NULL,
			description TEXT,
			default_domain TEXT,
			verified_domains TEXT,
			organization_name TEXT,
			enabled INTEGER DEFAULT 1,
			last_verified_at DATETIME,
			last_verified_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_m365_accounts_tenant_client ON m365_accounts(tenant_id, client_id)`,
		`CREATE TABLE IF NOT EXISTS m365_registration_invites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			account_ids TEXT,
			domain TEXT NOT NULL,
			domains TEXT,
			usage_location TEXT,
			sku_ids TEXT,
			max_uses INTEGER DEFAULT 1,
			used_count INTEGER DEFAULT 0,
			enabled INTEGER DEFAULT 1,
			force_change_password_next_sign_in INTEGER DEFAULT 0,
			batch_id TEXT,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_registration_invites_account ON m365_registration_invites(account_id)`,
		`CREATE TABLE IF NOT EXISTS m365_registration_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			invite_id INTEGER NOT NULL,
			account_id INTEGER NOT NULL,
			display_name TEXT NOT NULL,
			user_principal_name TEXT NOT NULL,
			graph_user_id TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (invite_id) REFERENCES m365_registration_invites(id) ON DELETE CASCADE,
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_registration_records_invite ON m365_registration_records(invite_id)`,
		`CREATE TABLE IF NOT EXISTS m365_sync_state (
			account_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			last_synced_at DATETIME,
			last_error TEXT,
			cursor_value TEXT,
			PRIMARY KEY (account_id, resource_type),
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure m365 schema: %w", err)
		}
	}
	if err := ensureTableColumn(ctx, db, "m365_accounts", "verified_domains", "TEXT"); err != nil {
		return err
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "account_ids", definition: "TEXT"},
		{name: "domains", definition: "TEXT"},
		{name: "batch_id", definition: "TEXT"},
	} {
		if err := ensureTableColumn(ctx, db, "m365_registration_invites", column.name, column.definition); err != nil {
			return err
		}
	}
	return ensurePublicPageSchema(ctx, db)
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
		items := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			items = append(items, safeAccount(account))
		}
		response.OK(w, map[string]interface{}{"items": items})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToAccount(payload, accountRecord{})
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		encryptedSecret, err := secure.SecureEncrypt(record.ClientSecret)
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
		result, err := db.ExecContext(
			r.Context(),
			`INSERT INTO m365_accounts (name, tenant_id, client_id, client_secret, description, default_domain, verified_domains, organization_name, enabled, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"accounts": exported})
}

func (s *Service) exportedAccounts(ctx context.Context) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, map[string]interface{}{
			"name":            account.Name,
			"tenantId":        account.TenantID,
			"clientId":        account.ClientID,
			"clientSecret":    secure.SecureDecrypt(account.ClientSecret),
			"description":     account.Description,
			"defaultDomain":   account.DefaultDomain,
			"verifiedDomains": account.VerifiedDomains,
			"organization":    account.Organization,
			"enabled":         account.Enabled,
		})
	}
	return items, nil
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	items := interfaceArray(payload["accounts"])
	if len(items) == 0 {
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

	if boolValue(payload["overwrite"], false) {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM m365_accounts`); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, item := range items {
		record, err := payloadToAccount(objectValue(item), accountRecord{})
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		encryptedSecret, err := secure.SecureEncrypt(record.ClientSecret)
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO m365_accounts (name, tenant_id, client_id, client_secret, description, default_domain, verified_domains, organization_name, enabled, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(tenant_id, client_id) DO UPDATE SET
			   name = excluded.name,
			   client_secret = excluded.client_secret,
			   description = excluded.description,
			   default_domain = excluded.default_domain,
			   verified_domains = excluded.verified_domains,
			   organization_name = excluded.organization_name,
			   enabled = excluded.enabled,
			   updated_at = CURRENT_TIMESTAMP`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
		); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{"imported": len(items)})
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
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_accounts WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		existing, err := loadAccount(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, errAccountNotFound) {
				response.Error(w, http.StatusNotFound, err.Error())
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToAccount(payload, existing)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		secretValue := existing.ClientSecret
		if incoming := strings.TrimSpace(stringValue(payload["clientSecret"], "")); incoming != "" {
			secretValue = incoming
		} else if !secure.IsEncrypted(secretValue) {
			secretValue = existing.ClientSecret
		}
		encryptedSecret, err := secure.SecureEncrypt(secretValue)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, err = db.ExecContext(
			r.Context(),
			`UPDATE m365_accounts
			 SET name = ?, tenant_id = ?, client_id = ?, client_secret = ?, description = ?, default_domain = ?, verified_domains = ?, organization_name = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
			id,
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) verifyAccount(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	org, err := s.fetchOrganization(r.Context(), account)
	db, dbErr := s.open(r.Context())
	if dbErr == nil {
		defer db.Close()
		if err != nil {
			_, _ = db.ExecContext(r.Context(), `UPDATE m365_accounts SET last_verified_at = CURRENT_TIMESTAMP, last_verified_error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, err.Error(), account.ID)
		} else {
			_, _ = db.ExecContext(
				r.Context(),
				`UPDATE m365_accounts
				 SET organization_name = ?, default_domain = ?, verified_domains = ?, last_verified_at = CURRENT_TIMESTAMP, last_verified_error = '', updated_at = CURRENT_TIMESTAMP
				 WHERE id = ?`,
				stringValue(org["displayName"], account.Organization),
				pickDefaultDomain(org),
				mustJSONString(extractVerifiedDomains(org)),
				account.ID,
			)
		}
	}
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"organization": org,
		"permissionsHint": []string{
			"User.Read.All",
			"User.ReadWrite.All",
			"LicenseAssignment.Read.All",
			"LicenseAssignment.ReadWrite.All",
			"Group.Create",
			"GroupMember.ReadWrite.All",
		},
	})
}

func (s *Service) organization(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	org, err := s.fetchOrganization(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, org)
}

func (s *Service) permissions(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	token, err := s.token(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	roleSet, err := tokenRoleSet(token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, fmt.Sprintf("permission detection failed: %v", err))
		return
	}
	items := make([]map[string]interface{}, 0, len(requiredPermissions))
	grantedCount := 0
	for _, permission := range requiredPermissions {
		granted := roleSet[permission.Name]
		if granted {
			grantedCount++
		}
		items = append(items, map[string]interface{}{
			"name":    permission.Name,
			"note":    permission.Note,
			"granted": granted,
		})
	}
	response.OK(w, map[string]interface{}{
		"items":          items,
		"grantedCount":   grantedCount,
		"missingCount":   len(requiredPermissions) - grantedCount,
		"tokenRoleCount": len(roleSet),
	})
}

func (s *Service) users(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := url.Values{}
		query.Set("$top", clampPositive(r.URL.Query().Get("top"), 50, 200))
		query.Set("$count", "true")
		query.Set("$select", "id,displayName,userPrincipalName,mail,jobTitle,department,officeLocation,usageLocation,accountEnabled,createdDateTime,assignedLicenses")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if search != "" {
			query.Set("$search", fmt.Sprintf(`"displayName:%s" OR "userPrincipalName:%s"`, escapeSearch(search), escapeSearch(search)))
		}
		path := "/users?" + query.Encode()
		headers := map[string]string{"ConsistencyLevel": "eventual"}
		result := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, headers, &result); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{
			"items":    objectArray(result["value"]),
			"count":    numberValue(result["@odata.count"]),
			"nextLink": stringValue(result["@odata.nextLink"], ""),
		})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		body, err := normalizeCreateUserPayload(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		created := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, created)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) userDetails(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	path := "/users/" + url.PathEscape(userID) + "?$select=id,displayName,mailNickname,userPrincipalName,mail,jobTitle,department,officeLocation,usageLocation,accountEnabled,createdDateTime,assignedLicenses,proxyAddresses,businessPhones"
	if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) userMutation(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		body := normalizeUserPatchPayload(payload)
		if value, ok := payload["accountEnabled"]; ok {
			body["accountEnabled"] = boolValue(value, true)
		}
		if password := strings.TrimSpace(stringValue(payload["password"], "")); password != "" {
			body["passwordProfile"] = map[string]interface{}{
				"password":                      password,
				"forceChangePasswordNextSignIn": boolValue(payload["forceChangePasswordNextSignIn"], false),
			}
		}
		if len(body) == 0 {
			response.Error(w, http.StatusBadRequest, "no supported fields provided")
			return
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPatch, "/users/"+url.PathEscape(userID), body, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	case http.MethodDelete:
		if err := s.graphJSON(r.Context(), account, http.MethodDelete, "/users/"+url.PathEscape(userID), nil, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) userLicenseDetails(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/users/"+url.PathEscape(userID)+"/licenseDetails", nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
}

func (s *Service) assignUserLicense(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	body := map[string]interface{}{
		"addLicenses":    normalizeLicenseAssignments(payload["addLicenses"]),
		"removeLicenses": stringArray(payload["removeLicenses"]),
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(userID)+"/assignLicense", body, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) listSKUs(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/subscribedSkus", nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	items := objectArray(result["value"])
	subscriptions := map[string]interface{}{}
	payload := map[string]interface{}{"items": items}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/directory/subscriptions", nil, nil, &subscriptions); err == nil {
		enrichSKUsWithSubscriptions(items, objectArray(subscriptions["value"]))
		payload["subscriptionLookupAvailable"] = true
	} else {
		payload["subscriptionLookupAvailable"] = false
	}
	response.OK(w, payload)
}

func (s *Service) groups(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result := map[string]interface{}{}
		query := url.Values{}
		query.Set("$top", clampPositive(r.URL.Query().Get("top"), 50, 200))
		query.Set("$select", "id,displayName,mail,mailEnabled,securityEnabled,createdDateTime")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if search != "" {
			query.Set("$search", fmt.Sprintf(`"displayName:%s"`, escapeSearch(search)))
		}
		headers := map[string]string{"ConsistencyLevel": "eventual"}
		if err := s.graphJSON(r.Context(), account, http.MethodGet, "/groups?"+query.Encode(), nil, headers, &result); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["displayName"], ""))
		mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
		if name == "" || mailNickname == "" {
			response.Error(w, http.StatusBadRequest, "displayName and mailNickname are required")
			return
		}
		body := map[string]interface{}{
			"displayName":     name,
			"mailEnabled":     boolValue(payload["mailEnabled"], false),
			"mailNickname":    mailNickname,
			"securityEnabled": boolValue(payload["securityEnabled"], true),
		}
		created := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups", body, nil, &created); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, created)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) groupMembers(w http.ResponseWriter, r *http.Request, idText, groupID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	path := "/groups/" + url.PathEscape(groupID) + "/members?$top=100&$select=id,displayName,userPrincipalName,mail"
	if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
}

func (s *Service) addGroupMember(w http.ResponseWriter, r *http.Request, idText, groupID, memberID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	body := map[string]interface{}{
		"@odata.id": strings.TrimRight(s.graphBase, "/") + "/directoryObjects/" + url.PathEscape(memberID),
	}
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups/"+url.PathEscape(groupID)+"/members/$ref", body, nil, nil); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"added": true})
}

func (s *Service) removeGroupMember(w http.ResponseWriter, r *http.Request, idText, groupID, memberID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	if err := s.graphJSON(r.Context(), account, http.MethodDelete, "/groups/"+url.PathEscape(groupID)+"/members/"+url.PathEscape(memberID)+"/$ref", nil, nil, nil); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"removed": true})
}

func (s *Service) assignGroupLicense(w http.ResponseWriter, r *http.Request, idText, groupID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	body := map[string]interface{}{
		"addLicenses":    normalizeLicenseAssignments(payload["addLicenses"]),
		"removeLicenses": stringArray(payload["removeLicenses"]),
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups/"+url.PathEscape(groupID)+"/assignLicense", body, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) invites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		items, err := loadInvites(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload := make([]map[string]interface{}, 0, len(items))
		now := time.Now().UTC()
		for _, item := range items {
			payload = append(payload, inviteToMap(item, now))
		}
		response.OK(w, map[string]interface{}{"items": payload})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToInvite(payload, inviteRecord{
			Enabled:                       true,
			MaxUses:                       1,
			ForceChangePasswordNextSignIn: false,
		})
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if record.Code == "" {
			record.Code = ""
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		record, err = normalizeInviteTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		skuJSON, err := json.Marshal(record.SKUIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		accountIDsJSON, err := json.Marshal(record.AccountIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		domainsJSON, err := json.Marshal(record.Domains)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		quantity := clampPositiveInt64(numberValue(payload["quantity"]), 1)
		if quantity > 200 {
			quantity = 200
		}
		batchID, err := generateInviteCode()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		codes := make([]string, 0, quantity)
		ids := make([]int64, 0, quantity)
		for i := int64(0); i < quantity; i++ {
			code := strings.TrimSpace(record.Code)
			if quantity > 1 || code == "" {
				code, err = generateInviteCode()
				if err != nil {
					response.Error(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			result, err := db.ExecContext(
				r.Context(),
				`INSERT INTO m365_registration_invites
				 (code, name, account_id, account_ids, domain, domains, usage_location, sku_ids, max_uses, used_count, enabled, force_change_password_next_sign_in, batch_id, expires_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				code,
				record.Name,
				record.AccountID,
				string(accountIDsJSON),
				record.Domain,
				string(domainsJSON),
				record.UsageLocation,
				string(skuJSON),
				boolToInt(record.Enabled),
				boolToInt(record.ForceChangePasswordNextSignIn),
				batchID,
				nullIfEmpty(record.ExpiresAt),
			)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			id, _ := result.LastInsertId()
			ids = append(ids, id)
			codes = append(codes, code)
		}
		response.OK(w, map[string]interface{}{
			"id":           firstInt64(ids),
			"code":         firstString(codes),
			"ids":          ids,
			"codes":        codes,
			"createdCount": quantity,
			"batchId":      batchID,
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) inviteMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_registration_invites WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		existing, err := loadInviteByID(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToInvite(payload, existing)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err = normalizeInviteTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		skuJSON, err := json.Marshal(record.SKUIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		accountIDsJSON, err := json.Marshal(record.AccountIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		domainsJSON, err := json.Marshal(record.Domains)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, err = db.ExecContext(
			r.Context(),
			`UPDATE m365_registration_invites
			 SET code = ?, name = ?, account_id = ?, account_ids = ?, domain = ?, domains = ?, usage_location = ?, sku_ids = ?, max_uses = ?, enabled = ?, force_change_password_next_sign_in = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			record.Code,
			record.Name,
			record.AccountID,
			string(accountIDsJSON),
			record.Domain,
			string(domainsJSON),
			record.UsageLocation,
			string(skuJSON),
			record.MaxUses,
			boolToInt(record.Enabled),
			boolToInt(record.ForceChangePasswordNextSignIn),
			nullIfEmpty(record.ExpiresAt),
			id,
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) registrations(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	items, err := loadRegistrations(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, registrationToMap(item))
	}
	response.OK(w, map[string]interface{}{"items": payload})
}

func (s *Service) publicInvite(w http.ResponseWriter, r *http.Request, code string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	record, err := loadInviteByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, publicInvitePayload(r.Context(), db, record, time.Now().UTC()))
}

func (s *Service) publicRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.publicRegisterDescriptor(w, r)
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.TrimSpace(stringValue(payload["code"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	password := strings.TrimSpace(stringValue(payload["password"], ""))
	displayName := strings.TrimSpace(stringValue(payload["displayName"], mailNickname))
	if code == "" || mailNickname == "" {
		response.Error(w, http.StatusBadRequest, "code and mailNickname are required")
		return
	}

	// password 可选：留空时由系统生成并随注册结果返回一次（initialPassword），
	// 与公开注册页「初始密码自动生成」的文案与渲染保持一致。
	initialPassword := ""
	if password == "" {
		generated, err := generateRegistrationPassword()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to generate password")
			return
		}
		password = generated
		initialPassword = generated
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	invite, err := loadInviteByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if available, reason := evaluateInviteAvailability(invite, time.Now().UTC()); !available {
		response.Error(w, http.StatusBadRequest, "invite unavailable: "+reason)
		return
	}

	target, err := resolveInviteRegistrationTarget(r.Context(), db, invite, payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	account, err := s.loadDecryptedAccount(r.Context(), strconv.FormatInt(target.ID, 10))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if displayName == "" {
		displayName = mailNickname
	}
	userPrincipalName := mailNickname + "@" + target.Domain
	body := map[string]interface{}{
		"accountEnabled":    true,
		"displayName":       displayName,
		"mailNickname":      mailNickname,
		"userPrincipalName": userPrincipalName,
		"passwordProfile": map[string]interface{}{
			"password":                      password,
			"forceChangePasswordNextSignIn": invite.ForceChangePasswordNextSignIn,
		},
	}
	body["usageLocation"] = resolvedInviteUsageLocation(invite.UsageLocation)

	created := map[string]interface{}{}
	createErr := s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created)
	graphUserID := strings.TrimSpace(stringValue(created["id"], ""))
	status := "success"
	warning := ""
	errorMessage := ""
	if createErr != nil {
		status = "failed"
		errorMessage = createErr.Error()
	} else if len(invite.SKUIDs) > 0 && graphUserID != "" {
		assignBody := map[string]interface{}{
			"addLicenses":    licenseAssignmentsFromIDs(invite.SKUIDs),
			"removeLicenses": []string{},
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(graphUserID)+"/assignLicense", assignBody, nil, nil); err != nil {
			status = "partial"
			errorMessage = err.Error()
			warning = err.Error()
		}
	}

	if persistErr := persistRegistrationResult(r.Context(), db, invite, target.ID, displayName, userPrincipalName, graphUserID, status, errorMessage); persistErr != nil {
		if warning != "" {
			warning += "; "
		}
		warning += "local record save failed"
	}

	if status == "failed" {
		response.Error(w, http.StatusBadGateway, errorMessage)
		return
	}

	response.OK(w, map[string]interface{}{
		"id":                graphUserID,
		"status":            status,
		"accountId":         target.ID,
		"domain":            target.Domain,
		"userPrincipalName": userPrincipalName,
		"initialPassword":   emptyToNil(initialPassword),
		"warning":           emptyToNil(warning),
	})
}

func (s *Service) publicRegisterDescriptor(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	payload := map[string]interface{}{
		"method": "POST",
		"fields": []string{"code", "mailNickname", "password", "displayName", "accountId", "domain"},
	}
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		record, err := loadInviteByCode(r.Context(), db, code)
		if err == nil {
			payload["invite"] = publicInvitePayload(r.Context(), db, record, time.Now().UTC())
		}
	}
	response.OK(w, payload)
}

func (s *Service) loadDecryptedAccount(ctx context.Context, idText string) (accountRecord, error) {
	id, err := parseID(idText)
	if err != nil {
		return accountRecord{}, fmt.Errorf("invalid account id")
	}
	db, err := s.open(ctx)
	if err != nil {
		return accountRecord{}, err
	}
	defer db.Close()
	account, err := loadAccount(ctx, db, id)
	if err != nil {
		return accountRecord{}, err
	}
	account.ClientSecret = secure.SecureDecrypt(account.ClientSecret)
	return account, nil
}

func (s *Service) fetchOrganization(ctx context.Context, account accountRecord) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	if err := s.graphJSON(ctx, account, http.MethodGet, "/organization", nil, nil, &result); err != nil {
		return nil, err
	}
	items := objectArray(result["value"])
	if len(items) == 0 {
		return map[string]interface{}{}, nil
	}
	return items[0], nil
}

func (s *Service) graphJSON(ctx context.Context, account accountRecord, method, path string, body interface{}, extraHeaders map[string]string, target interface{}) error {
	token, err := s.token(ctx, account)
	if err != nil {
		return err
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, joinURL(s.graphBase, path), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeGraphError(resp)
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *Service) token(ctx context.Context, account accountRecord) (string, error) {
	form := url.Values{}
	form.Set("client_id", account.ClientID)
	form.Set("client_secret", account.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")
	form.Set("grant_type", "client_credentials")
	endpoint := strings.TrimRight(s.loginBase, "/") + "/" + account.TenantID + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", decodeGraphError(resp)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", errors.New("empty access token")
	}
	return payload.AccessToken, nil
}

func tokenRoleSet(token string) (map[string]bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("access token is not a JWT")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode token payload: %w", err)
	}
	var payload struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, fmt.Errorf("parse token payload: %w", err)
	}
	roleSet := map[string]bool{}
	for _, role := range payload.Roles {
		role = strings.TrimSpace(role)
		if role != "" {
			roleSet[role] = true
		}
	}
	return roleSet, nil
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]accountRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, tenant_id, client_id, client_secret, description, default_domain, COALESCE(verified_domains, ''), organization_name, enabled, COALESCE(last_verified_at, ''), COALESCE(last_verified_error, ''), created_at, updated_at FROM m365_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []accountRecord{}
	for rows.Next() {
		record, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, record)
	}
	return accounts, rows.Err()
}

func loadAccount(ctx context.Context, db *sql.DB, id int64) (accountRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, tenant_id, client_id, client_secret, description, default_domain, COALESCE(verified_domains, ''), organization_name, enabled, COALESCE(last_verified_at, ''), COALESCE(last_verified_error, ''), created_at, updated_at FROM m365_accounts WHERE id = ?`, id)
	record, err := scanAccount(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accountRecord{}, errAccountNotFound
		}
		return accountRecord{}, err
	}
	return record, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner rowScanner) (accountRecord, error) {
	record := accountRecord{}
	var verifiedDomainsJSON string
	var enabled int
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.TenantID,
		&record.ClientID,
		&record.ClientSecret,
		&record.Description,
		&record.DefaultDomain,
		&verifiedDomainsJSON,
		&record.Organization,
		&enabled,
		&record.LastVerifiedAt,
		&record.LastVerifiedErr,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return accountRecord{}, err
	}
	record.Enabled = enabled != 0
	record.VerifiedDomains = normalizeDomainSlice(decodeStringSliceJSON(verifiedDomainsJSON))
	if len(record.VerifiedDomains) == 0 && strings.TrimSpace(record.DefaultDomain) != "" {
		record.VerifiedDomains = []string{strings.ToLower(strings.TrimSpace(record.DefaultDomain))}
	}
	return record, nil
}

func safeAccount(record accountRecord) map[string]interface{} {
	secretMask := maskSecret(record.ClientSecret)
	return map[string]interface{}{
		"id":              record.ID,
		"name":            record.Name,
		"tenantId":        record.TenantID,
		"clientId":        record.ClientID,
		"clientSecret":    secretMask,
		"description":     record.Description,
		"defaultDomain":   record.DefaultDomain,
		"verifiedDomains": record.VerifiedDomains,
		"organization":    record.Organization,
		"enabled":         record.Enabled,
		"lastVerifiedAt":  emptyToNil(record.LastVerifiedAt),
		"lastVerifiedErr": emptyToNil(record.LastVerifiedErr),
		"createdAt":       record.CreatedAt,
		"updatedAt":       record.UpdatedAt,
	}
}

func payloadToAccount(payload map[string]interface{}, base accountRecord) (accountRecord, error) {
	record := base
	record.Name = strings.TrimSpace(stringValue(payload["name"], record.Name))
	record.TenantID = strings.TrimSpace(stringValue(payload["tenantId"], stringValue(payload["tenant_id"], record.TenantID)))
	record.ClientID = strings.TrimSpace(stringValue(payload["clientId"], stringValue(payload["client_id"], record.ClientID)))
	record.ClientSecret = strings.TrimSpace(stringValue(payload["clientSecret"], stringValue(payload["client_secret"], record.ClientSecret)))
	record.Description = strings.TrimSpace(stringValue(payload["description"], record.Description))
	record.DefaultDomain = strings.TrimSpace(stringValue(payload["defaultDomain"], stringValue(payload["default_domain"], record.DefaultDomain)))
	if value, ok := payload["verifiedDomains"]; ok {
		record.VerifiedDomains = normalizeDomainSlice(stringArray(value))
	}
	if len(record.VerifiedDomains) == 0 && record.DefaultDomain != "" {
		record.VerifiedDomains = []string{strings.ToLower(record.DefaultDomain)}
	}
	if record.DefaultDomain == "" && len(record.VerifiedDomains) > 0 {
		record.DefaultDomain = record.VerifiedDomains[0]
	}
	record.Organization = strings.TrimSpace(stringValue(payload["organization"], stringValue(payload["organizationName"], record.Organization)))
	record.Enabled = boolValue(payload["enabled"], true)
	if record.Name == "" || record.TenantID == "" || record.ClientID == "" || record.ClientSecret == "" {
		return accountRecord{}, errors.New("name, tenantId, clientId and clientSecret are required")
	}
	return record, nil
}

func normalizeCreateUserPayload(payload map[string]interface{}) (map[string]interface{}, error) {
	displayName := strings.TrimSpace(stringValue(payload["displayName"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	userPrincipalName := strings.TrimSpace(stringValue(payload["userPrincipalName"], ""))
	password := strings.TrimSpace(stringValue(payload["password"], ""))
	if displayName == "" || mailNickname == "" || userPrincipalName == "" || password == "" {
		return nil, errors.New("displayName, mailNickname, userPrincipalName and password are required")
	}
	body := map[string]interface{}{
		"accountEnabled":    boolValue(payload["accountEnabled"], true),
		"displayName":       displayName,
		"mailNickname":      mailNickname,
		"userPrincipalName": userPrincipalName,
		"passwordProfile": map[string]interface{}{
			"password":                      password,
			"forceChangePasswordNextSignIn": boolValue(payload["forceChangePasswordNextSignIn"], true),
		},
	}
	for _, key := range []string{"department", "jobTitle", "officeLocation", "usageLocation"} {
		if value := strings.TrimSpace(stringValue(payload[key], "")); value != "" {
			body[key] = value
		}
	}
	return body, nil
}

func normalizeUserPatchPayload(payload map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, key := range []string{"displayName", "mailNickname", "userPrincipalName"} {
		if value, ok := payload[key]; ok {
			trimmed := strings.TrimSpace(stringValue(value, ""))
			if trimmed != "" {
				body[key] = trimmed
			}
		}
	}
	for _, key := range []string{"department", "jobTitle", "officeLocation", "usageLocation"} {
		if value, ok := payload[key]; ok {
			trimmed := strings.TrimSpace(stringValue(value, ""))
			if trimmed != "" {
				body[key] = trimmed
			}
		}
	}
	return body
}

func normalizeLicenseAssignments(value interface{}) []map[string]interface{} {
	items := []map[string]interface{}{}
	for _, raw := range interfaceArray(value) {
		item := objectValue(raw)
		skuID := strings.TrimSpace(stringValue(item["skuId"], ""))
		if skuID == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"skuId":         skuID,
			"disabledPlans": stringArray(item["disabledPlans"]),
		})
	}
	return items
}

func enrichSKUsWithSubscriptions(skus []map[string]interface{}, subscriptions []map[string]interface{}) {
	subscriptionsBySKU := map[string][]map[string]interface{}{}
	subscriptionsByID := map[string]map[string]interface{}{}
	for _, subscription := range subscriptions {
		normalized := normalizeCompanySubscription(subscription)
		id := strings.TrimSpace(stringValue(normalized["id"], ""))
		skuID := strings.TrimSpace(stringValue(normalized["skuId"], ""))
		if id != "" {
			subscriptionsByID[id] = normalized
		}
		if skuID != "" {
			subscriptionsBySKU[skuID] = append(subscriptionsBySKU[skuID], normalized)
		}
	}

	for _, sku := range skus {
		matched := []map[string]interface{}{}
		seen := map[string]bool{}
		addSubscription := func(subscription map[string]interface{}) {
			key := strings.TrimSpace(stringValue(subscription["id"], ""))
			if key == "" {
				key = strings.TrimSpace(stringValue(subscription["skuId"], ""))
			}
			if key != "" && seen[key] {
				return
			}
			if key != "" {
				seen[key] = true
			}
			matched = append(matched, subscription)
		}

		for _, subscriptionID := range stringArray(sku["subscriptionIds"]) {
			if subscription, ok := subscriptionsByID[subscriptionID]; ok {
				addSubscription(subscription)
			}
		}
		skuID := strings.TrimSpace(stringValue(sku["skuId"], ""))
		for _, subscription := range subscriptionsBySKU[skuID] {
			addSubscription(subscription)
		}
		if len(matched) == 0 {
			continue
		}

		sku["subscriptions"] = matched
		for _, subscription := range matched {
			nextLifecycleDateTime := strings.TrimSpace(stringValue(subscription["nextLifecycleDateTime"], ""))
			if nextLifecycleDateTime == "" {
				continue
			}
			current := strings.TrimSpace(stringValue(sku["nextLifecycleDateTime"], ""))
			if current == "" || nextLifecycleDateTime < current {
				sku["nextLifecycleDateTime"] = nextLifecycleDateTime
				sku["subscriptionStatus"] = subscription["status"]
				sku["isTrial"] = subscription["isTrial"]
			}
		}
	}
}

func normalizeCompanySubscription(subscription map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":                    stringValue(subscription["id"], ""),
		"skuId":                 stringValue(subscription["skuId"], ""),
		"skuPartNumber":         stringValue(subscription["skuPartNumber"], ""),
		"status":                stringValue(subscription["status"], ""),
		"isTrial":               boolValue(subscription["isTrial"], false),
		"nextLifecycleDateTime": stringValue(subscription["nextLifecycleDateTime"], ""),
	}
}

func loadInvites(ctx context.Context, db *sql.DB) ([]inviteRecord, error) {
	rows, err := db.QueryContext(ctx, inviteSelectSQL+` ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []inviteRecord{}
	for rows.Next() {
		record, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func loadInviteByID(ctx context.Context, db *sql.DB, id int64) (inviteRecord, error) {
	row := db.QueryRowContext(ctx, inviteSelectSQL+` WHERE i.id = ?`, id)
	return scanInvite(row)
}

func loadInviteByCode(ctx context.Context, db *sql.DB, code string) (inviteRecord, error) {
	row := db.QueryRowContext(ctx, inviteSelectSQL+` WHERE i.code = ?`, strings.TrimSpace(code))
	return scanInvite(row)
}

func scanInvite(scanner rowScanner) (inviteRecord, error) {
	record := inviteRecord{}
	var accountIDsJSON, domainsJSON, skuIDsJSON, batchID string
	var enabled, forceChange int
	if err := scanner.Scan(
		&record.ID,
		&record.Code,
		&record.Name,
		&record.AccountID,
		&record.AccountName,
		&accountIDsJSON,
		&record.Domain,
		&domainsJSON,
		&record.UsageLocation,
		&skuIDsJSON,
		&record.MaxUses,
		&record.UsedCount,
		&enabled,
		&forceChange,
		&batchID,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return inviteRecord{}, err
	}
	record.Enabled = enabled != 0
	record.ForceChangePasswordNextSignIn = forceChange != 0
	record.AccountIDs = decodeInt64SliceJSON(accountIDsJSON)
	if len(record.AccountIDs) == 0 && record.AccountID > 0 {
		record.AccountIDs = []int64{record.AccountID}
	}
	record.Domains = normalizeDomainSlice(decodeStringSliceJSON(domainsJSON))
	if len(record.Domains) == 0 && record.Domain != "" {
		record.Domains = []string{record.Domain}
	}
	record.SKUIDs = decodeStringSliceJSON(skuIDsJSON)
	record.BatchID = strings.TrimSpace(batchID)
	return record, nil
}

func loadRegistrations(ctx context.Context, db *sql.DB) ([]registrationRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT r.id, r.invite_id, COALESCE(i.name, ''), COALESCE(i.code, ''), r.account_id, COALESCE(a.name, ''), r.display_name, r.user_principal_name, COALESCE(r.graph_user_id, ''), r.status, COALESCE(r.error_message, ''), r.created_at
		 FROM m365_registration_records r
		 LEFT JOIN m365_registration_invites i ON i.id = r.invite_id
		 LEFT JOIN m365_accounts a ON a.id = r.account_id
		 ORDER BY r.created_at DESC, r.id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []registrationRecord{}
	for rows.Next() {
		record := registrationRecord{}
		if err := rows.Scan(
			&record.ID,
			&record.InviteID,
			&record.InviteName,
			&record.InviteCode,
			&record.AccountID,
			&record.AccountName,
			&record.DisplayName,
			&record.UserPrincipalName,
			&record.GraphUserID,
			&record.Status,
			&record.ErrorMessage,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func payloadToInvite(payload map[string]interface{}, base inviteRecord) (inviteRecord, error) {
	record := base
	if value, ok := payload["code"]; ok {
		record.Code = strings.TrimSpace(stringValue(value, record.Code))
	}
	record.Name = strings.TrimSpace(stringValue(payload["name"], record.Name))
	if value, ok := payload["accountId"]; ok {
		record.AccountID = numberValue(value)
	}
	if value, ok := payload["accountIds"]; ok {
		record.AccountIDs = int64Array(value)
	}
	record.Domain = strings.ToLower(strings.TrimSpace(stringValue(payload["domain"], record.Domain)))
	if value, ok := payload["domains"]; ok {
		record.Domains = normalizeDomainSlice(stringArray(value))
	}
	record.UsageLocation = strings.ToUpper(strings.TrimSpace(stringValue(payload["usageLocation"], record.UsageLocation)))
	if value, ok := payload["skuIds"]; ok {
		record.SKUIDs = stringArray(value)
	}
	if value, ok := payload["maxUses"]; ok {
		record.MaxUses = maxInt64(numberValue(value), 0)
	}
	if value, ok := payload["enabled"]; ok {
		record.Enabled = boolValue(value, record.Enabled)
	}
	if value, ok := payload["forceChangePasswordNextSignIn"]; ok {
		record.ForceChangePasswordNextSignIn = boolValue(value, record.ForceChangePasswordNextSignIn)
	}
	if value, ok := payload["expiresAt"]; ok {
		expiresAt, err := normalizeOptionalTimestamp(stringValue(value, ""))
		if err != nil {
			return inviteRecord{}, err
		}
		record.ExpiresAt = expiresAt
	}
	if record.Name == "" {
		return inviteRecord{}, errors.New("name is required")
	}
	if record.MaxUses < 0 {
		record.MaxUses = 0
	}
	return record, nil
}

func inviteToMap(record inviteRecord, now time.Time) map[string]interface{} {
	available, reason := evaluateInviteAvailability(record, now)
	remainingUses := interface{}(nil)
	if record.MaxUses > 0 {
		remainingUses = maxInt(int(record.MaxUses-record.UsedCount), 0)
	}
	return map[string]interface{}{
		"id":                            record.ID,
		"code":                          record.Code,
		"name":                          record.Name,
		"accountId":                     record.AccountID,
		"accountName":                   record.AccountName,
		"accountIds":                    record.AccountIDs,
		"domain":                        record.Domain,
		"domains":                       record.Domains,
		"usageLocation":                 record.UsageLocation,
		"skuIds":                        record.SKUIDs,
		"maxUses":                       record.MaxUses,
		"usedCount":                     record.UsedCount,
		"remainingUses":                 remainingUses,
		"enabled":                       record.Enabled,
		"forceChangePasswordNextSignIn": record.ForceChangePasswordNextSignIn,
		"batchId":                       emptyToNil(record.BatchID),
		"expiresAt":                     emptyToNil(record.ExpiresAt),
		"available":                     available,
		"availabilityReason":            emptyToNil(reason),
		"createdAt":                     record.CreatedAt,
		"updatedAt":                     record.UpdatedAt,
	}
}

func registrationToMap(record registrationRecord) map[string]interface{} {
	return map[string]interface{}{
		"id":                record.ID,
		"inviteId":          record.InviteID,
		"inviteName":        record.InviteName,
		"publicPageName":    record.InviteName,
		"inviteCode":        emptyToNil(record.InviteCode),
		"accountId":         record.AccountID,
		"accountName":       record.AccountName,
		"displayName":       record.DisplayName,
		"userPrincipalName": record.UserPrincipalName,
		"graphUserId":       emptyToNil(record.GraphUserID),
		"status":            record.Status,
		"errorMessage":      emptyToNil(record.ErrorMessage),
		"createdAt":         record.CreatedAt,
	}
}

// generateRegistrationPassword 生成公开注册的初始密码：12 位混合大小写字母、
// 数字与可见符号，避免弱口令触发 M365 密码策略拒绝。
func generateRegistrationPassword() (string, error) {
	const length = 12
	lower := "abcdefghijklmnopqrstuvwxyz"
	upper := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits := "0123456789"
	symbols := "-_@#$%!?"
	password := make([]byte, 0, length)
	for _, charset := range []string{lower, upper, digits, symbols} {
		next, err := randomCharsetByte(charset)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	for len(password) < length {
		next, err := randomCharsetByte(lower + upper + digits + symbols)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	if err := shuffleBytes(password); err != nil {
		return "", err
	}
	return string(password), nil
}

func generateInviteCode() (string, error) {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func generateTemporaryPassword() (string, error) {
	const (
		passwordLength = 18
		upperCharset   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		lowerCharset   = "abcdefghijkmnopqrstuvwxyz"
		digitCharset   = "23456789"
		symbolCharset  = "!@#$%^*-_+=?"
	)
	requiredCharsets := []string{upperCharset, lowerCharset, digitCharset, symbolCharset}
	allCharsets := upperCharset + lowerCharset + digitCharset + symbolCharset
	password := make([]byte, 0, passwordLength)

	for _, charset := range requiredCharsets {
		next, err := randomCharsetByte(charset)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	for len(password) < passwordLength {
		next, err := randomCharsetByte(allCharsets)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	if err := shuffleBytes(password); err != nil {
		return "", err
	}
	return string(password), nil
}

func randomCharsetByte(charset string) (byte, error) {
	if charset == "" {
		return 0, errors.New("empty charset")
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	if err != nil {
		return 0, err
	}
	return charset[index.Int64()], nil
}

func shuffleBytes(items []byte) error {
	for i := len(items) - 1; i > 0; i-- {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(index.Int64())
		items[i], items[j] = items[j], items[i]
	}
	return nil
}

func isPasswordComplexityError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "password complexity") ||
		strings.Contains(message, "password does not comply") ||
		strings.Contains(message, "specified password does not comply")
}

func evaluateInviteAvailability(record inviteRecord, now time.Time) (bool, string) {
	if !record.Enabled {
		return false, "disabled"
	}
	if record.MaxUses > 0 && record.UsedCount >= record.MaxUses {
		return false, "exhausted"
	}
	if record.ExpiresAt != "" {
		expiresAt, err := parseFlexibleTime(record.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			return false, "expired"
		}
	}
	return true, ""
}

func normalizeOptionalTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := parseFlexibleTime(value)
	if err != nil {
		return "", errors.New("invalid expiresAt")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func resolvedInviteUsageLocation(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized != "" {
		return normalized
	}
	return "CN"
}

func parseFlexibleTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("unsupported time format")
}

func decodeStringSliceJSON(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	items := []string{}
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return items
	}
	return []string{}
}

func decodeInt64SliceJSON(value string) []int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return []int64{}
	}
	items := []int64{}
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return items
	}
	return []int64{}
}

func int64Array(value interface{}) []int64 {
	result := []int64{}
	for _, item := range interfaceArray(value) {
		number := numberValue(item)
		if number > 0 {
			result = append(result, number)
		}
	}
	return uniqueInt64(result)
}

func normalizeInviteTargets(ctx context.Context, db *sql.DB, record inviteRecord) (inviteRecord, error) {
	if record.AccountID > 0 && len(record.AccountIDs) == 0 {
		record.AccountIDs = []int64{record.AccountID}
	}
	record.AccountIDs = uniqueInt64(record.AccountIDs)
	record.Domains = normalizeDomainSlice(append(record.Domains, record.Domain))
	if len(record.AccountIDs) == 0 && len(record.Domains) == 0 {
		return inviteRecord{}, errors.New("at least one account or domain is required")
	}

	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return inviteRecord{}, err
	}
	targets := resolveInviteTargetsFromAccounts(record, accounts)
	if len(targets) == 0 {
		return inviteRecord{}, errors.New("no available tenant/domain targets matched")
	}

	record.AccountID = targets[0].ID
	record.AccountName = targets[0].Name
	record.Domain = targets[0].Domain
	record.AccountIDs = make([]int64, 0, len(targets))
	record.Domains = make([]string, 0, len(targets))
	for _, target := range targets {
		record.AccountIDs = append(record.AccountIDs, target.ID)
		record.Domains = append(record.Domains, target.Domain)
	}
	record.AccountIDs = uniqueInt64(record.AccountIDs)
	record.Domains = normalizeDomainSlice(record.Domains)
	if record.MaxUses <= 0 {
		record.MaxUses = 1
	}
	return record, nil
}

type inviteTarget struct {
	ID     int64
	Name   string
	Domain string
}

func resolveInviteTargetsFromAccounts(record inviteRecord, accounts []accountRecord) []inviteTarget {
	accountFilter := map[int64]bool{}
	for _, id := range record.AccountIDs {
		if id > 0 {
			accountFilter[id] = true
		}
	}
	domainFilter := map[string]bool{}
	for _, domain := range record.Domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized != "" {
			domainFilter[normalized] = true
		}
	}

	if len(accountFilter) == 1 && len(domainFilter) > 0 {
		targets := []inviteTarget{}
		for _, account := range accounts {
			if !accountFilter[account.ID] {
				continue
			}
			for domain := range domainFilter {
				targets = append(targets, inviteTarget{
					ID:     account.ID,
					Name:   account.Name,
					Domain: domain,
				})
			}
			break
		}
		if len(targets) > 0 {
			sortInviteTargets(targets)
			return targets
		}
	}

	targets := []inviteTarget{}
	for _, account := range accounts {
		domains := accountDomains(account)
		if len(domains) == 0 {
			continue
		}
		if len(accountFilter) > 0 && !accountFilter[account.ID] {
			continue
		}
		for _, domain := range domains {
			if len(domainFilter) > 0 && !domainFilter[domain] {
				continue
			}
			targets = append(targets, inviteTarget{
				ID:     account.ID,
				Name:   account.Name,
				Domain: domain,
			})
		}
	}
	sortInviteTargets(targets)
	return targets
}

func sortInviteTargets(targets []inviteTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Domain != targets[j].Domain {
			return targets[i].Domain < targets[j].Domain
		}
		if targets[i].ID != targets[j].ID {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].Name < targets[j].Name
	})
}

func resolveInviteRegistrationTarget(ctx context.Context, db *sql.DB, invite inviteRecord, payload map[string]interface{}) (inviteTarget, error) {
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return inviteTarget{}, err
	}
	targets := resolveInviteTargetsFromAccounts(invite, accounts)
	if len(targets) == 0 {
		return inviteTarget{}, errors.New("invite has no available tenant targets")
	}

	accountID := numberValue(payload["accountId"])
	domain := strings.ToLower(strings.TrimSpace(stringValue(payload["domain"], "")))
	if accountID == 0 && domain == "" && len(targets) == 1 {
		return targets[0], nil
	}
	for _, target := range targets {
		if accountID > 0 && target.ID == accountID {
			if domain == "" || domain == target.Domain {
				return target, nil
			}
		}
		if domain != "" && target.Domain == domain {
			if accountID == 0 || accountID == target.ID {
				return target, nil
			}
		}
	}
	if len(targets) > 1 {
		return inviteTarget{}, errors.New("accountId or domain is required for this invite")
	}
	return inviteTarget{}, errors.New("selected account/domain is not allowed by this invite")
}

func publicInvitePayload(ctx context.Context, db *sql.DB, record inviteRecord, now time.Time) map[string]interface{} {
	payload := inviteToMap(record, now)
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		payload["targets"] = []map[string]interface{}{}
		return payload
	}
	targets := resolveInviteTargetsFromAccounts(record, accounts)
	targetItems := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		targetItems = append(targetItems, map[string]interface{}{
			"accountId":   target.ID,
			"accountName": target.Name,
			"domain":      target.Domain,
		})
	}
	payload["targets"] = targetItems
	payload["targetCount"] = len(targetItems)
	return payload
}

func licenseAssignmentsFromIDs(ids []string) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		items = append(items, map[string]interface{}{"skuId": normalized})
	}
	return items
}

func persistRegistrationResult(ctx context.Context, db *sql.DB, invite inviteRecord, accountID int64, displayName, userPrincipalName, graphUserID, status, errorMessage string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if status == "success" || status == "partial" {
		if _, err := tx.ExecContext(ctx, `UPDATE m365_registration_invites SET used_count = used_count + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, invite.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO m365_registration_records (invite_id, account_id, display_name, user_principal_name, graph_user_id, status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		invite.ID,
		accountID,
		displayName,
		userPrincipalName,
		nullIfEmpty(graphUserID),
		status,
		nullIfEmpty(errorMessage),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func decodeGraphError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var envelope graphErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("graph request failed: %s", message)
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func envURL(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}

func parseID(text string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	defer r.Body.Close()
	payload := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func stringValue(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return fallback
		}
		return typed
	case json.Number:
		return typed.String()
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return fallback
		}
		return text
	}
}

func objectValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok && typed != nil {
		return typed
	}
	return map[string]interface{}{}
}

func objectArray(value interface{}) []map[string]interface{} {
	list := []map[string]interface{}{}
	for _, item := range interfaceArray(value) {
		list = append(list, objectValue(item))
	}
	return list
}

func interfaceArray(value interface{}) []interface{} {
	if value == nil {
		return []interface{}{}
	}
	if typed, ok := value.([]interface{}); ok {
		return typed
	}
	return []interface{}{}
}

func stringArray(value interface{}) []string {
	result := []string{}
	for _, item := range interfaceArray(value) {
		text := strings.TrimSpace(stringValue(item, ""))
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func numberValue(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return fallback
		}
	case float64:
		return typed != 0
	default:
		return fallback
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func clampPositiveInt64(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func nullIfEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func pickDefaultDomain(org map[string]interface{}) string {
	domains := extractVerifiedDomains(org)
	if len(domains) > 0 {
		return domains[0]
	}
	return ""
}

func extractVerifiedDomains(org map[string]interface{}) []string {
	defaultDomains := []string{}
	otherDomains := []string{}
	for _, item := range interfaceArray(org["verifiedDomains"]) {
		domain := objectValue(item)
		name := strings.ToLower(strings.TrimSpace(stringValue(domain["name"], "")))
		if name == "" {
			continue
		}
		if boolValue(domain["isDefault"], false) {
			defaultDomains = append(defaultDomains, name)
			continue
		}
		otherDomains = append(otherDomains, name)
	}
	return normalizeDomainSlice(append(defaultDomains, otherDomains...))
}

func accountDomains(record accountRecord) []string {
	if len(record.VerifiedDomains) > 0 {
		return normalizeDomainSlice(record.VerifiedDomains)
	}
	if strings.TrimSpace(record.DefaultDomain) == "" {
		return []string{}
	}
	return []string{strings.ToLower(strings.TrimSpace(record.DefaultDomain))}
}

func mustJSONString(value interface{}) string {
	raw, err := jsonString(value)
	if err != nil {
		return "[]"
	}
	return raw
}

func maskSecret(secret string) string {
	plain := secure.SecureDecrypt(secret)
	if plain == "" {
		return ""
	}
	if len(plain) <= 8 {
		return strings.Repeat("*", len(plain))
	}
	return plain[:4] + strings.Repeat("*", 8) + plain[len(plain)-4:]
}

func emptyToNil(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func clampPositive(raw string, fallback, max int) string {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return strconv.Itoa(value)
}

func escapeSearch(value string) string {
	replacer := strings.NewReplacer(`"`, `\"`)
	return replacer.Replace(value)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstInt64(items []int64) int64 {
	if len(items) == 0 {
		return 0
	}
	return items[0]
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func uniqueInt64(items []int64) []int64 {
	result := make([]int64, 0, len(items))
	seen := map[int64]bool{}
	for _, item := range items {
		if item <= 0 || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func normalizeDomainSlice(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

func ensureTableColumn(ctx context.Context, db *sql.DB, tableName, columnName, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", tableName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, columnName) {
			return nil
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+tableName+` ADD COLUMN `+columnName+` `+definition); err != nil {
		return fmt.Errorf("alter %s add %s: %w", tableName, columnName, err)
	}
	return nil
}

const inviteSelectSQL = `SELECT i.id, i.code, i.name, i.account_id, COALESCE(a.name, ''), COALESCE(i.account_ids, ''), i.domain, COALESCE(i.domains, ''), COALESCE(i.usage_location, ''), COALESCE(i.sku_ids, ''), COALESCE(i.max_uses, 1), COALESCE(i.used_count, 0), COALESCE(i.enabled, 1), COALESCE(i.force_change_password_next_sign_in, 0), COALESCE(i.batch_id, ''), COALESCE(i.expires_at, ''), i.created_at, i.updated_at
FROM m365_registration_invites i
LEFT JOIN m365_accounts a ON a.id = i.account_id`

func (s *Service) writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAccountNotFound):
		response.Error(w, http.StatusNotFound, err.Error())
	default:
		response.Error(w, http.StatusBadRequest, err.Error())
	}
}
