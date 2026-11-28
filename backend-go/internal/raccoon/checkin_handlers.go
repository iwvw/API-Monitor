package raccoon

// 本文件实现小浣熊签到的调度与 HTTP 入口：每日自动签到（含失败重试）、
// 单账号/批量手动签到，以及设置页的开关。

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"

	"github.com/robfig/cron/v3"
)

// checkinCronSpec 是每日签到的触发时刻（站点时区）。
// 与同类插件一致取 9 点与 21 点各一次：9 点是当日额度刷新后的常规时点，
// 21 点作为漏签兜底（上游按天幂等，重复触发无副作用）。
const checkinCronSpec = "5 9,21 * * *"

// checkinAccountDelay 是账号间的限速间隔，避免同秒集中打上游。
const checkinAccountDelay = 2 * time.Second

// CheckinAll 对所有「签到未停用」的账号执行一次签到。
// 转发停用（Disabled）的账号同样参与 —— 签到开关独立于转发开关。
//
// 签到之后顺带跑一次首登奖励探测（一次性 3000×2，已结算的不再打上游）。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	results := s.checkinAll(ctx)
	s.claimOnboardingSettled(ctx)
	return results
}

// checkinAll 是签到本体（不含首登奖励）。
func (s *Service) checkinAll(ctx context.Context) []map[string]interface{} {
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
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		result, err := s.dailyCheckin(ctx, a)
		if err != nil {
			item["success"] = false
			item["error"] = err.Error()
			acc := a
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
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

// StartCheckinScheduler 启动每日自动签到（站点时区 + TZ watcher）。
// ctx 取消时退出。
func (s *Service) StartCheckinScheduler(ctx context.Context) {
	s.checkinOnce.Do(func() {
		go func() {
			loc := s.siteLocation(ctx)
			cr := s.newCheckinCron(loc, ctx)
			cr.Start()
			defer cr.Stop()

			// TZ watcher：站点时区变更后重建调度器（CONTEXT.md 硬性规则）。
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

// newCheckinCron 按给定时区构造签到调度器（未成功注册任务时返回空调度器）。
func (s *Service) newCheckinCron(loc *time.Location, ctx context.Context) *cron.Cron {
	cr := cron.New(cron.WithLocation(loc))
	if _, err := cr.AddFunc(checkinCronSpec, func() {
		if !s.Settings().AutoCheckin {
			return
		}
		s.CheckinAll(ctx)
	}); err != nil {
		applog.Warn(ctx, "raccoon", "add checkin job failed", "error", err.Error())
	}
	return cr
}

// handleCheckin 手动签到：POST /api/raccoon/checkin
//
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
	id := body.AccountID
	if id == "" {
		results := s.CheckinAll(ctx)
		responseJSON(w, map[string]interface{}{"success": true, "results": results})
		return
	}
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	result, err := s.dailyCheckin(ctx, acc)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
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
		"popups":  result.Popups,
	})
}
