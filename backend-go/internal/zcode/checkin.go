package zcode

// 本文件实现 ZCode 的「每日签到」= 领取当天那份赠送套餐。
//
// ── 上游协议（抄自 ZCode 客户端的 manualClaimPlan）──────────
//
//	探测  GET  {zcode}/api/v1/zcode-plan/billing/preview?app_version=&platform=
//	领取  POST {zcode}/api/v1/zcode-plan/billing/claim   body {"plan_id": "..."}
//
// 「今天领过没」等价于「今天这个 plan_id 领过没」：上游每天下发一个**带日期段
// 的新套餐**（`zcode-v3-start-plan-<日期>`），同一期再领回 `1003 already claimed`。
//
// ── 头集合：极简 + 一处硬要求 ───────────────────────────────
// preview 与 claim 都只必要地带 `Authorization: Bearer {jwt}`（探测允许匿名），
// 但 **`X-Device-Mid` 是硬要求**：缺它、或值不是 UUID 形态，网关一律回
// `400 {"code":3001,"msg":"parameter error"}` —— 连匿名探测也一样。
// 设备标识还必须**跨请求稳定**（风控据此关联同一设备的多次请求），
// 因此它随凭证落盘、缺失时生成一次后长期复用。
//
// ── 领取通常要人机验证 ──────────────────────────────────────
// claim 通常要带 `X-Aliyun-Captcha-Verify-Param`（阿里云无痕验证）。参考实现
// 在进程内跑阿里云官方混淆 SDK 求解 —— 那是 Node/Bun 生态的产物（需要 DOM 桩、
// canvas/WebGL/Worker 垫片），不适合在 Go 后端复刻。
//
// 因此本实现的口径是：
//   - **探测永远做**（不需要验证码），据此判断今天有没有可领套餐；
//   - **领取尝试做**：不带验证码直接打；上游回验证码类错误时如实报
//     「需要人机验证」并把 plan_id 带回前端，用户在官网点一下即可。
//
// 这样既不会因为「拿不到验证码」而完全不探测（那样连「今天有没有套餐」都不知道），
// 也不会假装领取成功。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"

	"github.com/robfig/cron/v3"
)

// checkinCronSpec 是每日领取的触发时刻（站点时区）。
// 19:00 是 ZCode 官方公告的每日赠送额度开启时刻；20:05 作为兜底（按天幂等）。
const checkinCronSpec = "5 19,20 * * *"

// checkinAccountDelay 是账号间的限速间隔。
const checkinAccountDelay = 2 * time.Second

// zcodeAppVersion 是探测/领取时申报的客户端版本（上游按它决定下发哪一档）。
const zcodeAppVersion = "1.0.0"

// CheckinResult 是一次领取的结果。
type CheckinResult struct {
	// Status：success（本次领到）/ already（今日已领）/ skipped（无可领套餐）
	// / captcha（需要人机验证）/ failed（失败，Error 非空）。
	Status string `json:"status"`
	// Message 是人类可读的说明。
	Message string `json:"message,omitempty"`
	// PlanID 是本次涉及的套餐 id（captcha 时供前端提示去官网点领取）。
	PlanID string `json:"planId,omitempty"`
}

// CheckinAll 对所有「签到未停用」的账号执行一次领取。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	results := make([]map[string]interface{}, 0)
	first := true
	for _, a := range s.Settings().Accounts {
		if a.CheckinDisabled {
			continue
		}
		if !first {
			select {
			case <-ctx.Done():
				return results
			case <-time.After(checkinAccountDelay):
			}
		}
		first = false
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname, "region": normalizeRegion(a.Region)}
		result, err := s.dailyClaim(ctx, a)
		if err != nil {
			item["success"] = false
			item["error"] = err.Error()
			s.markAccountError(ctx, a.ID, err.Error())
			results = append(results, item)
			continue
		}
		item["success"] = true
		item["status"] = result.Status
		item["message"] = result.Message
		if result.PlanID != "" {
			item["planId"] = result.PlanID
		}
		acc := a
		acc.LastError = ""
		if result.Status == "success" || result.Status == "already" {
			acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
		}
		_ = s.upsertAccount(ctx, acc)
		results = append(results, item)
	}
	return results
}

