package qoder

// 本文件是 Qoder 的「积分运营层」：余额查询与每日签到（领取每日 100 Credits）。
//
// 协议事实来自社区实测（qoder2api 系列反代与官方客户端抓包）：
//   - 余额：GET {openapi}/api/v2/quota/usage，Bearer device token，响应含
//     userQuota.{total,used,remaining,unit} 与 addOnQuota（赠送/签到积分进这里）；
//     isQuotaExceeded 判耗尽。
//   - 签到：GET {openapi}/sash/api/v1/me/campaigns 列出活动（找 actionType=CLAIM_BENEFIT
//     且 claimStatus=CLAIMABLE 的行）；POST {openapi}/sash/api/v1/me/campaigns/{id}/claim
//     领取（空 body）。仅需 Bearer token + cosy-clienttype:10，无需 COSY 签名。
//   - 幂等：已领取返回 CLAIMED / replayed=true，属成功而非失败。
//
// 区域差异（关键）：国内版（cn）活动列表**不需要真实机器身份**，多账号可各自领取；
// 国际版（intl）服务端按 Cosy-Machine* 真实身份过滤设备定向活动，缺身份时每日行
// 被静默隐藏。故 intl 的签到在未接入真实机器身份前可能只返回 VIEW_DETAILS 行。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// billingTimeout 是单次余额/签到类上游调用的超时。
const billingTimeout = 30 * time.Second

