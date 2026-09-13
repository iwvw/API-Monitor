package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) encryptField(value string) interface{} {
	if value == "" {
		return nil
	}
	enc, err := secure.SecureEncrypt(value)
	if err != nil {
		return value
	}
	return enc
}

func (s *Service) encryptFieldString(value string) string {
	encrypted := s.encryptField(value)
	if encrypted == nil {
		return ""
	}
	if text, ok := encrypted.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", encrypted)
}

func (s *Service) decryptField(field sql.NullString) string {
	if !field.Valid || field.String == "" {
		return ""
	}
	return secure.SecureDecrypt(field.String)
}

func parseJSONTags(s string) []string {
	if s == "" {
		return []string{}
	}
	var res []string
	if err := json.Unmarshal([]byte(s), &res); err != nil {
		return []string{}
	}
	return res
}

func coalesceStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func coalesceInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func cleanCountryCode(value string) string {
	code := strings.TrimSpace(value)
	if code == "" || strings.EqualFold(code, "auto") {
		return ""
	}
	return strings.ToLower(code)
}

func nullStringVal(s sql.NullString) interface{} {
	if !s.Valid {
		return nil
	}
	return s.String
}

func nullIntVal(i sql.NullInt64) interface{} {
	if !i.Valid {
		return nil
	}
	return i.Int64
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getStringVal(m map[string]interface{}, key, fallback string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return fallback
}

func getIntVal(m map[string]interface{}, key string, fallback int) int {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return int(f)
		}
	}
	return fallback
}

func getInt64Val(m map[string]interface{}, key string, fallback int64) int64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return int64(f)
		}
	}
	return fallback
}

func getFloatVal(m map[string]interface{}, key string, fallback float64) float64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return f
		}
	}
	return fallback
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return f
		}
	}
	return 0
}

func getBoolVal(m map[string]interface{}, key string, fallback bool) bool {
	val, ok := m[key]
	if !ok {
		return fallback
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return fallback
}

func normalizeTrafficLimitBytes(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func normalizeTrafficLimitMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "upload", "download":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "total"
	}
}

func trafficUsedBytesForMode(rxTotal, txTotal float64, mode string) int64 {
	var used float64
	switch normalizeTrafficLimitMode(mode) {
	case "upload":
		used = txTotal
	case "download":
		used = rxTotal
	default:
		used = rxTotal + txTotal
	}
	if used < 0 {
		return 0
	}
	return int64(used)
}

func trafficUsedBytesFromMetrics(metrics map[string]interface{}, mode string) int64 {
	if metrics == nil {
		return 0
	}
	network := mapValue(metrics["network"])
	rxTotal := firstFloatValue(metrics, "net_in_transfer", "net_rx_total", "rx_total_bytes")
	txTotal := firstFloatValue(metrics, "net_out_transfer", "net_tx_total", "tx_total_bytes")
	if networkRx := getFloatFromMap(network, "rx_total_bytes"); networkRx > 0 {
		rxTotal = networkRx
	}
	if networkTx := getFloatFromMap(network, "tx_total_bytes"); networkTx > 0 {
		txTotal = networkTx
	}
	return trafficUsedBytesForMode(rxTotal, txTotal, mode)
}

