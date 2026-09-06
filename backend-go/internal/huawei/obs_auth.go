package huawei

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// obsSigner 实现 OBS 对象存储的独立签名协议（OBS AK:signature，HMAC-SHA1）。
// 签名向量与 OBS 控制台/官方 SDK 兼容，已用真实凭证实测通过（列桶 200）。
type obsSigner struct {
	ak string
	sk string
}

// signHeaders 生成 OBS 请求头（Date + Authorization）。
// canonicalizedResource 形如 "/bucket"、"/" 或 "/bucket/object"；
// 普通 query（prefix/max-keys 等）不参与签名，仅出现在请求 URL。
func (o *obsSigner) signHeaders(method, canonicalizedResource, contentType, contentMD5 string) (map[string]string, error) {
	date := time.Now().UTC().Format(http.TimeFormat)

	stringToSign := strings.Join([]string{
		method,
		contentMD5,
		contentType,
		date,
		canonicalizedResource,
	}, "\n")

	mac := hmac.New(sha1.New, []byte(o.sk))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return nil, err
	}
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          date,
		"Authorization": fmt.Sprintf("OBS %s:%s", o.ak, signature),
		"Content-Type":  contentType,
		"Content-MD5":   contentMD5,
	}, nil
}

// obsCanonicalResource 构造 CanonicalizedResource：含子资源时按 key 排序拼接。
func obsCanonicalResource(bucket, object string, subResources map[string]string) string {
	var resource strings.Builder
	resource.WriteString("/")
	if bucket != "" {
		resource.WriteString(bucket)
	}
	if object != "" {
		resource.WriteString("/" + object)
	}
	if len(subResources) > 0 {
		keys := make([]string, 0, len(subResources))
		for key := range subResources {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if value := subResources[key]; value != "" {
				parts = append(parts, key+"="+value)
			} else {
				parts = append(parts, key)
			}
		}
		resource.WriteString("?" + strings.Join(parts, "&"))
	}
	return resource.String()
}

// obsRequest 发送 OBS 签名请求并返回响应字节。query 为普通查询参数（不参与签名）。
func (c *client) obsRequest(ctx context.Context, region, method, bucket, object string, query url.Values, contentType string, body []byte) ([]byte, error) {
	host := "obs." + region + ".myhuaweicloud." + siteSuffix(c.site)
	resourcePath := "/"
	if bucket != "" {
		resourcePath += bucket
	}
	if object != "" {
		resourcePath += "/" + escapeOBSPath(object)
	}
	headers, err := (&obsSigner{ak: c.ak, sk: c.sk}).signHeaders(method, resourcePath, contentType, "")
	if err != nil {
		return nil, err
	}

	endpoint := "https://" + host + resourcePath
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Date", headers["Date"])
	req.Header.Set("Authorization", headers["Authorization"])
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OBS %s %s: %d %s", method, resourcePath, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

// escapeOBSPath 对象名按 "/" 分段编码（保留目录层级）。
func escapeOBSPath(object string) string {
	segments := strings.Split(object, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return strings.Join(escaped, "/")
}