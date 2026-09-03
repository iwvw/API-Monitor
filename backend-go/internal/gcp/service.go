package gcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg       config.Config
	store     *database.Store
	schema    database.SchemaEnsurer
	httpHTTP  *http.Client
	tokens    *tokenCache
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:       cfg,
		store:     database.New(cfg),
		httpHTTP:  &http.Client{Timeout: requestTimeout},
		tokens:    newTokenCache(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	} else {
		applog.Warn(ctx, "gcp", "init schema open failed", "error", err)
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 用 EscapedPath 切分再逐段 PathUnescape：r.URL.Path 会把 %2F 解码成 /，
	// 导致对象名（可含斜杠）与 operation 名（多段路径）在切分后失配。这里保留
	// 每个动态段的原始编码，切分后再统一解码，斜杠对象名/operation 才能命中路由。
	escapedPath := r.URL.EscapedPath()
	if escapedPath == "" {
		escapedPath = r.URL.Path
	}
	path := strings.TrimPrefix(escapedPath, "/api/gcp")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
		for i, part := range parts {
			if decoded, err := url.PathUnescape(part); err == nil {
				parts[i] = decoded
			}
		}
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.accountVerify(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "projects" && r.Method == http.MethodGet:
		s.projects(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "default-project" && r.Method == http.MethodPut:
		s.setDefaultProject(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances":
		s.instances(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances":
		s.instanceDetail(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && parts[6] == "actions" && r.Method == http.MethodPost:
		s.instanceAction(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && parts[6] == "labels" && r.Method == http.MethodPost:
		s.instanceLabels(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "instances" && parts[6] == "operations" && r.Method == http.MethodGet:
		s.operationStatus(w, r, parts[1], parts[3], parts[7])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "disks":
		s.disks(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "disks":
		s.diskDetail(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "disks" && (parts[6] == "resize" || parts[6] == "snapshot") && r.Method == http.MethodPost:
		s.diskAction(w, r, parts[1], parts[3], parts[5], parts[6])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "zones" && r.Method == http.MethodGet:
		s.zones(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "machine-types" && r.Method == http.MethodGet:
		s.machineTypes(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "images" && r.Method == http.MethodGet:
		s.images(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "subnetworks" && r.Method == http.MethodGet:
		s.subnetworks(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "firewalls" && r.Method == http.MethodGet:
		s.firewalls(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "addresses" && r.Method == http.MethodGet:
		s.addresses(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "buckets" && r.Method == http.MethodGet:
		s.buckets(w, r, parts[1], parts[3])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "buckets" && r.Method == http.MethodPost:
		s.createBucket(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "buckets" && r.Method == http.MethodDelete:
		s.deleteBucket(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "buckets" && parts[4] == "objects":
		s.objects(w, r, parts[1], parts[3])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "buckets" && parts[4] == "objects" && parts[6] == "download-url" && r.Method == http.MethodGet:
		s.objectDownloadURL(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "buckets" && parts[4] == "objects" && parts[6] == "download" && r.Method == http.MethodGet:
		s.objectDownload(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "buckets" && parts[4] == "objects" && r.Method == http.MethodDelete:
		s.deleteObject(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "billing-accounts" && r.Method == http.MethodGet:
		s.billingAccounts(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "billing-info" && r.Method == http.MethodGet:
		s.billingInfo(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "projects" && parts[4] == "model-usage" && r.Method == http.MethodGet:
		s.modelUsage(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "billing-accounts" && parts[4] == "budgets" && r.Method == http.MethodGet:
		s.budgets(w, r, parts[1], parts[3])
	default:
		response.Error(w, http.StatusNotFound, "gcp route not implemented")
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
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "gcp account not found")
				return
			}
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		s.tokens.invalidateAccount(id)
		response.OK(w, map[string]interface{}{"id": id})
	case http.MethodDelete:
		if err := deleteAccount(r.Context(), db, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "gcp account not found")
				return
			}
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
	if err := s.verifyAccount(r.Context(), account); err != nil {
		_ = updateVerifyStatus(r.Context(), db, account.ID, "failed", err.Error())
		response.Error(w, http.StatusBadGateway, "验证 GCP 账号失败："+err.Error())
		return
	}
	_ = updateVerifyStatus(r.Context(), db, account.ID, "success", "")
	response.OK(w, map[string]interface{}{"status": "success", "message": "账号验证成功"})
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
			response.Error(w, http.StatusNotFound, "gcp account not found")
			return Account{}, nil, false
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return Account{}, nil, false
	}
	return account, db, true
}

// clientForAccount 解密账号 SA JSON 并构造带 token 缓存的 REST client。
func (s *Service) clientForAccount(ctx context.Context, w http.ResponseWriter, account Account, scope string) (*client, bool) {
	if account.ServiceAccountJSON == "" {
		if w != nil {
			response.Error(w, http.StatusBadRequest, "账号缺少 Service Account JSON")
		}
		return nil, false
	}
	provider := &tokenProvider{
		httpClient: s.httpHTTP,
		cache:      s.tokens,
		scope:      scope,
		key:        strconv.FormatInt(account.ID, 10) + ":" + scope,
	}
	sa, err := parseServiceAccount(account.ServiceAccountJSON)
	if err != nil {
		if w != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
		}
		return nil, false
	}
	provider.sa = sa
	return &client{http: s.httpHTTP, auth: provider}, true
}

// buildClient 纯构造版本（不写响应），供非 HTTP 路径复用。
func (s *Service) buildClient(account Account, scope string) (*client, error) {
	if account.ServiceAccountJSON == "" {
		return nil, errors.New("账号缺少 Service Account JSON")
	}
	provider := &tokenProvider{
		httpClient: s.httpHTTP,
		cache:      s.tokens,
		scope:      scope,
		key:        strconv.FormatInt(account.ID, 10) + ":" + scope,
	}
	sa, err := parseServiceAccount(account.ServiceAccountJSON)
	if err != nil {
		return nil, err
	}
	provider.sa = sa
	return &client{http: s.httpHTTP, auth: provider}, nil
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