package posthogcode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// errMissingToken 表示调用上游时没有可用的 access token。
var errMissingToken = errors.New("缺少 access token")

// UsageBucket 是网关返回的单个成本窗口状态。
type UsageBucket struct {
	UsedPercent float64 `json:"used_percent"`
	ResetsIn    int     `json:"resets_in_seconds"`
	ResetAt     string  `json:"reset_at"`
	Exceeded    bool    `json:"exceeded"`
}

// UsageCredits 是网关返回的额度桶状态。
// used_usd / limit_usd 为 null 表示该组织未启用按量计费（如免费层）。
type UsageCredits struct {
	Exhausted bool     `json:"exhausted"`
	UsedUSD   *float64 `json:"used_usd"`
	LimitUSD  *float64 `json:"limit_usd"`
}

// UsageInfo 是网关 /v1/usage/{product} 的响应子集。
type UsageInfo struct {
	Product             string       `json:"product"`
	UserID              int          `json:"user_id"`
	Burst               UsageBucket  `json:"burst"`
	Sustained           UsageBucket  `json:"sustained"`
	AICredits           UsageCredits `json:"ai_credits"`
	IsRateLimited       bool         `json:"is_rate_limited"`
	CodeUsageSubscribed bool         `json:"code_usage_subscribed"`
	BillingPeriodEnd    string       `json:"billing_period_end"`
}

// usageURL 构造指定账号的上游用量地址。注意：该端点不带产品路径段前缀，
// 产品名放在末段（与 /{product}/v1/* 的排布不同）。
func (s *Service) usageURL(acc Account) string {
	return gatewayBaseURL(s.accountRegion(acc)) + "/v1/usage/" + s.accountProduct()
}

// fetchUsage 拉取指定账号的上游用量与限额状态。
func (s *Service) fetchUsage(ctx context.Context, acc Account, accessToken string) (*UsageInfo, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errMissingToken
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.usageURL(acc), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &usageError{status: resp.StatusCode, msg: msg}
	}
	var out UsageInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// usageError 是上游用量接口的失败。
type usageError struct {
	status int
	msg    string
}

func (e *usageError) Error() string {
	if e.msg == "" {
		return "上游用量接口返回非 200"
	}
	return e.msg
}

// QuotaResource 是组织级额度资源的用量与上限（单位见资源自身）。
type QuotaResource struct {
	Limited bool     `json:"limited"`
	Usage   *float64 `json:"usage"`
	Limit   *float64 `json:"limit"`
}

// QuotaLimits 是 PostHog 组织级额度快照（/api/projects/{id}/quota_limits/）。
type QuotaLimits struct {
	// Credits 是 posthog_code_credits：token 费用与沙箱计算费之和，单位 credit。
	Credits QuotaResource `json:"-"`
	// CodeUsageBillingActive 表示该组织已为 Desktop 用量付费。
	CodeUsageBillingActive bool `json:"code_usage_billing_active"`
}

// quotaLimitsURL 构造指定账号所属项目的额度查询地址（Django 主站，非网关）。
// 额度按项目所属组织结算，故必须用账号自己的项目 ID 与区域。
func (s *Service) quotaLimitsURL(acc Account, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	return cloudBaseURL(s.accountRegion(acc)) + "/api/projects/" + projectID + "/quota_limits/"
}

// resolveProjectID 返回账号可用的项目 ID：优先账号自身记录，
// 缺失时用该账号的凭据查一次并回写（凭据无 project:read 时返回空串）。
func (s *Service) resolveProjectID(ctx context.Context, acc *Account, accessToken string) string {
	if id := strings.TrimSpace(acc.ProjectID); id != "" {
		return id
	}
	id := fetchDefaultProjectID(ctx, s.accountRegion(*acc), accessToken)
	if id == "" {
		return ""
	}
	acc.ProjectID = id
	// 回写失败不阻断本次查询，下次仍会重试。
	_ = s.upsertAccount(ctx, *acc)
	return id
}

