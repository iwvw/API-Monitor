package autoclaw

// 本文件实现 AutoClaw 的每日签到。
//
// ── 上游是什么（无参考项目可抄，从客户端产物里挖出来的）───────
// 桌面端首页侧栏有一个「每日签到」Banner，点击后走通用任务接口：
//
//	POST {userapi}/autoclaw-proxy/proxy/autoclaw-task-complete
//	body: {"task_id":"daily_signin"}
//	→ {"data":{"already_completed":true,"reward_points":0,"success":false,
//	           "task_id":"daily_signin"}}
//
// 三个要点：
//  1. **不是签到专用接口** —— 这是「完成客户端任务」的通用入口，`daily_signin`
//     只是其中一个 task_id（同级的还有 `daily_inspiration_center` / `upgrade_pc_app`）。
//     所以请求体只有一个字段。
//  2. **HTTP 200 + body 里的成功标记**：当天已签到时返回 200、`success:false`、
//     `already_completed:true`、`reward_points:0`。判「签到成功」要看
//     `reward_points > 0`（或 `success === true`），**不能只看 HTTP 状态码**。
//  3. 上游按天幂等：重复调用不会重复入账。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"

	"github.com/robfig/cron/v3"
)

// checkinCronSpec 是每日签到的触发时刻（站点时区）。
const checkinCronSpec = "5 9,21 * * *"

// checkinAccountDelay 是账号间的限速间隔。
const checkinAccountDelay = 2 * time.Second

// taskCompletePath 是「完成客户端任务」的通用入口。
const taskCompletePath = "/autoclaw-proxy/proxy/autoclaw-task-complete"

// dailySigninTaskID 是每日签到对应的 task_id。
const dailySigninTaskID = "daily_signin"

// CheckinResult 是一次签到的结果。
type CheckinResult struct {
	// Status：success（本次领到）/ already（今天已签）/ skipped（无签到体系）
	// / failed（失败，Error 非空）。
	Status string `json:"status"`
	// Message 是人类可读的说明。
	Message string `json:"message,omitempty"`
	// Gained 是本次到账积分。
	Gained int64 `json:"gained,omitempty"`
}

// checkinSupported 报告账号是否支持每日签到。
// AutoClaw 两地都有（同一套账号体系接口）。
func checkinSupported(Account) bool { return true }

// CheckinAll 对所有「签到未停用」的账号执行一次签到。
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
		result, err := s.dailyCheckin(ctx, a)
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
		if result.Gained > 0 {
			item["gained"] = result.Gained
		}
		acc := a
		acc.LastError = ""
		acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.upsertAccount(ctx, acc)
		results = append(results, item)
	}
	return results
}

// dailyCheckin 执行一次签到。
func (s *Service) dailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return CheckinResult{}, fmt.Errorf("账号缺少 access token")
	}
	if !checkinSupported(acc) {
		return CheckinResult{Status: "skipped", Message: regionLabel(acc.Region) + "无签到体系"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"task_id": dailySigninTaskID})
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodPost,
		userAPIBaseFor(acc.Region)+taskCompletePath, body, signedHeaders(acc),
		20*time.Second, maxErrorB)
	if err != nil {
		return CheckinResult{}, fmt.Errorf("签到请求失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return CheckinResult{}, fmt.Errorf("签到失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AlreadyCompleted bool  `json:"already_completed"`
			RewardPoints     int64 `json:"reward_points"`
			Success          bool  `json:"success"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return CheckinResult{}, fmt.Errorf("签到响应解析失败: %w", err)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return CheckinResult{}, fmt.Errorf("签到失败: code=%d %s", *resp.Code, resp.Msg)
	}
	// 判成功看 body 里的标记，不能只看 HTTP 状态码。
	switch {
	case resp.Data.RewardPoints > 0:
		return CheckinResult{
			Status:  "success",
			Message: fmt.Sprintf("签到成功，+%d 积分", resp.Data.RewardPoints),
			Gained:  resp.Data.RewardPoints,
		}, nil
	case resp.Data.Success:
		return CheckinResult{Status: "success", Message: "签到成功"}, nil
	case resp.Data.AlreadyCompleted:
		return CheckinResult{Status: "already", Message: "今天已签到"}, nil
	default:
		return CheckinResult{Status: "skipped", Message: "今日签到未产生奖励（活动可能未开启）"}, nil
	}
}

// handleCheckin 手动签到：POST /api/autoclaw/checkin
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
	result, err := s.dailyCheckin(ctx, acc)
	if err != nil {
		s.markAccountError(ctx, acc.ID, err.Error())
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	acc.LastError = ""
	acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.upsertAccount(ctx, acc)
	responseJSON(w, map[string]interface{}{
		"success": true,
		"status":  result.Status,
		"message": result.Message,
		"gained":  result.Gained,
	})
}

// StartCheckinScheduler 启动每日自动签到（站点时区 + TZ watcher）。
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
		applog.Warn(ctx, "autoclaw", "add checkin job failed", "error", err.Error())
	}
	return cr
}
