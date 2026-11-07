package workbuddy

// 本文件实现 WorkBuddy **国际版**的「每日活跃奖励」领取链路。
//
// ── 为什么需要单独一条链路 ──────────────────────────────────
// 国际版（www.workbuddy.ai）与国内版共用同一套计费接口族，但**活动档期不同**：
//
//	POST {billing}/v2/billing/meter/checkin-activity-status  {}   ← 探测
//	POST {billing}/v2/billing/meter/daily-checkin            {}   ← 领取
//
// 实测（2026-11-07，8 个真实国际版账号）：
//   - checkin-activity-status 全部 200，data.daily_credit=100；
//   - 但 active=false、today_checked_in=false、start_time/end_time/activity_name
//     全空 —— 即**接口族齐全、只是当期活动未开启**；
//   - 此时 daily-checkin 返回 10001「签到活动未开启或已过期」。
//
// 因此不能像旧实现那样把国际版整块写死为「无签到体系」：那样活动一开也永远
// 领不到。正确做法是**每次都探测**，活动开了才领，没开就返回中性结果。
//
// ── 活跃判定与保活（参考实现 Agent2Providers 的抓包结论）─────
// 「当日活跃」的判定通道与签到不同，三件事各管一段：
//
//	① POST {billing}/v2/report        一条 chat_request_send → 点亮成长中心连登
//	② POST {chat}/completions         一次免费模型最小对话   → 维持「账号有调用」
//	③ POST {web}/console/as/conversations/  网页通道建会话   → 每日活跃计数只认它
//
// ③ 需要网页 session Cookie：先用 POST /v2/plugin/device/auth/code 拿一次性
// deviceCode，再 GET /console/client-login?code=… 换 Set-Cookie（实测 302 +
// session=…，Max-Age=604800）。Cookie 只绑账号、不落盘（进程内缓存 7 天）。
//
// ── 硬约束 ──────────────────────────────────────────────────
// 三步保活都是**尽力而为**：任何一步失败都不应让签到整体报错，只把原因写进结果。
// 只有「登录态被上游拒绝（401/403）」才算账号级失败。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// intlFreeModels 是国际版可用于「保活」的免费模型链，按可用性依次尝试。
//
// 全部实测 credit=0（不消耗账号积分）：deepseek-v4.1-flash / hy3 / hy4free。
// 参考实现还列了 hy4-preview / hy4-preview-f，但当前 /v3/config 目录里已不存在
// 这两个 id，故不列入。
var intlFreeModels = []string{"deepseek-v4.1-flash", "hy3", "hy4free"}

// intlWebSessionTTL 是网页 session Cookie 的缓存时长（上游 Max-Age=604800）。
const intlWebSessionTTL = 7 * 24 * time.Hour

// intlWebSession 是一条账号的网页登录态（只存 Cookie，不落盘）。
type intlWebSession struct {
	cookie    string
	expiresAt time.Time
}

// activityStatus 是 checkin-activity-status 的归一化读数。
type activityStatus struct {
	// Available 表示探测接口本身答上了（区别于「接口说不活动」）。
	Available bool
	// Active 表示当日活跃条件已满足。
	Active bool
	// TodayCheckedIn 表示今日奖励已领取。
	TodayCheckedIn bool
	// DailyCredit 是每日可领积分（上游下发，缺失为 0）。
	DailyCredit int64
	// StreakDays 是活动连签天数。
	StreakDays int64
	// ActivityName / StartTime / EndTime 用于区分「活动未开」与「账号没资格」。
	ActivityName string
	StartTime    string
	EndTime      string
}

// activityRewardSupported 报告账号所在区域是否有「每日活跃奖励」这条链路。
//
// 国内版与国际版都有（两版共用同一套计费接口）；办公版是企业账号，实测
// daily-checkin 返回 10001「企业账号不支持该操作」，且没有个人成长体系。
func activityRewardSupported(acc Account) bool {
	region := normalizeRegion(acc.Region)
	return region == regionCN || region == regionIntl
}

// checkinSupported 报告账号是否有**独立的每日签到接口**（国内版）。
//
// 国际版没有可用的签到活动档期概念，改由 activityRewardSupported 那条链路
// 处理（探测 + 条件领取）；办公版企业账号不参与。
func checkinSupported(acc Account) bool {
	return normalizeRegion(acc.Region) == regionCN
}

