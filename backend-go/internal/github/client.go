package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	githubAPIVersion     = "2022-11-28"
	// maxGitHubBodyBytes 是 GitHub API 单次响应体上限（4MB）。
	// GitHub 常规端点（workflow runs/jobs 等）正常响应远小于此值；
	// 超限响应大概率异常（超大内容文件解析、响应异常），直接拒绝，
	// 避免瞬时大分配抬高 GC 峰值水位（小内存主机收益最大）。
	maxGitHubBodyBytes = 4 << 20
)

type apiClient struct {
	baseURL string
	client  *http.Client
}

func newAPIClient() *apiClient {
	return &apiClient{
		baseURL: defaultGitHubAPIBase,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *apiClient) get(ctx context.Context, token, path string, target interface{}) (rateLimitInfo, error) {
	return c.do(ctx, token, http.MethodGet, path, nil, target)
}

func (c *apiClient) post(ctx context.Context, token, path string, payload interface{}, target interface{}) (rateLimitInfo, error) {
	return c.do(ctx, token, http.MethodPost, path, payload, target)
}

func (c *apiClient) patch(ctx context.Context, token, path string, payload interface{}, target interface{}) (rateLimitInfo, error) {
	return c.do(ctx, token, http.MethodPatch, path, payload, target)
}

func (c *apiClient) do(ctx context.Context, token, method, path string, payload interface{}, target interface{}) (rateLimitInfo, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return rateLimitInfo{}, err
		}
		body = bytes.NewReader(raw)
	}
	endpoint := path
	if strings.HasPrefix(path, "/") {
		endpoint = c.baseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return rateLimitInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "API-Monitor-GitHub-Module")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return rateLimitInfo{}, err
	}
	defer res.Body.Close()
	rate := parseRateLimit(res.Header)
	if res.StatusCode == http.StatusNoContent {
		return rate, nil
	}
	// 多读 1 字节用于检测是否超限：读满上限+1 说明响应被截断，返回明确
	// 错误而不是静默交给 json.Unmarshal 碰运气（截断 JSON 解不出合理错误）。
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxGitHubBodyBytes+1))
	if err != nil {
		return rate, err
	}
	if len(raw) > maxGitHubBodyBytes {
		return rate, fmt.Errorf("GitHub API %s %s: response body exceeds %dMB limit", method, path, maxGitHubBodyBytes>>20)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var ghErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &ghErr) == nil && ghErr.Message != "" {
			msg = ghErr.Message
		}
		if msg == "" {
			msg = res.Status
		}
		return rate, fmt.Errorf("GitHub API %s %s: %s", method, path, msg)
	}
	if target == nil || len(raw) == 0 {
		return rate, nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return rate, err
	}
	return rate, nil
}

func parseRateLimit(header http.Header) rateLimitInfo {
	limit, _ := strconv.Atoi(header.Get("X-RateLimit-Limit"))
	remaining, _ := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	resetUnix, _ := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	var reset time.Time
	if resetUnix > 0 {
		reset = time.Unix(resetUnix, 0).UTC()
	}
	return rateLimitInfo{
		Limit:          limit,
		Remaining:      remaining,
		Reset:          reset,
		OAuthScopes:    header.Get("X-OAuth-Scopes"),
		AcceptedScopes: header.Get("X-Accepted-OAuth-Scopes"),
	}
}

func (c *apiClient) fetchRepository(ctx context.Context, token, owner, repo string) (githubRepoResponse, rateLimitInfo, error) {
	var result githubRepoResponse
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo)), &result)
	return result, rate, err
}

func (c *apiClient) fetchPullCount(ctx context.Context, token, owner, repo string) (int, rateLimitInfo, error) {
	var result struct {
		TotalCount int `json:"total_count"`
	}
	q := url.QueryEscape(fmt.Sprintf("repo:%s/%s is:pr is:open", owner, repo))
	rate, err := c.get(ctx, token, "/search/issues?q="+q+"&per_page=1", &result)
	return result.TotalCount, rate, err
}

func (c *apiClient) fetchWorkflowRuns(ctx context.Context, token, owner, repo string, limit int) (workflowRunResponse, rateLimitInfo, error) {
	var result workflowRunResponse
	if limit <= 0 {
		limit = 30
	}
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=%d", url.PathEscape(owner), url.PathEscape(repo), clamp(limit, 1, 100)), &result)
	return result, rate, err
}