func normalizeTrafficAlertPercent(value float64) float64 {
	if value <= 0 {
		return 100
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeTrafficCycleType(value string) string {
	normalized := strings.TrimSpace(value)
	switch normalized {
	case "calendar_month", "monthly", "custom", "none":
		return normalized
	default:
		return "none"
	}
}

func normalizeTrafficCycleDay(value int) int {
	if value < 1 {
		return 1
	}
	if value > 28 {
		return 28
	}
	return value
}

// trafficCycleWindow 计算当前所处的计费周期窗口 [start, end)。
// 只对 calendar_month 与 monthly 自动推导（基于站点时区）；custom 与 none 返回 ok=false，
// 表示不自动回滚（custom 由用户在界面上手动维护 start/end）。
func trafficCycleWindow(cycleType string, cycleDay int, loc *time.Location, now time.Time) (start, end time.Time, ok bool) {
	switch normalizeTrafficCycleType(cycleType) {
	case "calendar_month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
		return start, end, true
	case "monthly":
		day := normalizeTrafficCycleDay(cycleDay)
		candidate := time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, loc)
		if candidate.After(now) {
			start = candidate.AddDate(0, -1, 0)
		} else {
			start = candidate
		}
		end = start.AddDate(0, 1, 0)
		return start, end, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

// trafficUsedForCycle 在累计用量基础上减去周期基线，得到"当前周期内"用量。
// cycle 为 none（不启用周期）时原样返回累计值；基线未初始化（<=0）时同样返回累计值（过渡期）。
func trafficUsedForCycle(usedBytes int64, cycleType string, baseline int64) int64 {
	if normalizeTrafficCycleType(cycleType) == "none" || baseline <= 0 {
		return usedBytes
	}
	d := usedBytes - baseline
	if d < 0 {
		return 0
	}
	return d
}

func enrichTrafficQuota(info map[string]interface{}, metrics map[string]interface{}, limitBytes int64, limitMode string, alertEnabled bool, alertPercent float64, cycleType string, cycleBaseline int64) {
	if info == nil || limitBytes <= 0 {
		return
	}
	network, ok := info["network"].(map[string]interface{})
	if !ok || network == nil {
		network = map[string]interface{}{}
		info["network"] = network
	}

	rxTotal := getFloatFromMap(network, "rx_total_bytes")
	txTotal := getFloatFromMap(network, "tx_total_bytes")
	if rxTotal == 0 {
		rxTotal = getFloatValue(metrics, "net_in_transfer")
	}
	if txTotal == 0 {
		txTotal = getFloatValue(metrics, "net_out_transfer")
	}
	rawUsed := trafficUsedBytesForMode(rxTotal, txTotal, limitMode)
	usedBytes := trafficUsedForCycle(rawUsed, cycleType, cycleBaseline)
	percent := 0.0
	if limitBytes > 0 {
		percent = (float64(usedBytes) / float64(limitBytes)) * 100
	}

	network["traffic_used_bytes"] = usedBytes
	network["traffic_raw_bytes"] = rawUsed
	network["traffic_limit_bytes"] = limitBytes
	network["traffic_limit_mode"] = normalizeTrafficLimitMode(limitMode)
	network["traffic_percent"] = percent
	network["traffic_alert_enabled"] = alertEnabled
	network["traffic_alert_percent"] = normalizeTrafficAlertPercent(alertPercent)
	network["traffic_used"] = formatBytes(usedBytes)
	network["traffic_limit"] = formatBytes(limitBytes)
}

func hasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan %s column: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	return false, nil
}

func toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	case string:
		trimmed := strings.TrimSpace(strings.TrimSuffix(v, "%"))
		if trimmed == "" {
			return 0, errors.New("empty numeric string")
		}
		return strconv.ParseFloat(trimmed, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", value)
	}
}

func getServerCapabilities(host string, port int, username, authType, privateKey, password string, isOnline bool) map[string]interface{} {
	hasPassword := password != ""
	hasPrivateKey := privateKey != ""
	sshConfigured := host != "" && port > 0 && username != "" && ((authType == "key" && hasPrivateKey) || (authType != "key" && hasPassword))

	return map[string]interface{}{
		"is_online":         isOnline,
		"ssh_configured":    sshConfigured,
		"agent_connected":   isOnline,
		"has_password":      hasPassword,
		"has_private_key":   hasPrivateKey,
		"supports_agent":    true,
		"supports_ssh":      sshConfigured,
		"supports_sftp":     sshConfigured || isOnline,
		"supports_terminal": sshConfigured || isOnline,
		"supports_docker":   isOnline,
		"supports_metrics":  isOnline,
		"connection_type":   "agent",
		"capability_source": "go-serveragent",
	}
}

func buildGpuInfo(metrics map[string]interface{}) []map[string]interface{} {
	if raw, ok := metrics["gpu"]; ok {
		switch g := raw.(type) {
		case []map[string]interface{}:
			return g
		case []interface{}:
			result := make([]map[string]interface{}, 0, len(g))
			for _, item := range g {
				if obj, ok := item.(map[string]interface{}); ok {
					result = append(result, obj)
				}
			}
			if result != nil {
				return result
			}
		case map[string]interface{}:
			return []map[string]interface{}{g}
		}
	}

	model := extractGPUModel(metrics)
	usage := metricFloat(metrics, "gpu_usage")
	memUsed := metricFloat(metrics, "gpu_mem_used")
	memTotal := metricFloat(metrics, "gpu_mem_total")
	power := metricFloat(metrics, "gpu_power")
	temp := metricFloat(metrics, "gpu_temp")
	if usage == 0 && memUsed == 0 && memTotal == 0 && power == 0 && temp == 0 && model == "" {
		return []map[string]interface{}{}
	}
	if model == "" {
		model = "GPU"
	}

	return []map[string]interface{}{{
		"name":        model,
		"usage":       usage,
		"memUsed":     memUsed,
		"memTotal":    memTotal,
		"power":       power,
		"temp":        temp,
		"memoryUsed":  memUsed,
		"memoryTotal": memTotal,
	}}
}

func metricFloat(metrics map[string]interface{}, key string) float64 {
	if value, ok := metrics[key]; ok {
		if f, err := toFloat(value); err == nil {
			return f
		}
	}
	return 0
}

func stringMetric(metrics map[string]interface{}, key, fallback string) string {
	if value, ok := metrics[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func extractCPUModel(metrics map[string]interface{}) string {
	candidates := []string{
		stringMetric(metrics, "cpu_model", ""),
		stringMetric(metrics, "cpu_name", ""),
		stringMetric(metrics, "processor", ""),
		stringMetric(metrics, "model_name", ""),
		stringMetric(metrics, "hardware_model", ""),
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	if cpuRaw, ok := metrics["cpu"]; ok {
		switch cpu := cpuRaw.(type) {
		case []string:
			return strings.Join(filterNonEmptyStrings(cpu), " / ")
		case []interface{}:
			var models []string
			for _, item := range cpu {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					models = append(models, strings.TrimSpace(text))
				}
			}
			if len(models) > 0 {
				return strings.Join(models, " / ")
			}
		case map[string]interface{}:
			return firstNonEmpty(
				getString(cpu, "Model"),
				getString(cpu, "model"),
				getString(cpu, "Name"),
				getString(cpu, "name"),
			)
		}
	}
	return ""
}

func extractGPUModel(metrics map[string]interface{}) string {
	candidates := []string{
		stringMetric(metrics, "gpu_model", ""),
		stringMetric(metrics, "gpu_name", ""),
		stringMetric(metrics, "graphics", ""),
		stringMetric(metrics, "video", ""),
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	if gpuRaw, ok := metrics["gpu"]; ok {
		switch gpu := gpuRaw.(type) {
		case []string:
			return strings.Join(filterNonEmptyStrings(gpu), " / ")
		case []interface{}:
			var models []string
			for _, item := range gpu {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					models = append(models, strings.TrimSpace(text))
					continue
				}
				if obj, ok := item.(map[string]interface{}); ok {
					model := firstNonEmpty(
						getString(obj, "Model"),
						getString(obj, "model"),
						getString(obj, "Name"),
						getString(obj, "name"),
					)
					if model != "" {
						models = append(models, model)
					}
				}
			}
			if len(models) > 0 {
				return strings.Join(models, " / ")
			}
		case map[string]interface{}:
			return firstNonEmpty(
				getString(gpu, "Model"),
				getString(gpu, "model"),
				getString(gpu, "Name"),
				getString(gpu, "name"),
			)
		}
	}
	return ""
}

func filterNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, err := toFloat(v); err == nil {
			return f
		}
	}
	return 0
}

func firstFloatValue(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value := getFloatValue(m, key); value != 0 {
			return value
		}
	}
	return 0
}

func firstOptionalFloatValue(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, exists := m[key]
		if !exists || raw == nil {
			continue
		}
		value, err := toFloat(raw)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func hasUsableCoordinates(lat, lon float64) bool {
	return !(lat == 0 && lon == 0)
}

func getIntValue(m map[string]interface{}, key string) int {
	return int(getFloatValue(m, key))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatSpeed(bytesPerSecond float64) string {
	return formatBytes(int64(bytesPerSecond)) + "/s"
}

func formatUptime(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
