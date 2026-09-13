package m365

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
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

type rowScanner interface {
	Scan(dest ...interface{}) error
}

type inviteTarget struct {
	ID     int64
	Name   string
	Domain string
}
