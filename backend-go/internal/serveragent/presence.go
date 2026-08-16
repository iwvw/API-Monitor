package serveragent

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

type agentPresenceStatus string

const (
	agentPresenceOnline  agentPresenceStatus = "online"
	agentPresenceSuspect agentPresenceStatus = "suspect"
	agentPresenceOffline agentPresenceStatus = "offline"
)

type agentPresenceConfig struct {
	mode            string
	suspectAfter    time.Duration
	offlineAfter    time.Duration
	startupGrace    time.Duration
	recoverySamples int
}

type agentPresenceRecord struct {
	ServerID             string
	Status               agentPresenceStatus
	ConnectionActive     bool
	LastConnect          time.Time
	LastDisconnect       time.Time
	LastDisconnectReason string
	LastHeartbeat        time.Time
	LastMetricsSeen      time.Time
	ReconnectCount       int64
	Transport            string
	SampleIntervalMs     int64
	SuppressUntil        time.Time
	RecoverySamples      int
}

type agentPresenceManager struct {
	service   *Service
	cfg       agentPresenceConfig
	startedAt time.Time
	mu        sync.RWMutex
	records   map[string]*agentPresenceRecord
	stopCh    chan struct{}
	seedOnce  sync.Once
}

func newAgentPresenceManager(s *Service) *agentPresenceManager {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("API_MONITOR_AGENT_PRESENCE_MODE")))
	if mode == "" {
		mode = "stable"
	}
	return &agentPresenceManager{
		service:   s,
		startedAt: time.Now(),
		records:   make(map[string]*agentPresenceRecord),
		stopCh:    make(chan struct{}),
		cfg: agentPresenceConfig{
			mode:            mode,
			suspectAfter:    envDurationMs("API_MONITOR_AGENT_SUSPECT_AFTER_MS", 75*time.Second),
			offlineAfter:    envDurationMs("API_MONITOR_AGENT_OFFLINE_AFTER_MS", 180*time.Second),
			startupGrace:    envDurationMs("API_MONITOR_AGENT_STARTUP_GRACE_MS", 300*time.Second),
			recoverySamples: envIntDefault("API_MONITOR_AGENT_RECOVERY_SAMPLES", 2),
		},
	}
}

func (p *agentPresenceManager) legacyMode() bool {
	return p != nil && p.cfg.mode == "legacy"
}

func (p *agentPresenceManager) start() {
	if p == nil || p.legacyMode() {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.check()
			case <-p.stopCh:
				return
			}
		}
	}()
}

func (p *agentPresenceManager) stop() {
	if p == nil {
		return
	}
	close(p.stopCh)
}

func (p *agentPresenceManager) recordConnect(serverID, transport string) {
	if p == nil || serverID == "" {
		return
	}
	now := time.Now()
	p.mu.Lock()
	rec := p.ensureLocked(serverID)
	rec.ConnectionActive = true
	rec.LastConnect = now
	rec.ReconnectCount++
	rec.Transport = transport
	if rec.Status == "" {
		rec.Status = agentPresenceSuspect
	}
	p.mu.Unlock()
}

func (p *agentPresenceManager) recordDisconnect(serverID, reason string) {
	if p == nil || serverID == "" {
		return
	}
	now := time.Now()
	var changed bool
	p.mu.Lock()
	rec := p.ensureLocked(serverID)
	previous := rec.Status
	rec.ConnectionActive = false
	rec.LastDisconnect = now
	rec.LastDisconnectReason = strings.TrimSpace(reason)
	if rec.LastDisconnectReason == "" {
		rec.LastDisconnectReason = "disconnected"
	}
	rec.RecoverySamples = 0
	if rec.Status == agentPresenceOnline || rec.Status == "" {
		rec.Status = agentPresenceSuspect
		changed = previous != agentPresenceSuspect
	}
	p.mu.Unlock()

	if changed {
		p.applyInterrupted(serverID)
	}
}

func (p *agentPresenceManager) recordHeartbeat(serverID, source string, sampleIntervalMs int64) {
	if p == nil || serverID == "" {
		return
	}
	now := time.Now()
	var changed bool
	var previous agentPresenceStatus

	p.mu.Lock()
	rec := p.ensureLocked(serverID)
	previous = rec.Status
	rec.LastHeartbeat = now
	if source == "state" || source == "metrics" {
		rec.LastMetricsSeen = now
	}
	if sampleIntervalMs > 0 {
		rec.SampleIntervalMs = sampleIntervalMs
	}
	if previous == agentPresenceOffline {
		rec.RecoverySamples++
		if rec.RecoverySamples >= p.cfg.recoverySamples {
			rec.Status = agentPresenceOnline
			changed = true
		}
	} else {
		rec.RecoverySamples = p.cfg.recoverySamples
		if previous != agentPresenceOnline {
			rec.Status = agentPresenceOnline
			changed = true
		}
	}
	rec.ConnectionActive = true
	p.mu.Unlock()

	if changed {
		p.applyOnline(serverID, previous)
	}
}

