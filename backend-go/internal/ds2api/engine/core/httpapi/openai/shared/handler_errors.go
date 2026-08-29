package shared

import "net/http"

func WriteOpenAIError(w http.ResponseWriter, status int, message string) {
	WriteOpenAIErrorWithCode(w, status, message, "")
}

func WriteOpenAIErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	if code == "" {
		code = OpenAIErrorCode(status)
	}
	WriteJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    OpenAIErrorType(status),
			"code":    code,
			"param":   nil,
		},
	})
}

func OpenAIErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func OpenAIErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication_failed"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "invalid_request"
	}
}
