package uptime

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func scanAll(rows *sql.Rows) ([]map[string]interface{}, error) {
	items := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func scanMap(rows *sql.Rows) (map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	dest := make([]interface{}, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	row := map[string]interface{}{}
	for i, column := range columns {
		switch value := values[i].(type) {
		case []byte:
			row[column] = string(value)
		case int64, float64, string, nil:
			row[column] = value
		default:
			row[column] = value
		}
	}
	return row, nil
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]interface{}{}, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func objectValue(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	}
	return map[string]interface{}{}
}

func objectSlice(value interface{}) []map[string]interface{} {
	items := []map[string]interface{}{}
	if value == nil {
		return items
	}
	if typed, ok := value.([]interface{}); ok {
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				items = append(items, itemMap)
			}
		}
	}
	return items
}

func int64Slice(value interface{}) []int64 {
	out := []int64{}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if id := int64Value(item, 0); id > 0 {
				out = append(out, id)
			}
		}
	case []int64:
		out = append(out, typed...)
	case []int:
		for _, id := range typed {
			out = append(out, int64(id))
		}
	}
	return out
}

func firstExisting(data map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return fallback
		}
		return typed
	case []byte:
		if len(typed) == 0 {
			return fallback
		}
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		text := fmt.Sprint(value)
		if text == "<nil>" || text == "" {
			return fallback
		}
		return text
	}
}

func stringFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringPointer(value interface{}) *string {
	text := stringValue(value, "")
	if text == "" {
		return nil
	}
	return &text
}

func stringPtrValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return stringValue(value, "")
}

func nullableString(value interface{}) interface{} {
	text := stringValue(value, "")
	if text == "" {
		return nil
	}
	return text
}

func intValue(value interface{}, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		parsed, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil {
			return fallback
		}
		return parsed
	}
}

func int64Value(value interface{}, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		if err != nil {
			return fallback
		}
		return parsed
	}
}

func floatValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}

func nullableInt(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
	}
	return intValue(value, 0)
}

func intPtrValue(value interface{}) int {
	if value == nil {
		return 0
	}
	return intValue(value, 0)
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return parsed
		}
		number, err := strconv.Atoi(typed)
		if err == nil {
			return number != 0
		}
	}
	return fallback
}

func boolIntValue(value interface{}, fallback bool) int {
	return boolInt(boolValue(value, fallback))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stateText(value interface{}) string {
	text := stringValue(value, stateUp)
	switch text {
	case "ok":
		return stateUp
	case "pending":
		return statePendingDown
	case "firing":
		return stateDown
	case "recovery":
		return statePendingUp
	default:
		return text
	}
}

func parseTimeFallback(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05 -0700 MST"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return fallback
}

// parseTimeFallbackAny 兼容 DATETIME 列经 modernc sqlite 驱动返回的
// time.Time 值与字符串时间格式；解析失败回退 fallback。
func parseTimeFallbackAny(value interface{}, fallback time.Time) time.Time {
	if t, ok := value.(time.Time); ok {
		if t.IsZero() {
			return fallback
		}
		return t
	}
	return parseTimeFallback(stringValue(value, ""), fallback)
}

func formatDuration(ms int64) string {
	if ms < 60_000 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if ms < 3_600_000 {
		return fmt.Sprintf("%dm%ds", ms/60_000, (ms%60_000)/1000)
	}
	return fmt.Sprintf("%dh%dm", ms/3_600_000, (ms%3_600_000)/60_000)
}

func generateToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func escapeSVG(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
