package raccoon

// 本文件实现小浣熊的「一次性首登奖励」（新手任务）。
//
// ── 上游契约（实测 2026-11-09 + 参考实现 Agent2Providers 的 raccoon/onboarding.rs）──
//
//	POST /api/web/desktop/v1/login/points/grant   桌面端身份头
//	POST /api/web/mobile/v1/login/points/grant    手机端身份头（app-android / v1.0.3）
//	  → {"code":0,"data":{"granted":bool,"popup":{"points":3000,"expire_at":"..."}}}
//
// 两条端点**各领一次、互不共享额度**：桌面端领过的账号，手机端照样能拿满。
// 两端的 Bearer token 是同一把（JWT 无平台绑定）。
//
// ── 为什么「已领取」状态要自己记台账 ────────────────────────
// 上游 `granted:false` 同时表示「早就领过」与「本次没发」，没有独立的只读判定口；
// 账单里能查到历史入账（`桌面端登录奖励` / 移动端登录奖励），但那是**结果**不是
// **状态**，而且账单分页有上限。因此这里落一份本地台账（账号记录的
// `onboardingGrants`），一次探测无论 granted 真假都记账 —— 两种结果都代表
// 「这条任务已结算」，此后不再打上游。
//
// ── 与每日签到的关系 ────────────────────────────────────────
// 两者独立：签到是每天 300 积分（见 checkin.go），本文件是一次性的 3000×2。
// 签到成功后顺带跑一次首登探测，符合「加进来就自动领」的预期。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// onboardingTask 是一条一次性首登奖励。
type onboardingTask struct {
	// Key 是台账键（与参考实现同形，便于对账）。
	Key string `json:"key"`
	// Title 是人类可读名称。
	Title string `json:"title"`
	// Path 是上游端点路径。
	Path string `json:"path"`
	// Mobile 决定带哪套客户端身份头（不影响凭证）。
	Mobile bool `json:"mobile"`
	// NominalPoints 是名义分值（响应缺 popup.points 时的兜底）。
	NominalPoints int64 `json:"nominalPoints"`
}

// onboardingTasks 是任务表（数组顺序即展示顺序）。
var onboardingTasks = []onboardingTask{
	{
		Key:           "desktop_login_grant",
		Title:         "首次电脑端登录奖励",
		Path:          "/api/web/desktop/v1/login/points/grant",
		NominalPoints: 3000,
	},
	{
		Key:           "mobile_login_grant",
		Title:         "首次手机端登录奖励",
		Path:          "/api/web/mobile/v1/login/points/grant",
		Mobile:        true,
		NominalPoints: 3000,
	},
}

// OnboardingResult 是一条任务的领取结果。
type OnboardingResult struct {
	Key string `json:"key"`
	// Title 是人类可读名称。
	Title string `json:"title"`
	// Granted 表示本次真的发放了（false 表示已领过或未到发放条件）。
	Granted bool `json:"granted"`
	// Points 是本次到账积分（Granted 为 false 时为 0）。
	Points int64 `json:"points"`
	// Already 表示此前已结算（未打上游）。
	Already bool `json:"already"`
	// Error 是本条任务的失败原因（不影响其它任务）。
	Error string `json:"error,omitempty"`
}

// OnboardingStatus 是一个账号的首登奖励状态。
type OnboardingStatus struct {
	AccountID string             `json:"accountId"`
	Tasks     []OnboardingResult `json:"tasks"`
	// Earned 是台账里已结算任务的名义分值合计。
	Earned int64 `json:"earned"`
	// Total 是全部任务的名义分值合计。
	Total int64 `json:"total"`
	// Settled 表示两条都已结算（此后不会再打上游）。
	Settled bool `json:"settled"`
}

// onboardingTotal 返回全部任务的名义分值合计。
func onboardingTotal() int64 {
	var total int64
	for _, t := range onboardingTasks {
		total += t.NominalPoints
	}
	return total
}

// onboardingSettled 判断某任务是否已结算。
func onboardingSettled(acc Account, key string) bool {
	return strings.TrimSpace(acc.OnboardingGrants[key]) != ""
}

// onboardingStatusOf 依据台账组装状态（零上游请求）。
func onboardingStatusOf(acc Account) OnboardingStatus {
	status := OnboardingStatus{AccountID: acc.ID, Total: onboardingTotal()}
	settled := 0
	for _, task := range onboardingTasks {
		row := OnboardingResult{Key: task.Key, Title: task.Title}
		if onboardingSettled(acc, task.Key) {
			row.Already = true
			status.Earned += task.NominalPoints
			settled++
		}
		status.Tasks = append(status.Tasks, row)
	}
	status.Settled = settled == len(onboardingTasks)
	return status
}

