package assets

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

var errInvalidInput = errors.New("invalid input")

// Service 承载资产管理模块：手工登记册 + 纳管引用的混合模型。
type Service struct {
	cfg        config.Config
	store      *database.Store
	schemaOnce sync.Once
	schemaErr  error
	notifier   Notifier
	schedStart sync.Once
	schedMu    sync.Mutex
	scheduler  *cronRuntime
}

// Asset 是资产的对外结构。expire_at 统一存储为 UTC RFC3339 字符串，
// 展示与日期归属交给前端与 timeutil。
type Asset struct {
	ID             string   `json:"id"`
	Origin         string   `json:"origin"`
	Category       string   `json:"category"`
	AssetType      string   `json:"asset_type"`
	Name           string   `json:"name"`
	Provider       string   `json:"provider"`
	Owner          string   `json:"owner"`
	Location       string   `json:"location"`
	SerialNo       string   `json:"serial_no"`
	Model          string   `json:"model"`
	Status         string   `json:"status"`
	SourceModule   string   `json:"source_module"`
	SourceRefID    string   `json:"source_ref_id"`
	SourceSyncedAt string   `json:"source_synced_at,omitempty"`
	AcquireDate    string   `json:"acquire_date"`
	ExpireAt       string   `json:"expire_at"`
	WarnDays       []int    `json:"warn_days"`
	AutoRenew      bool     `json:"auto_renew"`
	CostAmount     float64  `json:"cost_amount"`
	CostCurrency   string   `json:"cost_currency"`
	CostCycle      string   `json:"cost_cycle"`
	Tags           []string `json:"tags"`
	Metadata       map[string]interface{} `json:"metadata"`
	Remark         string   `json:"remark"`
	DaysLeft       *int     `json:"days_left"`
	DerivedStatus  string   `json:"derived_status"`
	CreatedAt      string   `json:"created_at,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
}

type Settings struct {
	BaseCurrency  string             `json:"base_currency"`
	ExchangeRates map[string]float64 `json:"exchange_rates"`
	WarnDays      []int              `json:"warn_days"`
}

type ExpiringBucket struct {
	Expired  int `json:"expired"`
	Within7  int `json:"within_7"`
	Within30 int `json:"within_30"`
	Normal   int `json:"normal"`
	NoRenew  int `json:"no_renew"`
}

type CurrencyCost struct {
	Currency string  `json:"currency"`
	Monthly  float64 `json:"monthly"`
	OneTime  float64 `json:"one_time"`
	Count    int     `json:"count"`
}

// TypeStat 是按资产类型聚合的统计，用于总览的聚合仪表盘。
type TypeStat struct {
	AssetType    string  `json:"asset_type"`
	Category     string  `json:"category"`
	Count        int     `json:"count"`
	Expiring     int     `json:"expiring"`
	Expired      int     `json:"expired"`
	MonthlyByCcy []CurrencyCost `json:"monthly_by_currency"`
}

type Overview struct {
	PhysicalCount int                     `json:"physical_count"`
	VirtualCount  int                     `json:"virtual_count"`
	ExpiringCount int                     `json:"expiring_count"`
	ExpiredCount  int                     `json:"expired_count"`
	OrphanCount   int                     `json:"orphan_count"`
	Buckets       ExpiringBucket          `json:"buckets"`
	Costs         []CurrencyCost          `json:"costs"`
	TotalMonthly  map[string]float64      `json:"total_monthly"`
	TypeStats     []TypeStat              `json:"type_stats"`
	RecentExpiring []Asset                `json:"recent_expiring"`
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg, store: database.New(cfg)}
}

// Initialize 在启动时幂等建表，保证 schema 在进程启动后即可被审计与查询，
// 而不是等到首个请求才惰性创建。
func (s *Service) Initialize(ctx context.Context) error {
	db, err := s.store.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	s.schemaOnce.Do(func() {
		s.schemaErr = ensureSchema(ctx, db)
	})
	return s.schemaErr
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/assets")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 0:
		switch r.Method {
		case http.MethodGet:
			s.listAssets(w, r)
		case http.MethodPost:
			s.createAsset(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "overview":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.overview(w, r)
	case len(parts) == 1 && parts[0] == "expiring":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.expiring(w, r)
	case len(parts) == 1 && parts[0] == "settings":
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r)
		case http.MethodPut:
			s.updateSettings(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "settings" && parts[1] == "reset":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.resetSettings(w, r)
	case len(parts) == 1 && parts[0] == "categories":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listCategories(w, r)
	case len(parts) == 1 && parts[0] == "stats":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.overview(w, r)
	case len(parts) == 1 && parts[0] == "candidates":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.candidates(w, r)
	case len(parts) == 1 && parts[0] == "links":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.linkAssets(w, r)
	case len(parts) == 1 && parts[0] == "refresh-all":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.refreshAll(w, r)
	case len(parts) == 1 && parts[0] == "scan-expiry":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.scanExpiry(w, r)
	case len(parts) == 2 && parts[1] == "refresh" && r.Method == http.MethodPost:
		s.refreshAsset(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "events" && r.Method == http.MethodGet:
		s.listEvents(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "alerts" && r.Method == http.MethodGet:
		s.listAlerts(w, r, parts[0])
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			s.getAsset(w, r, parts[0])
		case http.MethodPut:
			s.updateAsset(w, r, parts[0])
		case http.MethodDelete:
			s.deleteAsset(w, r, parts[0])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		response.Error(w, http.StatusNotFound, "assets route not implemented")
	}
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	s.schemaOnce.Do(func() {
		s.schemaErr = ensureSchema(ctx, db)
	})
	if s.schemaErr != nil {
		_ = db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}
