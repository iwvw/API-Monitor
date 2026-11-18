package response

import (
	"encoding/json"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

type Envelope struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// JSON API 默认不缓存:鉴权响应含用户信息,公共状态页等确需缓存的路由
	// 会在此之前显式设置 Cache-Control(Set 会覆盖这里的默认值)。
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func OK(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusOK, Envelope{Success: true, Data: data})
}

func Error(w http.ResponseWriter, status int, message string) {
	payload := Envelope{Success: false, Error: message}
	if requestID := applog.RequestIDFromHeader(w); requestID != "" {
		payload.RequestID = requestID
	}
	JSON(w, status, payload)
}
