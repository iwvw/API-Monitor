package serveragent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type DangerousPattern struct {
	Pattern *regexp.Regexp
	Reason  string
}

var dangerousPatterns = []DangerousPattern{
	{regexp.MustCompile(`(?i)\brm\s+-[^\n;|&]*r[^\n;|&]*f\b`), "递归强制删除文件"},
	{regexp.MustCompile(`(?i)\bdd\s+if=.*\bof=`), "直接写入磁盘或块设备"},
	{regexp.MustCompile(`(?i)\bmkfs(?:\.[a-z0-9]+)?\b`), "格式化文件系统"},
	{regexp.MustCompile(`(?i)\b(shutdown|reboot|poweroff|halt)\b`), "重启或关闭主机"},
	{regexp.MustCompile(`(?i)\bdocker\s+(?:system\s+prune|rm|rmi|volume\s+rm)\b`), "删除 Docker 资源"},
	{regexp.MustCompile(`(?i)\bkubectl\s+delete\b`), "删除 Kubernetes 资源"},
	{regexp.MustCompile(`(?i)\bDROP\s+(?:DATABASE|TABLE)\b`), "删除数据库对象"},
	{regexp.MustCompile(`(?i)\bRemove-Item\b[^\n;|]*\s-(?:Recurse|r)\b`), "PowerShell 递归删除"},
	{regexp.MustCompile(`(?i)\b(Stop-Computer|Restart-Computer)\b`), "重启或关闭 Windows 主机"},
}

type DangerResult struct {
	Dangerous bool     `json:"dangerous"`
	Reasons   []string `json:"reasons"`
}

func DetectDangerousCommand(command string) DangerResult {
	var reasons []string
	seen := make(map[string]bool)
	for _, dp := range dangerousPatterns {
		if dp.Pattern.MatchString(command) {
			if !seen[dp.Reason] {
				seen[dp.Reason] = true
				reasons = append(reasons, dp.Reason)
			}
		}
	}
	if reasons == nil {
		reasons = []string{}
	}
	return DangerResult{
		Dangerous: len(reasons) > 0,
		Reasons:   reasons,
	}
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
