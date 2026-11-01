package aiagent

import (
	"context"
	"database/sql"
	"net/http"
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
	// CPULoad 1 分钟平均负载（Linux 有意义，Windows 通常缺失）
	CPULoad *float64 `json:"cpuLoad,omitempty"`
	// CPUCores / CPUThreads 核心数与线程数
	CPUCores   *int `json:"cpuCores,omitempty"`
	CPUThreads *int `json:"cpuThreads,omitempty"`
	// MemPercent 内存使用率（0-100）
	MemPercent *float64 `json:"memPercent,omitempty"`
	// MemUsed / MemTotal 内存用量与总量（字节）
	MemUsed  *int64 `json:"memUsed,omitempty"`
	MemTotal *int64 `json:"memTotal,omitempty"`
	// DiskPercent 磁盘使用率（0-100）
	DiskPercent *float64 `json:"diskPercent,omitempty"`
	// DiskUsed / DiskTotal 磁盘用量与总量（字节）
	DiskUsed  *int64 `json:"diskUsed,omitempty"`
	DiskTotal *int64 `json:"diskTotal,omitempty"`
	// Platform 主机平台（linux / windows / darwin）
	Platform string `json:"platform,omitempty"`
	// RecordedAt 该条指标的采集时刻（RFC3339）。前端据此判断数据是否新鲜 ——
	// 采集停摆时这里会停在旧时间，而不是静默显示一个过时的百分比。
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
		cpuUsage, cpuLoad, memUsage, diskUsage sql.NullFloat64
		memUsed, memTotal, diskUsed, diskTotal sql.NullInt64
		cpuCores, cpuThreads                   sql.NullInt64
		platform, recordedAt                   sql.NullString
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
	if cpuLoad.Valid {
		out.CPULoad = &cpuLoad.Float64
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
		out.DiskUsed = &diskUsed.Int64
	}
	if diskTotal.Valid {
		out.DiskTotal = &diskTotal.Int64
	}
	if platform.Valid {
		out.Platform = platform.String
	}
	if recordedAt.Valid {
		out.RecordedAt = recordedAt.String
	}
	return out, nil
}
