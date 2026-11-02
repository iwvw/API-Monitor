package huawei

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// huaweiSigner 实现华为云 API 的 AK/SK 签名（SDK-HMAC-SHA256）。
// 签名规则已对照官方 Go SDK（huaweicloud-sdk-go-v3/core/auth/signer/signer.go）
// 并用真实凭证实测校准，关键点：
//   - canonicalURI 末尾无条件补 "/"（仅用于签名，实际请求 URL 用原始路径）
//   - canonicalRequest 含第 6 段 contentHash（JSON body 的 sha256，空 body 为 sha256("")）
//   - 签名 key 直接用 SK（无 AWS 式 kDate/kRegion 派生链）
type huaweiSigner struct {
	ak string
	sk string
}

func escapePathSegment(s string) string {
	const hexDigits = "0123456789ABCDEF"
	hexCount := 0
	for i := 0; i < len(s); i++ {
		if shouldEscape(s[i]) {
			hexCount++
		}
	}
	if hexCount == 0 {
		return s
	}
	out := make([]byte, 0, len(s)+2*hexCount)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldEscape(c) {
			out = append(out, '%', hexDigits[c>>4], hexDigits[c&15])
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}

func shouldEscape(c byte) bool {
	if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '_' || c == '-' || c == '~' || c == '.' {
		return false
	}
	return true
}

// canonicalURI 对路径逐段 escape 后拼接，末尾补 "/"（仅签名用）。
func canonicalURI(path string) string {
	segments := strings.Split(path, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, escapePathSegment(segment))
	}
	uri := strings.Join(escaped, "/")
	if uri == "" || uri[len(uri)-1] != '/' {
		uri += "/"
	}
	return uri
}

// canonicalQueryString 对 query 的 key 排序、值 escape（%20 风格），返回 key=value&...。
func canonicalQueryString(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, key := range keys {
		values := query[key]
		sort.Strings(values)
		for _, value := range values {
			parts = append(parts, escapePathSegment(key)+"="+escapePathSegment(value))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// signHeaders 生成请求头（x-sdk-date + Authorization）。
func (s *huaweiSigner) signHeaders(host, method, path, query string, body []byte) (map[string]string, error) {
	return s.signHeadersAt(host, method, path, query, body, time.Now())
}

// signHeadersAt 可注入签名时间，便于测试断言确定性输出。
func (s *huaweiSigner) signHeadersAt(host, method, path, query string, body []byte, now time.Time) (map[string]string, error) {
	date := now.UTC().Format("20060102T150405Z")

	contentHash := sha256Hex([]byte(""))
	if len(body) > 0 {
		contentHash = sha256Hex(body)
	}

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(path),
		query,
		"host:" + host + "\n" + "x-sdk-date:" + date + "\n",
		"host;x-sdk-date",
		contentHash,
	}, "\n")

	stringToSign := strings.Join([]string{
		"SDK-HMAC-SHA256",
		date,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(s.sk))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return nil, err
	}
	signature := hex.EncodeToString(mac.Sum(nil))

	authorization := fmt.Sprintf(
		"SDK-HMAC-SHA256 Access=%s, SignedHeaders=host;x-sdk-date, Signature=%s",
		s.ak, signature,
	)
	return map[string]string{
		"x-sdk-date":    date,
		"Authorization": authorization,
	}, nil
}

func (s *huaweiSigner) validate() error {
	if strings.TrimSpace(s.ak) == "" {
		return fmt.Errorf("Access Key ID（AK）不能为空")
	}
	if strings.TrimSpace(s.sk) == "" {
		return fmt.Errorf("Secret Access Key（SK）不能为空")
	}
	return nil
}
