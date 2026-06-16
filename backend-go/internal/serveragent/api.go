package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ServerListItem 服务器列表项
type ServerListItem struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Host         string                 `json:"host"`
	Status       string                 `json:"status"`
	Type         string                 `json:"type"`
	Location     string                 `json:"location,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	IsOnline     bool                   `json:"is_online"`
	LastSeen     time.Time              `json:"last_seen,omitempty"`
	CPU          float64                `json:"cpu,omitempty"`
	Memory       float64                `json:"memory,omitempty"`
	Disk         float64                `json:"disk,omitempty"`
	NetworkRx    float64                `json:"network_rx,omitempty"`
	NetworkTx    float64                `json:"network_tx,omitempty"`
	Platform     string                 `json:"platform,omitempty"`
	AgentVersion string                 `json:"agent_version,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	Info         map[string]interface{} `json:"info,omitempty"`
}

// ServerDetail 服务器详情
type ServerDetail struct {
	ServerListItem
	Description string                 `json:"description,omitempty"`
	Country     string                 `json:"country,omitempty"`
	StartsAt    *time.Time             `json:"starts_at,omitempty"`
	ExpiresAt   *time.Time             `json:"expires_at,omitempty"`
	Hostname    string                 `json:"hostname,omitempty"`
	OS          string                 `json:"os,omitempty"`
	Arch        string                 `json:"arch,omitempty"`
	CPUCores    int                    `json:"cpu_cores,omitempty"`
	CPUThreads  int                    `json:"cpu_threads,omitempty"`
	CPUTemp     float64                `json:"cpu_temp,omitempty"`
	TotalMemory int64                  `json:"total_memory,omitempty"`
	UsedMemory  int64                  `json:"used_memory,omitempty"`
	TotalDisk   int64                  `json:"total_disk,omitempty"`
	UsedDisk    int64                  `json:"used_disk,omitempty"`
	GPUUsage    float64                `json:"gpu_usage,omitempty"`
	GPUMemUsed  int64                  `json:"gpu_mem_used,omitempty"`
	GPUMemTotal int64                  `json:"gpu_mem_total,omitempty"`
	GPUTemp     float64                `json:"gpu_temp,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// MetricsHistory 指标历史记录
type MetricsHistory struct {
	Time      time.Time `json:"time"`
	CPU       float64   `json:"cpu"`
	Memory    float64   `json:"memory"`
	Disk      float64   `json:"disk"`
	NetworkRx float64   `json:"network_rx"`
	NetworkTx float64   `json:"network_tx"`
	GPUUsage  float64   `json:"gpu_usage,omitempty"`
	CPUTemp   float64   `json:"cpu_temp,omitempty"`
	GPUTemp   float64   `json:"gpu_temp,omitempty"`
}

// HandleGetServers 获取服务器列表
func (s *Service) HandleGetServers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	// 查询所有服务器账户
	query := `
		SELECT
			id, name, host, status, tags,
			description, country, starts_at, expires_at, created_at, COALESCE(cached_info, '{}')
		FROM server_accounts
		ORDER BY order_index ASC, name ASC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	servers := make([]ServerListItem, 0)

	for rows.Next() {
		var item ServerListItem
		var tagsJSON, description, country sql.NullString
		var startsAt, expiresAt sql.NullTime
		var cachedInfo string

		err := rows.Scan(
			&item.ID, &item.Name, &item.Host, &item.Status, &tagsJSON,
			&description, &country, &startsAt, &expiresAt, &item.CreatedAt, &cachedInfo,
		)
		if err != nil {
			continue
		}

		// 解析 tags
		if tagsJSON.Valid && tagsJSON.String != "" {
			json.Unmarshal([]byte(tagsJSON.String), &item.Tags)
		}

		// 默认类型为 agent
		item.Type = "agent"

		// 解析已缓存的信息作为默认值
		var cachedMap map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo), &cachedMap); err == nil && len(cachedMap) > 0 {
			item.Info = s.buildInfoStruct(cachedMap)
		}

		// 检查 Agent 是否在线并更新最新实时指标
		conn, exists := s.engineIO.registry.Get(item.ID)
		item.IsOnline = exists
		if exists {
			item.Status = "online"
		} else if item.Status == "online" {
			item.Status = "offline"
		}
		if exists {
			item.LastSeen = conn.LastHeartbeat

			// 获取最新指标
			metadata := conn.GetMetadata()
			if cpu, ok := metadata["cpu"].(float64); ok {
				item.CPU = cpu
			}
			if memory, ok := metadata["memory"].(float64); ok {
				item.Memory = memory
			}
			if disk, ok := metadata["disk"].(float64); ok {
				item.Disk = disk
			}
			if platform, ok := metadata["platform"].(string); ok {
				item.Platform = platform
			}
			if version, ok := metadata["version"].(string); ok {
				item.AgentVersion = version
			}

			// 实时的最新指标格式化
			item.Info = s.buildInfoStruct(metadata)
		}

		servers = append(servers, item)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    servers,
		"total":   len(servers),
	})
}

