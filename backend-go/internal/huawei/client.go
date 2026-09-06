package huawei

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const requestTimeout = 30 * time.Second

// siteSuffix 返回华为云站点域名后缀：国内站 myhuaweicloud.cn、国际站 myhuaweicloud.com。
func siteSuffix(site string) string {
	if siteOrDefault(site) == "intl" {
		return "com"
	}
	return "cn"
}

// serviceHost 返回服务端点主机名（站点 + 区域推导）。
// 注意：rms / bss / iam 为全局服务，无区域前缀。
func serviceHost(site, region, service string) string {
	switch service {
	case "rms", "bss", "iam":
		return fmt.Sprintf("%s.myhuaweicloud.%s", service, siteSuffix(site))
	default:
		return fmt.Sprintf("%s.%s.myhuaweicloud.%s", service, region, siteSuffix(site))
	}
}

// serviceBaseURL 支持环境变量逐服务覆盖（HUAWEI_<SERVICE>_API_BASE_URL），测试指向 mock server。
func serviceBaseURL(site, region, service string) string {
	key := "HUAWEI_" + strings.ToUpper(service) + "_API_BASE_URL"
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "https://" + serviceHost(site, region, service)
}

// client 携带账号凭证，为一次请求签名。
type client struct {
	http *http.Client
	ak   string
	sk   string
	site string
	region string
}

// do 发起一次已签名请求。query 与签名共用 canonicalQueryString 编码，保证一致。
func (c *client) do(ctx context.Context, service, method, path string, query url.Values, body interface{}, out interface{}) error {
	base := serviceBaseURL(c.site, c.region, service)
	host := serviceHost(c.site, c.region, service)
	queryString := canonicalQueryString(query)

	var bodyBytes []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = raw
	}

	signer := &huaweiSigner{ak: c.ak, sk: c.sk}
	if err := signer.validate(); err != nil {
		return err
	}
	headers, err := signer.signHeaders(host, method, path, queryString, bodyBytes)
	if err != nil {
		return err
	}

	endpoint := base + path
	if queryString != "" {
		endpoint += "?" + queryString
	}

	var reader io.Reader
	if len(bodyBytes) > 0 {
		reader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Host", host)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Code    string `json:"error_code"`
			Message string `json:"error_msg"`
		}
		_ = json.Unmarshal(payload, &apiErr)
		if apiErr.Message != "" {
			return fmt.Errorf("华为云 %s %s: %d %s（%s）", method, path, resp.StatusCode, apiErr.Message, apiErr.Code)
		}
		return fmt.Errorf("华为云 %s %s: 状态码 %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode huawei response for %s %s: %w", method, path, err)
	}
	return nil
}

// listJSON 分页聚合：RMS/DNS 用 marker 分页，其余一次性返回。handle 处理单条资源。
func (c *client) listJSON(ctx context.Context, service, path string, query url.Values, listField string, markerParam string, handle func(raw json.RawMessage) error) error {
	for {
		values := url.Values{}
		for key, items := range query {
			for _, item := range items {
				values.Add(key, item)
			}
		}
		var rawBody bytes.Buffer
		if err := c.doInto(ctx, service, http.MethodGet, path, values, &rawBody); err != nil {
			return err
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(rawBody.Bytes(), &envelope); err != nil {
			return err
		}
		if listRaw, ok := envelope[listField]; ok {
			var rows []json.RawMessage
			if err := json.Unmarshal(listRaw, &rows); err != nil {
				return err
			}
			for _, entry := range rows {
				if err := handle(entry); err != nil {
					return err
				}
			}
		}
		var marker string
		if markerRaw, ok := envelope[markerParam]; ok {
			_ = json.Unmarshal(markerRaw, &marker)
		}
		if marker == "" {
			break
		}
		values.Set("marker", marker)
		query = values
	}
	return nil
}

// doInto 将响应 body 写入 bytes.Buffer（分页迭代用）。
func (c *client) doInto(ctx context.Context, service, method, path string, query url.Values, dest *bytes.Buffer) error {
	base := serviceBaseURL(c.site, c.region, service)
	host := serviceHost(c.site, c.region, service)
	queryString := canonicalQueryString(query)

	signer := &huaweiSigner{ak: c.ak, sk: c.sk}
	headers, err := signer.signHeaders(host, method, path, queryString, nil)
	if err != nil {
		return err
	}

	endpoint := base + path
	if queryString != "" {
		endpoint += "?" + queryString
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Host", host)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Code    string `json:"error_code"`
			Message string `json:"error_msg"`
		}
		_ = json.Unmarshal(payload, &apiErr)
		if apiErr.Message != "" {
			return fmt.Errorf("华为云 %s %s: %d %s（%s）", method, path, resp.StatusCode, apiErr.Message, apiErr.Code)
		}
		return fmt.Errorf("华为云 %s %s: 状态码 %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	_, err = dest.Write(payload)
	return err
}
