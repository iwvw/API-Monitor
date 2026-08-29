package claude

import "net/http"

func writeClaudeError(w http.ResponseWriter, status int, message string) {
	code := "invalid_request"
	switch status {
	case http.StatusUnauthorized:
		code = "authentication_failed"
	case http.StatusTooManyRequests:
		code = "rate_limit_exceeded"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusInternalServerError:
		code = "internal_error"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
			"code":    code,
			"param":   nil,
		},
	})
}