// dailyClaim 探测当日套餐并尝试领取。
func (s *Service) dailyClaim(ctx context.Context, acc Account) (CheckinResult, error) {
	if strings.TrimSpace(acc.JWT) == "" {
		return CheckinResult{}, fmt.Errorf("账号缺少套餐 jwt，无法查询/领取（请在官网登录后复制 jwt）")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if strings.TrimSpace(acc.DeviceMid) == "" {
		acc.DeviceMid = newDeviceMid()
	}

	plans, err := s.previewPlans(ctx, acc)
	if err != nil {
		return CheckinResult{}, err
	}
	if len(plans) == 0 {
		return CheckinResult{Status: "skipped", Message: "今日没有可领取的套餐（活动未开启或已领完）"}, nil
	}
	// 按上游顺序取第一份未领取的套餐。
	target := plans[0]
	result, err := s.claimPlan(ctx, acc, target)
	if err != nil {
		return CheckinResult{}, err
	}
	return result, nil
}

// planInfo 是 preview 返回的一份可领套餐。
type planInfo struct {
	ID   string
	Name string
}

// previewPlans 探测当日可领取的套餐。
func (s *Service) previewPlans(ctx context.Context, acc Account) ([]planInfo, error) {
	query := url.Values{}
	query.Set("app_version", zcodeAppVersion)
	query.Set("platform", "web")
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodGet,
		zcodeOrigin+"/api/v1/zcode-plan/billing/preview?"+query.Encode(), nil, acc.JWT,
		map[string]string{
			"Accept":       "application/json",
			"X-Device-Mid": acc.DeviceMid,
		}, 20*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("探测套餐失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("探测套餐失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Plans []struct {
				ID     string `json:"plan_id"`
				Name   string `json:"plan_name"`
				Status string `json:"status"`
			} `json:"plans"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("探测套餐响应解析失败: %w", err)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return nil, fmt.Errorf("探测套餐失败: code=%d %s", *resp.Code, resp.Msg)
	}
	out := make([]planInfo, 0, len(resp.Data.Plans))
	for _, p := range resp.Data.Plans {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		// 已领取（CLAIMED）的跳过。
		if strings.EqualFold(strings.TrimSpace(p.Status), "CLAIMED") {
			continue
		}
		out = append(out, planInfo{ID: id, Name: strings.TrimSpace(p.Name)})
	}
	return out, nil
}

// claimPlan 领取一份套餐。
//
// 不带验证码直接打：上游回验证码类错误时返回 captcha 状态（不是失败）。
func (s *Service) claimPlan(ctx context.Context, acc Account, plan planInfo) (CheckinResult, error) {
	body, _ := json.Marshal(map[string]string{"plan_id": plan.ID})
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodPost,
		zcodeOrigin+"/api/v1/zcode-plan/billing/claim", body, acc.JWT,
		map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
			"X-Device-Mid": acc.DeviceMid,
		}, 30*time.Second, maxErrorBytes)
	if err != nil {
		return CheckinResult{}, fmt.Errorf("领取套餐失败: %w", err)
	}
	var resp struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(data, &resp)
	code := 0
	if resp.Code != nil {
		code = *resp.Code
	}
	msg := firstNonEmpty(resp.Msg, jsonMessage(data))
	switch {
	case status >= 200 && status < 300 && code == 0:
		return CheckinResult{Status: "success", Message: "领取成功：" + plan.Name, PlanID: plan.ID}, nil
	case code == 1003 || strings.Contains(strings.ToLower(msg), "already claimed"):
		return CheckinResult{Status: "already", Message: "今日已领取", PlanID: plan.ID}, nil
	case isCaptchaError(msg) || status == http.StatusPreconditionFailed:
		return CheckinResult{
			Status:  "captcha",
			Message: "领取需要人机验证，请在 ZCode 官网点一下领取（套餐 " + plan.ID + "）",
			PlanID:  plan.ID,
		}, nil
	default:
		return CheckinResult{}, fmt.Errorf("领取套餐失败（HTTP %d, code=%d）：%s", status, code, msg)
	}
}

// isCaptchaError 判断上游错误是否为「需要人机验证」。
func isCaptchaError(msg string) bool {
	m := strings.ToLower(msg)
	for _, needle := range []string{"captcha", "verify", "验证", "3001"} {
		if strings.Contains(m, needle) {
			return true
		}
	}
	return false
}

// handleCheckin 手动签到：POST /api/zcode/checkin
// body 可选 {"accountId": "..."}：给了只签该账号，没给签全部。
func (s *Service) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		AccountID string `json:"accountId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	if body.AccountID == "" {
		responseJSON(w, map[string]interface{}{"success": true, "results": s.CheckinAll(ctx)})
		return
	}
	acc, ok := s.findAccount(body.AccountID)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	result, err := s.dailyClaim(ctx, acc)
	if err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	if result.Status == "success" || result.Status == "already" {
		acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
	}
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{
		"success": true,
		"status":  result.Status,
		"message": result.Message,
		"planId":  result.PlanID,
	})
}

// StartCheckinScheduler 启动每日自动领取（站点时区 + TZ watcher）。
func (s *Service) StartCheckinScheduler(ctx context.Context) {
	s.checkinOnce.Do(func() {
		go func() {
			loc := s.siteLocation(ctx)
			cr := s.newCheckinCron(loc, ctx)
			cr.Start()
			defer cr.Stop()

			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					next := s.siteLocation(ctx)
					if next.String() == loc.String() {
						continue
					}
					loc = next
					cr.Stop()
					cr = s.newCheckinCron(loc, ctx)
					cr.Start()
				}
			}
		}()
	})
}

// newCheckinCron 按给定时区构造签到调度器。
func (s *Service) newCheckinCron(loc *time.Location, ctx context.Context) *cron.Cron {
	cr := cron.New(cron.WithLocation(loc))
	if _, err := cr.AddFunc(checkinCronSpec, func() {
		if !s.Settings().AutoCheckin {
			return
		}
		s.CheckinAll(ctx)
	}); err != nil {
		applog.Warn(ctx, "zcode", "add checkin job failed", "error", err.Error())
	}
	return cr
}
