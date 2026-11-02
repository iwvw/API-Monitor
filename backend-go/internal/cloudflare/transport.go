package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func (s *Service) cfRequest(ctx context.Context, method, path string, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	target := s.apiBase + cloudflarePath(path)
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if strings.HasPrefix(key, "__") {
			continue
		}
		req.Header.Set(key, cleanHeader(value))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	var payload map[string]interface{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("HTTP %d: invalid JSON response", res.StatusCode)
		}
	} else {
		payload = map[string]interface{}{}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || payload["success"] == false {
		return nil, cloudflareError(res.StatusCode, payload, raw)
	}
	return payload, nil
}

func (s *Service) cfRawRequest(ctx context.Context, method, path string, headers map[string]string, accept, contentType string, body io.Reader) ([]byte, string, error) {
	target := s.apiBase + cloudflarePath(path)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, "", err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		if strings.HasPrefix(key, "__") {
			continue
		}
		req.Header.Set(key, cleanHeader(value))
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	// cfRawRequest 通用请求读取上限：超过该大小的响应不再静默截断，
	// 而是返回错误，避免大文件（如 R2 对象）被截断后以 200 交付损坏数据。
	const maxCFResponseBytes = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxCFResponseBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxCFResponseBytes {
		return nil, "", fmt.Errorf("response exceeds download limit of %d bytes", maxCFResponseBytes)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var payload map[string]interface{}
		if json.Unmarshal(raw, &payload) == nil {
			return nil, "", cloudflareError(res.StatusCode, payload, raw)
		}
		return nil, "", fmt.Errorf("HTTP %d: %s", res.StatusCode, string(raw))
	}
	return raw, res.Header.Get("Content-Type"), nil
}

func cloudflareError(status int, payload map[string]interface{}, raw []byte) error {
	messages := []string{}
	for _, item := range arrayValue(payload["errors"]) {
		object := objectValue(item)
		message := stringValue(object["message"], "")
		if message != "" {
			messages = append(messages, message)
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, ", "))
	}
	if message := stringValue(payload["message"], ""); message != "" {
		return errors.New(message)
	}
	if len(raw) > 0 {
		return fmt.Errorf("HTTP %d: %s", status, string(raw))
	}
	return fmt.Errorf("HTTP %d", status)
}

func authHeaders(apiToken, email string) map[string]string {
	if email != "" {
		return map[string]string{
			"X-Auth-Email": email,
			"X-Auth-Key":   apiToken,
		}
	}
	return map[string]string{"Authorization": "Bearer " + apiToken}
}

func validateCloudflareCredential(apiToken, cfAccountID string) error {
	if strings.HasPrefix(strings.TrimSpace(apiToken), "v1.0-") {
		return errors.New("Origin CA Key / Service Key 已被 Cloudflare 弃用，请改用 API Token；创建 Origin CA 证书需授予 Zone - SSL and Certificates - Edit 权限")
	}
	if strings.HasPrefix(strings.TrimSpace(apiToken), "cfat_") && strings.TrimSpace(cfAccountID) == "" {
		return errors.New("账户 API 令牌需要填写 Cloudflare Account ID")
	}
	return nil
}

func cleanHeader(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, value)
	return strings.TrimSpace(cleaned)
}

func cloudflarePath(path string) string {
	if strings.HasPrefix(path, "/client/v4") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "/client/v4" + path
}

func envURL(name, fallback string) string {
	if value := cleanURL(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func cleanURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	if _, err := url.ParseRequestURI(value); err != nil {
		return ""
	}
	return value
}