// grantTask 调一次发放探测，返回 (granted, points, error)。
//
// 上游按天、按账号幂等：已领过返回 `granted:false`（不是错误）。
func (s *Service) grantTask(ctx context.Context, acc Account, task onboardingTask) (bool, int64, error) {
	headers := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if task.Mobile {
		// 手机端身份：平台值带 app- 前缀，版本头带 v 前缀（依据官方安卓包实测）。
		headers["X-Client-Platform"] = "app-android"
		headers["X-Client-Version"] = "v1.0.3"
	} else {
		headers["X-Client-Platform"] = "desktop"
	}
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodPost,
		baseOrigin+task.Path, []byte("{}"), acc.AccessToken,
		headers, 25*time.Second, maxErrorBytes)
	if err != nil {
		return false, 0, fmt.Errorf("领取失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return false, 0, fmt.Errorf("领取失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Granted *bool `json:"granted"`
			Popup   *struct {
				Points int64 `json:"points"`
			} `json:"popup"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return false, 0, fmt.Errorf("领取响应解析失败: %w", err)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return false, 0, fmt.Errorf("领取失败: code=%d %s", *resp.Code, resp.Message)
	}
	if resp.Data.Granted == nil {
		// 缺 granted 无法判定，按失败处理（不记账，下次可重试）。
		return false, 0, fmt.Errorf("领取响应缺少 granted，尚未确认")
	}
	granted := *resp.Data.Granted
	if !granted {
		return false, 0, nil
	}
	points := task.NominalPoints
	if resp.Data.Popup != nil && resp.Data.Popup.Points > 0 {
		points = resp.Data.Popup.Points
	}
	return true, points, nil
}

// claimOnboarding 领取指定账号（或全部）的一次性首登奖励。
//
// alreadySettled 的任务不发请求；探测成功后记台账（无论 granted 真假）。
// 单条失败只影响该条，不中断其它任务。
func (s *Service) claimOnboarding(ctx context.Context, accountID string) []OnboardingStatus {
	st := s.Settings()
	out := make([]OnboardingStatus, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		if accountID != "" && a.ID != accountID {
			continue
		}
		if a.CheckinDisabled {
			out = append(out, onboardingStatusOf(a))
			continue
		}
		acc := a
		if err := s.ensureToken(ctx, &acc); err != nil {
			status := onboardingStatusOf(acc)
			for i := range status.Tasks {
				status.Tasks[i].Error = err.Error()
			}
			out = append(out, status)
			continue
		}
		status := OnboardingStatus{AccountID: acc.ID, Total: onboardingTotal()}
		settledCount := 0
		for _, task := range onboardingTasks {
			row := OnboardingResult{Key: task.Key, Title: task.Title}
			if onboardingSettled(acc, task.Key) {
				row.Already = true
				status.Earned += task.NominalPoints
				settledCount++
				status.Tasks = append(status.Tasks, row)
				continue
			}
			granted, points, err := s.grantTask(ctx, acc, task)
			if err != nil {
				row.Error = err.Error()
				status.Tasks = append(status.Tasks, row)
				continue
			}
			// 探测成功（无论 granted 真假）都记台账：这一步就是「已结算」的写入点。
			if acc.OnboardingGrants == nil {
				acc.OnboardingGrants = map[string]string{}
			}
			acc.OnboardingGrants[task.Key] = time.Now().UTC().Format(time.RFC3339)
			settledCount++
			row.Granted = granted
			if granted {
				row.Points = points
				status.Earned += task.NominalPoints
			}
			status.Tasks = append(status.Tasks, row)
		}
		status.Settled = settledCount == len(onboardingTasks)
		acc.LastOnboardingAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.upsertAccount(ctx, acc); err != nil {
			applog.Warn(ctx, "raccoon", "persist onboarding ledger failed", "account", acc.ID, "error", err.Error())
		}
		out = append(out, status)
	}
	return out
}

// handleOnboarding 处理首登奖励：GET 查状态（零上游），POST 领取。
func (s *Service) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("accountId"))
		st := s.Settings()
		out := make([]OnboardingStatus, 0, len(st.Accounts))
		for _, a := range st.Accounts {
			if id != "" && a.ID != id {
				continue
			}
			out = append(out, onboardingStatusOf(a))
		}
		responseJSON(w, map[string]interface{}{"success": true, "accounts": out})
	case http.MethodPost:
		var body struct {
			AccountID string `json:"accountId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		results := s.claimOnboarding(ctx, strings.TrimSpace(body.AccountID))
		var gained int64
		for _, st := range results {
			for _, task := range st.Tasks {
				gained += task.Points
			}
		}
		responseJSON(w, map[string]interface{}{
			"success":  true,
			"gained":   gained,
			"accounts": results,
		})
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// claimOnboardingSettled 在签到之后试探一次首登奖励。
//
// 只对「还有未结算任务」的账号发请求（已结算的零上游）；失败只记 warn，
// 不影响签到结果。
func (s *Service) claimOnboardingSettled(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if a.CheckinDisabled {
			continue
		}
		if onboardingStatusOf(a).Settled {
			continue
		}
		for _, status := range s.claimOnboarding(ctx, a.ID) {
			for _, task := range status.Tasks {
				switch {
				case task.Error != "":
					applog.Warn(ctx, "raccoon", "onboarding grant failed",
						"account", status.AccountID, "task", task.Key, "error", task.Error)
				case task.Granted:
					applog.Info(ctx, "raccoon", "onboarding grant succeeded",
						"account", status.AccountID, "task", task.Key, "points", task.Points)
				}
			}
		}
	}
}
