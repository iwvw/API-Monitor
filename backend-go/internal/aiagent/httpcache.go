package aiagent

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// etagFor 计算内容的强 ETag（内容哈希前 16 字节 hex）。
// 同一份内容必然得到同一个值，因此可用于「内容未变则 304」。
func etagFor(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}

// ifNoneMatchMatches 判断请求的 If-None-Match 是否命中给定 ETag。
// 支持 `*`、逗号分隔的多值，以及弱校验前缀 W/。
func ifNoneMatchMatches(r *http.Request, etag string) bool {
	raw := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if raw == "" {
		return false
	}
	if raw == "*" {
		return true
	}
	for _, candidate := range strings.Split(raw, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// writeNotModified 返回 304，并带上 ETag 与 no-cache。
//
// 客户端高频轮询（如桌面端每秒拉一次偏好、opencode 每秒探测 MCP 元数据）
// 时，内容未变直接 304，响应体为零字节——这是把轮询流量压到最低的
// 标准手段，且不改动客户端行为。
func writeNotModified(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotModified)
}
