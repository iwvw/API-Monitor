package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ==========================================
// DOCKER OPERATIONS
// ==========================================

func (s *Service) handleDockerRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		response.Error(w, http.StatusBadRequest, "Docker operation not specified")
		return
	}

	if subparts[0] == "check-update" && r.Method == http.MethodPost {
		s.handleDockerCheckUpdate(w, r, db)
		return
	}

	response.Error(w, http.StatusNotFound, "Docker operation not found")
}

func (s *Service) handleDockerCheckUpdate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID    string `json:"serverId"`
		ContainerID string `json:"containerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" {
		response.Error(w, http.StatusBadRequest, "serverId required")
		return
	}

	conn, ok := s.registry.Get(req.ServerID)
	if !ok {
		var cachedInfo string
		err := db.QueryRowContext(r.Context(), "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?", req.ServerID).Scan(&cachedInfo)
		if err == sql.ErrNoRows {
			response.Error(w, http.StatusNotFound, "server not found")
			return
		}
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var info map[string]interface{}
		_ = json.Unmarshal([]byte(cachedInfo), &info)
		results := dockerUpdateResultsFromCache(req.ServerID, req.ContainerID, info)
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    results,
		})
		return
	}

	mappedType := 11 // DOCKER_CHECK_UPDATE
	mappedData := map[string]interface{}{
		"container_id": req.ContainerID,
	}
	bytes, err := json.Marshal(mappedData)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to marshal task data: "+err.Error())
		return
	}
	dataStr := string(bytes)

	task := s.taskRegistry.Create(req.ServerID, "docker.checkUpdates", "container.checkUpdates")
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    mappedType,
		"data":    dataStr,
		"timeout": 180,
	}); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	eventCh := task.Subscribe()
	var finalEvent TaskEvent
	completed := false

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	for !completed {
		select {
		case event, ok := <-eventCh:
			if !ok {
				completed = true
				break
			}
			if event.Status == TaskCompleted || event.Status == TaskFailed {
				finalEvent = event
				completed = true
			}
		case <-ctx.Done():
			completed = true
		}
	}

	if finalEvent.Status != TaskCompleted {
		errMsg := "check update task failed or timed out"
		if finalEvent.Error != "" {
			errMsg = finalEvent.Error
		} else if ctx.Err() != nil {
			errMsg = "timeout waiting for agent response"
		}
		response.Error(w, http.StatusInternalServerError, errMsg)
		return
	}

	var results []map[string]interface{}
	resultStr, ok := finalEvent.Data.(string)
	if ok && resultStr != "" {
		_ = json.Unmarshal([]byte(resultStr), &results)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    results,
	})
}

func dockerUpdateResultsFromCache(serverID, containerID string, info map[string]interface{}) []map[string]interface{} {
	docker, _ := info["docker"].(map[string]interface{})
	containers := asMapList(docker["containers"])
	results := make([]map[string]interface{}, 0, len(containers))
	for _, container := range containers {
		id := stringFromAny(firstNonNil(container["id"], container["ID"], container["container_id"], container["containerId"]))
		if containerID != "" && id != "" && !strings.HasPrefix(id, containerID) && !strings.HasPrefix(containerID, id) {
			continue
		}
		image := stringFromAny(firstNonNil(container["image"], container["Image"]))
		name := stringFromAny(firstNonNil(container["name"], container["Name"], container["names"], container["Names"]))
		results = append(results, map[string]interface{}{
			"serverId":      serverID,
			"containerId":   id,
			"containerName": name,
			"image":         image,
			"has_update":    false,
			"hasUpdate":     false,
			"status":        "unknown",
			"message":       "update check requires live agent support",
		})
	}
	if results == nil {
		return []map[string]interface{}{}
	}
	return results
}

// ==========================================
// V2 TASKS
// ==========================================

func (s *Service) handleV2TasksRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 && r.Method == http.MethodGet {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    []interface{}{},
		})
		return
	}
	if len(subparts) == 0 && r.Method == http.MethodPost {
		s.handleCreateV2Task(w, r, db)
		return
	}
	if len(subparts) == 1 && subparts[0] == "stream" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		// 每次写入前由 writeNamedSSE 续期写超时；写失败说明客户端已不可达，
		// 必须退出循环释放订阅，避免事件在死连接上静默堆积。
		if err := s.writeNamedSSE(w, "ready", map[string]interface{}{"success": true}); err != nil {
			return
		}
		flusher.Flush()

		eventCh, cancel := s.taskRegistry.SubscribeAll()
		defer cancel()

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case event, ok := <-eventCh:
				if !ok {
					return
				}
				payload, include := s.buildTaskUpdatePayload(event)
				if !include {
					continue
				}
				if err := s.writeNamedSSE(w, "task.update", payload); err != nil {
					return
				}
				flusher.Flush()
			case <-ticker.C:
				if err := s.writeNamedSSE(w, "ping", map[string]interface{}{"ts": time.Now().UnixMilli()}); err != nil {
					return
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}

	response.Error(w, http.StatusNotFound, "v2 tasks route not found")
}

func (s *Service) buildTaskUpdatePayload(event TaskEvent) (map[string]interface{}, bool) {
	state := string(event.Status)
	if event.Status == TaskCompleted {
		state = "success"
	}
	payload := map[string]interface{}{
		"taskId":   event.TaskID,
		"state":    state,
		"progress": event.Progress,
	}
	if event.Type != "" {
		payload["event"] = event.Type
	}
	if event.Error != "" {
		payload["error"] = event.Error
		payload["message"] = event.Error
	}
	if msg, ok := taskEventMessage(event.Data); ok && payload["message"] == nil {
		payload["message"] = msg
	}

	task, exists := s.taskRegistry.Get(event.TaskID)
	if exists {
		taskType := task.Type
		action := task.Command
		payload["type"] = taskType
		payload["action"] = action
		payload["serverId"] = task.ServerID
		if strings.HasPrefix(taskType, "docker.internal.") {
			return payload, false
		}
		if strings.HasPrefix(taskType, "docker.") || strings.HasPrefix(action, "container.") ||
			strings.HasPrefix(action, "image.") || strings.HasPrefix(action, "network.") ||
			strings.HasPrefix(action, "volume.") || strings.HasPrefix(action, "compose.") {
			payload["domain"] = "docker"
			return payload, true
		}
	}

	dataMap, _ := event.Data.(map[string]interface{})
	if dataMap != nil {
		if typ, _ := dataMap["type"].(string); typ != "" {
			payload["type"] = typ
			if strings.HasPrefix(typ, "docker.") {
				payload["domain"] = "docker"
				if cmd, _ := dataMap["command"].(string); cmd != "" {
					payload["action"] = cmd
				}
				return payload, true
			}
		}
	}

	return payload, false
}

func taskEventMessage(data interface{}) (string, bool) {
	switch v := data.(type) {
	case string:
		return v, v != ""
	case map[string]interface{}:
		for _, key := range []string{"message", "detail_msg", "data"} {
			if msg, _ := v[key].(string); msg != "" {
				return msg, true
			}
		}
	}
	return "", false
}

func (s *Service) handleV2DockerRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "overview" && r.Method == http.MethodGet {
		s.handleDockerOverview(w, r, db)
		return
	}
	if len(subparts) >= 2 {
		s.handleDockerProxyRoutes(w, r, db, subparts)
		return
	}
	response.Error(w, http.StatusNotFound, "v2 docker route not found")
}

func (s *Service) handleDockerOverview(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	serverID := r.URL.Query().Get("serverId")
	query := "SELECT id, name, status, COALESCE(cached_info, '{}') FROM server_accounts"
	args := []interface{}{}
	if serverID != "" {
		query += " WHERE id = ?"
		args = append(args, serverID)
	}
	query += " ORDER BY order_index ASC, created_at DESC"

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type serverData struct {
		id         string
		name       string
		status     string
		cachedInfo string
	}

	var rawServers []serverData
	for rows.Next() {
		var d serverData
		if err := rows.Scan(&d.id, &d.name, &d.status, &d.cachedInfo); err == nil {
			rawServers = append(rawServers, d)
		}
	}

	scopeParam := r.URL.Query().Get("scope")
	scopes := make(map[string]bool)
	if scopeParam != "" {
		for _, sc := range strings.Split(scopeParam, ",") {
			scopes[strings.TrimSpace(sc)] = true
		}
	}

	servers := make([]map[string]interface{}, len(rawServers))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for idx, d := range rawServers {
		idx, d := idx, d
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			var info map[string]interface{}
			_ = json.Unmarshal([]byte(d.cachedInfo), &info)
			dockerInfo, _ := info["docker"].(map[string]interface{})
			installed := false
			if v, ok := dockerInfo["installed"].(bool); ok {
				installed = v
			}

			// Extract containers list
			containers := []interface{}{}
			if cList, ok := dockerInfo["containers"].([]interface{}); ok {
				containers = cList
			}

			// If online, dynamically fetch requested resources based on scope.
			var images interface{} = []interface{}{}
			var networks interface{} = []interface{}{}
			var volumes interface{} = []interface{}{}
			var stats interface{} = []interface{}{}
			var composeProjects interface{} = []interface{}{}

			var errOverview, errImages, errNetworks, errVolumes, errStats, errCompose string
			containersSource := "cache"

			if d.status == "online" {
				// Query in parallel
				type taskRes struct {
					key  string
					val  interface{}
					errS string
				}
				ch := make(chan taskRes, 6)
				tasksCount := 0

				runTask := func(key string, taskType int) {
					tasksCount++
					go func() {
						resStr, err := s.runAgentTaskAndWait(d.id, taskType, "", 10*time.Second)
						if err != nil {
							ch <- taskRes{key: key, val: []interface{}{}, errS: err.Error()}
							return
						}
						var parsed interface{}
						if err := json.Unmarshal([]byte(resStr), &parsed); err != nil {
							ch <- taskRes{key: key, val: []interface{}{}, errS: "agent returned invalid docker json: " + err.Error()}
							return
						}
						ch <- taskRes{key: key, val: parsed, errS: ""}
					}()
				}

				if scopes["containers"] {
					runTask("containers", 27) // DOCKER_CONTAINERS
				}
				if scopes["images"] {
					runTask("images", 13) // DOCKER_IMAGES
				}
				if scopes["networks"] {
					runTask("networks", 15) // DOCKER_NETWORKS
				}
				if scopes["volumes"] {
					runTask("volumes", 17) // DOCKER_VOLUMES
				}
				if scopes["stats"] {
					runTask("stats", 20) // DOCKER_STATS
				}
				if scopes["compose"] {
					runTask("compose", 21) // DOCKER_COMPOSE_LIST
				}

				for i := 0; i < tasksCount; i++ {
					res := <-ch
					switch res.key {
					case "containers":
						if res.errS == "" {
							containers = asInterfaceList(res.val)
							containersSource = "live"
						}
						errOverview = res.errS
					case "images":
						images = res.val
						errImages = res.errS
					case "networks":
						networks = res.val
						errNetworks = res.errS
					case "volumes":
						volumes = res.val
						errVolumes = res.errS
					case "stats":
						stats = res.val
						errStats = res.errS
					case "compose":
						composeProjects = res.val
						errCompose = res.errS
						if res.errS == "" {
							upsertDockerStackSnapshot(r.Context(), db, d.id, res.val)
						}
					}
				}

				// Installed can be inferred only from a Docker task that was actually requested and succeeded.
				dockerTaskSucceeded := containersSource == "live" ||
					(scopes["images"] && errImages == "") ||
					(scopes["networks"] && errNetworks == "") ||
					(scopes["volumes"] && errVolumes == "") ||
					(scopes["stats"] && errStats == "") ||
					(scopes["compose"] && errCompose == "")
				if dockerTaskSucceeded {
					installed = true
				}
			}

			running := getInt(dockerInfo, "running")
			stopped := getInt(dockerInfo, "stopped")

			// If running/stopped are 0, we can infer them from containers list if present
			if running == 0 && stopped == 0 && len(containers) > 0 {
				for _, c := range containers {
					if cMap, ok := c.(map[string]interface{}); ok {
						state, _ := cMap["state"].(string)
						if state == "running" {
							running++
						} else {
							stopped++
						}
					}
				}
			}

			servers[idx] = map[string]interface{}{
				"id":     d.id,
				"name":   d.name,
				"status": d.status,
				"docker": map[string]interface{}{
					"installed":  installed,
					"running":    running,
					"stopped":    stopped,
					"containers": containers,
				},
				"resources": map[string]interface{}{
					"containers":      containers,
					"images":          images,
					"networks":        networks,
					"volumes":         volumes,
					"stats":           stats,
					"composeProjects": composeProjects,
				},
				"errors": map[string]interface{}{
					"overview":        errOverview,
					"images":          errImages,
					"networks":        errNetworks,
					"volumes":         errVolumes,
					"stats":           errStats,
					"composeProjects": errCompose,
				},
				"source": map[string]interface{}{
					"containers": containersSource,
				},
			}
		}()
	}
	wg.Wait()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"servers": servers,
		},
	})
}

func (s *Service) handleCreateV2Task(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string                 `json:"serverId"`
		Domain   string                 `json:"domain"`
		Action   string                 `json:"action"`
		Type     string                 `json:"type"`
		Config   map[string]interface{} `json:"config"`
		Payload  map[string]interface{} `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" {
		response.Error(w, http.StatusBadRequest, "serverId required")
		return
	}

	var mappedType int
	var mappedData interface{}
	var timeoutSec int = 60

	switch req.Action {
	case "container.start", "container.stop", "container.restart", "container.pause", "container.unpause", "container.pull", "container.delete":
		containerID, _ := req.Payload["containerId"].(string)
		if containerID == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId")
			return
		}
		image, _ := req.Payload["image"].(string)
		actionPart := strings.Split(req.Action, ".")[1]
		mappedType = 10 // DOCKER_ACTION
		mappedData = map[string]interface{}{
			"action":       actionPart,
			"container_id": containerID,
			"image":        image,
		}
		timeoutSec = 120

	case "container.update":
		containerID, _ := req.Payload["containerId"].(string)
		containerName, _ := req.Payload["containerName"].(string)
		if containerID == "" || containerName == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId or containerName")
			return
		}
		image, _ := req.Payload["image"].(string)
		mappedType = 24 // DOCKER_UPDATE_CONTAINER
		mappedData = map[string]interface{}{
			"container_id":   containerID,
			"container_name": containerName,
			"image":          image,
		}
		timeoutSec = 600

	case "container.rename":
		containerID, _ := req.Payload["containerId"].(string)
		newName, _ := req.Payload["newName"].(string)
		if containerID == "" || newName == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId or newName")
			return
		}
		mappedType = 25 // DOCKER_RENAME_CONTAINER
		mappedData = map[string]interface{}{
			"container_id": containerID,
			"new_name":     newName,
		}
		timeoutSec = 60

	case "container.logs":
		containerID, _ := req.Payload["containerId"].(string)
		if containerID == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId")
			return
		}
		tail := 100
		if t, ok := req.Payload["tail"].(float64); ok {
			tail = int(t)
		}
		since, _ := req.Payload["since"].(string)
		mappedType = 19 // DOCKER_LOGS
		mappedData = map[string]interface{}{
			"container_id": containerID,
			"tail":         tail,
			"since":        since,
		}
		timeoutSec = 60

	case "container.checkUpdates":
		containerID, _ := req.Payload["containerId"].(string)
		mappedType = 11 // DOCKER_CHECK_UPDATE
		mappedData = map[string]interface{}{
			"container_id": containerID,
		}
		timeoutSec = 180

	case "container.create":
		image, _ := req.Payload["image"].(string)
		if image == "" {
			response.Error(w, http.StatusBadRequest, "missing image")
			return
		}
		if violations := validateDockerCreatePolicy(req.Payload); len(violations) > 0 {
			response.Error(w, http.StatusBadRequest, "docker create policy violation: "+strings.Join(violations, "; "))
			return
		}
		name, _ := req.Payload["name"].(string)
		ports, _ := req.Payload["ports"]
		if ports == nil {
			ports = []interface{}{}
		}
		volumes, _ := req.Payload["volumes"]
		if volumes == nil {
			volumes = []interface{}{}
		}
		env, _ := req.Payload["env"]
		if env == nil {
			env = map[string]interface{}{}
		}
		network, _ := req.Payload["network"].(string)
		restart, _ := req.Payload["restart"].(string)
		if restart == "" {
			restart = "unless-stopped"
		}
		privileged, _ := req.Payload["privileged"].(bool)
		extraArgs, _ := req.Payload["extraArgs"]
		if extraArgs == nil {
			extraArgs = []interface{}{}
		}

		mappedType = 23 // DOCKER_CREATE_CONTAINER
		mappedData = map[string]interface{}{
			"name":       name,
			"image":      image,
			"ports":      ports,
			"volumes":    volumes,
			"env":        env,
			"network":    network,
			"restart":    restart,
			"privileged": privileged,
			"extra_args": extraArgs,
		}
		timeoutSec = 300

	case "image.list":
		mappedType = 13 // DOCKER_IMAGES
		mappedData = ""
		timeoutSec = 60

	case "image.pull", "image.remove", "image.prune":
		imageRef, _ := req.Payload["image"].(string)
		if imageRef == "" {
			imageRef, _ = req.Payload["imageId"].(string)
		}
		if imageRef == "" {
			imageRef, _ = req.Payload["id"].(string)
		}
		actionPart := strings.Split(req.Action, ".")[1]
		mappedType = 14 // DOCKER_IMAGE_ACTION
		mappedData = map[string]interface{}{
			"action": actionPart,
			"image":  imageRef,
		}
		if actionPart == "pull" {
			timeoutSec = 300
		} else {
			timeoutSec = 60
		}

	case "network.list":
		mappedType = 15 // DOCKER_NETWORKS
		mappedData = ""
		timeoutSec = 60

	case "network.create", "network.remove", "network.connect", "network.disconnect", "network.prune":
		actionPart := strings.Split(req.Action, ".")[1]
		name, _ := req.Payload["name"].(string)
		driver, _ := req.Payload["driver"].(string)
		subnet, _ := req.Payload["subnet"].(string)
		gateway, _ := req.Payload["gateway"].(string)
		container, _ := req.Payload["container"].(string)
		mappedType = 16 // DOCKER_NETWORK_ACTION
		mappedData = map[string]interface{}{
			"action":    actionPart,
			"name":      name,
			"driver":    driver,
			"subnet":    subnet,
			"gateway":   gateway,
			"container": container,
		}
		timeoutSec = 60

	case "volume.list":
		mappedType = 17 // DOCKER_VOLUMES
		mappedData = ""
		timeoutSec = 60

	case "volume.create", "volume.remove", "volume.prune":
		actionPart := strings.Split(req.Action, ".")[1]
		name, _ := req.Payload["name"].(string)
		driver, _ := req.Payload["driver"].(string)
		mappedType = 18 // DOCKER_VOLUME_ACTION
		mappedData = map[string]interface{}{
			"action": actionPart,
			"name":   name,
			"driver": driver,
		}
		timeoutSec = 60

	case "stats.list":
		mappedType = 20 // DOCKER_STATS
		mappedData = ""
		timeoutSec = 60

	case "compose.list":
		mappedType = 21 // DOCKER_COMPOSE_LIST
		mappedData = ""
		timeoutSec = 60

	case "compose.up", "compose.down", "compose.start", "compose.stop", "compose.restart", "compose.pull", "compose.update":
		actionPart := strings.Split(req.Action, ".")[1]
		project, _ := req.Payload["project"].(string)
		if project == "" {
			project, _ = req.Payload["projectName"].(string)
		}
		if project == "" {
			project, _ = req.Payload["Name"].(string)
		}
		if project == "" {
			project, _ = req.Payload["name"].(string)
		}
		configFile := dockerComposeConfigFileFromPayload(req.Payload)
		if project == "" && configFile == "" {
			response.Error(w, http.StatusBadRequest, "missing project or configFile")
			return
		}
		mappedType = 22 // DOCKER_COMPOSE_ACTION
		mappedData = map[string]interface{}{
			"action":      actionPart,
			"project":     project,
			"config_file": configFile,
		}
		if actionPart == "pull" || actionPart == "update" {
			timeoutSec = 300
		} else {
			timeoutSec = 120
		}

	default:
		response.Error(w, http.StatusBadRequest, "unsupported action: "+req.Action)
		return
	}

	var dataStr string
	if str, ok := mappedData.(string); ok {
		dataStr = str
	} else {
		bytes, err := json.Marshal(mappedData)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to marshal task data: "+err.Error())
			return
		}
		dataStr = string(bytes)
	}

	taskType := req.Type
	if taskType == "" {
		taskType = strings.Trim(strings.Join([]string{req.Domain, req.Action}, "."), ".")
	}
	if taskType == "" {
		taskType = "server.task"
	}

	conn, ok := s.registry.Get(req.ServerID)
	if !ok {
		response.Error(w, http.StatusServiceUnavailable, "agent offline")
		return
	}

	task := s.taskRegistry.Create(req.ServerID, taskType, req.Action)
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    mappedType,
		"data":    dataStr,
		"timeout": timeoutSec,
	}); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"taskId":  task.ID,
		"data": map[string]interface{}{
			"taskId": task.ID,
			"state":  string(task.GetStatus()),
		},
	})
}
