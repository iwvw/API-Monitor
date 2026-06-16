package serveragent

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// handleAgentRoutes dispatches Agent-related routes (Wave 5b)
func (s *Service) handleAgentRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		response.Error(w, http.StatusNotFound, "agent sub-route required")
		return
	}

	switch {
	// POST /api/server/agent/quick-install
	case len(subparts) == 1 && subparts[0] == "quick-install" && r.Method == http.MethodPost:
		s.handleAgentQuickInstall(w, r, db)

	// POST /api/server/agent/regenerate-key
	case len(subparts) == 1 && subparts[0] == "regenerate-key" && r.Method == http.MethodPost:
		s.generateNewAgentKey(w, r, db)

	// POST /api/server/agent/heartbeat
	case len(subparts) == 1 && subparts[0] == "heartbeat" && r.Method == http.MethodPost:
		s.handleAgentHeartbeat(w, r, db)

	// GET /api/server/agent/key
	case len(subparts) == 1 && subparts[0] == "key" && r.Method == http.MethodGet:
		accountID := r.URL.Query().Get("account_id")
		if accountID == "" {
			response.Error(w, http.StatusBadRequest, "account_id required")
			return
		}
		key, err := s.getOrGenerateAgentKey(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"key": key})

	// POST /api/server/agent/key/generate
	case len(subparts) == 2 && subparts[0] == "key" && subparts[1] == "generate" && r.Method == http.MethodPost:
		s.generateNewAgentKey(w, r, db)

	// GET /api/server/agent/command/{id}
	case len(subparts) == 2 && subparts[0] == "command" && r.Method == http.MethodGet:
		accountID := subparts[1]
		s.getAgentInstallCommand(w, r, db, accountID)

	// GET /api/server/agent/install/win/{id}/{key}
	case len(subparts) == 4 && subparts[0] == "install" && subparts[1] == "win" && r.Method == http.MethodGet:
		accountID := subparts[2]
		agentKey := subparts[3]
		s.getWindowsAgentInstallScript(w, r, db, accountID, agentKey)

	// GET /api/server/agent/install/linux/{id}
	case len(subparts) == 3 && subparts[0] == "install" && subparts[1] == "linux" && r.Method == http.MethodGet:
		accountID := subparts[2]
		s.getAgentInstallScript(w, r, db, accountID)

	// GET /api/server/agent/install-script/{id}
	case len(subparts) == 2 && subparts[0] == "install-script" && r.Method == http.MethodGet:
		accountID := subparts[1]
		s.getAgentInstallScript(w, r, db, accountID)

	// GET /api/server/agent/status/{id}
	case len(subparts) == 2 && subparts[0] == "status" && r.Method == http.MethodGet:
		accountID := subparts[1]
		s.getAgentStatus(w, r, db, accountID)

	// POST /api/server/agent/auto-install/{id}
	case len(subparts) == 2 && subparts[0] == "auto-install" && r.Method == http.MethodPost:
		accountID := subparts[1]
		s.handleAgentAutoInstall(w, r, db, accountID)

	// GET /api/server/agent/connection-info/{id}
	case len(subparts) == 2 && subparts[0] == "connection-info" && r.Method == http.MethodGet:
		accountID := subparts[1]
		s.handleAgentConnectionInfo(w, r, db, accountID)

	// POST /api/server/agent/uninstall/{id}
	case len(subparts) == 2 && subparts[0] == "uninstall" && r.Method == http.MethodPost:
		accountID := subparts[1]
		s.handleAgentUninstall(w, r, db, accountID)

	default:
		response.Error(w, http.StatusNotFound, "agent route not found: "+strings.Join(subparts, "/"))
	}
}

// handleMetricsRoutes dispatches Metrics-related routes (Wave 5b)
func (s *Service) handleMetricsRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "history" {
		switch r.Method {
		case http.MethodGet:
			s.listMetricsHistory(w, r, db)
		case http.MethodDelete:
			s.clearMetricsHistory(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) < 2 {
		response.Error(w, http.StatusNotFound, "metrics sub-route requires type and server ID")
		return
	}

	metricType := subparts[0]
	serverID := subparts[1]

	switch metricType {
	// GET /api/server/metrics/history/{id}
	case "history":
		if r.Method == http.MethodGet {
			s.getMetricsHistory(w, r, db, serverID)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}

	// GET /api/server/metrics/latest/{id}
	case "latest":
		if r.Method == http.MethodGet {
			s.getLatestMetrics(w, r, db, serverID)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}

	default:
		response.Error(w, http.StatusNotFound, "metrics type not found: "+metricType)
	}
}

// handleNetworkQualityRoutes dispatches Network Quality routes (Wave 5b)
func (s *Service) handleNetworkQualityRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) < 1 {
		response.Error(w, http.StatusBadRequest, "server ID required")
		return
	}

	serverID := subparts[0]

	if len(subparts) == 2 && subparts[1] == "collect" && r.Method == http.MethodPost {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"serverId": serverID,
				"samples":  []interface{}{},
			},
		})
	} else if r.Method == http.MethodGet {
		s.getNetworkQuality(w, r, db, serverID)
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleTasksRoutes dispatches Tasks-related routes (Wave 5b)
func (s *Service) handleTasksRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		// POST /api/server/tasks
		if r.Method == http.MethodPost {
			s.createTask(w, r, s.taskRegistry)
		} else if r.Method == http.MethodGet {
			// GET /api/server/tasks - list all tasks
			s.taskRegistry.mu.RLock()
			tasks := make([]*Task, 0, len(s.taskRegistry.tasks))
			for _, task := range s.taskRegistry.tasks {
				tasks = append(tasks, task)
			}
			s.taskRegistry.mu.RUnlock()
			response.OK(w, tasks)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	// GET /api/server/tasks/{id}/stream
	if len(subparts) == 2 && subparts[1] == "stream" && r.Method == http.MethodGet {
		taskID := subparts[0]
		s.streamTask(w, r, s.taskRegistry, taskID)
		return
	}

	response.Error(w, http.StatusNotFound, "tasks route not found")
}