func (c *apiClient) fetchWorkflowJobs(ctx context.Context, token, owner, repo string, runID int64) (workflowJobResponse, rateLimitInfo, error) {
	var result workflowJobResponse
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?per_page=100", url.PathEscape(owner), url.PathEscape(repo), runID), &result)
	return result, rate, err
}

func (c *apiClient) fetchWorkflows(ctx context.Context, token, owner, repo string) (workflowListResponse, rateLimitInfo, error) {
	var result workflowListResponse
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/workflows?per_page=100", url.PathEscape(owner), url.PathEscape(repo)), &result)
	return result, rate, err
}

func (c *apiClient) fetchWorkflowFile(ctx context.Context, token, owner, repo, path, ref string) (string, workflowFileContentResponse, rateLimitInfo, error) {
	var result workflowFileContentResponse
	escapedPath := escapePathSegments(path)
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", url.PathEscape(owner), url.PathEscape(repo), escapedPath)
	if strings.TrimSpace(ref) != "" {
		apiPath += "?ref=" + url.QueryEscape(strings.TrimSpace(ref))
	}
	rate, err := c.get(ctx, token, apiPath, &result)
	if err != nil {
		return "", result, rate, err
	}
	content := strings.ReplaceAll(result.Content, "\n", "")
	if strings.EqualFold(result.Encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", result, rate, err
		}
		return string(decoded), result, rate, nil
	}
	return result.Content, result, rate, nil
}

func (c *apiClient) fetchBranches(ctx context.Context, token, owner, repo string) ([]branchResponse, rateLimitInfo, error) {
	var result []branchResponse
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/branches?per_page=100", url.PathEscape(owner), url.PathEscape(repo)), &result)
	return result, rate, err
}

func (c *apiClient) fetchLatestRelease(ctx context.Context, token, owner, repo string) (map[string]interface{}, rateLimitInfo, error) {
	var result map[string]interface{}
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/releases/latest", url.PathEscape(owner), url.PathEscape(repo)), &result)
	if err != nil && strings.Contains(err.Error(), "Not Found") {
		return map[string]interface{}{}, rate, nil
	}
	return result, rate, err
}

func (c *apiClient) fetchCommits(ctx context.Context, token, owner, repo string, since time.Time) ([]map[string]interface{}, rateLimitInfo, error) {
	var result []map[string]interface{}
	path := fmt.Sprintf("/repos/%s/%s/commits?per_page=100", url.PathEscape(owner), url.PathEscape(repo))
	if !since.IsZero() {
		path += "&since=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
	}
	rate, err := c.get(ctx, token, path, &result)
	return result, rate, err
}

func (c *apiClient) fetchContributors(ctx context.Context, token, owner, repo string) ([]map[string]interface{}, rateLimitInfo, error) {
	var result []map[string]interface{}
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/contributors?per_page=100", url.PathEscape(owner), url.PathEscape(repo)), &result)
	return result, rate, err
}

func (c *apiClient) fetchTraffic(ctx context.Context, token, owner, repo string) (map[string]int, rateLimitInfo, error) {
	output := map[string]int{}
	var views struct {
		Count   int `json:"count"`
		Uniques int `json:"uniques"`
	}
	rate, err := c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/traffic/views", url.PathEscape(owner), url.PathEscape(repo)), &views)
	if err != nil {
		return output, rate, err
	}
	output["views"] = views.Count
	output["view_uniques"] = views.Uniques
	var clones struct {
		Count   int `json:"count"`
		Uniques int `json:"uniques"`
	}
	rate, err = c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/traffic/clones", url.PathEscape(owner), url.PathEscape(repo)), &clones)
	if err != nil {
		return output, rate, err
	}
	output["clones"] = clones.Count
	output["clone_uniques"] = clones.Uniques
	return output, rate, nil
}

func (c *apiClient) testToken(ctx context.Context, token string, repo *Repository) (map[string]interface{}, rateLimitInfo, error) {
	var user map[string]interface{}
	rate, err := c.get(ctx, token, "/user", &user)
	if err != nil {
		return nil, rate, err
	}
	var limits map[string]interface{}
	rate, err = c.get(ctx, token, "/rate_limit", &limits)
	if err != nil {
		return nil, rate, err
	}
	permissions := inferPermissions(user, rate)
	if repo != nil {
		permissions["repository"] = repo.FullName
		permissions["checks"] = c.probeRepositoryPermissions(ctx, token, *repo)
	}
	return map[string]interface{}{"user": user, "rate_limit": limits, "permissions": permissions}, rate, nil
}

