package qoder

// 本文件是 Qoder 的「积分运营层」：余额查询与每日签到（领取每日 100 Credits）。
//
// 协议事实来自社区实测（qoder2api 系列反代与官方客户端抓包）：
//   - 余额：GET {openapi}/api/v2/quota/usage，Bearer device token，响应含
//     userQuota.{total,used,remaining,unit}、addOnQuota（赠送/签到积分进这里）
//     与 dedicatedResourcePackages（活动奖励，如 SOTA 专属积分）。
//     isQuotaExceeded 判耗尽。
//   - 签到：GET {openapi}/sash/api/v1/me/campaigns 列出活动（找
//     actionType=CLAIM_BENEFIT 且 claimStatus=CLAIMABLE 的行）；
//     POST {openapi}/sash/api/v1/me/campaigns/{id}/claim 领取（空 body）。
//     仅需 Bearer token + cosy-clienttype:10，无需 COSY 签名。
//   - 幂等：已领取返回 CLAIMED / replayed=true，属成功而非失败。
//
// 区域差异（关键）：国内版（cn）活动列表**不需要真实机器身份**，且**不能**发
// 自造指纹（发了反而拿不到活动）；国际版（intl）服务端按 cosy 真实设备身份
// 过滤设备定向活动，缺真实指纹时每日行被静默隐藏（no_campaign）。国际版还
// 存在「懒下发」：官方客户端首次领取后，之后不带指纹也能查到活动。
//
// 连续签到统计：上游 legacy /sash/api/v1/me/daily-check-in/status 在国内已
// DISABLED（恒 0）、国际版 404，故本地按「活动窗口 startAt 日期」记账。

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
	// Packages 是上游下发的「专属资源包」（活动奖励，如 SOTA 专属积分），
	// 单独成桶：带独立过期时间与使用范围，不与 user/addon 混合。
	Packages []QuotaPackage `json:"packages,omitempty"`
	// ResetAt 是上游给出的额度重置时刻（Unix 秒，可能为 0）。
	ResetAt int64 `json:"resetAt,omitempty"`
}

// QuotaPackage 是一个专属资源包。
type QuotaPackage struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	Desc      string  `json:"description,omitempty"`
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit,omitempty"`
	ExpiresAt int64   `json:"expiresAt,omitempty"`
	Available bool    `json:"available,omitempty"`
	Status    string  `json:"status,omitempty"`
}

// quotaBucket 是一组额度（套餐或赠送）。
type quotaBucket struct {
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
}

// dedicatedPackageRaw 是上游 dedicatedResourcePackages 的原始条目。
type dedicatedPackageRaw struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Total       float64 `json:"total"`
	Used        float64 `json:"used"`
	Remaining   float64 `json:"remaining"`
	Unit        string  `json:"unit"`
	ExpiresAt   int64   `json:"expiresAt"`
	Available   bool    `json:"available"`
	Status      string  `json:"status"`
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
	// DedicatedResourcePackages 是活动奖励资源包（如 SOTA 专属积分）。
	DedicatedResourcePackages []dedicatedPackageRaw `json:"dedicatedResourcePackages"`
}

