package serveragent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	dockerTaskAction      = 10
	dockerTaskImages      = 13
	dockerTaskImageAction = 14
	dockerTaskNetworks    = 15
	dockerTaskNetworkAct  = 16
	dockerTaskVolumes     = 17
	dockerTaskVolumeAct   = 18
	dockerTaskLogs        = 19
	dockerTaskStats       = 20
	dockerTaskComposeList = 21
	dockerTaskComposeAct  = 22
	dockerTaskContainers  = 27
)

func (s *Service) handleDockerProxyRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) < 2 {
		response.Error(w, http.StatusBadRequest, "docker server id and resource are required")
		return
	}

	serverID := strings.TrimSpace(subparts[0])
	if serverID == "" {
		response.Error(w, http.StatusBadRequest, "server id required")
		return
	}
	if !s.serverAccountExists(r, db, serverID) {
		response.Error(w, http.StatusNotFound, "server not found")
		return
	}

	resource := subparts[1]
	rest := subparts[2:]
	switch resource {
	case "containers":
		s.handleDockerProxyContainers(w, r, serverID, rest)
	case "images":
		s.handleDockerProxyImages(w, r, serverID, rest)
	case "networks":
		s.handleDockerProxyNetworks(w, r, serverID, rest)
	case "volumes":
		s.handleDockerProxyVolumes(w, r, serverID, rest)
	case "compose":
		s.handleDockerProxyCompose(w, r, db, serverID, rest)
	case "stacks":
		s.handleDockerProxyStacks(w, r, db, serverID, rest)
	default:
		response.Error(w, http.StatusNotFound, "docker resource not found")
	}
}

func (s *Service) serverAccountExists(r *http.Request, db *sql.DB, serverID string) bool {
	var id string
	err := db.QueryRowContext(r.Context(), "SELECT id FROM server_accounts WHERE id = ?", serverID).Scan(&id)
	return err == nil
}

func (s *Service) handleDockerProxyContainers(w http.ResponseWriter, r *http.Request, serverID string, rest []string) {
	if len(rest) == 1 && rest[0] == "json" && r.Method == http.MethodGet {
		s.writeDockerTaskJSON(w, r, serverID, dockerTaskContainers, "", 15*time.Second)
		return
	}
	if len(rest) == 1 && rest[0] == "stats" && r.Method == http.MethodGet {
		s.writeDockerTaskJSON(w, r, serverID, dockerTaskStats, "", 15*time.Second)
		return
	}
	if len(rest) == 2 {
		containerID, err := pathValue(rest[0])
		if err != nil || containerID == "" {
			response.Error(w, http.StatusBadRequest, "container id required")
			return
		}
		action := rest[1]
		if action == "logs" && r.Method == http.MethodGet {
			s.writeDockerContainerLogs(w, r, serverID, containerID)
			return
		}
		if r.Method == http.MethodPost && isDockerContainerAction(action) {
			body := map[string]interface{}{
				"action":       action,
				"container_id": containerID,
			}
			s.writeDockerActionResult(w, r, serverID, dockerTaskAction, body, 120*time.Second)
			return
		}
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		containerID, err := pathValue(rest[0])
		if err != nil || containerID == "" {
			response.Error(w, http.StatusBadRequest, "container id required")
			return
		}
		body := map[string]interface{}{
			"action":       "delete",
			"container_id": containerID,
		}
		s.writeDockerActionResult(w, r, serverID, dockerTaskAction, body, 120*time.Second)
		return
	}

	response.Error(w, http.StatusNotFound, "docker containers route not found")
}

func (s *Service) handleDockerProxyImages(w http.ResponseWriter, r *http.Request, serverID string, rest []string) {
	if len(rest) == 1 && rest[0] == "json" && r.Method == http.MethodGet {
		s.writeDockerTaskJSON(w, r, serverID, dockerTaskImages, "", 30*time.Second)
		return
	}
	if len(rest) == 1 && rest[0] == "prune" && r.Method == http.MethodPost {
		s.writeDockerActionResult(w, r, serverID, dockerTaskImageAction, map[string]interface{}{"action": "prune"}, 60*time.Second)
		return
	}
	if len(rest) == 0 && r.Method == http.MethodDelete {
		image := strings.TrimSpace(r.URL.Query().Get("image"))
		if image == "" {
			response.Error(w, http.StatusBadRequest, "image required")
			return
		}
		s.writeDockerActionResult(w, r, serverID, dockerTaskImageAction, map[string]interface{}{"action": "remove", "image": image}, 60*time.Second)
		return
	}
	if len(rest) >= 1 && r.Method == http.MethodDelete {
		image, err := pathValue(strings.Join(rest, "/"))
		if err != nil || image == "" {
			response.Error(w, http.StatusBadRequest, "image required")
			return
		}
		s.writeDockerActionResult(w, r, serverID, dockerTaskImageAction, map[string]interface{}{"action": "remove", "image": image}, 60*time.Second)
		return
	}

	response.Error(w, http.StatusNotFound, "docker images route not found")
}