func (p *agentPresenceManager) suppress(serverID string, duration time.Duration) {
	if p == nil || serverID == "" || duration <= 0 {
		return
	}
	until := time.Now().Add(duration)
	p.mu.Lock()
	rec := p.ensureLocked(serverID)
	if until.After(rec.SuppressUntil) {
		rec.SuppressUntil = until
	}
	p.mu.Unlock()
}

func (p *agentPresenceManager) snapshot(serverID string) map[string]interface{} {
	if p == nil {
		return map[string]interface{}{}
	}
	p.mu.RLock()
	rec := p.records[serverID]
	if rec == nil {
		p.mu.RUnlock()
		return map[string]interface{}{
			"presence_status":   "offline",
			"connection_active": false,
		}
	}
	out := map[string]interface{}{
		"presence_status":        string(rec.Status),
		"connection_active":      rec.ConnectionActive,
		"last_connect_at":        timeToMillis(rec.LastConnect),
		"last_disconnect_at":     timeToMillis(rec.LastDisconnect),
		"last_disconnect_reason": rec.LastDisconnectReason,
		"last_heartbeat_at":      timeToMillis(rec.LastHeartbeat),
		"last_metrics_seen_at":   timeToMillis(rec.LastMetricsSeen),
		"reconnect_count":        rec.ReconnectCount,
		"transport":              rec.Transport,
		"sample_interval_ms":     rec.SampleIntervalMs,
		"suppress_until":         timeToMillis(rec.SuppressUntil),
	}
	if !rec.LastMetricsSeen.IsZero() {
		out["metrics_age_ms"] = int64(time.Since(rec.LastMetricsSeen) / time.Millisecond)
	} else {
		out["metrics_age_ms"] = int64(0)
	}
	p.mu.RUnlock()
	return out
}

func (p *agentPresenceManager) ensureLocked(serverID string) *agentPresenceRecord {
	rec := p.records[serverID]
	if rec == nil {
		rec = &agentPresenceRecord{ServerID: serverID, Status: agentPresenceOffline}
		p.records[serverID] = rec
	}
	return rec
}

func (p *agentPresenceManager) check() {
	if p == nil || p.legacyMode() {
		return
	}
	// 数据库播种只需一次：运行期间新上线的服务器会通过
	// recordConnect/recordHeartbeat 直接建立记录，无需每 5 秒全表扫描。
	p.seedOnce.Do(p.ensureOnlineRows)
	now := time.Now()
	var offlineIDs []string
	var suspectIDs []string
	type refreshTarget struct {
		serverID string
		status   string
	}
	var refreshTargets []refreshTarget

	p.mu.Lock()
	for serverID, rec := range p.records {
		if rec.Status == agentPresenceOffline {
			refreshTargets = append(refreshTargets, refreshTarget{serverID: serverID, status: "offline"})
			continue
		}
		lastSignal := maxTime(rec.LastHeartbeat, rec.LastMetricsSeen, rec.LastConnect)
		if lastSignal.IsZero() {
			lastSignal = p.startedAt
		}
		age := now.Sub(lastSignal)
		offlineAfter := p.offlineAfterFor(rec)
		if age >= offlineAfter {
			rec.Status = agentPresenceOffline
			rec.ConnectionActive = false
			rec.RecoverySamples = 0
			offlineIDs = append(offlineIDs, serverID)
			continue
		}
		if rec.Status == agentPresenceOnline && age >= p.cfg.suspectAfter {
			rec.Status = agentPresenceSuspect
			suspectIDs = append(suspectIDs, serverID)
		} else if rec.Status == agentPresenceSuspect {
			refreshTargets = append(refreshTargets, refreshTarget{serverID: serverID, status: "interrupted"})
		} else {
			// 在线状态也周期触发 resolve 自愈：后端重启后若无状态变化事件，
			// 残留的 open 生命周期消息（隔离/离线通知）需要被编辑为恢复内容并清除。
			refreshTargets = append(refreshTargets, refreshTarget{serverID: serverID, status: "online"})
		}
	}
	p.mu.Unlock()

	for _, serverID := range suspectIDs {
		p.applyInterrupted(serverID)
	}
	for _, serverID := range offlineIDs {
		p.applyOffline(serverID)
	}
	for _, target := range refreshTargets {
		p.refreshNotification(target.serverID, target.status)
	}
}

func (p *agentPresenceManager) offlineAfterFor(rec *agentPresenceRecord) time.Duration {
	offlineAfter := p.cfg.offlineAfter
	if rec.SampleIntervalMs > 0 {
		dynamic := time.Duration(rec.SampleIntervalMs*6) * time.Millisecond
		if dynamic > offlineAfter {
			offlineAfter = dynamic
		}
	}
	return offlineAfter
}