// fetchActivityStatus 探测一次「每日活跃奖励」活动状态。
func (s *Service) fetchActivityStatus(ctx context.Context, acc *Account) (activityStatus, error) {
	region := normalizeRegion(acc.Region)
	data, err := s.doMeterJSON(ctx, acc, http.MethodPost,
		billingBaseFor(region)+"/v2/billing/meter/checkin-activity-status", meterHeaders(acc), "{}")
	if err != nil {
		return activityStatus{}, err
	}
	var raw struct {
		Active         bool   `json:"active"`
		TodayCheckedIn bool   `json:"today_checked_in"`
		DailyCredit    int64  `json:"daily_credit"`
		StreakDays     int64  `json:"streak_days"`
		ActivityName   string `json:"activity_name"`
		StartTime      string `json:"start_time"`
		EndTime        string `json:"end_time"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return activityStatus{}, fmt.Errorf("活动状态解析失败: %w", err)
	}
	return activityStatus{
		Available:      true,
		Active:         raw.Active,
		TodayCheckedIn: raw.TodayCheckedIn,
		DailyCredit:    raw.DailyCredit,
		StreakDays:     raw.StreakDays,
		ActivityName:   raw.ActivityName,
		StartTime:      raw.StartTime,
		EndTime:        raw.EndTime,
	}, nil
}

// claimActivityReward 在活动开启且当日未领时领取一次。
func (s *Service) claimActivityReward(ctx context.Context, acc *Account) (CheckinResult, error) {
	region := normalizeRegion(acc.Region)
	data, err := s.doMeterJSON(ctx, acc, http.MethodPost,
		endpointCheckin(region), meterHeaders(acc), "{}")
	if err != nil {
		if isAlreadyCheckinError(err) {
			return CheckinResult{Status: "already", Message: "今天已签到"}, nil
		}
		return CheckinResult{}, err
	}
	var resp struct {
		Credit     int64 `json:"credit"`
		StreakDays int64 `json:"streak_days"`
	}
	_ = json.Unmarshal(data, &resp)
	msg := "签到成功"
	if resp.Credit > 0 {
		msg = fmt.Sprintf("签到成功，+%d 积分", resp.Credit)
	}
	return CheckinResult{
		Status:     "success",
		Message:    msg,
		Gained:     resp.Credit,
		StreakDays: resp.StreakDays,
	}, nil
}

// dailyCheckinActivity 是「探测 → 条件领取 → 保活」的完整一轮（国际版主路径）。
//
// 返回的 CheckinResult.Status 语义：
//   - success：本次领到了奖励；
//   - already：今日已领（幂等成功）；
//   - skipped：活动未开启（正常状态，不是失败）；
//   - failed：账号级失败（登录态失效等），error 非空。
func (s *Service) dailyCheckinActivity(ctx context.Context, acc Account) (CheckinResult, error) {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return CheckinResult{}, fmt.Errorf("账号缺少 access token")
	}
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()

	status, err := s.fetchActivityStatus(ctx, &acc)
	if err != nil {
		return CheckinResult{}, err
	}
	switch {
	case status.TodayCheckedIn:
		// 今日已领：仍跑一次保活，避免「已领就不管」把连登断掉。
		s.keepAliveIntl(ctx, &acc)
		return CheckinResult{Status: "already", Message: "今天已签到", StreakDays: status.StreakDays}, nil
	case status.Active:
		result, err := s.claimActivityReward(ctx, &acc)
		if err != nil {
			return CheckinResult{}, err
		}
		s.keepAliveIntl(ctx, &acc)
		return result, nil
	default:
		// 活动未开启：做保活（让连登与日活继续累计），并如实说明没有可领的奖励。
		s.keepAliveIntl(ctx, &acc)
		return CheckinResult{
			Status:     "skipped",
			Message:    intlInactiveMessage(status),
			StreakDays: status.StreakDays,
		}, nil
	}
}

// intlInactiveMessage 生成「活动未开启」的中性说明，把可辨识的线索一并带出。
func intlInactiveMessage(status activityStatus) string {
	if status.StartTime == "" && status.EndTime == "" {
		return fmt.Sprintf("每日活跃奖励活动当前未开启（每日 %d 积分，活动开启后自动领取）；已执行活跃保活", status.DailyCredit)
	}
	return fmt.Sprintf("每日活跃奖励活动未开启（档期 %s ~ %s，每日 %d 积分）；已执行活跃保活",
		status.StartTime, status.EndTime, status.DailyCredit)
}

// keepAliveIntl 执行国际版的「尽力保活」：活跃上报 + 免费模型对话 + 网页会话。
//
// 三步互相独立，任一步失败只记日志，不影响其余两步，也不让签到整体失败。
func (s *Service) keepAliveIntl(ctx context.Context, acc *Account) {
	if normalizeRegion(acc.Region) != regionIntl {
		return
	}
	if err := s.reportChatActivity(ctx, *acc); err != nil {
		applog.Warn(ctx, "workbuddy", "intl activity report failed", "account", acc.ID, "error", err.Error())
	}
	if err := s.pokeFreeModel(ctx, acc); err != nil {
		applog.Warn(ctx, "workbuddy", "intl free model keepalive failed", "account", acc.ID, "error", err.Error())
	}
	if err := s.createWebConversation(ctx, acc); err != nil {
		applog.Warn(ctx, "workbuddy", "intl web conversation keepalive failed", "account", acc.ID, "error", err.Error())
	}
}

// pokeFreeModel 用免费模型发一次最小流式对话，维持账号「当日有调用」。
//
// 请求必须长得像官方客户端：复用 backendHeaders（含会话头族、链路头族、
// X-Domain 与桌面端指纹），并且**首条消息必须是 system** —— 实测缺 system 时
// 上游返回 400 code 11128「first message is not system prompt」。
// 响应体必须读到（不读的请求上游可能不计活跃），但有上限。
func (s *Service) pokeFreeModel(ctx context.Context, acc *Account) error {
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	var lastErr error
	for _, model := range intlFreeModels {
		body, err := json.Marshal(map[string]any{
			"model":      model,
			"stream":     true,
			"max_tokens": 16,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "user", "content": "hi"},
			},
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			endpointChat(normalizeRegion(acc.Region)), strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		backendHeaders(req, acc, "")
		req.Header.Set("Accept", "text/event-stream")
		resp, err := s.httpClientFor(acc.ID).Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		// 有界读体：上游要看到「读完了」才可能计活跃，但不做无界缓冲。
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status >= 200 && status < 300 {
			return nil
		}
		lastErr = fmt.Errorf("免费模型 %s 返回 HTTP %d", model, status)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的免费模型")
	}
	return lastErr
}

// webSessionOf 返回账号的网页 session Cookie，过期或不存在时重新获取。
func (s *Service) webSessionOf(ctx context.Context, acc *Account) (string, error) {
	s.intlSessionMu.Lock()
	cached, ok := s.intlSessions[acc.ID]
	s.intlSessionMu.Unlock()
	if ok && time.Now().Before(cached.expiresAt) && cached.cookie != "" {
		return cached.cookie, nil
	}
	cookie, err := s.acquireWebSession(ctx, acc)
	if err != nil {
		return "", err
	}
	s.intlSessionMu.Lock()
	if s.intlSessions == nil {
		s.intlSessions = map[string]intlWebSession{}
	}
	s.intlSessions[acc.ID] = intlWebSession{cookie: cookie, expiresAt: time.Now().Add(intlWebSessionTTL)}
	s.intlSessionMu.Unlock()
	return cookie, nil
}

// invalidateWebSession 丢弃账号的网页 Cookie（上游 401 时调用，下次重取）。
func (s *Service) invalidateWebSession(accountID string) {
	s.intlSessionMu.Lock()
	delete(s.intlSessions, accountID)
	s.intlSessionMu.Unlock()
}

// acquireWebSession 用一次性 deviceCode 换网页 session Cookie。
//
// 链路：POST /v2/plugin/device/auth/code → data.deviceCode
//
//	GET  /console/client-login?code=…&target=/profile/growth-center → 302 + Set-Cookie
//
// 第二步必须**禁止自动跟随重定向**，否则拿不到 Set-Cookie 那一跳。
func (s *Service) acquireWebSession(ctx context.Context, acc *Account) (string, error) {
	region := normalizeRegion(acc.Region)
	base := webBaseFor(region)

	// ① 取一次性登录码。
	codeData, err := s.doMeterJSON(ctx, acc, http.MethodPost,
		billingBaseFor(region)+"/v2/plugin/device/auth/code", deviceAuthHeaders(acc), "{}")
	if err != nil {
		return "", fmt.Errorf("获取网页登录码失败: %w", err)
	}
	var codeResp struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.Unmarshal(codeData, &codeResp); err != nil {
		return "", fmt.Errorf("网页登录码解析失败: %w", err)
	}
	code := strings.TrimSpace(codeResp.DeviceCode)
	if code == "" {
		return "", fmt.Errorf("上游未返回网页登录码")
	}

	// ② 换 Cookie：不跟随重定向，直接读 302 的 Set-Cookie。
	loginURL := base + "/console/client-login?" + url.Values{
		"code":   {code},
		"target": {"/profile/growth-center"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", webUserAgent)
	client := &http.Client{
		Timeout:       checkinQueryTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("官网登录失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	for _, raw := range resp.Header.Values("Set-Cookie") {
		if cookie := sessionCookieOf(raw); cookie != "" {
			return cookie, nil
		}
	}
	return "", fmt.Errorf("官网没有返回有效 session Cookie（HTTP %d）", resp.StatusCode)
}

// webUserAgent 是网页链路的浏览器标识（网页接口只认浏览器 UA，不认桌面端）。
const webUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// sessionCookieOf 从一条 Set-Cookie 里取出 `session=...` 的键值对。
//
// 只认 name 恰为 session 的那条：上游还会下发 tgw_l7_route 等无关 Cookie，
// 一并带上会让网页接口拒绝（或行为不可预期）。
func sessionCookieOf(setCookie string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(setCookie), ";")
	name, value, ok := strings.Cut(first, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), "session") {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "session=" + value
}

// deviceAuthHeaders 是取网页登录码用的头：桌面端身份 + refresh token。
func deviceAuthHeaders(acc *Account) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "*/*")
		r.Header.Set("User-Agent", "node")
		r.Header.Set("Sec-Fetch-Mode", "cors")
		r.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		if acc.UID != "" {
			r.Header.Set("X-User-Id", acc.UID)
		}
		if acc.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
			r.Header.Set("X-Tenant-Id", acc.EnterpriseID)
		}
		if acc.RefreshToken != "" {
			r.Header.Set("X-Refresh-Token", acc.RefreshToken)
		}
		r.Header.Set("X-Product", "SaaS")
	}
}

// createWebConversation 在网页通道建一个带 prompt 的会话。
//
// 这是「当日活跃」计数的**唯一**判定通道（桌面端 chat/completions 与
// /v2/report 都不进这个计数）。成功判据：HTTP 2xx 且响应 code ∈ {0,缺失}
// 且 data.id 非空。
func (s *Service) createWebConversation(ctx context.Context, acc *Account) error {
	region := normalizeRegion(acc.Region)
	base := webBaseFor(region)
	cookie, err := s.webSessionOf(ctx, acc)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"prompt":             "Hi",
		"model":              intlFreeModels[0],
		"conversationOrigin": "workbuddy-app",
		"plugins": []map[string]string{
			{"name": "weixinpay", "marketplace": "codebuddy-builtin"},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/console/as/conversations/", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/profile/growth-center")
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := s.httpClientFor(acc.ID).Do(req)
	if err != nil {
		return fmt.Errorf("网页会话请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Cookie 已失效：丢掉缓存，下次重新换一次。
		s.invalidateWebSession(acc.ID)
		return fmt.Errorf("网页登录态被拒绝（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("网页会话返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Code *int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("网页会话响应解析失败: %w", err)
	}
	if payload.Code != nil && *payload.Code != 0 {
		return fmt.Errorf("网页会话返回业务码 %d", *payload.Code)
	}
	if strings.TrimSpace(payload.Data.ID) == "" {
		return fmt.Errorf("网页会话未返回 id")
	}
	return nil
}

// intlSessionCache 是网页 session 的进程内缓存（按账号 ID）。
type intlSessionCache struct {
	mu       sync.Mutex
	sessions map[string]intlWebSession
}
