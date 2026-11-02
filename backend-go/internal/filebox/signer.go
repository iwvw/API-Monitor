package filebox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ComputeSignature 计算与 Agent storage_server 对称的 HMAC-SHA256 签名
func ComputeSignature(method, code, filename string, expires, maxSize int64, secret string) string {
	message := fmt.Sprintf(
		"%s\n%s\n%s\n%d\n%d",
		strings.ToUpper(method),
		code,
		filename,
		expires,
		maxSize,
	)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// BuildSignedURL 构建发往 Agent storage_server 的签名完整直链 URL
func BuildSignedURL(method, host string, port int, code, filename string, maxSize int64, ttl time.Duration, secret string) (string, error) {
	if port <= 0 {
		port = 61208
	}
	expires := time.Now().Add(ttl).Unix()
	sig := ComputeSignature(method, code, filename, expires, maxSize, secret)

	baseURL := fmt.Sprintf("http://%s:%d/storage/%s/%s", host, port, url.PathEscape(code), url.PathEscape(filename))
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	q := parsed.Query()
	q.Set("expires", fmt.Sprintf("%d", expires))
	if maxSize > 0 {
		q.Set("max_size", fmt.Sprintf("%d", maxSize))
	}
	q.Set("signature", sig)
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

// VerifySignature 主站或测试中验证 HMAC 签名
func VerifySignature(method, code, filename string, expires, maxSize int64, sig, secret string) bool {
	if expires <= time.Now().Unix() {
		return false
	}
	expected := ComputeSignature(method, code, filename, expires, maxSize, secret)
	return strings.EqualFold(expected, strings.TrimSpace(sig))
}
