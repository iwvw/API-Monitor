package proxypool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// Pool 是一个可独立管理的出口代理池。proxies 为有序代理列表（http/https/socks5）。
type Pool struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Proxies []string `json:"proxies"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Service 是独立代理池插件的后端：持久化（proxy_pools + proxypool_state）、
// 健康状态与按池选择出口代理。openai 端点 / openaibeta 插件可通过注入的
// SelectProxy 复用同一套池与健康数据。
type Service struct {
	cfg   config.Config
	store *database.Store
	mu    sync.Mutex
	// cursor 是每个池的轮询游标（进程内）。
	cursor map[string]uint64
	// runtimeByPool 是每个池的进程内探活缓存（探活结果、出口 IP）。
	runtimeByPool map[string]*runtime
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:           cfg,
		store:         database.New(cfg),
		cursor:        map[string]uint64{},
		runtimeByPool: map[string]*runtime{},
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
		`CREATE TABLE IF NOT EXISTS proxy_pools (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			proxies TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS proxypool_state (
			pool_id TEXT NOT NULL,
			proxy TEXT NOT NULL,
			kind TEXT NOT NULL,
			until DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (pool_id, proxy, kind)
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("proxypool ensure schema: %w", err)
		}
	}
	return nil
}

// List 返回全部代理池。
func (s *Service) List(ctx context.Context) ([]Pool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, name, proxies, created_at, updated_at FROM proxy_pools ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := []Pool{}
	for rows.Next() {
		var p Pool
		var proxiesRaw string
		if err := rows.Scan(&p.ID, &p.Name, &proxiesRaw, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(proxiesRaw), &p.Proxies)
		pools = append(pools, p)
	}
	return pools, rows.Err()
}

// Get 返回单个池；不存在返回 nil。
func (s *Service) Get(ctx context.Context, id string) (*Pool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var p Pool
	var proxiesRaw string
	err = db.QueryRowContext(ctx, `SELECT id, name, proxies, created_at, updated_at FROM proxy_pools WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &proxiesRaw, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(proxiesRaw), &p.Proxies)
	return &p, nil
}

// Create 新建池并返回完整对象。
func (s *Service) Create(ctx context.Context, id, name string, proxies []string) (*Pool, error) {
	proxies = cleanProxies(proxies)
	if id == "" {
		return nil, fmt.Errorf("池 ID 不能为空")
	}
	if name == "" {
		name = id
	}
	now := time.Now().UTC().Format(time.RFC3339)
	proxiesJSON, _ := json.Marshal(proxies)
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO proxy_pools (id, name, proxies, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, string(proxiesJSON), now, now); err != nil {
		return nil, err
	}
	return &Pool{ID: id, Name: name, Proxies: proxies, CreatedAt: now, UpdatedAt: now}, nil
}

// Update 更新池的名称与代理列表。
func (s *Service) Update(ctx context.Context, id, name string, proxies []string) error {
	proxies = cleanProxies(proxies)
	proxiesJSON, _ := json.Marshal(proxies)
	now := time.Now().UTC().Format(time.RFC3339)
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
		UPDATE proxy_pools SET name = ?, proxies = ?, updated_at = ? WHERE id = ?`,
		name, string(proxiesJSON), now, id); err != nil {
		return err
	}
	return nil
}

// Delete 删除池及其健康状态。
func (s *Service) Delete(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM proxy_pools WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM proxypool_state WHERE pool_id = ?`, id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cursor, id)
	s.mu.Unlock()
	return nil
}

// cleanProxies 清洗代理列表：去空、去空白、去重，仅保留 http/https/socks5。
func cleanProxies(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" || seen[p] {
			continue
		}
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "socks5://") {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