// HandleGetServerDetail 获取服务器详情
func (s *Service) HandleGetServerDetail(w http.ResponseWriter, r *http.Request) {
	// 从路径提取 serverID: /api/server/s/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/server/s/")
	serverID := path
	if idx := strings.Index(serverID, "/"); idx > 0 {
		serverID = serverID[:idx]
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	// 查询服务器基础信息
	query := `
		SELECT
			id, name, host, status, tags,
			description, country, starts_at, expires_at, created_at, COALESCE(cached_info, '{}')
		FROM server_accounts
		WHERE id = ?
	`

	var detail ServerDetail
	var tagsJSON, description, country sql.NullString
	var startsAt, expiresAt sql.NullTime
	var cachedInfo string

	err = db.QueryRowContext(ctx, query, serverID).Scan(
		&detail.ID, &detail.Name, &detail.Host, &detail.Status, &tagsJSON,
		&description, &country, &startsAt, &expiresAt, &detail.CreatedAt, &cachedInfo,
	)

	if err != nil {
		response.Error(w, http.StatusNotFound, "server not found")
		return
	}

	// 解析字段
	if tagsJSON.Valid && tagsJSON.String != "" {
		json.Unmarshal([]byte(tagsJSON.String), &detail.Tags)
	}
	if description.Valid {
		detail.Description = description.String
	}
	if country.Valid {
		detail.Country = country.String
	}
	// 默认类型为 agent
	detail.Type = "agent"
	if startsAt.Valid {
		detail.StartsAt = &startsAt.Time
	}
	if expiresAt.Valid {
		detail.ExpiresAt = &expiresAt.Time
	}

	// 解析已缓存的信息作为默认值
	var cachedMap map[string]interface{}
	if err := json.Unmarshal([]byte(cachedInfo), &cachedMap); err == nil && len(cachedMap) > 0 {
		detail.Info = s.buildInfoStruct(cachedMap)
	}

	// 检查 Agent 连接状态
	conn, exists := s.engineIO.registry.Get(serverID)
	detail.IsOnline = exists
	if exists {
		detail.Status = "online"
	} else if detail.Status == "online" {
		detail.Status = "offline"
	}
	if exists {
		detail.LastSeen = conn.LastHeartbeat

		// 从内存获取实时数据
		metadata := conn.GetMetadata()
		detail.Metadata = metadata

		if cpu, ok := metadata["cpu"].(float64); ok {
			detail.CPU = cpu
		}
		if memory, ok := metadata["memory"].(float64); ok {
			detail.Memory = memory
		}
		if disk, ok := metadata["disk"].(float64); ok {
			detail.Disk = disk
		}
		if platform, ok := metadata["platform"].(string); ok {
			detail.Platform = platform
		}
		if version, ok := metadata["version"].(string); ok {
			detail.AgentVersion = version
		}
		if hostname, ok := metadata["hostname"].(string); ok {
			detail.Hostname = hostname
		}

		// 实时的最新指标格式化
		detail.Info = s.buildInfoStruct(metadata)
	}

	// 查询最新的指标记录
	metricsQuery := `
		SELECT
			cpu_usage, mem_used, mem_total, disk_used, disk_total,
			cpu_cores, cpu_threads, cpu_temp, platform,
			gpu_usage, gpu_mem_used, gpu_mem_total, gpu_temp,
			net_rx, net_tx, recorded_at
		FROM server_metrics_history
		WHERE server_id = ?
		ORDER BY recorded_at DESC
		LIMIT 1
	`

	var cpuUsage, cpuTemp, gpuUsage, gpuTemp, netRx, netTx sql.NullFloat64
	var memUsed, memTotal, diskUsed, diskTotal, gpuMemUsed, gpuMemTotal sql.NullInt64
	var cpuCores, cpuThreads sql.NullInt64
	var platform sql.NullString
	var recordedAt sql.NullTime

	err = db.QueryRowContext(ctx, metricsQuery, serverID).Scan(
		&cpuUsage, &memUsed, &memTotal, &diskUsed, &diskTotal,
		&cpuCores, &cpuThreads, &cpuTemp, &platform,
		&gpuUsage, &gpuMemUsed, &gpuMemTotal, &gpuTemp,
		&netRx, &netTx, &recordedAt,
	)

	if err == nil {
		if cpuUsage.Valid {
			detail.CPU = cpuUsage.Float64
		}
		if memUsed.Valid && memTotal.Valid && memTotal.Int64 > 0 {
			detail.Memory = float64(memUsed.Int64) / float64(memTotal.Int64) * 100
			detail.UsedMemory = memUsed.Int64
			detail.TotalMemory = memTotal.Int64
		}
		if diskUsed.Valid && diskTotal.Valid && diskTotal.Int64 > 0 {
			detail.Disk = float64(diskUsed.Int64) / float64(diskTotal.Int64) * 100
			detail.UsedDisk = diskUsed.Int64
			detail.TotalDisk = diskTotal.Int64
		}
		if cpuCores.Valid {
			detail.CPUCores = int(cpuCores.Int64)
		}
		if cpuThreads.Valid {
			detail.CPUThreads = int(cpuThreads.Int64)
		}
		if cpuTemp.Valid {
			detail.CPUTemp = cpuTemp.Float64
		}
		if gpuUsage.Valid {
			detail.GPUUsage = gpuUsage.Float64
		}
		if gpuMemUsed.Valid {
			detail.GPUMemUsed = gpuMemUsed.Int64
		}
		if gpuMemTotal.Valid {
			detail.GPUMemTotal = gpuMemTotal.Int64
		}
		if gpuTemp.Valid {
			detail.GPUTemp = gpuTemp.Float64
		}
		if netRx.Valid {
			detail.NetworkRx = netRx.Float64
		}
		if netTx.Valid {
			detail.NetworkTx = netTx.Float64
		}
		if platform.Valid && detail.Platform == "" {
			detail.Platform = platform.String
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    detail,
	})
}

// HandleGetServerHistory 获取服务器历史数据
func (s *Service) HandleGetServerHistory(w http.ResponseWriter, r *http.Request) {
	// 提取 serverID: /api/server/s/{id}/history
	path := strings.TrimPrefix(r.URL.Path, "/api/server/s/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		response.Error(w, http.StatusBadRequest, "invalid path")
		return
	}
	serverID := parts[0]

	// 解析查询参数
	query := r.URL.Query()
	hoursStr := query.Get("hours")
	hours := 12 // 默认 12 小时
	if hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	// 查询历史指标
	historyQuery := `
		SELECT
			recorded_at, cpu_usage, mem_used, mem_total, disk_used, disk_total,
			net_rx, net_tx, gpu_usage, cpu_temp, gpu_temp
		FROM server_metrics_history
		WHERE server_id = ? AND recorded_at >= datetime('now', '-' || ? || ' hours')
		ORDER BY recorded_at ASC
	`

	rows, err := db.QueryContext(ctx, historyQuery, serverID, hours)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	history := make([]MetricsHistory, 0)

	for rows.Next() {
		var record MetricsHistory
		var cpuUsage, netRx, netTx, gpuUsage, cpuTemp, gpuTemp sql.NullFloat64
		var memUsed, memTotal, diskUsed, diskTotal sql.NullInt64

		err := rows.Scan(
			&record.Time, &cpuUsage, &memUsed, &memTotal, &diskUsed, &diskTotal,
			&netRx, &netTx, &gpuUsage, &cpuTemp, &gpuTemp,
		)
		if err != nil {
			continue
		}

		if cpuUsage.Valid {
			record.CPU = cpuUsage.Float64
		}
		if memUsed.Valid && memTotal.Valid && memTotal.Int64 > 0 {
			record.Memory = float64(memUsed.Int64) / float64(memTotal.Int64) * 100
		}
		if diskUsed.Valid && diskTotal.Valid && diskTotal.Int64 > 0 {
			record.Disk = float64(diskUsed.Int64) / float64(diskTotal.Int64) * 100
		}
		if netRx.Valid {
			record.NetworkRx = netRx.Float64
		}
		if netTx.Valid {
			record.NetworkTx = netTx.Float64
		}
		if gpuUsage.Valid {
			record.GPUUsage = gpuUsage.Float64
		}
		if cpuTemp.Valid {
			record.CPUTemp = cpuTemp.Float64
		}
		if gpuTemp.Valid {
			record.GPUTemp = gpuTemp.Float64
		}

		history = append(history, record)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    history,
	})
}

