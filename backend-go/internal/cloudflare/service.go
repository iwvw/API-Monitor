package cloudflare

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	defaultAPIBase = "https://api.cloudflare.com"
	requestTimeout = 20 * time.Second
)

type Service struct {
	cfg     config.Config
	store   *database.Store
	schema  database.SchemaEnsurer
	client  *http.Client
	apiBase string
}

type verificationResult struct {
	Valid     bool
	Status    string
	ExpiresOn interface{}
	Email     string
	Error     string
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:     cfg,
		store:   database.New(cfg),
		client:  &http.Client{Timeout: requestTimeout},
		apiBase: envURL("CLOUDFLARE_API_BASE_URL", defaultAPIBase),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/cloudflare")
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
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets":
		s.r2Buckets(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "metrics":
		s.r2Metrics(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets":
		s.deleteR2Bucket(w, r, parts[1], parts[4])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects":
		s.r2Objects(w, r, parts[1], parts[4])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[6] == "folder-download":
		s.r2FolderDownload(w, r, parts[1], parts[4])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects":
		s.r2ObjectMutation(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "download-info":
		s.r2ObjectDownloadInfo(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "download":
		s.r2ObjectDownload(w, r, parts[1], parts[4], parts[6])
	case len(parts) == 8 && parts[0] == "accounts" && parts[2] == "r2" && parts[3] == "buckets" && parts[5] == "objects" && parts[7] == "preview":
		s.r2ObjectPreview(w, r, parts[1], parts[4], parts[6])

	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "tunnels":
		s.tunnels(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "tunnels":
		s.tunnelMutation(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "configuration":
		s.tunnelConfiguration(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "token":
		s.tunnelToken(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "tunnels" && parts[4] == "connections":
		s.tunnelConnections(w, r, parts[1], parts[3])

	case len(parts) == 1 && parts[0] == "accounts":
		s.accounts(w, r)
	case len(parts) == 1 && parts[0] == "record-types" && r.Method == http.MethodGet:
		response.JSON(w, http.StatusOK, supportedRecordTypes())
	case len(parts) == 1 && parts[0] == "zones" && r.Method == http.MethodGet:
		s.allZones(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "export" && r.Method == http.MethodGet:
		s.exportAccounts(w, r)
	case len(parts) == 2 && parts[0] == "export" && parts[1] == "accounts" && r.Method == http.MethodGet:
		s.exportAccountsRaw(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "accounts" && r.Method == http.MethodPost:
		s.importAccounts(w, r)
	case len(parts) == 1 && parts[0] == "templates":
		s.templates(w, r)
	case len(parts) == 2 && parts[0] == "templates":
		s.templateMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "templates" && parts[2] == "apply" && r.Method == http.MethodPost:
		s.applyTemplate(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "templates" && r.Method == http.MethodPost:
		s.importTemplates(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.accountMutation(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyStoredAccount(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "token" && r.Method == http.MethodGet:
		s.accountToken(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "cf-account-id" && r.Method == http.MethodGet:
		s.cloudflareAccountIDRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "zones":
		s.accountZones(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "pages":
		s.pages(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "pages":
		s.pagesProject(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "deployments":
		s.pagesDeployments(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "deployments":
		s.deletePagesDeployment(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "domains":
		s.pagesDomains(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "pages" && parts[4] == "domains":
		s.deletePagesDomain(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "workers":
		s.workers(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "workers":
		s.workerScript(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "toggle" && r.Method == http.MethodPost:
		s.toggleWorker(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "analytics" && r.Method == http.MethodGet:
		s.workerAnalytics(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "domains":
		s.workerDomains(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "workers" && parts[4] == "domains" && r.Method == http.MethodDelete:
		s.deleteWorkerDomain(w, r, parts[1], parts[3], parts[5])
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "zones" && r.Method == http.MethodDelete:
		s.deleteZone(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "workers" && parts[5] == "routes":
		s.workerRoutes(w, r, parts[1], parts[3])
	case len(parts) == 7 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "workers" && parts[5] == "routes":
		s.workerRouteMutation(w, r, parts[1], parts[3], parts[6])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "records":
		s.zoneRecords(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "purge" && r.Method == http.MethodPost:
		s.purgeZoneCache(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "ssl":
		s.zoneSSL(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "analytics" && r.Method == http.MethodGet:
		s.zoneAnalytics(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "switch" && r.Method == http.MethodPost:
		s.switchDNSContent(w, r, parts[1], parts[3])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "batch" && r.Method == http.MethodPost:
		s.batchCreateRecords(w, r, parts[1], parts[3])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "zones" && parts[4] == "records" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s.recordMutation(w, r, parts[1], parts[3], parts[5])

	// Email Routing
	case len(parts) == 4 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing":
		s.emailRoutingSettings(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "zones":
		s.emailRoutingZones(w, r, parts[1])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "addresses":
		s.emailRoutingAddresses(w, r, parts[1])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "addresses":
		s.emailRoutingDeleteAddress(w, r, parts[1], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "rules":
		s.emailRoutingRules(w, r, parts[1])
	case len(parts) == 6 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "rules":
		s.emailRoutingDeleteRule(w, r, parts[1], parts[5])
	case len(parts) == 5 && parts[0] == "accounts" && parts[2] == "email" && parts[3] == "routing" && parts[4] == "inbox":
		s.emailRoutingInbox(w, r, parts[1])
	default:
		response.Error(w, http.StatusNotFound, "cloudflare route not implemented")
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
