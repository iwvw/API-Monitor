package gemini

import "net/http"

func writeGeminiError(w http.ResponseWriter, status int, message string) {
	errorStatus := "INVALID_ARGUMENT"
	switch status {
	case http.StatusUnauthorized:
		errorStatus = "UNAUTHENTICATED"
	case http.StatusForbidden:
		errorStatus = "PERMISSION_DENIED"
	case http.StatusTooManyRequests:
		errorStatus = "RESOURCE_EXHAUSTED"
	case http.StatusNotFound:
		errorStatus = "NOT_FOUND"
	default:
		if status >= 500 {
			errorStatus = "INTERNAL"
		}
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
			"status":  errorStatus,
		},
	})
}