// fetchQuotaLimits 查询指定账号项目的组织额度。
// 该端点要求 project:read；token 未携带该 scope 时返回错误，由调用方降级。
func (s *Service) fetchQuotaLimits(ctx context.Context, acc Account, projectID, accessToken string) (*QuotaLimits, error) {
	url := s.quotaLimitsURL(acc, projectID)
	if url == "" {
		return nil, errors.New("账号未记录项目 ID，无法查询组织额度")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, errMissingToken
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &usageError{status: resp.StatusCode, msg: msg}
	}

	var parsed struct {
		Limited                map[string]QuotaResource `json:"limited"`
		CodeUsageBillingActive bool                     `json:"code_usage_billing_active"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := &QuotaLimits{CodeUsageBillingActive: parsed.CodeUsageBillingActive}
	if r, ok := parsed.Limited["posthog_code_credits"]; ok {
		out.Credits = r
	}
	return out, nil
}

// AccountUsage 是单个账号的用量与额度快照。
type AccountUsage struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Project string `json:"projectId"`
	// ScopeReady 为假表示凭据缺 project:read，查不到额度。
	ScopeReady bool `json:"scopeReady"`
	// Usage 是网关返回的速率窗口状态；取不到时为 null。
	Usage *UsageInfo `json:"usage,omitempty"`
	// UsedCredits / LimitCredits 单位为 credit（1 credit = $0.01）。
	UsedCredits   *float64 `json:"usedCredits"`
	LimitCredits  *float64 `json:"limitCredits"`
	Limited       bool     `json:"limited"`
	BillingActive bool     `json:"billingActive"`
	// Error 是该账号查询失败的原因；成功时为空。
	Error string `json:"error,omitempty"`
}

// collectAccountUsage 并发采集各账号的用量与额度。
// 每个账号各自持有 token、区域、项目 ID 与额度桶，互不影响；单账号失败只标记该行。
// 并发化是因为每账号要打两次上游（usage + quota），串行在多账号时延迟线性增长。
func (s *Service) collectAccountUsage(ctx context.Context, accounts []Account) []AccountUsage {
	out := make([]AccountUsage, len(accounts))
	var wg sync.WaitGroup
	// 限制并发，避免账号很多时一次性打满上游。
	sem := make(chan struct{}, 4)
	for i := range accounts {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[idx] = s.collectOneAccountUsage(ctx, accounts[idx])
		}(i)
	}
	wg.Wait()
	return out
}

// collectOneAccountUsage 采集单个账号的用量与额度。
func (s *Service) collectOneAccountUsage(ctx context.Context, acc Account) AccountUsage {
	row := AccountUsage{
		ID:         acc.ID,
		Email:      acc.Email,
		Project:    acc.ProjectID,
		ScopeReady: hasProjectRead(acc.Scope),
	}
	if !accountAvailable(acc) {
		row.Error = "账号不可用（已停用或 token 已过期）"
		return row
	}
	token, err := s.ensureFreshToken(ctx, &acc)
	if err != nil {
		row.Error = err.Error()
		return row
	}
	// 速率窗口：网关按 token 归属用户统计。
	if u, err := s.fetchUsage(ctx, acc, token); err == nil {
		row.Usage = u
	}
	// 组织额度：必须用该账号自己的区域与项目 ID。
	projectID := s.resolveProjectID(ctx, &acc, token)
	row.Project = projectID
	if projectID == "" {
		row.Error = "账号未记录项目 ID（凭据可能缺少 project:read）"
		return row
	}
	quota, err := s.fetchQuotaLimits(ctx, acc, projectID, token)
	if err != nil {
		row.Error = err.Error()
		return row
	}
	row.UsedCredits = quota.Credits.Usage
	row.LimitCredits = quota.Credits.Limit
	row.Limited = quota.Credits.Limited
	row.BillingActive = quota.CodeUsageBillingActive
	// 刷新额度快照，供 least-used 策略选号。
	s.recordQuotaSnapshot(acc.ID, quota.Credits.Usage, quota.Credits.Limit)
	return row
}

// handleUsage 采集所有账号的用量与额度，并给出合计。
// 多账号时各账号额度独立（各自组织），合计仅作概览。
func (s *Service) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	all := s.Settings().Accounts
	if len(all) == 0 {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "没有账号，请先完成授权"})
		return
	}

	rows := s.collectAccountUsage(r.Context(), all)

	// 合计只累加成功取到额度的账号，避免把失败行当 0 计入。
	var usedSum, limitSum float64
	okCount := 0
	for _, row := range rows {
		if row.UsedCredits == nil || row.LimitCredits == nil {
			continue
		}
		usedSum += *row.UsedCredits
		limitSum += *row.LimitCredits
		okCount++
	}

	payload := map[string]interface{}{
		"success":      true,
		"accounts":     rows,
		"usdPerCredit": 0.01,
	}
	if okCount > 0 {
		payload["total"] = map[string]interface{}{
			"usedCredits":  usedSum,
			"limitCredits": limitSum,
			"accountCount": okCount,
		}
	}
	// 单账号时保留顶层字段，兼容最简展示路径。
	if len(rows) == 1 {
		payload["usage"] = rows[0].Usage
		payload["account"] = rows[0].Email
		if rows[0].Error == "" {
			payload["quota"] = map[string]interface{}{
				"usedCredits":   rows[0].UsedCredits,
				"limitCredits":  rows[0].LimitCredits,
				"limited":       rows[0].Limited,
				"billingActive": rows[0].BillingActive,
				"usdPerCredit":  0.01,
			}
		} else {
			payload["quotaError"] = rows[0].Error
			payload["scopeReady"] = rows[0].ScopeReady
		}
	}
	responseJSON(w, payload)
}
