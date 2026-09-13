package serveragent

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
)

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

func (s *Service) StartupError() error { return s.startupErr }

func (s *Service) SetCloudflareTunnelManager(manager cloudflare.ManagedTunnelAPI) {
	s.cloudflare = manager
}

func (s *Service) validateAgentConnection(ctx context.Context, serverID, key string) error {
	serverID = strings.TrimSpace(serverID)
	key = strings.TrimSpace(key)
	if serverID == "" {
		return errors.New("server_id is required")
	}
	if key == "" {
		return errors.New("agent key is required")
	}

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := s.validateAgentKeyForServer(ctx, db, serverID, key); err != nil {
		return err
	}

	var exists int
	if err := db.QueryRowContext(ctx, "SELECT 1 FROM server_accounts WHERE id = ?", serverID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("server not found")
		}
		return err
	}
	return nil
}

func (s *Service) recordAgentSignal(serverID, source string, payload map[string]interface{}) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	if s.registry != nil {
		s.registry.UpdateHeartbeat(serverID)
	}
	sampleIntervalMs := int64(0)
	if payload != nil {
		sampleIntervalMs = getInt64Val(payload, "sample_interval_ms", 0)
		if sampleIntervalMs <= 0 {
			sampleIntervalMs = getInt64Val(payload, "metrics_sample_interval_ms", 0)
		}
	}
	if s.presence != nil {
		s.presence.recordHeartbeat(serverID, source, sampleIntervalMs)
	}
}

func (s *Service) markAgentOnlineLegacy(serverID string) {
	if serverID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := s.open(ctx)
		if err != nil {
			return
		}
		defer db.Close()
		now := time.Now().Format("2006-01-02 15:04:05")
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts
			SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, updated_at = ?
			WHERE id = ?`, now, now, serverID)
		serverName, serverHost := s.serverIdentity(ctx, db, serverID)
		s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "online")
	}()
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "online", true)
	}
}

func (s *Service) markAgentOfflineLegacy(serverID string) {
	if serverID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := s.open(ctx)
		if err != nil {
			return
		}
		defer db.Close()
		now := time.Now().Format("2006-01-02 15:04:05")
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts
			SET status = 'offline', last_check_time = ?, last_check_status = 'disconnected', updated_at = ?
			WHERE id = ?`, now, now, serverID)
		serverName, serverHost := s.serverIdentity(ctx, db, serverID)
		s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "offline")
	}()
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "offline", false)
	}
}

func (s *Service) serverIdentity(ctx context.Context, db *sql.DB, serverID string) (string, string) {
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

func (s *Service) triggerServerStatusNotification(ctx context.Context, serverID, serverName, serverHost, status string) {
	if s.notifier == nil {
		return
	}
	eventData := map[string]interface{}{
		"serverId":   serverID,
		"serverName": serverName,
		"host":       serverHost,
		"hostname":   serverName,
		"status":     status,
	}
	_ = s.notifier.Trigger(ctx, "server", status, eventData)
}

func (s *Service) shouldPersistRealtimeMetrics(serverID string, now time.Time) bool {
	if serverID == "" {
		return false
	}
	interval := s.realtimePersistInterval
	if interval <= 0 {
		interval = defaultRealtimeMetricsPersistInterval
	}
	s.lastPersistMu.Lock()
	defer s.lastPersistMu.Unlock()
	last := s.lastPersist[serverID]
	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	s.lastPersist[serverID] = now
	return true
}

func resolveRealtimeMetricsPersistInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS"))
	if raw == "" {
		return defaultRealtimeMetricsPersistInterval
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultRealtimeMetricsPersistInterval
	}
	interval := time.Duration(value) * time.Millisecond
	if interval < minRealtimeMetricsPersistInterval {
		return minRealtimeMetricsPersistInterval
	}
	return interval
}

func (s *Service) shouldPersistNetworkQuality(serverID string, now time.Time) bool {
	if serverID == "" {
		return false
	}
	interval := s.networkQualityPersistInterval
	if interval <= 0 {
		return false
	}
	s.lastNetworkQualityPersistMu.Lock()
	defer s.lastNetworkQualityPersistMu.Unlock()
	last := s.lastNetworkQualityPersist[serverID]
	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	s.lastNetworkQualityPersist[serverID] = now
	return true
}

func resolveNetworkQualityPersistInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS"))
	if raw == "" {
		return defaultNetworkQualityPersistInterval
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return defaultNetworkQualityPersistInterval
	}
	if value == 0 {
		return 0
	}
	interval := time.Duration(value) * time.Millisecond
	if interval < minNetworkQualityPersistInterval {
		return minNetworkQualityPersistInterval
	}
	return interval
}

func (s *Service) BroadcastUptimeHeartbeat(monitorID int64, beat map[string]interface{}) {
	if s.metricsHub == nil {
		return
	}
	s.metricsHub.BroadcastRootEvent("uptime:heartbeat", map[string]interface{}{
		"monitorId": monitorID,
		"beat":      beat,
	})
}