func (s *Service) handleDockerProxyNetworks(w http.ResponseWriter, r *http.Request, serverID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		s.writeDockerTaskJSON(w, r, serverID, dockerTaskNetworks, "", 30*time.Second)
		return
	}
	if len(rest) == 1 && rest[0] == "prune" && r.Method == http.MethodPost {
		s.writeDockerActionResult(w, r, serverID, dockerTaskNetworkAct, map[string]interface{}{"action": "prune"}, 60*time.Second)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		name, err := pathValue(rest[0])
		if err != nil || name == "" {
			response.Error(w, http.StatusBadRequest, "network name required")
			return
		}
		s.writeDockerActionResult(w, r, serverID, dockerTaskNetworkAct, map[string]interface{}{"action": "remove", "name": name}, 60*time.Second)
		return
	}

	response.Error(w, http.StatusNotFound, "docker networks route not found")
}

func (s *Service) handleDockerProxyVolumes(w http.ResponseWriter, r *http.Request, serverID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		s.writeDockerTaskJSON(w, r, serverID, dockerTaskVolumes, "", 30*time.Second)
		return
	}
	if len(rest) == 1 && rest[0] == "prune" && r.Method == http.MethodPost {
		s.writeDockerActionResult(w, r, serverID, dockerTaskVolumeAct, map[string]interface{}{"action": "prune"}, 60*time.Second)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		name, err := pathValue(rest[0])
		if err != nil || name == "" {
			response.Error(w, http.StatusBadRequest, "volume name required")
			return
		}
		s.writeDockerActionResult(w, r, serverID, dockerTaskVolumeAct, map[string]interface{}{"action": "remove", "name": name}, 60*time.Second)
		return
	}

	response.Error(w, http.StatusNotFound, "docker volumes route not found")
}

func (s *Service) handleDockerProxyCompose(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string, rest []string) {
	if len(rest) == 1 && rest[0] == "projects" && r.Method == http.MethodGet {
		s.writeDockerComposeProjects(w, r, db, serverID)
		return
	}
	if len(rest) == 2 && r.Method == http.MethodPost {
		project, err := pathValue(rest[0])
		if err != nil || project == "" {
			response.Error(w, http.StatusBadRequest, "compose project required")
			return
		}
		action := rest[1]
		if !isDockerComposeAction(action) {
			response.Error(w, http.StatusBadRequest, "unsupported compose action")
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body == nil {
			body = map[string]interface{}{}
		}
		normalizeDockerComposeActionBody(body, project, action)
		s.writeDockerComposeActionResult(w, r, db, serverID, project, action, body, dockerComposeActionTimeout(action))
		return
	}

	response.Error(w, http.StatusNotFound, "docker compose route not found")
}

func (s *Service) handleDockerProxyStacks(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		s.handleDockerStacks(w, r, db, serverID)
		return
	}
	if len(rest) == 1 && rest[0] == "sync" && r.Method == http.MethodPost {
		s.syncDockerStacks(w, r, db, serverID)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		project, err := pathValue(rest[0])
		if err != nil || project == "" {
			response.Error(w, http.StatusBadRequest, "stack name required")
			return
		}
		if err := deleteDockerStackRecord(r.Context(), db, serverID, project); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}
	if len(rest) == 2 && r.Method == http.MethodPost {
		project, err := pathValue(rest[0])
		if err != nil || project == "" {
			response.Error(w, http.StatusBadRequest, "stack name required")
			return
		}
		action := rest[1]
		if !isDockerComposeAction(action) {
			response.Error(w, http.StatusBadRequest, "unsupported stack action")
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body == nil {
			body = map[string]interface{}{}
		}
		normalizeDockerComposeActionBody(body, project, action)
		s.writeDockerComposeActionResult(w, r, db, serverID, project, action, body, dockerComposeActionTimeout(action))
		return
	}

	response.Error(w, http.StatusNotFound, "docker stacks route not found")
}

func (s *Service) syncDockerStacks(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	result, err := s.runAgentTaskAndWait(serverID, dockerTaskComposeList, "", 30*time.Second)
	if err != nil {
		writeDockerProxyError(w, err)
		return
	}
	var parsed interface{}
	if strings.TrimSpace(result) == "" {
		parsed = []interface{}{}
	} else if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		response.Error(w, http.StatusBadGateway, "agent returned invalid docker compose json: "+err.Error())
		return
	}
	upsertDockerStackSnapshot(r.Context(), db, serverID, parsed)
	s.handleDockerStacks(w, r, db, serverID)
}

