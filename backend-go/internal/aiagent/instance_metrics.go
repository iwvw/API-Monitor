package aiagent

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// instanceHostMetrics 是「整机」实时指标（CPU / 内存 / 磁盘 / 负载）。
//
// 与 probeInstance 返回的进程级指标（pid / memoryBytes / cpuPercent）区分开：
// 那个描述的是 opencode 进程自身，而这里描述的是整台主机 —— 用户真正关心的
// 「编译时主机压力」属于后者（编译由 agent 拉起的子进程执行，进程级数字会低估）。
//
// 数据源是 serveragent 模块持续落库的 server_metrics_history（主机 Agent 上报，
// 约每 10 秒一条）。此处只读最新一条，不触发采集。
type instanceHostMetrics struct {
	// ServerID 是主机实例 ID（与 /api/server/accounts 的 id 同一体系）
	ServerID string `json:"serverId"`
	// CPUPercent 整机 CPU 使用率（0-100）
	CPUPercent *float64 `json:"cpuPercent,omitempty"`
	// CPULoad 1 分钟平均负载（Linux / macOS 有意义；Windows 通常缺失）
	//
	// ⚠️ 源列 cpu_load 是 **TEXT**，存的是运维惯用的三段格式
	// `"1.26 2.06 1.89"`（1/5/15 分钟）。这里解析出第一段；原始串另见 CPULoadRaw。
	CPULoad *float64 `json:"cpuLoad,omitempty"`
	// CPULoadRaw 原始三段负载文本，原样透出给需要 5/15 分钟值的客户端。
	CPULoadRaw string `json:"cpuLoadRaw,omitempty"`
	// CPUCores / CPUThreads 核心数与线程数
	CPUCores   *int `json:"cpuCores,omitempty"`
	CPUThreads *int `json:"cpuThreads,omitempty"`
	// MemPercent 内存使用率（0-100）
	MemPercent *float64 `json:"memPercent,omitempty"`
	// MemUsed / MemTotal 内存用量与总量（源表原样透出，**单位不统一**）
	//
	// ⚠️ 这两列由主机 Agent 原样上报：Linux 主机是 **MB**（实测 ARM 主机
	// mem_total=23972，主机实为 24 GB），Windows 主机是 KB（实测笔电
	// mem_total=24307）。源表没有单位标记，读取侧**不要**当字节换算或相加。
	// 只需要用量的客户端请用 memPercent。
	MemUsed  *int64 `json:"memUsed,omitempty"`
	MemTotal *int64 `json:"memTotal,omitempty"`
	// DiskPercent 磁盘使用率（0-100）
	DiskPercent *float64 `json:"diskPercent,omitempty"`
	// DiskUsed / DiskTotal 磁盘用量与总量，**带单位的字符串**（如 "100.2 GB"）
	//
	// ⚠️ 源列 disk_used / disk_total 是 TEXT，存的是格式化后的容量串。
	// 读取侧一律按字符串处理；需要比例请用 diskPercent。
	DiskUsed  string `json:"diskUsed,omitempty"`
	DiskTotal string `json:"diskTotal,omitempty"`
	// Platform 主机平台，取自主机 Agent 上报的平台名（Linux 上是发行版名，
	// 如 "Ubuntu"；Windows 上是 "Windows"）
	Platform string `json:"platform,omitempty"`
	// RecordedAt 该条指标的采集时刻，**带时区的 RFC3339**。
	//
	// ⚠️ 源列由 serveragent 用 datetime('now') 写入，即 **UTC** 且不带后缀
	// （如 "2026-11-01 14:35:53"）。原样透出会让客户端按本地时间解析，非 UTC
	// 站点看到的「新鲜度」差好几个小时。这里统一补成 `...Z`。解析失败时原样返回。
	RecordedAt string `json:"recordedAt,omitempty"`
}