// Quota 是一个账号的积分额度快照（下发前端）。
type Quota struct {
	// Limit/Used/Remaining 是套餐额度 + 赠送额度（addOnQuota）的合计。
	Limit     float64 `json:"limit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
	Exhausted bool    `json:"exhausted"`
	UserType  string  `json:"userType,omitempty"`
	// PlanRemaining 是套餐额度剩余；AddOnRemaining 是赠送/签到额度剩余。
	PlanRemaining  float64 `json:"planRemaining,omitempty"`
	AddOnRemaining float64 `json:"addOnRemaining,omitempty"`
}

// quotaBucket 是一组额度（套餐或赠送）。
type quotaBucket struct {
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
}

// quotaUsageResponse 是 GET /api/v2/quota/usage 的响应。
type quotaUsageResponse struct {
	UserType      string      `json:"userType"`
	IsQuotaExceed bool        `json:"isQuotaExceeded"`
	LimitExceeded bool        `json:"limitExceeded"`
	ExpiresAt     int64       `json:"expiresAt"`
	UpgradeURL    string      `json:"upgradeUrl"`
	ModelUsage    []any       `json:"modelUsage"`
	UserQuota     quotaBucket `json:"userQuota"`
	// AddOnQuota 是赠送/签到额度（每日 100 Credits 进这里）。
	AddOnQuota quotaBucket `json:"addOnQuota"`
	// OrgResourcePackage 是企业资源包（部分账号有）。
	OrgResourcePackage quotaBucket `json:"orgResourcePackage"`
}

// campaign 是活动列表里的一项。
type campaign struct {
	CampaignID  string `json:"campaignId"`
	CampaignKey string `json:"campaignKey"`
	ActionType  string `json:"actionType"`
	ClaimStatus string `json:"claimStatus"`
	Benefit     *struct {
		Kind   string  `json:"kind"`
		Amount float64 `json:"amount"`
	} `json:"benefit"`
}

// campaignsResponse 是 GET /sash/api/v1/me/campaigns 的响应。
type campaignsResponse struct {
	ShowCampaign bool       `json:"showCampaign"`
	Claimable    bool       `json:"claimable"`
	Campaigns    []campaign `json:"campaigns"`
}

// CheckinResult 是一次签到尝试的结果。
type CheckinResult struct {
	// Status：success（本次领取成功）/ already（今日已领取，幂等）/ none（暂无可领取）/
	// failed（上游失败，error 非空）。
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Gained 是本次到账 Credits。
	Gained float64 `json:"gained,omitempty"`
}

// billingHeaders 施加 openapi 控制面（余额/活动/签到）的请求头。
// 官方桌面端用 cosy-clienttype:10，无需 COSY 签名。
func billingHeaders(acc *Account) func(*http.Request) {
	cfg := regionConfigFor(acc.Region)
	return func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("User-Agent", "Qoder")
		r.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		r.Header.Set("Cosy-ClientType", "10")
		r.Header.Set("Cosy-Version", "0.3.3")
		r.Header.Set("Origin", cfg.website)
		r.Header.Set("Referer", cfg.website+"/")
	}
}

// endpointQuota 返回区域余额接口 URL。
func (s *Service) endpointQuota(region string) string {
	return s.openAPIBaseFor(region) + "/api/v2/quota/usage"
}

// endpointCampaigns 返回区域活动列表接口 URL。
func (s *Service) endpointCampaigns(region string) string {
	return s.openAPIBaseFor(region) + "/sash/api/v1/me/campaigns"
}

// endpointCampaignClaim 返回区域领取接口 URL。
func (s *Service) endpointCampaignClaim(region, id string) string {
	return s.openAPIBaseFor(region) + "/sash/api/v1/me/campaigns/" + id + "/claim"
}

// doBillingJSON 发控制面请求并解 JSON。HTTP 非 2xx 时返回错误（含状态码）。
func (s *Service) doBillingJSON(ctx context.Context, acc *Account, method, fullURL string, body []byte, out any) error {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return err
	}
	billingHeaders(acc)(req)
	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return &upstreamError{
			status:     resp.StatusCode,
			retryable:  isRetryableHTTP(resp.StatusCode),
			authBroken: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
			msg:        fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 200)),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析上游响应失败: %w", err)
	}
	return nil
}

// FetchQuota 查询账号的积分额度（GET /api/v2/quota/usage）。
func (s *Service) FetchQuota(ctx context.Context, acc Account) (Quota, error) {
	ctx, cancel := context.WithTimeout(ctx, billingTimeout)
	defer cancel()
	var resp quotaUsageResponse
	if err := s.doBillingJSON(ctx, &acc, http.MethodGet, s.endpointQuota(acc.Region), nil, &resp); err != nil {
		return Quota{}, err
	}
	// 套餐额度 + 赠送额度 + 企业资源包合计；每日签到积分进 addOnQuota。
	q := Quota{
		Limit:          resp.UserQuota.Total + resp.AddOnQuota.Total + resp.OrgResourcePackage.Total,
		Used:           resp.UserQuota.Used + resp.AddOnQuota.Used + resp.OrgResourcePackage.Used,
		Remaining:      resp.UserQuota.Remaining + resp.AddOnQuota.Remaining + resp.OrgResourcePackage.Remaining,
		Unit:           strings.TrimSpace(resp.UserQuota.Unit),
		UserType:       strings.TrimSpace(resp.UserType),
		PlanRemaining:  resp.UserQuota.Remaining,
		AddOnRemaining: resp.AddOnQuota.Remaining,
	}
	if q.Unit == "" {
		q.Unit = strings.TrimSpace(resp.AddOnQuota.Unit)
	}
	if q.Unit == "" {
		q.Unit = "credits"
	}
	// 耗尽判定：以「剩余为 0 且总额大于 0」为准；isQuotaExceeded 仅在无套餐额度时
	// 才权威（免费账号 userQuota.total=0 且 isQuotaExceeded=true，但签到额度可用）。
	q.Exhausted = q.Limit > 0 && q.Remaining <= 0
	if q.Limit == 0 {
		q.Exhausted = resp.IsQuotaExceed || resp.LimitExceeded
	}
	return q, nil
}

// DailyCheckin 执行每日签到：列出活动 → 领取所有可领取的 CLAIM_BENEFIT。
// 已领取或无可用活动返回对应状态而非错误。
func (s *Service) DailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	ctx, cancel := context.WithTimeout(ctx, billingTimeout)
	defer cancel()

	var list campaignsResponse
	if err := s.doBillingJSON(ctx, &acc, http.MethodGet, s.endpointCampaigns(acc.Region), nil, &list); err != nil {
		return CheckinResult{}, err
	}

	targets := make([]campaign, 0)
	for _, c := range list.Campaigns {
		if strings.EqualFold(c.ActionType, "CLAIM_BENEFIT") && strings.EqualFold(c.ClaimStatus, "CLAIMABLE") {
			targets = append(targets, c)
		}
	}
	if len(targets) == 0 {
		// 若存在已领取的 CLAIM_BENEFIT，视为「今日已签到」。
		for _, c := range list.Campaigns {
			if strings.EqualFold(c.ActionType, "CLAIM_BENEFIT") && strings.EqualFold(c.ClaimStatus, "CLAIMED") {
				return CheckinResult{Status: "already", Message: "今天已签到"}, nil
			}
		}
		return CheckinResult{Status: "none", Message: "暂无可领取权益"}, nil
	}

	var gained float64
	claimed := 0
	for _, c := range targets {
		var res struct {
			Status   string `json:"status"`
			Replayed bool   `json:"replayed"`
			Benefit  *struct {
				Amount float64 `json:"amount"`
			} `json:"benefit"`
		}
		if err := s.doBillingJSON(ctx, &acc, http.MethodPost, s.endpointCampaignClaim(acc.Region, c.CampaignID), []byte("{}"), &res); err != nil {
			// 单条失败不阻断其它活动；若这是唯一目标则返回错误。
			if len(targets) == 1 {
				return CheckinResult{}, err
			}
			continue
		}
		claimed++
		if res.Benefit != nil {
			gained += res.Benefit.Amount
		}
	}
	if claimed == 0 {
		return CheckinResult{Status: "none", Message: "暂无可领取权益"}, nil
	}
	msg := "签到成功"
	if gained > 0 {
		msg = fmt.Sprintf("签到成功，+%g Credits", gained)
	}
	return CheckinResult{Status: "success", Message: msg, Gained: gained}, nil
}
