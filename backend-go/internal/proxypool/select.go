package proxypool

import (
	"context"
	"strings"
	"time"
)

// SelectProxy 按池选择下一个可用出口代理（轮询游标 + 健康过滤）：
//   - 冷却（cool）/ 429 冻结 / 坏代理沉淀（sunk）中的代理跳过
//   - 全部不可用时返回 ""（调用方按直连兜底）
// 供 openai 端点与 openaibeta 等插件复用同一套池与健康数据。
func (s *Service) SelectProxy(ctx context.Context, poolID, _ string) (string, error) {
	pool, err := s.Get(ctx, poolID)
	if err != nil || pool == nil {
		return "", err
	}
	pool.Proxies = cleanProxies(pool.Proxies)
	if len(pool.Proxies) == 0 {
		return "", nil
	}
	blocked, err := s.loadBlocked(ctx, poolID)
	if err != nil {
		return "", err
	}
	available := make([]string, 0, len(pool.Proxies))
	for _, p := range pool.Proxies {
		if !blocked[p] {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		return "", nil
	}
	idx := s.nextCursor(poolID) % uint64(len(available))
	return available[idx], nil
}

// loadBlocked 读取池中处于禁用/冷却状态的代理（kind ∈ cool/429/sunk 且 until 未过期）。
func (s *Service) loadBlocked(ctx context.Context, poolID string) (map[string]bool, error) {
	blocked := map[string]bool{}
	db, err := s.open(ctx)
	if err != nil {
		return blocked, err
	}
	defer db.Close()
	now := time.Now()
	rows, err := db.QueryContext(ctx, `
		SELECT proxy, kind, until FROM proxypool_state
		WHERE pool_id = ? AND until > ?`, poolID, now.UTC().Format(time.RFC3339))
	if err != nil {
		return blocked, nil
	}
	defer rows.Close()
	for rows.Next() {
		var proxy, kind, untilRaw string
		if err := rows.Scan(&proxy, &kind, &untilRaw); err != nil {
			continue
		}
		if kind == "cool" || kind == "429" || kind == "sunk" {
			blocked[proxy] = true
		}
	}
	return blocked, rows.Err()
}

// ReportResult 反馈单次使用结果并更新健康状态：
//   - ok=false 且 ratelimit：写入 429 冻结（默认 30s）
//   - ok=false 且非 ratelimit：写入 cool 冷却（默认 30s）；连续失败可视为坏代理
//   - ok=true：清除该代理在本池的失败状态
// 供 openai 端点 / openaibeta 在转发失败/成功时回写，与面板测速共用健康数据。
func (s *Service) ReportResult(ctx context.Context, poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) error {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" || poolID == "" {
		return nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	if ok {
		_, err := db.ExecContext(ctx, `DELETE FROM proxypool_state WHERE pool_id = ? AND proxy = ?`, poolID, proxy)
		return err
	}

	dur := 30 * time.Second
	if retryAfter != nil && *retryAfter > 0 {
		dur = *retryAfter
	}
	kind := "cool"
	if ratelimit {
		kind = "429"
	}
	until := time.Now().Add(dur).UTC().Format(time.RFC3339)
	if ratelimit {
		// 429 同时记录失败计数，连续 3 次 429 视为坏代理沉淀。
		if _, err := db.ExecContext(ctx, `
			INSERT INTO proxypool_state(pool_id, proxy, kind, until) VALUES (?, ?, ?, ?)
			ON CONFLICT(pool_id, proxy, kind) DO UPDATE SET until = excluded.until`,
			poolID, proxy, "fail_count", until); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO proxypool_state(pool_id, proxy, kind, until) VALUES (?, ?, ?, ?)
		ON CONFLICT(pool_id, proxy, kind) DO UPDATE SET until = excluded.until`,
		poolID, proxy, kind, until)
	return err
}

// UnbanPool 清除池内全部禁用/冷却状态（面板「一键解封」）。
func (s *Service) UnbanPool(ctx context.Context, poolID string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM proxypool_state WHERE pool_id = ?`, poolID)
	return err
}

// BlockedCount 返回池中当前禁用/冷却的代理数。
func (s *Service) BlockedCount(ctx context.Context, poolID string) (int, error) {
	blocked, err := s.loadBlocked(ctx, poolID)
	if err != nil {
		return 0, err
	}
	return len(blocked), nil
}

// nextCursor 返回池的轮询游标并自增。
func (s *Service) nextCursor(poolID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cursor[poolID]
	s.cursor[poolID] = c + 1
	return c
}