// handleInstanceHostMetrics 处理 GET /instances/{id}/metrics。
//
// 鉴权与 handleInstanceStatus 完全一致：requireAuth + requireInstanceAccess，
// 即「持有 aiagent 模块 token 且被授权该实例」即可读取。这条路由存在的唯一目的
// 就是让前端（OCC）能拿到整机指标 —— 面板自身的 /api/server/metrics/latest/{id}
// 要求后台 session Cookie，模块 token 进不去。
func (s *Service) handleInstanceHostMetrics(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.getInstance(r.Context(), db, id)
	if err != nil {
		db.Close()
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		db.Close()
		return
	}
	// 实例被禁用时与 status 接口保持一致：不暴露指标。
	if !instance.Enabled {
		db.Close()
		writeError(w, http.StatusForbidden, CodeForbidden, "instance disabled")
		return
	}

	metrics, err := readLatestHostMetrics(r.Context(), db, instance.ServerID)
	// 读完即归还唯一的 SQLite 连接（与 handleInstanceStatus 同样的约定）。
	db.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "metrics unavailable")
		return
	}

	// 没有历史数据时返回 data:null 而不是 404：实例可能是刚建的、或主机 Agent
	// 还没上报过。前端据此显示「暂无数据」，而不是把整个面板打成错误态。
	//
	// 注意不能直接透传 nil：writeOK 用的 response.Envelope 对 data 带
	// `omitempty`，nil 会让整个 data 字段消失（客户端读到 undefined）。
	// 判断「无数据」请以 `data === null`（或字段缺失都当成无数据）为准。
	if metrics == nil {
		writeOK(w, nil)
		return
	}
	writeOK(w, metrics)
}

// readLatestHostMetrics 读取某主机最新一条整机指标。
//
// 表由 serveragent 模块写入（server_metrics_history）。没有记录时返回 (nil, nil)，
// 让调用方区分「没数据」与「查询失败」。
func readLatestHostMetrics(ctx context.Context, db *sql.DB, serverID string) (*instanceHostMetrics, error) {
	const query = `SELECT
		cpu_usage, cpu_load, cpu_cores, cpu_threads,
		mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage,
		platform, recorded_at
	FROM server_metrics_history
	WHERE server_id = ?
	ORDER BY recorded_at DESC, id DESC
	LIMIT 1`

	var (
		cpuUsage, memUsage, diskUsage sql.NullFloat64
		// cpu_load / disk_used / disk_total 在源表里都是 TEXT（见结构体注释），
		// 只能按 NullString 扫；按数值扫会 Scan 失败，整个接口变成 500。
		cpuLoad, diskUsed, diskTotal sql.NullString
		memUsed, memTotal            sql.NullInt64
		cpuCores, cpuThreads         sql.NullInt64
		platform, recordedAt         sql.NullString
	)

	row := db.QueryRowContext(ctx, query, serverID)
	err := row.Scan(
		&cpuUsage, &cpuLoad, &cpuCores, &cpuThreads,
		&memUsed, &memTotal, &memUsage,
		&diskUsed, &diskTotal, &diskUsage,
		&platform, &recordedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := &instanceHostMetrics{ServerID: serverID}
	if cpuUsage.Valid {
		out.CPUPercent = &cpuUsage.Float64
	}
	if cpuLoad.Valid && cpuLoad.String != "" {
		out.CPULoadRaw = cpuLoad.String
		// 三段格式取第一段（1 分钟）；解析不了就只留原始串，不编造数值。
		if first := strings.Fields(cpuLoad.String); len(first) > 0 {
			if v, convErr := strconv.ParseFloat(first[0], 64); convErr == nil {
				out.CPULoad = &v
			}
		}
	}
	if cpuCores.Valid {
		v := int(cpuCores.Int64)
		out.CPUCores = &v
	}
	if cpuThreads.Valid {
		v := int(cpuThreads.Int64)
		out.CPUThreads = &v
	}
	if memUsage.Valid {
		out.MemPercent = &memUsage.Float64
	}
	if memUsed.Valid {
		out.MemUsed = &memUsed.Int64
	}
	if memTotal.Valid {
		out.MemTotal = &memTotal.Int64
	}
	if diskUsage.Valid {
		out.DiskPercent = &diskUsage.Float64
	}
	if diskUsed.Valid {
		out.DiskUsed = diskUsed.String
	}
	if diskTotal.Valid {
		out.DiskTotal = diskTotal.String
	}
	if platform.Valid {
		out.Platform = platform.String
	}
	if recordedAt.Valid {
		out.RecordedAt = normalizeRecordedAt(recordedAt.String)
	}
	return out, nil
}

// normalizeRecordedAt 把 server_metrics_history.recorded_at 补成带 UTC 时区的
// RFC3339。源列由 serveragent 用 datetime('now') 写入，是 UTC 且不带后缀
// （"2006-01-02 15:04:05"）；原样透出会被客户端按本地时间解析，非 UTC 站点
// 据此判断「数据新鲜度」会差好几个小时。解析不了就原样返回，不猜。
func normalizeRecordedAt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return raw
}
