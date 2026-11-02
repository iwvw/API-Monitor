package gcp

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

const (
	defaultComputeBaseURL   = "https://compute.googleapis.com/compute/v1/"
	defaultCRMBaseURL       = "https://cloudresourcemanager.googleapis.com/v1/"
	defaultCloudBillingBase = "https://cloudbilling.googleapis.com/v1/"
	defaultBudgetsBaseURL   = "https://billingbudgets.googleapis.com/v1/"
	defaultStorageBaseURL   = "https://storage.googleapis.com/storage/v1/"
	defaultUploadBaseURL    = "https://storage.googleapis.com/upload/storage/v1/"
	defaultMonitoringBaseURL = "https://monitoring.googleapis.com/v3/"
	defaultTokenURL         = "https://oauth2.googleapis.com/token"
	requestTimeout          = 30 * time.Second
	maxPageSize             = 500
)

var baseURLs = map[string]string{
	"compute":  envOr("GCP_COMPUTE_API_BASE_URL", defaultComputeBaseURL),
	"crm":      envOr("GCP_CRM_API_BASE_URL", defaultCRMBaseURL),
	"billing":  envOr("GCP_CLOUDBILLING_API_BASE_URL", defaultCloudBillingBase),
	"budgets":  envOr("GCP_BILLINGBUDGETS_API_BASE_URL", defaultBudgetsBaseURL),
	"storage":  envOr("GCP_STORAGE_API_BASE_URL", defaultStorageBaseURL),
	"upload":   envOr("GCP_STORAGE_UPLOAD_API_BASE_URL", defaultUploadBaseURL),
	"monitoring": envOr("GCP_MONITORING_API_BASE_URL", defaultMonitoringBaseURL),
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return strings.TrimRight(value, "/") + "/"
	}
	return fallback
}

type authProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

type client struct {
	http *http.Client
	auth authProvider
}

func (c *client) do(ctx context.Context, method, api, path string, query url.Values, body interface{}, out interface{}) error {
	base, ok := baseURLs[api]
	if !ok {
		return fmt.Errorf("unknown gcp api scope %q", api)
	}
	endpoint := base + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("gcp auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

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
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &apiErr)
		if apiErr.Error.Message != "" {
			return fmt.Errorf("GCP API %s %s: %d %s", method, path, resp.StatusCode, apiErr.Error.Message)
		}
		return fmt.Errorf("GCP API %s %s: unexpected status %d", method, path, resp.StatusCode)
	}

	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode gcp response for %s %s: %w", method, path, err)
	}
	return nil
}