// campaign 是活动列表里的一项。
type campaign struct {
	CampaignID  string `json:"campaignId"`
	CampaignKey string `json:"campaignKey"`
	ActionType  string `json:"actionType"`
	ClaimStatus string `json:"claimStatus"`
	// StartAt/EndAt 是活动窗口（Unix 秒；窗口为每日 10:00 → 次日 09:59 UTC+8）。
	StartAt int64 `json:"startAt"`
	EndAt   int64 `json:"endAt"`
	Benefit *struct {
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
	// need_fingerprint（国际版缺真实指纹）/ failed（上游失败，error 非空）。
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Gained 是本次到账 Credits。
	Gained float64 `json:"gained,omitempty"`
	// WindowDate 是本次领取对应的活动窗口日期（YYYY-MM-DD，UTC+8）。
	WindowDate string `json:"windowDate,omitempty"`
	// StreakDays/TotalDays/TotalCredits 是本地签到历史统计。
	StreakDays   int     `json:"streakDays,omitempty"`
	TotalDays    int     `json:"totalDays,omitempty"`
	TotalCredits float64 `json:"totalCredits,omitempty"`
	// CampaignCount 是上游下发的活动条目数（0 = 未下发任何活动，重要风险信号）。
	CampaignCount int `json:"campaignCount"`
	// Fingerprint 表示本次签到是否携带了真实设备指纹。
	Fingerprint bool `json:"fingerprint"`
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
		r.Header.Set("Cosy-ClientType", newClientType)
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
	return s.doBillingJSONWithHeaders(ctx, acc, method, fullURL, body, nil, out)
}

// doBillingJSONWithHeaders 同上，但可附加额外请求头（签到用的设备指纹头）。
func (s *Service) doBillingJSONWithHeaders(ctx context.Context, acc *Account, method, fullURL string, body []byte, extra map[string]string, out any) error {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return err
	}
	billingHeaders(acc)(req)
	for k, v := range extra {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if method == http.MethodPost && len(body) == 0 {
		req.ContentLength = 0
	}
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

// checkinExtraHeaders 返回签到请求要附带的设备指纹头。
// 国内版默认**不带**任何机器头（实测发自造指纹反而拿不到活动）；
// 只有用户显式配置了真实指纹的账号才带（与 qoder2api 行为一致）。
// 国际版靠真实指纹下发活动，缺指纹时由调用方（DailyCheckin）先拦截。
func checkinExtraHeaders(acc *Account, ident deviceIdentity, fullDevice bool) map[string]string {
	h := map[string]string{}
	if !ident.Real {
		return h
	}
	applyHeaders(h, checkinHeaders(*acc, ident, fullDevice))
	// authorization / accept 等由 billingHeaders 统一设置，避免两处不一致。
	delete(h, "authorization")
	delete(h, "accept")
	delete(h, "accept-language")
	return h
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
		ResetAt:        normalizeUnixSeconds(resp.ExpiresAt),
	}
	if q.Unit == "" {
		q.Unit = strings.TrimSpace(resp.AddOnQuota.Unit)
	}
	if q.Unit == "" {
		q.Unit = "credits"
	}
	// 专属资源包单独成桶（不并入合计，避免把「限 Sonus 使用」的 SOTA 积分算成通用额度）。
	for _, p := range resp.DedicatedResourcePackages {
		q.Packages = append(q.Packages, QuotaPackage{
			ID:        p.ID,
			Name:      p.Name,
			Desc:      p.Description,
			Total:     p.Total,
			Used:      p.Used,
			Remaining: p.Remaining,
			Unit:      p.Unit,
			ExpiresAt: normalizeUnixSeconds(p.ExpiresAt),
			Available: p.Available,
			Status:    p.Status,
		})
	}
	// 耗尽判定：以「剩余为 0 且总额大于 0」为准；isQuotaExceeded 仅在无套餐额度时
	// 才权威（免费账号 userQuota.total=0 且 isQuotaExceeded=true，但签到额度可用）。
	q.Exhausted = q.Limit > 0 && q.Remaining <= 0
	if q.Limit == 0 {
		q.Exhausted = resp.IsQuotaExceed || resp.LimitExceeded
	}
	return q, nil
}

// normalizeUnixSeconds 把毫秒/秒时间戳归一到秒；非法值返回 0。
func normalizeUnixSeconds(v int64) int64 {
	if v <= 0 {
		return 0
	}
	if v > 1e12 {
		return v / 1000
	}
	return v
}

// DailyCheckin 执行每日签到：列出活动 → 领取所有可领取的 CLAIM_BENEFIT。
// 已领取或无可用活动返回对应状态而非错误。ident 是账号设备身份（国际版必需）。
//
// 判断顺序（实测校准，2026-11-15 用真实国际/国内账号验证）：
//  1. 先查 campaigns（**不要**因为「国际版无指纹」就直接返回，否则已领取的账号
//     会被误报成缺指纹——国际版存在「懒下发」，官方客户端领过一次后即使不带
//     指纹也能查到 CLAIMED 活动）；
//  2. 有可领取 → 领取；
//  3. 有 CLAIM_BENEFIT 但都已领取 → already（并补记窗口日期）；
//  4. 完全没有 CLAIM_BENEFIT：国际版缺指纹 → need_fingerprint（这才是真缺指纹），
//     其余 → none。
func (s *Service) DailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	ident := identityForAccount(acc)
	ctx, cancel := context.WithTimeout(ctx, billingTimeout)
	defer cancel()

	settings := s.Settings()
	extra := checkinExtraHeaders(&acc, ident, settings.checkinFullDeviceHeaders())
	res := CheckinResult{Fingerprint: ident.Real}

	var list campaignsResponse
	if err := s.doBillingJSONWithHeaders(ctx, &acc, http.MethodGet, s.endpointCampaigns(acc.Region), nil, extra, &list); err != nil {
		return CheckinResult{}, err
	}
	res.CampaignCount = len(list.Campaigns)

	targets, alreadyClaimed := pickClaimableCampaigns(list.Campaigns)
	if len(targets) == 0 {
		if alreadyClaimed {
			date := ""
			for _, c := range list.Campaigns {
				if strings.EqualFold(c.ActionType, "CLAIM_BENEFIT") {
					if d := campaignWindowDate(c); d != "" {
						date = d
					}
				}
			}
			res.Status = "already"
			res.WindowDate = date
			res.Message = campaignWindowHint(date, "今天已签到")
			// 补记窗口日期但不计积分：可能是官方客户端领的，金额未知，记为 0
			// （已有非 0 记录不会被覆盖）。这样连签天数不会因为「在别处领过」而断。
			if date != "" {
				s.recordCheckin(ctx, acc.ID, date, 0)
			}
			s.fillCheckinStats(ctx, acc.ID, &res)
			return res, nil
		}
		if checkinNeedsFingerprint(acc, ident) {
			// 国际版完全没有下发 CLAIM_BENEFIT，且账号没有真实指纹：
			// 这才是「缺指纹导致活动被隐藏」的典型形态。
			res.Status = "need_fingerprint"
			res.Message = "国际版签到活动按真实设备指纹下发，当前上游未下发可领取活动；" +
				"请在账号里配置 checkinFingerprint（官方客户端抓包/提取），" +
				"或先在官方客户端手动领取一次（之后会转为懒下发）"
			res.CampaignCount = len(list.Campaigns)
			return res, nil
		}
		res.Status = "none"
		res.Message = "暂无可领取权益"
		return res, nil
	}

	var gained float64
	claimed := 0
	windowDate := ""
	rejected := 0
	for _, c := range targets {
		var claim struct {
			GrantID  string `json:"grantId"`
			Status   string `json:"status"`
			Replayed bool   `json:"replayed"`
			Benefit  *struct {
				Amount float64 `json:"amount"`
			} `json:"benefit"`
		}
		if err := s.doBillingJSONWithHeaders(ctx, &acc, http.MethodPost, s.endpointCampaignClaim(acc.Region, c.CampaignID), nil, extra, &claim); err != nil {
			// 单条失败不阻断其它活动；若这是唯一目标则返回错误。
			if len(targets) == 1 {
				return CheckinResult{}, err
			}
			continue
		}
		if d := campaignWindowDate(c); d != "" && windowDate == "" {
			windowDate = d
		}
		if claim.Replayed {
			continue
		}
		status := strings.ToUpper(strings.TrimSpace(claim.Status))
		if status != "" && status != "CLAIMED" && status != "GRANTED" {
			// 未知/失败状态不计入成功，避免把上游拒绝误报成「签到成功」。
			rejected++
			continue
		}
		claimed++
		if claim.Benefit != nil {
			gained += claim.Benefit.Amount
		} else if c.Benefit != nil {
			gained += c.Benefit.Amount
		}
	}
	if claimed == 0 {
		if rejected > 0 {
			// 上游明确拒绝了本次领取（非 replayed、非 CLAIMED），不能报「已签到」。
			res.Status = "failed"
			res.Message = "上游拒绝了本次签到领取（状态非 CLAIMED）"
			return res, nil
		}
		res.Status = "already"
		res.Message = "今天已签到"
		if windowDate != "" {
			res.WindowDate = windowDate
			res.Message = campaignWindowHint(windowDate, "今天已签到")
			s.recordCheckin(ctx, acc.ID, windowDate, 0)
		}
		s.fillCheckinStats(ctx, acc.ID, &res)
		return res, nil
	}
	res.Status = "success"
	res.Gained = gained
	res.WindowDate = windowDate
	if gained > 0 {
		res.Message = fmt.Sprintf("签到成功，+%g Credits", gained)
	} else {
		res.Message = "签到成功"
	}
	// 记账：领取成功按窗口日期写入本地历史（同日幂等）。
	amount := gained
	if amount == 0 {
		amount = 100
	}
	streak, total, credits := s.recordCheckin(ctx, acc.ID, firstNonEmpty(windowDate, todayIn(cstZone)), amount)
	res.StreakDays, res.TotalDays, res.TotalCredits = streak, total, credits
	res.Message += streakSuffix(&res)
	return res, nil
}

// fillCheckinStats 把本地签到历史统计填入结果（只读，不记账）。
func (s *Service) fillCheckinStats(ctx context.Context, accountID string, res *CheckinResult) {
	streak, total, credits := s.localCheckinStats(ctx, accountID)
	res.StreakDays, res.TotalDays, res.TotalCredits = streak, total, credits
	if res.Status == "already" {
		res.Message += streakSuffix(res)
	}
}
