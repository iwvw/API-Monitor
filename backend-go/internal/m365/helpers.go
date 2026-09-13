package m365

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func parseFlexibleTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("unsupported time format")
}

func decodeStringSliceJSON(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	items := []string{}
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return items
	}
	return []string{}
}

func decodeInt64SliceJSON(value string) []int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return []int64{}
	}
	items := []int64{}
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return items
	}
	return []int64{}
}

func int64Array(value interface{}) []int64 {
	result := []int64{}
	for _, item := range interfaceArray(value) {
		number := numberValue(item)
		if number > 0 {
			result = append(result, number)
		}
	}
	return uniqueInt64(result)
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func envURL(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}

func parseID(text string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	defer r.Body.Close()
	payload := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
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
	case json.Number:
		return typed.String()
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return fallback
		}
		return text
	}
}

func objectValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok && typed != nil {
		return typed
	}
	return map[string]interface{}{}
}

func objectArray(value interface{}) []map[string]interface{} {
	list := []map[string]interface{}{}
	for _, item := range interfaceArray(value) {
		list = append(list, objectValue(item))
	}
	return list
}

func interfaceArray(value interface{}) []interface{} {
	if value == nil {
		return []interface{}{}
	}
	if typed, ok := value.([]interface{}); ok {
		return typed
	}
	return []interface{}{}
}

func stringArray(value interface{}) []string {
	result := []string{}
	for _, item := range interfaceArray(value) {
		text := strings.TrimSpace(stringValue(item, ""))
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func numberValue(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return fallback
		}
	case float64:
		return typed != 0
	default:
		return fallback
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func clampPositiveInt64(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func nullIfEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func pickDefaultDomain(org map[string]interface{}) string {
	domains := extractVerifiedDomains(org)
	if len(domains) > 0 {
		return domains[0]
	}
	return ""
}

func extractVerifiedDomains(org map[string]interface{}) []string {
	defaultDomains := []string{}
	otherDomains := []string{}
	for _, item := range interfaceArray(org["verifiedDomains"]) {
		domain := objectValue(item)
		name := strings.ToLower(strings.TrimSpace(stringValue(domain["name"], "")))
		if name == "" {
			continue
		}
		if boolValue(domain["isDefault"], false) {
			defaultDomains = append(defaultDomains, name)
			continue
		}
		otherDomains = append(otherDomains, name)
	}
	return normalizeDomainSlice(append(defaultDomains, otherDomains...))
}

func accountDomains(record accountRecord) []string {
	if len(record.VerifiedDomains) > 0 {
		return normalizeDomainSlice(record.VerifiedDomains)
	}
	if strings.TrimSpace(record.DefaultDomain) == "" {
		return []string{}
	}
	return []string{strings.ToLower(strings.TrimSpace(record.DefaultDomain))}
}

func mustJSONString(value interface{}) string {
	raw, err := jsonString(value)
	if err != nil {
		return "[]"
	}
	return raw
}

func maskSecret(secret string) string {
	plain := secure.SecureDecrypt(secret)
	if plain == "" {
		return ""
	}
	if len(plain) <= 8 {
		return strings.Repeat("*", len(plain))
	}
	return plain[:4] + strings.Repeat("*", 8) + plain[len(plain)-4:]
}

func emptyToNil(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func clampPositive(raw string, fallback, max int) string {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return strconv.Itoa(value)
}

func escapeSearch(value string) string {
	replacer := strings.NewReplacer(`"`, `\"`)
	return replacer.Replace(value)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstInt64(items []int64) int64 {
	if len(items) == 0 {
		return 0
	}
	return items[0]
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func uniqueInt64(items []int64) []int64 {
	result := make([]int64, 0, len(items))
	seen := map[int64]bool{}
	for _, item := range items {
		if item <= 0 || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func normalizeDomainSlice(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}
