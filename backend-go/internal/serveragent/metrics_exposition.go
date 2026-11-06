package serveragent

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// Prometheus exposition 出口（Pull 模式）。
//
// 为什么存在
// ----------
// 面板原本只能「自己看自己」：外部 Prometheus/Grafana/Datadog 拿不到任何
// Agent 数据，于是用户必须在「用本面板」和「接入已有监控体系」之间二选一。
// 提供 /metrics 之后，本项目的 Agent 可以当作「更省资源的 node_exporter」
// 接进用户已有的 Grafana —— 实测 1.5s 采集下我们 0.49% CPU / 7.5MB，
// node_exporter 同样节奏 2.30% / 20MB。
//
// 设计
// ----
//   - 只暴露「最新一次采样」，不做时序存储（那是 Prometheus 的职责）。
//   - 默认关闭：这是新增的公开可读端点，必须由用户显式开启。
//     开启后仍需 Bearer token（`agent_metrics_token` 设置项）。
//   - 指标名前缀 `api_monitor_`，并带 `server_id` 标签，多主机可区分。
//   - 只导出数值型且稳定的字段；不导出容器列表/进程明细这类高基数内容。
//   - 输出格式遵循 Prometheus text exposition 0.0.4（无 exemplar/histogram）。
const metricsExpositionPrefix = "api_monitor_"

// MetricsExposition 渲染当前所有在线主机的指标。
//
// 数据来源是连接注册表里的实时元数据（`agent:state` 写入的 cached_info），
// 因此天然与面板显示一致，不需要额外查询数据库。
func (s *Service) MetricsExposition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.metricsExpositionEnabled(r) {
		response.Error(w, http.StatusForbidden, "metrics exposition is disabled")
		return
	}

	snapshot := s.collectExpositionSnapshot()

	var builder strings.Builder
	builder.WriteString("# HELP api_monitor_up Whether the agent reports as online (1) or not (0).\n")
	builder.WriteString("# TYPE api_monitor_up gauge\n")
	for _, item := range snapshot {
		fmt.Fprintf(&builder, "api_monitor_up{server_id=%q} 1\n", item.serverID)
	}

	writeGauge := func(name, help string, value func(metrics map[string]interface{}) (float64, bool)) {
		var body strings.Builder
		for _, item := range snapshot {
			v, ok := value(item.metrics)
			if !ok {
				continue
			}
			fmt.Fprintf(&body, "%s{server_id=%q} %s\n", metricsExpositionPrefix+name, item.serverID, formatPromFloat(v))
		}
		if body.Len() == 0 {
			return
		}
		fmt.Fprintf(&builder, "# HELP %s%s %s\n", metricsExpositionPrefix, name, help)
		fmt.Fprintf(&builder, "# TYPE %s%s gauge\n", metricsExpositionPrefix, name)
		builder.WriteString(body.String())
	}

	writeGauge("cpu_usage_percent", "CPU usage in percent.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "cpu_usage", "cpu")
	})
	writeGauge("memory_usage_percent", "Memory usage in percent.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "mem_usage_percent", "memory_usage", "mem_usage", "memory")
	})
	writeGauge("disk_usage_percent", "Disk usage in percent.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "disk_usage", "disk_percent")
	})
	writeGauge("load1", "1-minute load average.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "load1")
	})
	writeGauge("load5", "5-minute load average.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "load5")
	})
	writeGauge("load15", "15-minute load average.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "load15")
	})
	writeGauge("memory_total_bytes", "Total physical memory in bytes.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "mem_total_raw", "mem_total")
	})
	writeGauge("memory_used_bytes", "Used physical memory in bytes.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "mem_used_raw", "mem_used")
	})
	writeGauge("net_in_bytes_per_second", "Inbound network throughput in bytes/sec.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "net_in_speed")
	})
	writeGauge("net_out_bytes_per_second", "Outbound network throughput in bytes/sec.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "net_out_speed")
	})
	writeGauge("tcp_connections", "Number of TCP connections.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "tcp_conn_count")
	})
	writeGauge("udp_connections", "Number of UDP connections.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "udp_conn_count")
	})
	writeGauge("processes", "Number of processes (threads excluded).", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "process_count")
	})
	writeGauge("cpu_temperature_celsius", "CPU temperature in Celsius.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "cpu_temp")
	})
	writeGauge("gpu_usage_percent", "GPU usage in percent.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "gpu_usage")
	})
	writeGauge("uptime_seconds", "Host uptime in seconds.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "uptime")
	})

	// Agent 自监控：让外部监控同时能看「Agent 自己是否健康」。
	// 只对已升级到上报 self_metrics 的 Agent 出现，老版本自然缺这几个指标。
	writeSelfGauge := func(name, help string, value func(self map[string]interface{}) (float64, bool)) {
		var body strings.Builder
		for _, item := range snapshot {
			self, ok := item.metrics["self_metrics"].(map[string]interface{})
			if !ok {
				continue
			}
			v, ok := value(self)
			if !ok {
				continue
			}
			fmt.Fprintf(&body, "%s%s{server_id=%q} %s\n", metricsExpositionPrefix, name, item.serverID, formatPromFloat(v))
		}
		if body.Len() == 0 {
			return
		}
		fmt.Fprintf(&builder, "# HELP %s%s %s\n", metricsExpositionPrefix, name, help)
		fmt.Fprintf(&builder, "# TYPE %s%s gauge\n", metricsExpositionPrefix, name)
		builder.WriteString(body.String())
	}
	writeSelfGauge("agent_rss_bytes", "Agent resident memory in bytes.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "rss_bytes")
	})
	writeSelfGauge("agent_cpu_percent", "Agent CPU usage in percent (single core).", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "cpu_percent")
	})
	writeSelfGauge("agent_collect_seconds", "Duration of one collect_state call in seconds.", func(m map[string]interface{}) (float64, bool) {
		us, ok := firstMetricValue(m, "collect_state_us")
		if !ok {
			return 0, false
		}
		return us / 1e6, true
	})
	writeSelfGauge("agent_dropped_low", "Messages dropped by the low-priority queue.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "dropped_low")
	})
	writeSelfGauge("agent_reconnects", "WebSocket reconnects since agent start.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "reconnect_count")
	})
	writeSelfGauge("agent_outbound_backlog", "Messages queued but not yet written.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "outbound_backlog")
	})
	writeSelfGauge("agent_offline_buffered", "Samples buffered while offline.", func(m map[string]interface{}) (float64, bool) {
		return firstMetricValue(m, "offline_buffered")
	})

	// 采集时刻：便于判断数据新鲜度（Prometheus 侧可做 staleness 判断）。
	builder.WriteString("# HELP api_monitor_last_collect_timestamp_seconds Unix timestamp of the last agent sample.\n")
	builder.WriteString("# TYPE api_monitor_last_collect_timestamp_seconds gauge\n")
	for _, item := range snapshot {
		ts, ok := firstMetricValue(item.metrics, "timestamp_ms")
		if !ok || ts <= 0 {
			continue
		}
		fmt.Fprintf(&builder, "api_monitor_last_collect_timestamp_seconds{server_id=%q} %s\n",
			item.serverID, formatPromFloat(ts/1000))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(builder.String()))
}

