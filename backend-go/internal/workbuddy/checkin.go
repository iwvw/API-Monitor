package workbuddy

// 本文件是 WorkBuddy 的「积分运营层」：每日签到、对话活跃上报、连登档位兑换与抽奖、
// 补签卡补签。协议事实全部经真实账号实测（2026-09-15），要点如下：
//
//   - 签到：POST {billing}/v2/billing/meter/daily-checkin，空 body。成功返回
//     data.credit（本次到账积分）与 data.streak_days；重复签到返回业务码 10001/14001
//     「今天已签到」，属幂等成功而非失败。
//   - 国际版（workbuddy.ai）无签到体系：同接口返回 10001「签到活动未开启或已过期」，
//     且 /activity/growth/streak 直接 500。故签到与连登类操作**只对国内版账号执行**。
//   - 活跃上报：POST {billing}/v2/report，body 为 chat_request_send 事件数组，事件必须
//     带 userId（缺失时上游 200 但静默丢弃）；一条上报同时点亮连登并解锁 first_buddy。
//   - 连登：GET/POST {web}/activity/growth/{streak,redeem,lottery/summary,lottery/draw}。
//     档位按连续登录天数解锁，未解锁兑换返回 403；抽奖次数只能由兑换发放。
//   - 补签：POST {web}/activity/growth/makeup-cards/use，body {"target_date":"YYYY-MM-DD"}；
//     无补签卡余额返回 403「no makeup card balance」。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// checkinQueryTimeout 是单次签到/活跃/连登类上游调用的超时。
const checkinQueryTimeout = 30 * time.Second

// CheckinResult 是一次签到尝试的结果（下发前端与写回账号状态）。
type CheckinResult struct {
	// Status：success（本次签到成功）/ already（今天已签到，幂等）/ skipped（该账号无签到体系）
	// / failed（上游失败，error 非空）。
	Status string `json:"status"`
	// Message 是人类可读的结果说明。
	Message string `json:"message,omitempty"`
	// Gained 是本次到账积分（已签到/跳过时为 0）。
	Gained int64 `json:"gained,omitempty"`
	// StreakDays 是上游返回的连续登录天数。
	StreakDays int64 `json:"streakDays,omitempty"`
}

// checkinSupported 报告账号是否属于签到体系（仅国内版）。
// 国际版同接口返回 10001「签到活动未开启或已过期」，且连登接口 500，
// 因此对国际版账号直接跳过，避免每天两次无效请求与错误日志。
func checkinSupported(acc Account) bool {
	return normalizeRegion(acc.Region) == regionCN
}

// isAlreadyCheckinError 判定错误是否为「今天已签到」的幂等拒绝。
// 只认上游业务码错误（doJSON 的 "上游业务码 N: msg"），网络层错误不算——
// 否则网络抖动会被误记为签到成功，账号当天实际漏签。
func isAlreadyCheckinError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "上游业务码") {
		return false
	}
	return strings.Contains(msg, "已签到") || strings.Contains(strings.ToLower(msg), "already")
}

// isLockedError 判定错误是否为「未解锁」类业务拒绝（连登档位天数不足、无补签卡）。
// 这类属预期状态而非故障，调用方应静默跳过。
func isLockedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "连续登录天数不足") ||
		strings.Contains(msg, "no makeup card balance") ||
		strings.Contains(msg, "已兑换") ||
		strings.Contains(msg, "claimed")
}

// billingBaseOverride / webBaseOverride 仅供测试把计费域与成长域指向本地 mock 服务；
// 为空时按区域派生（生产路径不变）。
var (
	billingBaseOverride string
	webBaseOverride     string
)

// billingBaseFor 返回生效的计费域基址（签到 / 活跃上报）。
func billingBaseFor(region string) string {
	if v := strings.TrimSpace(billingBaseOverride); v != "" {
		return v
	}
	return regionBillingHost(region)
}

// webBaseFor 返回生效的官网成长中心基址（连登 / 兑换 / 抽奖 / 补签）。
func webBaseFor(region string) string {
	if v := strings.TrimSpace(webBaseOverride); v != "" {
		return v
	}
	return regionWebHost(region)
}

