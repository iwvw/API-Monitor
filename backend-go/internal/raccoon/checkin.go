package raccoon

// 本文件实现小浣熊的「每日积分」签到链路。
//
// ── 上游契约（实测 2026-11-07 + 参考实现 Agent2Providers 的 raccoon/balance.rs）──
//
//	GET  /api/web/office/v3/setting_info        ← 发放触发器（桌面端身份头）
//	GET  /api/web/office/v3/setting_info        ← 有组织时改用
//	     /api/web/org/user/office/v1/setting_info
//	GET  /api/web/points/v2/bills               ← 核对今日实际入账（注意是 v2）
//
// 小浣熊**没有**独立的签到/领取接口：官方客户端每次启动或回前台都会打一次
// setting_info，服务端按自然日给首次请求的账号发放每日积分，并在响应里带
// `point_grant_popups` / `point_grant_toast`。因此「签到」= 每天打一次
// setting_info，再用账单确认积分真的到账。
//
// ── 为什么要核对账单，而不是只看 HTTP 200 ────────────────────
// 发放是按天幂等的：当天已发过时接口照样 200，只是 popups 为空。而「popups 为空」
// 既可能是「今天已经领过」也可能是「今天还没发」，无法区分。账单（bills）才是
// 权威来源：找当天的 daily 发放记录，有记录即今日已完成。
//
// ── 账单接口的两个坑（都实测踩过）────────────────────────────
//   - 路径是 **/points/v2/bills**，`/points/v1/bill` 返回 404 page not found；
//   - 分页用 `paging.limit` + `cursor`，响应里 `data.items` / `has_more` /
//     `next_cursor`。
//
// ── 日期归属 ────────────────────────────────────────────────
// 上游的自然日即 UTC+8 零点（与站点时区无关），因此「今天」必须按 **Asia/Shanghai**
// 计算 —— 用站点时区会让海外部署把 16 小时的账单认成昨天，导致「今日已领」判定整体错位。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// checkinZone 是上游自然日所在时区（UTC+8）。
var checkinZone = time.FixedZone("CST", 8*3600)

// checkinQueryTimeout 是单次签到类上游调用的超时。
const checkinQueryTimeout = 30 * time.Second

// checkinBillLimit 是核对账单时拉取的条数（够覆盖当天即可）。
const checkinBillLimit = 20

// billFetchRounds 是账单分页最多翻几轮（防止异常 has_more 死循环）。
const billFetchRounds = 5

// CheckinResult 是一次签到的结果（下发前端）。
type CheckinResult struct {
	// Status：success（本次领到）/ already（今天已领）/ skipped（无签到体系）
	// / failed（失败，Error 非空）。
	Status string `json:"status"`
	// Message 是人类可读的说明。
	Message string `json:"message,omitempty"`
	// Gained 是本次到账积分（已领/跳过时为 0）。
	Gained int64 `json:"gained,omitempty"`
	// Popups 是上游下发的发放提示（原样透出，供前端展示）。
	Popups json.RawMessage `json:"popups,omitempty"`
}

// checkinSupported 报告账号是否支持每日积分签到。
// 小浣熊所有账号都参与（没有区域/企业类型区分）。
func checkinSupported(Account) bool { return true }

// settingInfoPath 返回 setting_info 路径：有组织身份时走组织域，否则走个人域。
func settingInfoPath(orgCode string) string {
	if strings.TrimSpace(orgCode) == "" {
		return "/api/web/office/v3/setting_info"
	}
	return "/api/web/org/user/office/v1/setting_info"
}

