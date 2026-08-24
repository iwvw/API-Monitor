package serveragent

import (
	"fmt"
	"strings"
)

func validateDockerCreatePolicy(payload map[string]interface{}) []string {
	var violations []string
	if privileged, _ := payload["privileged"].(bool); privileged {
		violations = append(violations, "privileged containers are not allowed")
	}

	for _, arg := range stringListFromPayload(firstNonNil(payload["extraArgs"], payload["extra_args"])) {
		normalized := strings.ToLower(strings.TrimSpace(arg))
		if normalized == "" {
			continue
		}
		switch {
		case strings.HasPrefix(normalized, "--privileged"):
			violations = append(violations, "extraArgs cannot enable privileged mode")
		case normalized == "--pid=host" || normalized == "--pid host":
			violations = append(violations, "extraArgs cannot use host PID namespace")
		case normalized == "--network=host" || normalized == "--net=host" || normalized == "--network host" || normalized == "--net host":
			violations = append(violations, "extraArgs cannot use host network namespace")
		case strings.HasPrefix(normalized, "--device"):
			violations = append(violations, "extraArgs cannot map host devices")
		case strings.HasPrefix(normalized, "--cap-add"):
			violations = append(violations, "extraArgs cannot add Linux capabilities")
		case strings.HasPrefix(normalized, "--security-opt"):
			violations = append(violations, "extraArgs cannot set security options")
		case strings.HasPrefix(normalized, "--sysctl"):
			violations = append(violations, "extraArgs cannot set sysctls")
		}
	}

	return violations
}

func stringListFromPayload(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