// upstreamCallError 保留上游的业务码与原文。
//
// 为什么不能复用 doJSON：它遇到 HTTP 4xx/5xx 直接返回「上游 HTTP N」，会丢掉信封里的
// 业务码与文案。而签到与连登恰恰**依赖业务码判语义**——重复签到是 HTTP 400 +
// code 10001「今天已签到」，连登未解锁是 HTTP 403 + 「连续登录天数不足」，
// 都是 2xx 之外的"预期状态"。丢掉码就只能当故障处理，幂等判定会失效。
type upstreamCallError struct {
	Status int
	Code   int
	Msg    string
}

func (e *upstreamCallError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("上游业务码 %d: %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("上游 HTTP %d", e.Status)
}

// doMeterJSON 发签到/连登类请求并解信封，错误保留业务码与文案。
// HTTP 非 2xx 且信封可解析时以业务码为准（上游对预期状态用 4xx 表达）。
func (s *Service) doMeterJSON(ctx context.Context, acc *Account, method, fullURL string, headers func(*http.Request), body string) (json.RawMessage, error) {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	env, status, err := doEnvelope(ctx, s.httpClientFor(acc.ID), method, fullURL, headers, rdr)
	if err != nil {
		return nil, err
	}
	if status >= 400 && env.Code == 0 {
		return nil, &upstreamCallError{Status: status}
	}
	if env.Code != 0 {
		return nil, &upstreamCallError{Status: status, Code: env.Code, Msg: env.Msg}
	}
	return env.Data, nil
}

// endpointCheckin 返回区域签到接口 URL。
func endpointCheckin(region string) string {
	return billingBaseFor(region) + "/v2/billing/meter/daily-checkin"
}

// endpointReport 返回区域活跃上报接口 URL。
func endpointReport(region string) string {
	return billingBaseFor(region) + "/v2/report"
}

// endpointGrowth 返回区域成长中心接口 URL（签到之外的连登/兑换/抽奖）。
func endpointGrowth(region, path string) string {
	return webBaseFor(region) + path
}

// meterHeaders 施加 billing/meter 族（签到、余额）的请求头，与 balance.go 的查询口径一致。
func meterHeaders(acc *Account) func(*http.Request) {
	region := normalizeRegion(acc.Region)
	referer := billingBaseFor(region)
	return func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		r.Header.Set("User-Agent", regionUA(region))
		r.Header.Set("Origin", referer)
		r.Header.Set("Referer", referer+"/")
		r.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		r.Header.Set("X-CodeBuddy-Request", "1")
		if acc.UID != "" {
			r.Header.Set("X-User-Id", acc.UID)
		}
		if acc.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
			r.Header.Set("X-Tenant-Id", acc.EnterpriseID)
		}
		if acc.Domain != "" {
			r.Header.Set("X-Domain", acc.Domain)
		}
		r.Header.Set("X-Product", "SaaS")
	}
}

