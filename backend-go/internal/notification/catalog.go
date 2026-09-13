package notification

import (
	"fmt"
	"strings"

)

func eventCatalog() []map[string]interface{} {
	return []map[string]interface{}{
		{"module": "uptime", "events": []string{"down", "up", "pending", "resource.created", "resource.deleted", "ssl_expiry"}, "dynamic_events": []string{"down", "up"}},
		{"module": "server", "events": []string{"offline", "online", "interrupted", "degraded", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal", "traffic_high", "traffic_normal"}, "dynamic_events": []string{"offline", "online", "interrupted", "degraded", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal", "traffic_high", "traffic_normal"}},
		{"module": "system", "events": []string{"database.backup", "database.import", "log.cleanup", "migration.failed", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal"}, "dynamic_events": []string{"cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal"}},
		{"module": "filebox", "events": []string{"resource.created", "resource.deleted", "cleanup"}},
		{"module": "github", "events": []string{"action_failed", "action_recovered", "release_published", "star_spike", "issue_opened", "pull_request_opened", "repository_unreachable", "token_invalid", "rate_limit_low", "webhook_delivery_failed", "webhook_ping"}, "dynamic_events": []string{"action_failed", "action_recovered"}},
		{"module": "totp", "events": []string{"resource.created", "resource.updated", "resource.deleted", "security.revealed", "backup.imported", "backup.exported"}},
		{"module": "openai", "events": []string{"gateway_error_high", "gateway_error_normal"}, "dynamic_events": []string{"gateway_error_high", "gateway_error_normal"}},
		{"module": "antigravity", "events": []string{"quota_window_refreshed"}},
		{"module": "cron", "events": []string{"task.completed", "task.failed", "workflow.completed", "workflow.failed"}, "dynamic_events": []string{}},
	}
}

func validateSourceModule(module string) error {
	switch strings.ToLower(strings.TrimSpace(module)) {
	case "music", "openlist":
		return fmt.Errorf("%w: source module retired", errInvalidInput)
	default:
		return nil
	}
}