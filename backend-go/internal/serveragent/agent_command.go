package serveragent

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// getAgentInstallCommand returns Agent install commands for the frontend modal.
func (s *Service) getAgentInstallCommand(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
	var name, host string
	var port int
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port FROM server_accounts WHERE id = ?", accountID).Scan(&name, &host, &port)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "Account not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	agentKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	proto, serverURL := resolveInstallOrigin(r)
	baseURL := fmt.Sprintf("%s://%s", proto, serverURL)
	installScriptURL := appendInstallProtocol(fmt.Sprintf("%s/api/server/agent/install/linux/%s/%s", baseURL, accountID, agentKey), proto)
	winInstallURL := appendInstallProtocol(fmt.Sprintf("%s/api/server/agent/install/win/%s/%s", baseURL, accountID, agentKey), proto)

	installCommand := fmt.Sprintf(`curl -fsSL %s | bash`, installScriptURL)
	winInstallCommand := fmt.Sprintf(`powershell -c "irm %s | iex"`, winInstallURL)
	manualCommand := fmt.Sprintf(`# Download and run Agent
wget %s -O install-agent.sh
chmod +x install-agent.sh
sudo ./install-agent.sh`, installScriptURL)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"serverName":        name,
			"serverHost":        host,
			"serverPort":        port,
			"serverId":          accountID,
			"agentKey":          agentKey,
			"baseUrl":           baseURL,
			"apiUrl":            baseURL,
			"installScriptUrl":  installScriptURL,
			"installCommand":    installCommand,
			"winInstallCommand": winInstallCommand,
			"manualCommand":     manualCommand,
			"curlCommand":       installCommand,
			"timestamp":         time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}