// webHeaders 施加官网成长中心（workbuddy.cn）的请求头形状：Web 端指纹。
func webHeaders(acc *Account) func(*http.Request) {
	region := normalizeRegion(acc.Region)
	base := webBaseFor(region)
	return func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/plain, */*")
		r.Header.Set("User-Agent", regionUA(region))
		r.Header.Set("Origin", base)
		r.Header.Set("Referer", base+"/profile/growth-center")
		r.Header.Set("x-client-platform", "web")
		r.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		r.Header.Set("X-CodeBuddy-Request", "1")
		if acc.UID != "" {
			r.Header.Set("X-User-Id", acc.UID)
		}
		if acc.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
			r.Header.Set("X-Tenant-Id", acc.EnterpriseID)
		}
		if acc.Domain != "" {
			r.Header.Set("X-Domain", acc.Domain)
		}
	}
}

// dailyCheckin 执行一次签到。重复签到返回 already 状态而非错误。
func (s *Service) dailyCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return CheckinResult{}, fmt.Errorf("账号缺少 access token")
	}
	if !checkinSupported(acc) {
		return CheckinResult{Status: "skipped", Message: regionLabel(acc.Region) + "无签到体系，已跳过"}, nil
	}
	region := normalizeRegion(acc.Region)
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	data, err := s.doMeterJSON(ctx, &acc, http.MethodPost,
		endpointCheckin(region), meterHeaders(&acc), "{}")
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
	return CheckinResult{
		Status:     "success",
		Message:    fmt.Sprintf("签到成功，+%d 积分", resp.Credit),
		Gained:     resp.Credit,
		StreakDays: resp.StreakDays,
	}, nil
}

// chatRequestEvent 是 chat_request_send 事件的完整形状（照抄客户端实测载荷）。
// 事件必须带 userId（=账号 uid）：缺失时上游返回 200 但静默丢弃，连登不会点亮。
type chatRequestEvent struct {
	EventCode             string `json:"eventCode"`
	Timestamp             int64  `json:"timestamp"`
	ReportDelay           int    `json:"reportDelay"`
	Mode                  string `json:"mode"`
	ConversationID        string `json:"conversationId"`
	RequestID             string `json:"requestId"`
	InputLength           int    `json:"inputLength"`
	RequestModelID        string `json:"requestModelId"`
	RequestModelName      string `json:"requestModelName"`
	IsPlan                bool   `json:"isPlan"`
	IsAutoExecuteTerminal bool   `json:"isAutoExecuteTerminal"`
	IsAutoModify          bool   `json:"isAutoModify"`
	CodebaseEnable        bool   `json:"codebaseEnable"`
	MaxToken              int    `json:"maxToken"`
	MaxSteps              int    `json:"maxSteps"`
	Temperature           int    `json:"temperature"`
	MaxRetries            int    `json:"maxRetries"`
	MentionContexts       []any  `json:"mentionContexts"`
	KnowledgeID           []any  `json:"knowledgeId"`
	KnowledgeName         []any  `json:"knowledgeName"`
	CodebaseID            string `json:"codebaseId"`
	MentionContextCount   int    `json:"mentionContextCount"`
	Command               string `json:"command"`
	ExpertID              string `json:"expertId"`
	RecommendID           string `json:"recommendId"`
	SkillID               string `json:"skillId"`
	SkillCount            int    `json:"skillCount"`
	TotalCount            int    `json:"totalCount"`
	FileURI               string `json:"fileUri"`
	PresentAt             int64  `json:"presentAt"`
	TraceID               string `json:"traceId"`
	RootRequestID         string `json:"rootRequestId"`
	ParentConversationID  string `json:"parentConversationId"`
	AgentName             string `json:"agentName"`
	AgentType             string `json:"agentType"`
	UserID                string `json:"userId"`
}

// reportChatActivity 发送一条对话活跃上报。一条上报同时点亮连登并解锁 first_buddy 任务。
// conversationID 由调用方生成（无需真实会话，上游不校验一致性）。
func (s *Service) reportChatActivity(ctx context.Context, acc Account) error {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return fmt.Errorf("账号缺少 access token")
	}
	region := normalizeRegion(acc.Region)
	now := time.Now().UnixMilli()
	cid := fmt.Sprintf("wb-%d", now)
	ev := chatRequestEvent{
		EventCode:            "chat_request_send",
		Timestamp:            now,
		Mode:                 "craft",
		ConversationID:       cid,
		RequestID:            cid,
		InputLength:          12,
		RequestModelID:       "deepseek-v4-flash",
		RequestModelName:     "DeepSeek V4 Flash",
		MentionContexts:      []any{},
		KnowledgeID:          []any{},
		KnowledgeName:        []any{},
		PresentAt:            now,
		RootRequestID:        cid,
		ParentConversationID: cid,
		AgentName:            "default",
		AgentType:            "conversation",
		UserID:               acc.UID,
	}
	raw, err := json.Marshal([]chatRequestEvent{ev})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	_, err = s.doMeterJSON(ctx, &acc, http.MethodPost,
		endpointReport(region), meterHeaders(&acc), string(raw))
	return err
}

// growthStreakFull 拉取连登完整状态（天数、补签卡余额、各档位解锁与领取状态）。
func (s *Service) growthStreakFull(ctx context.Context, acc Account) (StreakFull, error) {
	var out StreakFull
	region := normalizeRegion(acc.Region)
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	data, err := s.doMeterJSON(ctx, &acc, http.MethodGet,
		endpointGrowth(region, "/activity/growth/streak"), webHeaders(&acc), "")
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("连登状态解析失败: %w", err)
	}
	return out, nil
}

// StreakFull 是连登状态的响应形状。
type StreakFull struct {
	Streak struct {
		Days              int `json:"days"`
		MonthTotalDays    int `json:"month_total_days"`
		NextTierRemaining int `json:"next_tier_remaining"`
	} `json:"streak"`
	MakeupCards struct {
		Balance int `json:"balance"`
		Max     int `json:"max"`
	} `json:"makeup_cards"`
	RedemptionStatus struct {
		Tier7dStatus  string `json:"tier_7d_status"`
		Tier14dStatus string `json:"tier_14d_status"`
		Tier28dStatus string `json:"tier_28d_status"`
		Tiers         []struct {
			Tier    string `json:"tier"`
			Days    int    `json:"days"`
			Credit  int64  `json:"credit"`
			Energy  int64  `json:"energy"`
			Cards   int    `json:"cards"`
			Chances int    `json:"chances"`
		} `json:"tiers"`
	} `json:"redemption_status"`
}

// clientToken 生成幂等令牌（与前端 randomUUID 同语义）。
func clientToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

// redeemTier 兑换连登档位（tier: 7d/14d/28d）。未解锁返回业务错误，调用方按 locked 跳过。
func (s *Service) redeemTier(ctx context.Context, acc Account, tier string) error {
	region := normalizeRegion(acc.Region)
	body, _ := json.Marshal(map[string]any{"tier": tier, "client_token": clientToken()})
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	_, err := s.doMeterJSON(ctx, &acc, http.MethodPost,
		endpointGrowth(region, "/activity/growth/redeem"), webHeaders(&acc), string(body))
	return err
}

// lotteryChances 查询当前抽奖次数。
func (s *Service) lotteryChances(ctx context.Context, acc Account) (int, error) {
	region := normalizeRegion(acc.Region)
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	data, err := s.doMeterJSON(ctx, &acc, http.MethodGet,
		endpointGrowth(region, "/activity/growth/lottery/summary"), webHeaders(&acc), "")
	if err != nil {
		return 0, err
	}
	var resp struct {
		Chances int `json:"chances"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}
	return resp.Chances, nil
}

// lotteryDraw 抽奖一次，返回原始奖品载荷。
func (s *Service) lotteryDraw(ctx context.Context, acc Account) (json.RawMessage, error) {
	region := normalizeRegion(acc.Region)
	body, _ := json.Marshal(map[string]any{"client_token": clientToken()})
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	data, err := s.doMeterJSON(ctx, &acc, http.MethodPost,
		endpointGrowth(region, "/activity/growth/lottery/draw"), webHeaders(&acc), string(body))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// useMakeupCard 用补签卡补签指定日期（保连登连续天数）。
func (s *Service) useMakeupCard(ctx context.Context, acc Account, date string) error {
	region := normalizeRegion(acc.Region)
	body, _ := json.Marshal(map[string]any{"target_date": date})
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	_, err := s.doMeterJSON(ctx, &acc, http.MethodPost,
		endpointGrowth(region, "/activity/growth/makeup-cards/use"), webHeaders(&acc), string(body))
	return err
}

// StreakBonusOutcome 是连登管家的一次执行结果（下发前端）。
type StreakBonusOutcome struct {
	Days          int      `json:"days"`
	MakeupUsed    string   `json:"makeupUsed,omitempty"`
	RedeemedTiers []string `json:"redeemedTiers,omitempty"`
	DrawCount     int      `json:"drawCount,omitempty"`
	Prizes        []string `json:"prizes,omitempty"`
}

// runStreakBonus 是连登管家：补签昨日 → 兑换所有已解锁档位 → 抽完所有次数。
// 幂等：未解锁/已领取/无次数均静默跳过，可每日重复执行。
func (s *Service) runStreakBonus(ctx context.Context, acc Account) (StreakBonusOutcome, error) {
	var out StreakBonusOutcome
	if !checkinSupported(acc) {
		return out, nil
	}
	full, err := s.growthStreakFull(ctx, acc)
	if err != nil {
		return out, err
	}
	out.Days = full.Streak.Days

	// 1) 补签：昨日漏签且有补签卡时补上（连登一断就要重攒，优先保住）。
	if full.MakeupCards.Balance > 0 {
		if missed, err := s.heatmapYesterdayMissed(ctx, acc); err == nil && missed {
			// 「昨日」是日历语义，必须按站点时区归属，不能用服务器本地时钟。
			yesterday := time.Now().In(s.siteLocation(ctx)).AddDate(0, 0, -1).Format("2006-01-02")
			if err := s.useMakeupCard(ctx, acc, yesterday); err == nil {
				out.MakeupUsed = yesterday
				applog.Info(ctx, "workbuddy", "streak makeup card used", "account", acc.ID, "date", yesterday)
			}
		}
	}

	// 2) 兑换所有已解锁档位（locked/claimed 跳过）。
	statuses := map[string]string{
		"7d":  full.RedemptionStatus.Tier7dStatus,
		"14d": full.RedemptionStatus.Tier14dStatus,
		"28d": full.RedemptionStatus.Tier28dStatus,
	}
	for _, tier := range full.RedemptionStatus.Tiers {
		status := statuses[tier.Tier]
		if status == "locked" || status == "claimed" {
			continue
		}
		if err := s.redeemTier(ctx, acc, tier.Tier); err != nil {
			if !isLockedError(err) {
				applog.Warn(ctx, "workbuddy", "streak redeem failed", "account", acc.ID, "tier", tier.Tier, "error", err.Error())
			}
			continue
		}
		out.RedeemedTiers = append(out.RedeemedTiers, tier.Tier)
		applog.Info(ctx, "workbuddy", "streak tier redeemed", "account", acc.ID, "tier", tier.Tier,
			"credit", tier.Credit, "energy", tier.Energy, "chances", tier.Chances)
	}

	// 3) 抽完所有抽奖次数（兑换刚发的次数已在服务端累加）。
	chances, err := s.lotteryChances(ctx, acc)
	if err != nil {
		return out, nil
	}
	for i := 0; i < chances; i++ {
		raw, err := s.lotteryDraw(ctx, acc)
		if err != nil {
			applog.Warn(ctx, "workbuddy", "streak lottery draw failed", "account", acc.ID, "error", err.Error())
			break
		}
		out.DrawCount++
		out.Prizes = append(out.Prizes, compactJSON(raw))
	}
	return out, nil
}

// heatmapYesterdayMissed 查询昨日是否漏签（补签的前置判据）。
func (s *Service) heatmapYesterdayMissed(ctx context.Context, acc Account) (bool, error) {
	region := normalizeRegion(acc.Region)
	ctx, cancel := context.WithTimeout(ctx, checkinQueryTimeout)
	defer cancel()
	data, err := s.doMeterJSON(ctx, &acc, http.MethodGet,
		endpointGrowth(region, "/activity/growth/heatmap"), webHeaders(&acc), "")
	if err != nil {
		return false, err
	}
	var resp struct {
		Cells []struct {
			Date  string `json:"date"`
			Score int64  `json:"score"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return false, err
	}
	// 「昨日」按站点时区归属，与 runStreakBonus 的补签日期保持一致。
	yesterday := time.Now().In(s.siteLocation(ctx)).AddDate(0, 0, -1).Format("2006-01-02")
	for _, c := range resp.Cells {
		if c.Date == yesterday {
			return c.Score <= 0, nil
		}
	}
	return false, nil
}

// compactJSON 裁剪载荷以便单行日志。
func compactJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