func (c *apiClient) probeRepositoryPermissions(ctx context.Context, token string, repo Repository) []map[string]interface{} {
	checks := []map[string]interface{}{}
	addCheck := func(key, label, level string, err error) {
		status := "success"
		message := "可用"
		if err != nil {
			status = "failed"
			message = err.Error()
		}
		checks = append(checks, map[string]interface{}{
			"key":     key,
			"label":   label,
			"level":   level,
			"status":  status,
			"message": message,
		})
	}

	_, _, err := c.fetchRepository(ctx, token, repo.Owner, repo.Name)
	addCheck("metadata", "仓库元数据", "Metadata: read", err)

	_, _, err = c.fetchWorkflowRuns(ctx, token, repo.Owner, repo.Name, 1)
	addCheck("actions_read", "Actions 读取", "Actions: read", err)

	_, _, err = c.fetchTraffic(ctx, token, repo.Owner, repo.Name)
	addCheck("administration_read", "流量统计", "Administration: read", err)

	var hooks []webhookResponse
	_, err = c.get(ctx, token, fmt.Sprintf("/repos/%s/%s/hooks?per_page=1", url.PathEscape(repo.Owner), url.PathEscape(repo.Name)), &hooks)
	addCheck("webhooks_read", "Webhook 读取", "Webhooks: read", err)

	checks = append(checks, map[string]interface{}{
		"key":     "actions_write",
		"label":   "Actions 操作",
		"level":   "Actions: write",
		"status":  "skipped",
		"message": "为避免触发工作流，写权限不自动探测；请按权限清单配置。",
	})
	checks = append(checks, map[string]interface{}{
		"key":     "webhooks_write",
		"label":   "Webhook 配置",
		"level":   "Webhooks: write",
		"status":  "skipped",
		"message": "写权限不执行破坏性探测；使用“自动配置”进行验证。",
	})
	return checks
}

func (c *apiClient) rerunWorkflowRun(ctx context.Context, token, owner, repo string, runID int64, failedOnly bool) (rateLimitInfo, error) {
	suffix := "rerun"
	if failedOnly {
		suffix = "rerun-failed-jobs"
	}
	rate, err := c.post(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/%s", url.PathEscape(owner), url.PathEscape(repo), runID, suffix), map[string]interface{}{}, nil)
	return rate, err
}

func (c *apiClient) cancelWorkflowRun(ctx context.Context, token, owner, repo string, runID int64) (rateLimitInfo, error) {
	rate, err := c.post(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/cancel", url.PathEscape(owner), url.PathEscape(repo), runID), map[string]interface{}{}, nil)
	return rate, err
}

func (c *apiClient) dispatchWorkflow(ctx context.Context, token, owner, repo, workflowID, ref string, inputs map[string]interface{}) (rateLimitInfo, error) {
	if strings.TrimSpace(ref) == "" {
		return rateLimitInfo{}, errors.New("ref is required")
	}
	body := map[string]interface{}{"ref": ref}
	if len(inputs) > 0 {
		body["inputs"] = inputs
	}
	rate, err := c.post(ctx, token, fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowID)), body, nil)
	return rate, err
}

func (c *apiClient) configureWebhook(ctx context.Context, token, owner, repo, payloadURL, secret string) (int64, bool, rateLimitInfo, error) {
	path := fmt.Sprintf("/repos/%s/%s/hooks", url.PathEscape(owner), url.PathEscape(repo))
	var hooks []webhookResponse
	rate, err := c.get(ctx, token, path+"?per_page=100", &hooks)
	if err != nil {
		return 0, false, rate, err
	}
	payload := map[string]interface{}{
		"name":   "web",
		"active": true,
		"events": []string{"workflow_run", "workflow_job", "release", "issues", "pull_request", "star"},
		"config": map[string]interface{}{
			"url":          payloadURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	}
	for _, hook := range hooks {
		if hook.Config.URL != payloadURL {
			continue
		}
		_, err = c.patch(ctx, token, fmt.Sprintf("%s/%d", path, hook.ID), payload, nil)
		return hook.ID, false, rate, err
	}
	var created webhookResponse
	rate, err = c.post(ctx, token, path, payload, &created)
	return created.ID, true, rate, err
}

func inferPermissions(user map[string]interface{}, rate rateLimitInfo) map[string]interface{} {
	return map[string]interface{}{
		"metadata":             true,
		"private_repositories": user["plan"] != nil,
		"actions":              "unknown",
		"traffic":              "requires_push_access",
		"scopes":               splitScopes(rate.OAuthScopes),
		"accepted_scopes":      splitScopes(rate.AcceptedScopes),
	}
}

func splitScopes(raw string) []string {
	parts := []string{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return parts
}