// dailyCheckin 执行一次「每日积分」签到。
//
// 步骤：① 先查账单判断今天是否已领；② 未领则打 setting_info 触发发放；
// ③ 再查账单确认到账（发放可能是异步的，给一次短暂重试）。
func (s *Service) dailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return CheckinResult{}, fmt.Errorf("账号缺少 access token")
	}
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	if err := s.ensureToken(ctx, &acc); err != nil {
		return CheckinResult{}, err
	}
	today := time.Now().In(checkinZone).Format("2006-01-02")

	before, beforeErr := s.dailyGrants(ctx, &acc, today)
	if beforeErr != nil {
		// 账单查不到不等于今天没发；继续走发放，只是无法给出精确的到账核对。
		applog.Warn(ctx, "raccoon", "read daily bills failed", "account", acc.ID, "error", beforeErr.Error())
	}
	if len(before) > 0 {
		return CheckinResult{Status: "already", Message: "今天已签到"}, nil
	}

	popups, err := s.triggerDailyGrant(ctx, &acc)
	if err != nil {
		return CheckinResult{}, err
	}

	// 到账可能是异步的：短重试一次，避免把「刚发还没落账」误报成失败。
	var after []dailyGrant
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return CheckinResult{}, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		after, err = s.dailyGrants(ctx, &acc, today)
		if err != nil {
			continue
		}
		if len(after) > 0 {
			break
		}
	}

	result := CheckinResult{Status: "success", Popups: popups}
	switch {
	case len(after) > 0:
		result.Gained = sumGrantPoints(after)
		if result.Gained > 0 {
			result.Message = fmt.Sprintf("签到成功，+%d 积分", result.Gained)
		} else {
			result.Message = "签到成功，积分已发放"
		}
	case beforeErr != nil:
		// 账单读不到：如实说明，不谎称成功也不报失败。
		result.Status = "success"
		result.Message = "已触发每日积分发放，但账单接口不可用，未能核对到账"
	default:
		// 账单可读且今天仍无记录：发放请求成功但没有到账记录。
		result.Status = "skipped"
		result.Message = "已触发每日积分发放，但今日暂未见入账记录（可能已由官方客户端领取）"
	}
	return result, nil
}

// dailyGrant 是一条当日积分发放记录。
type dailyGrant struct {
	Points int64
	Event  string
}

// sumGrantPoints 汇总一组发放记录的积分。
func sumGrantPoints(grants []dailyGrant) int64 {
	var total int64
	for _, g := range grants {
		if g.Points > 0 {
			total += g.Points
		}
	}
	return total
}

// triggerDailyGrant 打一次 setting_info 触发每日积分发放，返回发放提示。
func (s *Service) triggerDailyGrant(ctx context.Context, acc *Account) (json.RawMessage, error) {
	status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodGet,
		baseOrigin+settingInfoPath(""), nil, acc.AccessToken,
		desktopHeaders(), 20*time.Second, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("触发每日积分发放失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("触发每日积分发放失败: HTTP %d: %s", status, jsonMessage(data))
	}
	var resp struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("每日积分响应解析失败: %w", err)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return nil, fmt.Errorf("触发每日积分发放失败: code=%d", *resp.Code)
	}
	var payload struct {
		Popups json.RawMessage `json:"point_grant_popups"`
		Toast  json.RawMessage `json:"point_grant_toast"`
	}
	_ = json.Unmarshal(resp.Data, &payload)
	if len(payload.Popups) > 0 || len(payload.Toast) > 0 {
		combined, _ := json.Marshal(map[string]json.RawMessage{
			"popups": payload.Popups,
			"toast":  payload.Toast,
		})
		return combined, nil
	}
	return nil, nil
}

