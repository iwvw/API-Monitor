package uptime

import (
	"net"
	"strconv"
	"strings"
)

func normalizeMonitorConfig(data map[string]interface{}, existing map[string]interface{}) map[string]interface{} {
	out := copyMap(existing)
	for key, value := range parseJSONMap(data["config_json"]) {
		out[key] = value
	}
	for key, value := range objectValue(data["config"]) {
		out[key] = value
	}
	for _, key := range []string{"jsonQueryPath", "jsonQueryOperator", "jsonExpectedValue", "expectedValue"} {
		if value, ok := data[key]; ok {
			out[key] = value
		}
	}
	if value, ok := firstExisting(data, "pushGraceSeconds", "push_grace_seconds"); ok {
		out["graceSeconds"] = value
	}
	for key, value := range out {
		if value == nil || stringValue(value, "") == "" {
			delete(out, key)
		}
	}
	return out
}

func shouldUpdateConfig(data map[string]interface{}) bool {
	for _, key := range []string{"config", "config_json", "jsonQueryPath", "jsonQueryOperator", "jsonExpectedValue", "expectedValue", "pushGraceSeconds", "push_grace_seconds"} {
		if _, ok := data[key]; ok {
			return true
		}
	}
	return false
}

func monitorKey(monitor map[string]interface{}) string {
	return strings.ToLower(strings.Join([]string{
		stringValue(monitor["name"], ""),
		stringValue(monitor["type"], "http"),
		stringValue(monitor["url"], ""),
		stringValue(monitor["hostname"], ""),
		strconv.Itoa(intValue(monitor["port"], 0)),
	}, "|"))
}

func monitorTarget(monitor map[string]interface{}) string {
	if target := stringValue(monitor["url"], ""); target != "" {
		return target
	}
	host := stringValue(monitor["hostname"], "")
	port := intValue(monitor["port"], 0)
	if port > 0 {
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	return host
}

func normalizeSlug(value interface{}) string {
	text := strings.ToLower(strings.TrimSpace(stringValue(value, "status")))
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "status"
	}
	return slug
}

func normalizeTargets(value interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				out = append(out, map[string]interface{}{"type": stringValue(firstNonNil(itemMap["type"], itemMap["targetType"]), "monitor"), "id": firstNonNil(itemMap["id"], itemMap["targetId"])})
			} else {
				out = append(out, map[string]interface{}{"type": "monitor", "id": item})
			}
		}
	case []int64:
		for _, id := range typed {
			out = append(out, map[string]interface{}{"type": "monitor", "id": id})
		}
	}
	return out
}

func remapMonitorIDs(page map[string]interface{}, idMap map[string]int64) {
	ids := int64Slice(firstNonNil(page["monitorIds"], page["monitor_ids"]))
	out := []int64{}
	for _, id := range ids {
		if mapped, ok := idMap[strconv.FormatInt(id, 10)]; ok {
			out = append(out, mapped)
		} else {
			out = append(out, id)
		}
	}
	page["monitorIds"] = out
}

func remapTargets(item map[string]interface{}, idMap map[string]int64) {
	targets := normalizeTargets(firstNonNil(item["targets"], item["targetIds"]))
	for _, target := range targets {
		if stringValue(target["type"], "monitor") == "monitor" {
			key := stringValue(target["id"], "")
			if mapped, ok := idMap[key]; ok {
				target["id"] = mapped
			}
		}
	}
	item["targets"] = targets
}

func mapObjects(items []map[string]interface{}, mapper func(map[string]interface{}) map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, mapper(item))
	}
	return out
}