// listJSON 通用分页列表迭代，支持三类响应形态：
//   - 顶层数组字段（如 Resource Manager projects.list 的 "projects"）
//   - 普通数组 items（如 storage objects.list 的 "items"）
//   - aggregatedList 的 items map（如 "zones/{zone}": {"instances": [...]}）
//
// subKeys 用于从 aggregated 的 zone/region 值里找资源数组；非聚合请求传 nil 走 items。
func (c *client) listJSON(ctx context.Context, method, api, path string, query url.Values, topField string, subKeys []string, handle func(raw json.RawMessage) error) error {
	page := query.Get("pageToken")
	pageSizeParam := "maxResults"
	switch api {
	case "crm", "billing", "budgets":
		pageSizeParam = "pageSize"
	}
	for {
		values := url.Values{}
		for key, items := range query {
			for _, item := range items {
				values.Add(key, item)
			}
		}
		if page != "" {
			values.Set("pageToken", page)
		}
		values.Set(pageSizeParam, fmt.Sprintf("%d", maxPageSize))

		var envelope struct {
			Top        json.RawMessage            `json:"items"`
			TopField   json.RawMessage            `json:"-"`
			NextPageToken string                  `json:"nextPageToken"`
		}
		// 先用空 struct 抓原始 body，避免字段名冲突
		var body bytes.Buffer
		if err := c.doInto(ctx, method, api, path, values, nil, &body); err != nil {
			return err
		}
		var rawBody map[string]json.RawMessage
		if err := json.Unmarshal(body.Bytes(), &rawBody); err != nil {
			return err
		}
		if fieldRaw, ok := rawBody[topField]; ok {
			envelope.TopField = fieldRaw
		} else if itemsRaw, ok := rawBody["items"]; ok {
			envelope.Top = itemsRaw
		}
		if tokenRaw, ok := rawBody["nextPageToken"]; ok {
			_ = json.Unmarshal(tokenRaw, &envelope.NextPageToken)
		}

		if len(envelope.TopField) > 0 {
			var rows []json.RawMessage
			_ = json.Unmarshal(envelope.TopField, &rows)
			for _, entry := range rows {
				if err := handle(entry); err != nil {
					return err
				}
			}
		} else if len(envelope.Top) > 0 && envelope.Top[0] == '{' {
			// aggregated items map
			var grouped map[string]json.RawMessage
			_ = json.Unmarshal(envelope.Top, &grouped)
			for _, groupRaw := range grouped {
				var group map[string]json.RawMessage
				if err := json.Unmarshal(groupRaw, &group); err != nil {
					continue
				}
				for _, subKey := range subKeys {
					if entriesRaw, ok := group[subKey]; ok {
						var entries []json.RawMessage
						if err := json.Unmarshal(entriesRaw, &entries); err != nil {
							continue
						}
						for _, entry := range entries {
							if err := handle(entry); err != nil {
								return err
							}
						}
					}
				}
			}
		} else if len(envelope.Top) > 0 {
			var rows []json.RawMessage
			_ = json.Unmarshal(envelope.Top, &rows)
			for _, entry := range rows {
				if err := handle(entry); err != nil {
					return err
				}
			}
		}
		if envelope.NextPageToken == "" {
			break
		}
		page = envelope.NextPageToken
	}
	return nil
}

// downloadMedia 用 Bearer token 拉取对象媒体字节（alt=media），返回内容和媒体类型。
func (c *client) downloadMedia(ctx context.Context, bucket, object string) ([]byte, string, error) {
	query := url.Values{}
	query.Set("alt", "media")
	base, ok := baseURLs["storage"]
	if !ok {
		return nil, "", fmt.Errorf("unknown gcp api scope %q", "storage")
	}
	endpoint := base + "b/" + url.PathEscape(bucket) + "/o/" + url.PathEscape(object) + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("gcp auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20+1))
	if err != nil {
		return nil, "", err
	}
	if len(payload) > 256<<20 {
		return nil, "", fmt.Errorf("GCP download %s: object exceeds 256MB", object)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("GCP download %s: status %d %s", object, resp.StatusCode, string(payload))
	}
	return payload, resp.Header.Get("Content-Type"), nil
}

// uploadRaw 向 upload 端点发送原始字节（GCS media upload，uploadType=media）。
func (c *client) uploadRaw(ctx context.Context, bucket, objectName, contentType string, data []byte) error {
	base, ok := baseURLs["upload"]
	if !ok {
		return fmt.Errorf("unknown gcp api scope %q", "upload")
	}
	query := url.Values{}
	query.Set("uploadType", "media")
	query.Set("name", objectName)
	endpoint := base + "b/" + url.PathEscape(bucket) + "/o?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("gcp auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
		return fmt.Errorf("GCP upload %s: status %d %s", objectName, resp.StatusCode, string(payload))
	}
	return nil
}

// doInto 将 HTTP 响应 body 写入 dest bytes.Buffer（无 8MB 限制场景用 listJSON）。
func (c *client) doInto(ctx context.Context, method, api, path string, query url.Values, body interface{}, dest *bytes.Buffer) error {
	base, ok := baseURLs[api]
	if !ok {
		return fmt.Errorf("unknown gcp api scope %q", api)
	}
	endpoint := base + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("gcp auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &apiErr)
		if apiErr.Error.Message != "" {
			return fmt.Errorf("GCP API %s %s: %d %s", method, path, resp.StatusCode, apiErr.Error.Message)
		}
		return fmt.Errorf("GCP API %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	_, err = dest.Write(payload)
	return err
}