func (s *Service) writeDockerComposeProjects(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	result, err := s.runAgentTaskAndWait(serverID, dockerTaskComposeList, "", 30*time.Second)
	if err != nil {
		writeDockerProxyError(w, err)
		return
	}
	var parsed interface{}
	if strings.TrimSpace(result) == "" {
		parsed = []interface{}{}
	} else if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		response.Error(w, http.StatusBadGateway, "agent returned invalid docker compose json: "+err.Error())
		return
	}
	upsertDockerStackSnapshot(r.Context(), db, serverID, parsed)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": parsed})
}

func (s *Service) writeDockerContainerLogs(w http.ResponseWriter, r *http.Request, serverID, containerID string) {
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 10000 {
			tail = n
		}
	}
	data, err := json.Marshal(map[string]interface{}{
		"container_id": containerID,
		"tail":         tail,
		"since":        r.URL.Query().Get("since"),
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := s.runAgentTaskAndWait(serverID, dockerTaskLogs, string(data), 60*time.Second)
	if err != nil {
		writeDockerProxyError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(result))
}

func (s *Service) writeDockerTaskJSON(w http.ResponseWriter, r *http.Request, serverID string, taskType int, command string, timeout time.Duration) {
	result, err := s.runAgentTaskAndWait(serverID, taskType, command, timeout)
	if err != nil {
		writeDockerProxyError(w, err)
		return
	}
	var parsed interface{}
	if strings.TrimSpace(result) == "" {
		parsed = []interface{}{}
	} else if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		response.Error(w, http.StatusBadGateway, "agent returned invalid docker json: "+err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": parsed})
}

func (s *Service) writeDockerActionResult(w http.ResponseWriter, r *http.Request, serverID string, taskType int, body map[string]interface{}, timeout time.Duration) {
	data, err := json.Marshal(body)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := s.runAgentTaskAndWait(serverID, taskType, string(data), timeout)
	if err != nil {
		writeDockerProxyError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": result,
		"data": map[string]interface{}{
			"message": result,
		},
	})
}

func (s *Service) writeDockerComposeActionResult(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID, project, action string, body map[string]interface{}, timeout time.Duration) {
	data, err := json.Marshal(body)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := s.runAgentTaskAndWait(serverID, dockerTaskComposeAct, string(data), timeout)
	if err != nil {
		_ = updateDockerStackActionState(r.Context(), db, serverID, project, action, err.Error())
		writeDockerProxyError(w, err)
		return
	}
	_ = updateDockerStackActionState(r.Context(), db, serverID, project, action, "")
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": result,
		"data": map[string]interface{}{
			"message": result,
		},
	})
}

func normalizeDockerComposeActionBody(body map[string]interface{}, project, action string) {
	body["project"] = project
	body["action"] = action
	body["config_file"] = dockerComposeConfigFileFromPayload(body)
}

func dockerComposeConfigFileFromPayload(payload map[string]interface{}) string {
	for _, key := range []string{"config_file", "configFile", "configFiles", "ConfigFiles", "configDir", "config_dir"} {
		if value, ok := payload[key]; ok {
			return stringifyComposeConfigFiles(value)
		}
	}
	return ""
}

func stringifyComposeConfigFiles(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, ",")
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if str := strings.TrimSpace(stringFromAny(item)); str != "" {
				parts = append(parts, str)
			}
		}
		return strings.Join(parts, ",")
	default:
		return strings.TrimSpace(stringFromAny(value))
	}
}

func writeDockerProxyError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if strings.Contains(err.Error(), "agent offline") {
		status = http.StatusServiceUnavailable
	}
	response.Error(w, status, err.Error())
}

func pathValue(value string) (string, error) {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", fmt.Errorf("invalid path value: %w", err)
	}
	return decoded, nil
}

func isDockerContainerAction(action string) bool {
	switch action {
	case "start", "stop", "restart", "pause", "unpause":
		return true
	default:
		return false
	}
}

func isDockerComposeAction(action string) bool {
	switch action {
	case "up", "down", "start", "stop", "restart", "pull":
		return true
	default:
		return false
	}
}

func dockerComposeActionTimeout(action string) time.Duration {
	if action == "pull" {
		return 300 * time.Second
	}
	return 120 * time.Second
}
