package assets

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		response.Error(w, http.StatusBadRequest, "request parameter validation failed")
		return false
	}
	return true
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(buf)), nil
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func trimmedString(value interface{}) string {
	return strings.TrimSpace(stringValue(value))
}

func boolValue(value interface{}, fallback bool) bool {
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
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" {
			return true
		}
		if normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off" {
			return false
		}
	}
	return fallback
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func numberValue(value interface{}, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func jsonString(value interface{}) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func parseStringList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err == nil {
		return result
	}
	return []string{}
}

func parseObject(value string) map[string]interface{} {
	if strings.TrimSpace(value) == "" {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func parseFloatMap(value string) map[string]float64 {
	if strings.TrimSpace(value) == "" {
		return map[string]float64{}
	}
	var result map[string]float64
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return map[string]float64{}
	}
	return result
}

func parseWarnDays(value string) []int {
	if strings.TrimSpace(value) == "" {
		return []int{}
	}
	var result []int
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return []int{}
	}
	out := make([]int, 0, len(result))
	for _, day := range result {
		if day > 0 {
			out = append(out, day)
		}
	}
	return out
}

// warnDaysFromPayload 归一化来自请求体的告警阈值数组。
func warnDaysFromPayload(value interface{}) []int {
	raw, ok := value.([]interface{})
	if !ok {
		if typed, isInts := value.([]int); isInts {
			out := make([]int, 0, len(typed))
			for _, day := range typed {
				if day > 0 {
					out = append(out, day)
				}
			}
			return out
		}
		return []int{}
	}
	out := make([]int, 0, len(raw))
	for _, item := range raw {
		if day := intValue(item, 0); day > 0 {
			out = append(out, day)
		}
	}
	return out
}

func stringListFromPayload(value interface{}) []string {
	raw, ok := value.([]interface{})
	if !ok {
		if typed, isStrings := value.([]string); isStrings {
			return typed
		}
		if text, isString := value.(string); isString {
			return parseStringList(text)
		}
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := trimmedString(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func boundedLimit(value string) int {
	limit := intValue(value, defaultLimit)
	if limit < 1 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func boundedOffset(value string) int {
	offset := intValue(value, 0)
	if offset < 0 {
		return 0
	}
	return offset
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