func (p *agentPresenceManager) ensureOnlineRows() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := p.service.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id FROM server_accounts WHERE status = 'online'`)
	if err != nil {
		return
	}
	defer rows.Close()

	p.mu.Lock()
	defer p.mu.Unlock()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			if _, exists := p.records[id]; !exists {
				p.records[id] = &agentPresenceRecord{
					ServerID:         id,
					Status:           agentPresenceOnline,
					LastHeartbeat:    p.startedAt,
					LastMetricsSeen:  p.startedAt,
					RecoverySamples:  p.cfg.recoverySamples,
					ConnectionActive: false,
					SampleIntervalMs: 0,
				}
			}
		}
	}
}

func (p *agentPresenceManager) applyInterrupted(serverID string) {
	if p.service.metricsHub != nil {
		p.service.metricsHub.BroadcastServerStatus(serverID, "interrupted", false)
	}
	if !p.notificationsSuppressed(serverID, time.Now()) {
		p.triggerNotification(context.Background(), serverID, "interrupted")
	}
}

func (p *agentPresenceManager) applyOnline(serverID string, previous agentPresenceStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := p.service.open(ctx)
	if err == nil {
		now := time.Now().Format("2006-01-02 15:04:05")
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts
			SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, updated_at = ?
			WHERE id = ?`, now, now, serverID)
		db.Close()
	}
	if p.service.metricsHub != nil {
		p.service.metricsHub.BroadcastServerStatus(serverID, "online", true)
	}
	if previous == agentPresenceOffline && !p.notificationsSuppressed(serverID, time.Now()) {
		p.triggerNotification(ctx, serverID, "online")
	}
}

func (p *agentPresenceManager) applyOffline(serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := p.service.open(ctx)
	if err == nil {
		now := time.Now().Format("2006-01-02 15:04:05")
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts
			SET status = 'offline', last_check_time = ?, last_check_status = 'disconnected', updated_at = ?
			WHERE id = ?`, now, now, serverID)
		db.Close()
	}
	if p.service.metricsHub != nil {
		p.service.metricsHub.BroadcastServerStatus(serverID, "offline", false)
	}
	if !p.notificationsSuppressed(serverID, time.Now()) {
		p.triggerNotification(ctx, serverID, "offline")
	}
}

func (p *agentPresenceManager) notificationsSuppressed(serverID string, now time.Time) bool {
	if now.Sub(p.startedAt) < p.cfg.startupGrace {
		return true
	}
	p.mu.RLock()
	rec := p.records[serverID]
	suppressed := rec != nil && now.Before(rec.SuppressUntil)
	p.mu.RUnlock()
	return suppressed
}

func (p *agentPresenceManager) triggerNotification(ctx context.Context, serverID, status string) {
	if p.service.notifier == nil {
		return
	}
	db, err := p.service.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	serverName, serverHost := p.serverIdentity(ctx, db, serverID)
	eventData := map[string]interface{}{
		"serverId":   serverID,
		"serverName": serverName,
		"host":       serverHost,
		"hostname":   serverName,
		"status":     status,
	}
	if err := p.service.notifier.Trigger(ctx, "server", status, eventData); err != nil {
		applog.Warn(ctx, "serveragent", "failed to trigger presence notification", "server_id", serverID, "status", status, "error", err.Error())
	}
}

func (p *agentPresenceManager) refreshNotification(serverID, status string) {
	updater, ok := p.service.notifier.(interface {
		RefreshLifecycle(context.Context, string, string, map[string]interface{}) error
	})
	if !ok {
		return
	}
	// 刷新/自愈只编辑或补发已存在的生命周期消息（RefreshLifecycle 内部已有 30s 节流），
	// 不产生新消息轰炸，因此不受 notificationsSuppressed（启动宽限）抑制；
	// 新建 open 消息的 Trigger 路径仍受抑制。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := p.service.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	serverName, serverHost := p.serverIdentity(ctx, db, serverID)
	eventData := map[string]interface{}{
		"serverId": serverID, "serverName": serverName, "host": serverHost,
		"hostname": serverName, "status": status,
	}
	p.mu.RLock()
	rec := p.records[serverID]
	if rec != nil {
		lastActive := maxTime(rec.LastHeartbeat, rec.LastMetricsSeen, rec.LastConnect)
		if !lastActive.IsZero() {
			eventData["lastActive"] = lastActive.UTC().Format(time.RFC3339)
		}
	}
	p.mu.RUnlock()
	_ = updater.RefreshLifecycle(ctx, "server", status, eventData)
}

func (p *agentPresenceManager) serverIdentity(ctx context.Context, db *sql.DB, serverID string) (string, string) {
	var serverName, serverHost string
	_ = db.QueryRowContext(ctx, `SELECT name, host FROM server_accounts WHERE id = ?`, serverID).Scan(&serverName, &serverHost)
	if serverName == "" {
		serverName = serverID
	}
	if serverHost == "" {
		serverHost = serverName
	}
	return serverName, serverHost
}

func maxTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if value.After(out) {
			out = value
		}
	}
	return out
}

func envDurationMs(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return time.Duration(parsed) * time.Millisecond
}

func envIntDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
