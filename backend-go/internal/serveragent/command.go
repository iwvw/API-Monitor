package serveragent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/commandguard"
)

type DangerResult struct {
	Dangerous bool     `json:"dangerous"`
	Reasons   []string `json:"reasons"`
}

// DetectDangerousCommand 委托给共享的 commandguard 包，规则唯一来源在那里。
func DetectDangerousCommand(command string) DangerResult {
	r := commandguard.Detect(command)
	return DangerResult{Dangerous: r.Dangerous, Reasons: r.Reasons}
}

func NormalizeList(value interface{}) []string {
	if value == nil {
		return []string{}
	}

	switch val := value.(type) {
	case []string:
		var result []string
		for _, s := range val {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if result == nil {
			return []string{}
		}
		return result
	case []interface{}:
		var result []string
		for _, v := range val {
			if s, ok := v.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		if result == nil {
			return []string{}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return []string{}
		}

		// Try parsing as JSON array
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			var parsed []interface{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return NormalizeList(parsed)
			}
		}

		// Fall back to comma separation
		parts := strings.Split(trimmed, ",")
		var result []string
		for _, part := range parts {
			t := strings.TrimSpace(part)
			if t != "" {
				result = append(result, t)
			}
		}
		if result == nil {
			return []string{}
		}
		return result
	}

	return []string{}
}

func SerializeList(value interface{}) string {
	list := NormalizeList(value)
	bytes, _ := json.Marshal(list)
	return string(bytes)
}

func BuildCommandVariables(server map[string]interface{}, extra map[string]interface{}) map[string]string {
	variables := make(map[string]string)

	// Defaults from server
	if host, ok := server["host"].(string); ok {
		variables["host"] = host
	} else {
		variables["host"] = ""
	}
	if name, ok := server["name"].(string); ok {
		variables["name"] = name
	} else {
		variables["name"] = ""
	}
	if port, ok := server["port"]; ok {
		variables["port"] = fmt.Sprintf("%v", port)
	} else {
		variables["port"] = "22"
	}
	if username, ok := server["username"].(string); ok {
		variables["username"] = username
	} else {
		variables["username"] = ""
	}

	// Dynamic datetime
	now := time.Now()
	variables["date"] = now.Format("2006-01-02")
	variables["datetime"] = now.Format("2006-01-02T15:04:05.000Z")
	variables["cwd"] = ""

	// Merge extra variables
	for k, v := range extra {
		if v != nil {
			variables[k] = fmt.Sprintf("%v", v)
		}
	}

	return variables
}

var variableRegexp = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func RenderCommandTemplate(command string, variables map[string]string) string {
	return variableRegexp.ReplaceAllStringFunc(command, func(match string) string {
		key := match[1 : len(match)-1]
		if val, ok := variables[key]; ok {
			return val
		}
		return match
	})
}