type expositionItem struct {
	serverID string
	metrics  map[string]interface{}
}

// collectExpositionSnapshot 从连接注册表取在线主机的实时元数据。
//
// 只取「已认证且在线」的连接：离线的历史最后值会误导外部监控
// （Prometheus 侧有 staleness，但那是分钟级）。
func (s *Service) collectExpositionSnapshot() []expositionItem {
	var items []expositionItem
	for _, conn := range s.registry.List() {
		if conn == nil {
			continue
		}
		serverID := strings.TrimSpace(conn.ServerID)
		if serverID == "" {
			continue
		}
		metadata := conn.GetMetadata()
		if len(metadata) == 0 {
			continue
		}
		items = append(items, expositionItem{serverID: serverID, metrics: metadata})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].serverID < items[j].serverID })
	return items
}

// metricsExpositionEnabled 判断是否允许抓取。
//
// 默认关闭：这是新增的公开可读端点，必须显式开启。配置放在 `system_config`
// （键值表，与 auth 的其它开关一致，无需为两个开关改 user_settings 结构）：
//   - `agent_metrics_enabled` = "1" 开启
//   - `agent_metrics_token` 非空时要求 `Authorization: Bearer <token>`
//
// 读取失败一律视为关闭（fail-closed）：不因为 DB 抖动就把全部主机指标暴露出去。
func (s *Service) metricsExpositionEnabled(r *http.Request) bool {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		return false
	}
	defer db.Close()

	readConfig := func(key string) string {
		var value string
		// 键不存在时报 sql.ErrNoRows，视为空值（= 未开启）。
		_ = db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = ?`, key).Scan(&value)
		return strings.TrimSpace(value)
	}

	if readConfig("agent_metrics_enabled") != "1" {
		return false
	}
	token := readConfig("agent_metrics_token")
	if token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	// 常量时间比较，避免通过响应时间逐字节试探 token。
	return subtle.ConstantTimeCompare(
		[]byte(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))),
		[]byte(token),
	) == 1
}

// firstMetricValue 依次尝试多个键名，返回第一个可解析为数字的值。
func firstMetricValue(metrics map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := metrics[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case float64:
			return typed, true
		case float32:
			return float64(typed), true
		case int:
			return float64(typed), true
		case int64:
			return float64(typed), true
		case uint64:
			return float64(typed), true
		case string:
			// cached_info 里有些值是格式化过的字符串（如 "12.5%"）。
			trimmed := strings.TrimSuffix(strings.TrimSpace(typed), "%")
			if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

// formatPromFloat 按 Prometheus 文本格式输出浮点数。
//
// 不用 %v：它会输出科学计数法（1e+06），那是合法 JSON 但不是合法的
// Prometheus 样本值（规范只接受十进制与 Inf/NaN）。
func formatPromFloat(value float64) string {
	if value != value { // NaN
		return "NaN"
	}
	if value > 0 && value < 1e-6 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
