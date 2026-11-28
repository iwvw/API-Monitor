package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

type managedRuntimeFact struct {
	Status    string `json:"status"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

type managedNodeFact struct {
	Status       string `json:"status"`
	Revision     int64  `json:"revision"`
	AssignedPort int    `json:"assigned_port"`
	StatsPort    int    `json:"stats_port"`
}

func (s *Service) startManagedProxyFactsLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sem := make(chan struct{}, 4)
		for _, connection := range s.registry.List() {
			serverID := strings.TrimSpace(connection.ServerID)
			if serverID == "" {
				continue
			}
			sem <- struct{}{}
			go func(id string) {
				defer func() { <-sem }()
				s.reconcileManagedProxyFacts(id)
			}(serverID)
		}
	}
}

func (s *Service) reconcileManagedProxyFacts(serverID string) {
	if _, busy := s.taskRegistry.ActiveTask(proxyTaskResource(serverID)); busy {
		return
	}
	ctx, cancel := context.WithTimeout(s.backgroundCtx, 2*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	var runtime, desiredVersion, desiredStatus string
	err = db.QueryRowContext(ctx, `SELECT runtime,version,desired_status FROM managed_proxy_runtimes WHERE server_id=?`, serverID).Scan(&runtime, &desiredVersion, &desiredStatus)
	if err == nil {
		release, ok := managedProxyRuntime(runtime)
		if ok {
			if desiredVersion == "" {
				desiredVersion = release.Version
			}
			payload, _ := json.Marshal(proxyFactPayload("status_runtime", "runtime-"+serverID, desiredVersion, release))
			if raw, probeErr := s.runProxyRuntimeProbeAndWait(serverID, string(payload)); probeErr == nil {
				var fact managedRuntimeFact
				if json.Unmarshal([]byte(raw), &fact) == nil {
					applyStatus, lastError := "running", ""
					if !fact.Installed || fact.Version != desiredVersion {
						applyStatus, lastError = "drifted", "Agent 上的代理程序版本与面板期望不一致"
					}
					if desiredStatus == "removed" && !fact.Installed {
						applyStatus, lastError = "not_installed", ""
					}
					if _, execErr := db.ExecContext(ctx, `UPDATE managed_proxy_runtimes SET observed_status=?,observed_version=?,observed_at=datetime('now'),apply_status=?,last_error=?,updated_at=datetime('now') WHERE server_id=?`, fact.Status, fact.Version, applyStatus, lastError, serverID); execErr != nil {
						applog.Warn(ctx, "serveragent", "managed proxy runtime fact update failed", "server_id", serverID, "error", execErr.Error())
					}
				}
			}
		}
	} else if err != sql.ErrNoRows {
		return
	}

	rows, err := db.QueryContext(ctx, `SELECT id,revision,enabled,assigned_port FROM managed_proxy_nodes WHERE server_id=? ORDER BY created_at`, serverID)
	if err != nil {
		return
	}
	type desiredNode struct {
		id                    string
		revision              int64
		enabled, assignedPort int
	}
	nodes := []desiredNode{}
	for rows.Next() {
		var node desiredNode
		if rows.Scan(&node.id, &node.revision, &node.enabled, &node.assignedPort) == nil {
			nodes = append(nodes, node)
		}
	}
	rows.Close()
	if release, ok := managedProxyRuntime(runtime); ok {
		for _, node := range nodes {
			if _, busy := s.taskRegistry.ActiveTask(proxyTaskResource(serverID)); busy {
				return
			}
			payload, _ := json.Marshal(proxyFactPayload("status_node", node.id, release.Version, release))
			raw, probeErr := s.runProxyRuntimeProbeAndWait(serverID, string(payload))
			if probeErr != nil {
				continue // old Agents may not implement status_node; preserve last known state.
			}
			var fact managedNodeFact
			if json.Unmarshal([]byte(raw), &fact) != nil || fact.Status == "" {
				continue
			}
			applyStatus, publishable, lastError := "drifted", 0, "Agent 实际状态与面板期望不一致"
			matches := fact.Revision == node.revision && fact.AssignedPort == node.assignedPort
			if node.enabled == 1 && fact.Status == "running" && matches {
				applyStatus, publishable, lastError = "running", 1, ""
			} else if node.enabled == 0 && (fact.Status == "stopped" || fact.Status == "missing") {
				applyStatus, lastError = "stopped", ""
			}
			if _, execErr := db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET observed_status=?,observed_revision=?,observed_port=?,stats_port=?,observed_at=datetime('now'),apply_status=?,publishable=CASE WHEN access_mode='cloudflare_tunnel' AND NOT EXISTS(SELECT 1 FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running') THEN 0 ELSE ? END,last_error=?,updated_at=datetime('now') WHERE id=?`, fact.Status, fact.Revision, fact.AssignedPort, fact.StatsPort, applyStatus, serverID, publishable, lastError, node.id); execErr != nil {
				applog.Warn(ctx, "serveragent", "managed proxy node fact update failed", "server_id", serverID, "node_id", node.id, "error", execErr.Error())
			}
		}
	}
}

func proxyFactPayload(operation, nodeID, version string, release proxyRuntimeRelease) map[string]interface{} {
	return map[string]interface{}{
		"operation": operation, "node_id": nodeID, "revision": 0,
		"runtime": release.Runtime, "runtime_version": version,
		"asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256,
		"asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256,
		"asset_format": release.AssetFormat,
		"config":       "{}", "port_min": 45654, "port_max": 55654, "transport": "tcp",
	}
}
