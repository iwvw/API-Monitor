package subscription

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if w != nil {
			response.Error(w, http.StatusBadRequest, "request parameter validation failed")
		}
		return false
	}
	return true
}

func queryInt(r *http.Request, key string) int {
	value, _ := strconv.Atoi(r.URL.Query().Get(key))
	return value
}

func randomID(prefix string) string {
	return prefix + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "_" + randomHex(4)
}

func randomToken() string {
	return randomHex(24)
}

func randomCredential() string {
	buffer := make([]byte, 24)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func randomUUID() string {
	raw := randomHex(16)
	if len(raw) != 32 {
		return "00000000-0000-4000-8000-" + randomHex(6)
	}
	return raw[0:8] + "-" + raw[8:12] + "-4" + raw[13:16] + "-8" + raw[17:20] + "-" + raw[20:32]
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value != "" {
			return strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func intDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func firstSub(items []Subscription) interface{} {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func firstProfile(items []NodeLibrary) interface{} {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func normalizeTrafficSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "panel", "server", "node_servers", "upstream":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "manual"
	}
}

func normalizeCycleType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "monthly", "custom":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "none"
	}
}

func isExplicitFalse(r *http.Request, field string) bool {
	return false
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	return floatVal(m[key])
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	return floatVal(m[key])
}

func firstFloatValue(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value := getFloatValue(m, key); value != 0 {
			return value
		}
	}
	return 0
}

func stringVal(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func floatVal(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func nodeFingerprint(node Node) string {
	return strings.ToLower(fmt.Sprintf("%s|%s|%d", strings.TrimSpace(node.Type), strings.TrimSpace(node.Server), node.Port))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func parseTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.Add(24*time.Hour - time.Second), nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func jsonString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func isLinuxPlatform(platform, platformVersion string) bool {
	value := strings.ToLower(strings.TrimSpace(platform + " " + platformVersion))
	if value == "" {
		// Older Agent records did not persist a platform. Keep them manageable;
		// explicit non-Linux platforms below are still rejected.
		return true
	}
	if strings.Contains(value, "windows") || strings.Contains(value, "darwin") || strings.Contains(value, "macos") || strings.Contains(value, "freebsd") {
		return false
	}
	for _, marker := range []string{"linux", "ubuntu", "debian", "centos", "rhel", "red hat", "fedora", "rocky", "alma", "alpine", "arch", "opensuse", "sles", "oracle linux", "amzn", "amazon linux"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
