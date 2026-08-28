package openaibeta

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/config"
)

// Settings 是 Beta 插件持久化配置（openaibeta_settings 单行 JSON）。
type Settings struct {
	// Enabled 是插件总开关；关闭时中继直接 404。
	Enabled bool `json:"enabled"`
	// ProxyEndpointID 复用模型网关某个端点的出口代理池（proxy_pool）；空 = 直连。
	// 代理冷却/禁用状态读取 openai_proxy_state，与网关共享同一份代理健康数据。
	ProxyEndpointID string `json:"proxyEndpointId"`

	RequestTimeout           int    `json:"requestTimeout"`
	MaxRetries               int    `json:"maxRetries"`
	StreamIdleTimeoutSeconds int    `json:"streamIdleTimeoutSeconds"`
	MaxN                     int    `json:"maxN"`
	DebugMode                bool   `json:"debugMode"`
	DropMaxTokens            bool   `json:"dropMaxTokens"`
	AggregateStream          bool   `json:"aggregateStream"`
	ModelTurnGuardEnabled    bool   `json:"modelTurnGuardEnabled"`
	VertexAPIKey             string `json:"vertexAPIKey"`
	CountTokensQuerySignature string `json:"countTokensQuerySignature"`

	Models   []engineconfig.ModelEntry `json:"models"`
	AliasMap map[string]string         `json:"aliasMap"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:                  false,
		RequestTimeout:           180,
		MaxRetries:               1,
		StreamIdleTimeoutSeconds: 30,
		MaxN:                     8,
		ModelTurnGuardEnabled:    true,
		Models:                   engineconfig.DefaultModelRegistry(),
		AliasMap:                 map[string]string{},
	}
}

// Service 是模型网关 Beta 插件（内嵌 Gemini 免费中继）的后端服务。
type Service struct {
	cfg       config.Config
	store     *database.Store
	schemaOnce sync.Once
	schemaErr error

	mu       sync.RWMutex
	settings Settings

	client *relayClient
}

// New 构造服务并加载持久化设置。
func New(cfg config.Config) *Service {
	s := &Service{
		cfg:   cfg,
		store: database.New(cfg),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		s.loadSettings(ctx, db)
		db.Close()
	}
	s.rebuildClient()
	return s
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
		db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS openaibeta_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			data TEXT NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("openaibeta ensure schema: %w", err)
		}
	}
	return nil
}

// loadSettings 从 DB 读取设置；无记录时写入默认值并同步模型 store。
func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM openaibeta_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err != sql.ErrNoRows {
			return
		}
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		s.syncModelStore()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO openaibeta_settings (id, data) VALUES (1, ?)`, string(data))
		return
	}
	var cfg Settings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		cfg = defaultSettings()
	}
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
	s.syncModelStore()
}

// syncModelStore 把设置里的模型注册表/别名同步进引擎 config 内存 store。
func (s *Service) syncModelStore() {
	s.mu.RLock()
	models := s.settings.Models
	alias := s.settings.AliasMap
	s.mu.RUnlock()
	if len(models) == 0 {
		models = engineconfig.DefaultModelRegistry()
	}
	if alias == nil {
		alias = map[string]string{}
	}
	engineconfig.SetModelStore(models, alias)
}

// Settings 返回当前设置的只读副本。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(s.settings)
}

// SaveSettings 校验并持久化设置，随后重建中继客户端与模型 store。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.RequestTimeout <= 0 {
		next.RequestTimeout = 180
	}
	if next.MaxRetries < 0 {
		next.MaxRetries = 0
	}
	if next.StreamIdleTimeoutSeconds <= 0 {
		next.StreamIdleTimeoutSeconds = 30
	}
	if next.MaxN <= 0 {
		next.MaxN = 1
	}
	if next.MaxN > 8 {
		next.MaxN = 8
	}
	if next.Models == nil {
		next.Models = engineconfig.DefaultModelRegistry()
	}
	if next.AliasMap == nil {
		next.AliasMap = map[string]string{}
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO openaibeta_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	s.syncModelStore()
	s.rebuildClient()
	return nil
}

func cloneSettings(in Settings) Settings {
	out := in
	if in.Models != nil {
		out.Models = append([]engineconfig.ModelEntry(nil), in.Models...)
	}
	if in.AliasMap != nil {
		out.AliasMap = make(map[string]string, len(in.AliasMap))
		for k, v := range in.AliasMap {
			out.AliasMap[k] = v
		}
	}
	return out
}
