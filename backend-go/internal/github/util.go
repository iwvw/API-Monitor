package github

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func readObject(r *http.Request) (map[string]interface{}, error) {
	defer r.Body.Close()
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("request body must be an object")
	}
	return payload, nil
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func stringValue(payload map[string]interface{}, key, fallback string) string {
	if payload == nil {
		return fallback
	}
	return asString(payload[key], fallback)
}

func asString(value interface{}, fallback ...string) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case nil:
		if len(fallback) > 0 {
			return fallback[0]
		}
		return ""
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "<nil>" {
			s = ""
		}
		if s == "" && len(fallback) > 0 {
			return fallback[0]
		}
		return s
	}
}

func objectValue(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if obj, ok := value.(map[string]interface{}); ok {
		return obj
	}
	return map[string]interface{}{}
}

func intValue(payload map[string]interface{}, key string, fallback int) int {
	if payload == nil {
		return fallback
	}
	return intFromAnyDefault(payload[key], fallback)
}

func intFromAny(value interface{}) int {
	return intFromAnyDefault(value, 0)
}

func intFromAnyDefault(value interface{}, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return fallback
}

func int64Value(value interface{}, fallback int64) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return n
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func boolValue(value interface{}, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case float64:
		return v != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on", "enabled":
			return true
		case "false", "0", "no", "off", "disabled":
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

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func firstError(current, next error) error {
	if current != nil {
		return current
	}
	return next
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeGitHubPublicSlug(value, fallback string) string {
	text := strings.TrimSpace(strings.ToLower(firstNonEmpty(value, fallback)))
	if text == "" {
		text = strings.TrimSpace(strings.ToLower(fallback))
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return strings.TrimSpace(strings.ToLower(fallback))
	}
	return slug
}

func normalizeGitHubPublicDomain(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.Split(normalized, "/")[0]
	normalized = strings.TrimSuffix(normalized, "/")
	if host, _, err := net.SplitHostPort(normalized); err == nil {
		return host
	}
	return normalized
}

func int64SliceValue(value interface{}) []int64 {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]int64, 0, len(items))
	for _, item := range items {
		id := int64Value(item, 0)
		if id > 0 {
			result = append(result, id)
		}
	}
	return result
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func escapePathSegments(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func intQuery(r *http.Request, key string, fallback int) int {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}

func int64Query(r *http.Request, key string, fallback int64) int64 {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func boolQuery(r *http.Request, key string, fallback bool) bool {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		return boolValue(value, fallback)
	}
	return fallback
}

func timeOrNil(value time.Time) interface{} {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func nullEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullString(value sql.NullString) interface{} {
	if value.Valid {
		return value.String
	}
	return nil
}

func normalizeDBValue(value interface{}) interface{} {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func compactStoredEventPayload(eventType string, payload map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	addCompactString(result, "repositoryId", payload["repositoryId"], 0)
	addCompactString(result, "repositoryFullName", payload["repositoryFullName"], 200)
	addCompactString(result, "owner", payload["owner"], 120)
	addCompactString(result, "repo", payload["repo"], 120)
	addCompactString(result, "htmlUrl", payload["htmlUrl"], 400)
	addCompactString(result, "eventType", payload["eventType"], 120)
	addCompactString(result, "severity", payload["severity"], 32)
	addCompactString(result, "collectedAt", payload["collectedAt"], 64)
	addCompactString(result, "action", payload["action"], 64)

	switch eventType {
	case "action_failed", "action_recovered":
		if run := compactWorkflowRunValue(payload); len(run) > 0 {
			result["workflow_run"] = run
		}
	case "release_published":
		if release := compactReleaseValue(payload); len(release) > 0 {
			result["release"] = release
		}
	case "issue_opened":
		if issue := compactIssueLikeValue(payload, false); len(issue) > 0 {
			result["issue"] = issue
		}
	case "pull_request_opened":
		if pullRequest := compactIssueLikeValue(payload, true); len(pullRequest) > 0 {
			result["pull_request"] = pullRequest
		}
	case "star_spike":
		if sender := compactUserValue(payload["sender"]); len(sender) > 0 {
			result["sender"] = sender
		}
		addCompactString(result, "starred_at", payload["starred_at"], 64)
	case "webhook_ping":
		addCompactString(result, "zen", payload["zen"], 240)
		if hookID := int64Value(payload["hook_id"], 0); hookID > 0 {
			result["hook_id"] = hookID
		}
	default:
		copyCompactKeys(result, payload, "id", "number", "state", "status", "conclusion", "created_at", "updated_at")
	}
	return result
}

func compactWebhookDeliveryPayload(eventType string, payload map[string]interface{}, raw []byte) map[string]interface{} {
	result := map[string]interface{}{
		"storage_mode": "summary",
		"raw_bytes":    len(raw),
	}
	addCompactString(result, "event_type", eventType, 64)
	addCompactString(result, "action", payload["action"], 64)
	if repository := compactRepositoryValue(payload["repository"]); len(repository) > 0 {
		result["repository"] = repository
	}
	if sender := compactUserValue(payload["sender"]); len(sender) > 0 {
		result["sender"] = sender
	}

	switch eventType {
	case "workflow_run":
		if run := compactWorkflowRunValue(payload["workflow_run"]); len(run) > 0 {
			result["workflow_run"] = run
		}
	case "release":
		if release := compactReleaseValue(payload["release"]); len(release) > 0 {
			result["release"] = release
		}
	case "issues":
		if issue := compactIssueLikeValue(payload["issue"], false); len(issue) > 0 {
			result["issue"] = issue
		}
	case "pull_request":
		if pullRequest := compactIssueLikeValue(payload["pull_request"], true); len(pullRequest) > 0 {
			result["pull_request"] = pullRequest
		}
	case "star":
		addCompactString(result, "starred_at", payload["starred_at"], 64)
	case "ping":
		addCompactString(result, "zen", payload["zen"], 240)
		if hookID := int64Value(payload["hook_id"], 0); hookID > 0 {
			result["hook_id"] = hookID
		}
	}

	return result
}

func compactWorkflowRunValue(value interface{}) map[string]interface{} {
	run := objectValue(value)
	if len(run) == 0 {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	copyCompactKeys(result, run, "id", "run_number", "run_attempt", "workflow_id")
	addCompactString(result, "name", run["name"], 200)
	addCompactString(result, "display_title", run["display_title"], 240)
	addCompactString(result, "html_url", run["html_url"], 400)
	addCompactString(result, "status", run["status"], 32)
	addCompactString(result, "conclusion", run["conclusion"], 32)
	addCompactString(result, "event", run["event"], 64)
	addCompactString(result, "head_branch", run["head_branch"], 120)
	addCompactString(result, "head_sha", run["head_sha"], 64)
	addCompactString(result, "created_at", run["created_at"], 64)
	addCompactString(result, "updated_at", run["updated_at"], 64)
	addCompactString(result, "run_started_at", run["run_started_at"], 64)
	if actor := compactUserValue(run["actor"]); len(actor) > 0 {
		result["actor"] = actor
	}
	if triggeringActor := compactUserValue(run["triggering_actor"]); len(triggeringActor) > 0 {
		result["triggering_actor"] = triggeringActor
	}
	return result
}

func compactReleaseValue(value interface{}) map[string]interface{} {
	release := objectValue(value)
	if len(release) == 0 {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	copyCompactKeys(result, release, "id")
	addCompactString(result, "tag_name", release["tag_name"], 120)
	addCompactString(result, "name", release["name"], 240)
	addCompactString(result, "html_url", release["html_url"], 400)
	addCompactString(result, "target_commitish", release["target_commitish"], 120)
	addCompactString(result, "created_at", release["created_at"], 64)
	addCompactString(result, "published_at", release["published_at"], 64)
	if value, ok := release["draft"]; ok {
		result["draft"] = boolValue(value, false)
	}
	if value, ok := release["prerelease"]; ok {
		result["prerelease"] = boolValue(value, false)
	}
	if author := compactUserValue(release["author"]); len(author) > 0 {
		result["author"] = author
	}
	return result
}

func compactIssueLikeValue(value interface{}, pullRequest bool) map[string]interface{} {
	item := objectValue(value)
	if len(item) == 0 {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	copyCompactKeys(result, item, "id", "number")
	addCompactString(result, "title", item["title"], 240)
	addCompactString(result, "html_url", item["html_url"], 400)
	addCompactString(result, "state", item["state"], 32)
	addCompactString(result, "created_at", item["created_at"], 64)
	addCompactString(result, "updated_at", item["updated_at"], 64)
	addCompactString(result, "closed_at", item["closed_at"], 64)
	if user := compactUserValue(item["user"]); len(user) > 0 {
		result["user"] = user
	}
	if labels := compactLabels(item["labels"]); len(labels) > 0 {
		result["labels"] = labels
	}
	if value, ok := item["draft"]; ok {
		result["draft"] = boolValue(value, false)
	}
	if pullRequest {
		addCompactString(result, "merged_at", item["merged_at"], 64)
	}
	return result
}

func compactLabels(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		label := objectValue(item)
		name := truncateCompactString(asString(label["name"]), 80)
		if name != "" {
			result = append(result, name)
		}
		if len(result) >= 5 {
			break
		}
	}
	return result
}

func compactUserValue(value interface{}) map[string]interface{} {
	user := objectValue(value)
	if len(user) == 0 {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	addCompactString(result, "login", user["login"], 120)
	addCompactString(result, "html_url", user["html_url"], 400)
	addCompactString(result, "type", user["type"], 32)
	return result
}

func compactRepositoryValue(value interface{}) map[string]interface{} {
	repository := objectValue(value)
	if len(repository) == 0 {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	copyCompactKeys(result, repository, "id")
	addCompactString(result, "name", repository["name"], 120)
	addCompactString(result, "full_name", repository["full_name"], 200)
	addCompactString(result, "html_url", repository["html_url"], 400)
	addCompactString(result, "default_branch", repository["default_branch"], 120)
	return result
}

func copyCompactKeys(target map[string]interface{}, source map[string]interface{}, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key]; ok && value != nil {
			target[key] = normalizeDBValue(value)
		}
	}
}

func addCompactString(target map[string]interface{}, key string, value interface{}, maxLen int) {
	text := asString(value)
	text = truncateCompactString(text, maxLen)
	if text != "" {
		target[key] = text
	}
}

func truncateCompactString(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxLen == 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