// dailyGrants 读取今天的积分发放记录（bills 接口，v2 路径）。
func (s *Service) dailyGrants(ctx context.Context, acc *Account, today string) ([]dailyGrant, error) {
	var (
		out    []dailyGrant
		cursor string
	)
	for round := 0; round < billFetchRounds; round++ {
		query := url.Values{}
		query.Set("paging.limit", fmt.Sprintf("%d", checkinBillLimit))
		query.Set("type", "all")
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		status, data, err := s.doJSON(ctx, s.httpClientFor(acc.ID), http.MethodGet,
			baseOrigin+"/api/web/points/v2/bills?"+query.Encode(), nil, acc.AccessToken,
			desktopHeaders(), 25*time.Second, maxCatalogBytes)
		if err != nil {
			return nil, fmt.Errorf("读取积分账单失败: %w", err)
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("读取积分账单失败: HTTP %d", status)
		}
		var resp struct {
			Code *int `json:"code"`
			Data struct {
				Items      []map[string]any `json:"items"`
				HasMore    bool             `json:"has_more"`
				NextCursor string           `json:"next_cursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("积分账单解析失败: %w", err)
		}
		if resp.Code != nil && *resp.Code != 0 {
			return nil, fmt.Errorf("读取积分账单失败: code=%d", *resp.Code)
		}
		stop := false
		for _, item := range resp.Data.Items {
			grant, ok, beforeDay := parseGrant(item, today)
			if beforeDay {
				// 账单按时间倒序：遇到更早的日期即可停止翻页。
				stop = true
				break
			}
			if ok {
				out = append(out, grant)
			}
		}
		if stop || !resp.Data.HasMore || resp.Data.NextCursor == "" {
			break
		}
		cursor = resp.Data.NextCursor
	}
	return out, nil
}

// parseGrant 判断一条账单是否属于「今天、daily 类型、正积分」的发放。
//
// 返回 (记录, 是否命中, 是否已翻到更早日期)。第三个返回值让调用方能提前停止翻页。
func parseGrant(item map[string]any, today string) (dailyGrant, bool, bool) {
	day := billDate(item)
	if day == "" {
		return dailyGrant{}, false, false
	}
	if day < today {
		return dailyGrant{}, false, true
	}
	if day > today {
		// 未来的记录（时区边界）：跳过但不停止。
		return dailyGrant{}, false, false
	}
	points := billPoints(item)
	if points <= 0 {
		return dailyGrant{}, false, false
	}
	event, _ := item["event_name"].(string)
	// 只认 daily 类发放：账单里还混着消费、补签、过期等其它事件。
	if !isDailyGrantEvent(event) {
		return dailyGrant{}, false, false
	}
	return dailyGrant{Points: points, Event: event}, true, false
}

// isDailyGrantEvent 判断账单事件名是否属于「每日发放」。
//
// 上游没有稳定的枚举，按词根判定；实测样本形如 daily_grant / daily_signin /
// daily_question_reward。makeup / expire / consume 等一律不算。
func isDailyGrantEvent(event string) bool {
	e := strings.ToLower(strings.TrimSpace(event))
	if e == "" {
		return false
	}
	for _, bad := range []string{"makeup", "expire", "consume", "refund", "topup"} {
		if strings.Contains(e, bad) {
			return false
		}
	}
	return strings.Contains(e, "daily")
}

// billDate 从账单条目里取日期（YYYY-MM-DD）。
//
// 上游不同事件用不同字段名，按常见顺序尝试；时间戳统一按 UTC+8 归属。
func billDate(item map[string]any) string {
	for _, key := range []string{"bill_date", "date", "created_date", "day"} {
		if v, ok := item[key].(string); ok && strings.TrimSpace(v) != "" {
			return normalizeDay(strings.TrimSpace(v))
		}
	}
	for _, key := range []string{"created_at", "create_time", "timestamp", "time"} {
		if ms := jsonInt(item[key]); ms > 0 {
			return time.UnixMilli(ms).In(checkinZone).Format("2006-01-02")
		}
	}
	return ""
}

// normalizeDay 把上游的日期串归一为 YYYY-MM-DD（兼容带时间部分与 RFC3339）。
func normalizeDay(raw string) string {
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		return raw[:10]
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006/01/02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.In(checkinZone).Format("2006-01-02")
		}
	}
	return ""
}

// billPoints 从账单条目里取积分变化量（正数才算发放）。
func billPoints(item map[string]any) int64 {
	for _, key := range []string{"points", "point", "credit", "amount", "change_points"} {
		if v := jsonInt(item[key]); v != 0 {
			return v
		}
	}
	return 0
}

// desktopHeaders 是桌面端身份的请求头（发放触发器与账单都要求）。
func desktopHeaders() map[string]string {
	return map[string]string{
		"Accept":             "application/json",
		"X-Client-Platform":  "desktop",
		"X-Raccoon-Language": "zh",
	}
}
