package serveragent

import (
	"database/sql"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// handleAgentUninstall handles POST /api/server/agent/uninstall/{id}
func (s *Service) handleAgentUninstall(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
	// 从数据库中删除 Agent 关联
	_, err := db.ExecContext(r.Context(), `UPDATE server_accounts SET status = 'offline', last_check_status = 'uninstalled' WHERE id = ?`, accountID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update server status: "+err.Error())
		return
	}

	// 断开 Agent 连接（如果存在）
	if _, exists := s.registry.Get(accountID); exists {
		s.registry.Disconnect(accountID)
		// 广播服务器离线状态
		if s.metricsHub != nil {
			s.metricsHub.BroadcastServerStatus(accountID, "offline", false)
		}
	}

	response.OK(w, map[string]interface{}{
		"success": true,
		"message": "Agent uninstalled successfully",
	})
}
