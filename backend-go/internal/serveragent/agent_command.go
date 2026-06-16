package serveragent

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// getAgentInstallCommand 获取 Agent 安装命令信息（前端弹窗用）
func (s *Service) getAgentInstallCommand(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
	// 查询账号信息
	var name, host string
	var port int
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port FROM server_accounts WHERE id = ?", accountID).Scan(&name, &host, &port)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 获取或生成 Agent key
	agentKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	// 获取服务器地址
	serverURL := r.Header.Get("X-Forwarded-Host")
	if serverURL == "" {
		serverURL = r.Host
	}

	// 判断协议
	proto := "https"
	if r.Header.Get("X-Forwarded-Proto") == "http" || strings.HasPrefix(serverURL, "localhost") || strings.HasPrefix(serverURL, "127.0.0.1") {
		proto = "http"
	}

	baseURL := fmt.Sprintf("%s://%s", proto, serverURL)
	installScriptURL := fmt.Sprintf("%s/api/server/agent/install-script/%s", baseURL, accountID)

	// 生成安装命令
	installCommand := fmt.Sprintf(`curl -fsSL %s | bash`, installScriptURL)

	// 生成手动安装命令
	manualCommand := fmt.Sprintf(`# 下载并运行 Agent
wget %s -O install-agent.sh
chmod +x install-agent.sh
sudo ./install-agent.sh`, installScriptURL)

	// 返回数据
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"serverName":       name,
			"serverHost":       host,
			"serverPort":       port,
			"serverId":         accountID,
			"agentKey":         agentKey,
			"baseUrl":          baseURL,
			"installScriptUrl": installScriptURL,
			"installCommand":   installCommand,
			"manualCommand":    manualCommand,
			"curlCommand":      installCommand,
			"timestamp":        time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}