func asFloat64(v interface{}) float64 {
	if v == nil {
		return 0.0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return 0.0
}

func asInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case float32:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	case string:
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func parseDiskString(diskStr string) []map[string]interface{} {
	if diskStr == "" {
		return nil
	}
	re := regexp.MustCompile(`([^/]+)/([^\s]+)\s+\(([^)]+)\)`)
	matches := re.FindStringSubmatch(diskStr)
	if len(matches) == 4 {
		return []map[string]interface{}{
			{
				"device": "/",
				"used":   strings.TrimSpace(matches[1]),
				"total":  strings.TrimSpace(matches[2]),
				"usage":  strings.TrimSpace(matches[3]),
			},
		}
	}
	return []map[string]interface{}{
		{
			"device": "/",
			"used":   "-",
			"total":  "-",
			"usage":  "0%",
		},
	}
}

func (s *Service) buildInfoStruct(cached map[string]interface{}) map[string]interface{} {
	if cached == nil {
		return nil
	}
	info := make(map[string]interface{})

	// CPU
	cpu := make(map[string]interface{})
	cpu["Load"] = getString(cached, "load")
	cpu["Cores"] = getInt(cached, "cores")
	cpu["LogicalCores"] = getInt(cached, "logical_cores")
	cpu["PhysicalCores"] = getInt(cached, "physical_cores")

	cpuUsage := getFloat(cached, "cpu_usage")
	if cpuUsage == 0.0 {
		cpuUsage = getFloat(cached, "cpu")
	}
	cpu["Usage"] = fmt.Sprintf("%.1f%%", cpuUsage)

	cpuTemp := getFloat(cached, "cpu_temp")
	if cpuTemp > 0 {
		cpu["Temp"] = cpuTemp
	} else {
		cpu["Temp"] = nil
	}

	cpuPower := getFloat(cached, "cpu_power")
	if cpuPower > 0 {
		cpu["Power"] = fmt.Sprintf("%.1fW", cpuPower)
	} else {
		cpu["Power"] = ""
	}
	info["cpu"] = cpu

	// Memory
	memory := make(map[string]interface{})
	memPercent := getFloat(cached, "mem_percent")
	if memPercent == 0.0 {
		memPercent = getFloat(cached, "mem_usage_percent")
	}
	if memPercent == 0.0 {
		memPercent = getFloat(cached, "memory")
	}
	memory["Usage"] = fmt.Sprintf("%.0f%%", memPercent)

	memStr := getString(cached, "mem")
	if idx := strings.Index(memStr, "/"); idx > 0 {
		memory["Used"] = memStr[:idx]
		memory["Total"] = memStr[idx+1:]
	} else {
		usedMb := getInt(cached, "mem_used_mb")
		totalMb := getInt(cached, "mem_total_mb")
		if usedMb > 0 && totalMb > 0 {
			memory["Used"] = fmt.Sprintf("%dMB", usedMb)
			memory["Total"] = fmt.Sprintf("%dMB", totalMb)
		} else {
			memory["Used"] = "-"
			memory["Total"] = "-"
		}
	}
	info["memory"] = memory

	// Disk
	diskStr := getString(cached, "disk")
	diskArray := parseDiskString(diskStr)
	if diskStr == "" || (len(diskArray) > 0 && diskArray[0]["used"] == "-") {
		used := getString(cached, "disk_used")
		total := getString(cached, "disk_total")
		percent := getFloat(cached, "disk_percent")
		if percent == 0.0 {
			percent = getFloat(cached, "disk_usage")
		}
		if used != "" && total != "" {
			diskArray = []map[string]interface{}{
				{
					"device": "/",
					"used":   used,
					"total":  total,
					"usage":  fmt.Sprintf("%.0f%%", percent),
				},
			}
		}
	}
	info["disk"] = diskArray

	// GPU
	gpu := make(map[string]interface{})
	var model string
	if mStr := getString(cached, "gpu_model"); mStr != "" {
		model = mStr
	} else if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
		model = getString(gpuObj, "Model")
	} else if gpuArr, ok := cached["gpu"].([]interface{}); ok {
		var models []string
		for _, item := range gpuArr {
			if s, ok := item.(string); ok && s != "" {
				models = append(models, s)
			}
		}
		model = strings.Join(models, " / ")
	}
	gpu["Model"] = model

	gpuUsage := getFloat(cached, "gpu_usage")
	if gpuUsage == 0.0 {
		if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
			gpuUsage = getFloat(gpuObj, "Usage")
		}
	}
	gpu["Usage"] = fmt.Sprintf("%.0f%%", gpuUsage)

	gpuMem := getString(cached, "gpu_mem")
	if gpuMem == "" {
		if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
			gpuMem = getString(gpuObj, "Memory")
		}
	}
	gpu["Memory"] = gpuMem

	gpuPower := getFloat(cached, "gpu_power")
	if gpuPower == 0.0 {
		if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
			gpuPower = getFloat(gpuObj, "Power")
		}
	}
	if gpuPower > 0 {
		gpu["Power"] = fmt.Sprintf("%.1fW", gpuPower)
	} else {
		gpu["Power"] = ""
	}

	var gpuTemp interface{} = nil
	if t, ok := cached["gpu_temp"]; ok && t != nil {
		gpuTemp = getFloat(cached, "gpu_temp")
	} else if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
		if t, ok := gpuObj["Temp"]; ok && t != nil {
			gpuTemp = getFloat(gpuObj, "Temp")
		}
	}
	gpu["Temp"] = gpuTemp

	gpuMemPercent := getFloat(cached, "gpu_mem_percent")
	if gpuMemPercent == 0.0 {
		if gpuObj, ok := cached["gpu"].(map[string]interface{}); ok {
			gpuMemPercent = getFloat(gpuObj, "Percent")
		}
	}
	if gpuMemPercent == 0.0 {
		used := getFloat(cached, "gpu_mem_used")
		total := getFloat(cached, "gpu_mem_total")
		if used > 0 && total > 0 {
			gpuMemPercent = (used / total) * 100
		}
	}
	gpu["Percent"] = gpuMemPercent

	if gpu["Memory"] == "" {
		used := getFloat(cached, "gpu_mem_used")
		total := getFloat(cached, "gpu_mem_total")
		if used > 0 && total > 0 {
			usedMB := used / (1024 * 1024)
			totalMB := total / (1024 * 1024)
			gpu["Memory"] = fmt.Sprintf("%.0f/%.0fMB", usedMB, totalMB)
		}
	}
	info["gpu"] = gpu

	// Network
	netMap := make(map[string]interface{})
	if network, ok := cached["network"].(map[string]interface{}); ok {
		for k, v := range network {
			netMap[k] = v
		}
	}
	if netMap["rx_speed"] == nil || netMap["rx_speed"] == "" {
		if rxSpeed := cached["net_rx"]; rxSpeed != nil {
			netMap["rx_speed"] = formatSpeed(asFloat64(rxSpeed))
		} else {
			netMap["rx_speed"] = formatSpeed(asFloat64(cached["net_in_speed"]))
		}
	}
	if netMap["tx_speed"] == nil || netMap["tx_speed"] == "" {
		if txSpeed := cached["net_tx"]; txSpeed != nil {
			netMap["tx_speed"] = formatSpeed(asFloat64(txSpeed))
		} else {
			netMap["tx_speed"] = formatSpeed(asFloat64(cached["net_out_speed"]))
		}
	}
	if netMap["rx_total"] == nil || netMap["rx_total"] == "" {
		if rxTotal := cached["net_in_transfer"]; rxTotal != nil {
			netMap["rx_total"] = formatBytes(asInt64(rxTotal))
		}
	}
	if netMap["tx_total"] == nil || netMap["tx_total"] == "" {
		if txTotal := cached["net_out_transfer"]; txTotal != nil {
			netMap["tx_total"] = formatBytes(asInt64(txTotal))
		}
	}
	info["network"] = netMap

	// Docker
	if docker, ok := cached["docker"]; ok {
		info["docker"] = docker
	}

	info["platform"] = getString(cached, "platform")
	info["platformVersion"] = getString(cached, "platform_version")
	info["agentVersion"] = getString(cached, "agent_version")
	info["ip"] = getString(cached, "ip")
	info["uptime"] = getString(cached, "uptime")

	var lastUpdate string
	if lu := getString(cached, "lastUpdate"); lu != "" {
		lastUpdate = lu
	} else if tsMs := getFloat(cached, "timestamp_ms"); tsMs > 0 {
		t := time.UnixMilli(int64(tsMs))
		lastUpdate = t.Format("15:04:05")
	} else {
		lastUpdate = time.Now().Format("15:04:05")
	}
	info["lastUpdate"] = lastUpdate

	return info
}
